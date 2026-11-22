package gate

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"easycode/internal/application/audit"
	"easycode/internal/application/event"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Failure codes of decisions (openapi Problem).
const (
	CodeInvalidCSRF       = "invalid-csrf"
	CodeReauthentication  = "reauthentication-required"
	CodePendingHumanCheck = "pending-human-check"
)

// CheckView is a check result of a round.
type CheckView struct {
	Check    string
	Type     string
	Severity string
	Status   string
	Evidence json.RawMessage
	// ConfirmedBy is who confirmed (or refused) a human check, nil while pending.
	ConfirmedBy *DecisionView
}

// DecisionView is the human or orchestrator decision of a round.
type DecisionView struct {
	Result       string
	ByType       string
	By           appproject.UserRef
	At           time.Time
	Reason       string
	FailedChecks []string
}

// RoundView is a gate round (GateRound).
type RoundView struct {
	Gate         string
	Stage        string
	Round        int
	State        string
	ApprovalMode string
	OpenedAt     time.Time
	Outputs      []OutputRef
	Checks       []CheckView
	Decision     *DecisionView
	// ReworkWave is the code of the rework wave a rejection created (ER-12-14), "" when none.
	ReworkWave string
	// BlockingFindings are the active critical or high findings of the gate scope; NonBlockingFindings
	// the other active ones (gate report, US-31 CA-3).
	BlockingFindings    []FindingRef
	NonBlockingFindings []FindingRef
	// Evaluation is the current evaluation of the round; ReevaluatedBy the finding whose opening made it
	// evaluate again (US-31 CA-3), "" when none.
	Evaluation    int
	ReevaluatedBy string
}

// FindingRef is a finding listed in a round (FindingSummary).
type FindingRef struct {
	Code, Title, Severity, Category, Status, DestinationStage, OriginStage, Author string
	Cycles                                                                         int
	OpenedAt                                                                       time.Time
}

// OutputRef is an output version of the round.
type OutputRef struct {
	Code    string
	Type    string
	Version int
	State   string
}

// DecisionStore is the api side of the gate persistence.
type DecisionStore interface {
	Rounds(ctx context.Context, projectID, stage string) ([]RoundView, error)
	Round(ctx context.Context, projectID, stage string, round int) (RoundView, error)
	// Decide locks the round, records the confirmation and the human checks (confirmed when approving,
	// failed when rejecting) and moves the round (approve or reject).
	Decide(ctx context.Context, principal identity.Principal, projectID, stage string, round int, approve bool, reason string, confirmed []string) error
}

// Decision is approveGate or rejectGate.
type Decision struct {
	ProjectID       string
	Gate            string
	Round           int
	Approve         bool
	ConfirmedChecks []string
	Reason          string
	Comment         string
	CSRFToken       string
}

// Decisions are the synchronous human decisions of the api (interno.md section 1).
type Decisions struct {
	tx       transaction.Runner
	projects appproject.Repository
	store    DecisionStore
	outbox   event.Outbox
	trail    audit.Trail
	now      func() time.Time
}

// NewDecisions builds the service.
func NewDecisions(tx transaction.Runner, projects appproject.Repository, store DecisionStore, outbox event.Outbox, trail audit.Trail) *Decisions {
	return &Decisions{tx: tx, projects: projects, store: store, outbox: outbox, trail: trail, now: time.Now}
}

func stageOf(gate string) (string, error) {
	stage, ok := strings.CutPrefix(gate, "gate.")
	if !ok || stage == "" {
		return "", failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return stage, nil
}

// Rounds is listGateRounds.
func (d *Decisions) Rounds(ctx context.Context, principal identity.Principal, projectID, gate string) ([]RoundView, error) {
	stage, err := stageOf(gate)
	if err != nil {
		return nil, err
	}
	if _, err := appproject.Authorize(ctx, d.projects, principal, projectID); err != nil {
		return nil, err
	}
	return d.store.Rounds(ctx, projectID, stage)
}

// Round is getGateRound.
func (d *Decisions) Round(ctx context.Context, principal identity.Principal, projectID, gate string, round int) (RoundView, error) {
	stage, err := stageOf(gate)
	if err != nil {
		return RoundView{}, err
	}
	if _, err := appproject.Authorize(ctx, d.projects, principal, projectID); err != nil {
		return RoundView{}, err
	}
	return d.store.Round(ctx, projectID, stage, round)
}

// Decide applies approve or reject in one transaction with audit and outbox (US-06 basics, ADR-19:
// anti-CSRF token; approve also needs an authentication of at most 10 min).
func (d *Decisions) Decide(ctx context.Context, principal identity.Principal, session identity.Session, decision Decision) (RoundView, error) {
	stage, err := stageOf(decision.Gate)
	if err != nil {
		return RoundView{}, err
	}
	if _, err := appproject.Authorize(ctx, d.projects, principal, decision.ProjectID, identity.RoleApprover); err != nil {
		return RoundView{}, err
	}
	if session.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(decision.CSRFToken)) != 1 {
		return RoundView{}, failure.New(failure.KindForbidden, CodeInvalidCSRF, nil)
	}
	if decision.Approve && !session.RecentlyAuthenticated(d.now()) {
		return RoundView{}, failure.New(failure.KindUnauthenticated, CodeReauthentication, nil)
	}
	if !decision.Approve && strings.TrimSpace(decision.Reason) == "" {
		return RoundView{}, failure.Invalid(failure.FieldError{Field: "reason", Message: "required"})
	}
	current, err := d.store.Round(ctx, decision.ProjectID, stage, decision.Round)
	if err != nil {
		return RoundView{}, err
	}
	if current.State != StateAwaitingHuman {
		return RoundView{}, failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", current.State).With("available_actions", []string{})
	}
	if decision.Approve {
		if missing, unknown := humanChecks(current.Checks, decision.ConfirmedChecks); len(missing) > 0 || len(unknown) > 0 {
			// missing_checks: human checks of the round still to confirm; checks: confirmed ids the round does not have.
			return RoundView{}, failure.New(failure.KindInvalidInput, CodePendingHumanCheck, nil).
				With("missing_checks", missing).With("checks", unknown)
		}
	}
	err = d.tx.InTx(ctx, func(ctx context.Context) error {
		if err := d.store.Decide(ctx, principal, decision.ProjectID, stage, decision.Round, decision.Approve, decision.Reason, decision.ConfirmedChecks); err != nil {
			return err
		}
		eventType, action, to := "br.easycode.gate.approved.v1", "gate.approve", StateApproved
		confirmed := orEmpty(decision.ConfirmedChecks)
		data := map[string]any{"gate": decision.Gate, "round": decision.Round, "stage": stage, "ok_checks": len(current.Checks),
			"actor": map[string]any{"type": "human", "id": principal.Subject, "origin": "web"}, "approvals": 1, "confirmed_human_checks": confirmed}
		if !decision.Approve {
			eventType, action, to = "br.easycode.gate.rejected.v1", "gate.reject", StateRejected
			data = map[string]any{"gate": decision.Gate, "round": decision.Round, "stage": stage, "failed_checks": []string{},
				"blocking_findings": []string{}, "human_reason": true, "actor": map[string]any{"type": "human", "id": principal.Subject, "origin": "web"}}
		}
		data["transition"] = map[string]any{"machine": "gate", "from_state": StateAwaitingHuman, "to_state": to}
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		correlation := deterministicUUID(decision.ProjectID + "/" + stage + "/" + itoa(decision.Round))
		envelope, err := d.outbox.Append(ctx, domainevent.Draft{Type: eventType, Subject: roundSubject(decision.Gate, decision.Round),
			ProjectID: decision.ProjectID, CorrelationID: correlation, Data: encoded})
		if err != nil {
			return err
		}
		_, err = d.trail.Append(ctx, domainaudit.Record{
			ProjectID: decision.ProjectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject}, Origin: domainaudit.OriginWeb,
			Action: action, Target: roundSubject(decision.Gate, decision.Round), Decision: to, Justification: decision.Reason,
			EventID: envelope.ID, CorrelationID: correlation,
		})
		return err
	})
	if err != nil {
		return RoundView{}, err
	}
	return d.store.Round(ctx, decision.ProjectID, stage, decision.Round)
}

// deterministicUUID derives a UUID (version 8) from a key, so the decision and its effects share
// one correlation id.
func deterministicUUID(key string) string {
	sum := sha256.Sum256([]byte(key))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x80
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// humanChecks compares the human checks of the round with the confirmed ones: the human checks
// not confirmed and the confirmed ids that are not human checks of the round (US-06 RN-3).
func humanChecks(checks []CheckView, confirmed []string) (missing, unknown []string) {
	human := map[string]bool{}
	missing, unknown = []string{}, []string{}
	for _, c := range checks {
		if c.Type == "human" {
			human[c.Check] = true
		}
	}
	given := map[string]bool{}
	for _, id := range confirmed {
		given[id] = true
		if !human[id] {
			unknown = append(unknown, id)
		}
	}
	for _, c := range checks {
		if c.Type == "human" && !given[c.Check] {
			missing = append(missing, c.Check)
		}
	}
	return missing, unknown
}
