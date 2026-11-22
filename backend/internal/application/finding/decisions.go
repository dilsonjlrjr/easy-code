package finding

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"easycode/internal/application/audit"
	"easycode/internal/application/event"
	appproject "easycode/internal/application/project"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	domainevent "easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	domainfinding "easycode/internal/domain/finding"
	"easycode/internal/domain/identity"
)

// Failure codes of the decisions (openapi Problem).
const (
	CodeInvalidCSRF      = "invalid-csrf"
	CodeReauthentication = "reauthentication-required"
)

// Filter is listFindings.
type Filter struct {
	Statuses    []string
	Severities  []string
	Origin      string
	Destination string
	Author      string
	Limit       int
	Cursor      string
}

// Page is a page of findings.
type Page struct {
	Items      []domainfinding.Finding
	NextCursor string
}

// Decision is a human decision on a finding (accept_risk, authorize_new_rework, decide_scope_change, discard).
type Decision struct {
	ProjectID     string
	Finding       string
	Justification string
	ValidUntil    *time.Time
	ModifiedBy    string   // scope change: requirements-agent | human
	StartingPoint []string // scope change: requirements artifacts
	Reason        string   // discard: no_evidence | duplicate
	DuplicateOf   string
	CSRFToken     string
}

// DecisionStore is the api side of the finding persistence (synchronous decisions, interno.md section 1).
type DecisionStore interface {
	List(ctx context.Context, projectID string, filter Filter) (Page, error)
	// Find returns the finding (nil when unknown); lock takes the row for the decision.
	Find(ctx context.Context, projectID, code string, lock bool) (*domainfinding.Finding, error)
	Accept(ctx context.Context, principal identity.Principal, projectID, code, justification string, validUntil *time.Time, from string) error
	DecideEscalation(ctx context.Context, principal identity.Principal, projectID, code, decision, justification, modifiedBy string, starting []string, status string) error
	Discard(ctx context.Context, projectID, code, reason, duplicateOf string) error
	// ArtifactTypes returns the type of each existing artifact among the codes.
	ArtifactTypes(ctx context.Context, projectID string, codes []string) (map[string]string, error)
}

// Decisions are the finding use cases of the api.
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

var statuses = []string{domainfinding.StatusOpen, domainfinding.StatusInRework, domainfinding.StatusRevalidating, domainfinding.StatusClosed,
	domainfinding.StatusEscalated, domainfinding.StatusAwaitingScope, domainfinding.StatusAccepted, domainfinding.StatusDiscarded}

// List is listFindings (severity first, then opening date).
func (d *Decisions) List(ctx context.Context, principal identity.Principal, projectID string, filter Filter) (Page, error) {
	if _, err := appproject.Authorize(ctx, d.projects, principal, projectID); err != nil {
		return Page{}, err
	}
	for _, s := range filter.Statuses {
		if !contains(statuses, s) {
			return Page{}, failure.Invalid(failure.FieldError{Field: "status", Message: "enum"})
		}
	}
	for _, s := range filter.Severities {
		if domainfinding.Severity(s) != s || s == "" {
			return Page{}, failure.Invalid(failure.FieldError{Field: "severity", Message: "enum"})
		}
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	return d.store.List(ctx, projectID, filter)
}

// View is a finding with the actions the user may take on it.
type View struct {
	domainfinding.Finding
	Actions []string
}

// Get is getFinding.
func (d *Decisions) Get(ctx context.Context, principal identity.Principal, projectID, code string) (View, error) {
	access, err := appproject.Authorize(ctx, d.projects, principal, projectID)
	if err != nil {
		return View{}, err
	}
	f, err := d.store.Find(ctx, projectID, code, false)
	if err != nil {
		return View{}, err
	}
	if f == nil {
		return View{}, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	view := View{Finding: *f, Actions: []string{}}
	if identity.Has(access.Roles, identity.RoleApprover) {
		view.Actions = Actions(f.Status)
	}
	return view, nil
}

// Actions are the human triggers available in a finding state (estados.yaml, machine finding).
func Actions(status string) []string {
	switch status {
	case domainfinding.StatusOpen, domainfinding.StatusInRework:
		return []string{"accept_risk", "discard"}
	case domainfinding.StatusRevalidating:
		return []string{"accept_risk"}
	case domainfinding.StatusEscalated:
		return []string{"accept_risk", "authorize_new_rework", "decide_scope_change", "discard"}
	case domainfinding.StatusAccepted:
		return []string{"accept_risk"} // renewal of the acceptance (D-29)
	}
	return []string{}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// critical applies the rules of a critical decision (ADR-19): approver role, anti-CSRF token and an SSO
// authentication of at most 10 minutes; only by the web session.
func (d *Decisions) critical(ctx context.Context, principal identity.Principal, session identity.Session, projectID, token string) error {
	if _, err := appproject.Authorize(ctx, d.projects, principal, projectID, identity.RoleApprover); err != nil {
		return err
	}
	if session.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(token)) != 1 {
		return failure.New(failure.KindForbidden, CodeInvalidCSRF, nil)
	}
	if !session.RecentlyAuthenticated(d.now()) {
		return failure.New(failure.KindUnauthenticated, CodeReauthentication, nil)
	}
	return nil
}

func (d *Decisions) conflict(f *domainfinding.Finding) error {
	return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", f.Status).With("available_actions", Actions(f.Status))
}

func (d *Decisions) load(ctx context.Context, projectID, code string) (*domainfinding.Finding, error) {
	f, err := d.store.Find(ctx, projectID, code, true)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
	}
	return f, nil
}

// record writes the event and the audit record of a decision in the transaction.
func (d *Decisions) record(ctx context.Context, principal identity.Principal, f *domainfinding.Finding, eventType, action, to, justification string,
	extra map[string]any) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	actor := map[string]any{"type": "human", "id": principal.Subject, "origin": "web"}
	data := map[string]any{"finding": f.Code, "severity": f.Severity, "category": f.Category, "destination_stage": f.DestinationStage,
		"origin_stage": f.OriginStage, "author": f.Author, "actor": actor,
		"transition": map[string]any{"machine": "finding", "from_state": f.Status, "to_state": to}}
	for k, v := range extra {
		data[k] = v
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	envelope, err := d.outbox.Append(ctx, domainevent.Draft{Type: eventType, Subject: "findings/" + f.Code, ProjectID: f.ProjectID,
		CorrelationID: id.String(), Data: encoded})
	if err != nil {
		return err
	}
	_, err = d.trail.Append(ctx, domainaudit.Record{ProjectID: f.ProjectID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
		Origin: domainaudit.OriginWeb, Action: action, Target: "findings/" + f.Code, Decision: to, Justification: justification,
		EventID: envelope.ID, CorrelationID: id.String()})
	return err
}

func validJustification(text string) error {
	if len([]rune(strings.TrimSpace(text))) < 10 {
		return failure.Invalid(failure.FieldError{Field: "justification", Message: "required"})
	}
	return nil
}

// AcceptRisk is acceptFindingRisk (US-33): only an approver, by the web, with a recent authentication;
// justification required, validity optional and never in the past. On an accepted finding it renews the
// acceptance. Whoever accepts cannot approve the gate.release of the same cycle (policy.separation_of_duties).
func (d *Decisions) AcceptRisk(ctx context.Context, principal identity.Principal, session identity.Session, decision Decision) (View, error) {
	if err := d.critical(ctx, principal, session, decision.ProjectID, decision.CSRFToken); err != nil {
		return View{}, err
	}
	if err := validJustification(decision.Justification); err != nil {
		return View{}, err
	}
	if v := decision.ValidUntil; v != nil {
		now := d.now().UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if v.Before(today) {
			return View{}, failure.Invalid(failure.FieldError{Field: "valid_until", Message: "past"})
		}
	}
	err := d.tx.InTx(ctx, func(ctx context.Context) error {
		f, err := d.load(ctx, decision.ProjectID, decision.Finding)
		if err != nil {
			return err
		}
		if !contains(Actions(f.Status), "accept_risk") {
			return d.conflict(f)
		}
		if err := d.store.Accept(ctx, principal, decision.ProjectID, f.Code, decision.Justification, decision.ValidUntil, f.Status); err != nil {
			return err
		}
		extra := map[string]any{"canceled_waves": []string{}}
		if decision.ValidUntil != nil {
			extra["valid_until"] = decision.ValidUntil.Format("2006-01-02")
		}
		if f.Status == domainfinding.StatusAccepted {
			extra["renewal"] = true
		}
		return d.record(ctx, principal, f, "br.easycode.finding.accepted.v1", "finding.accept_risk", domainfinding.StatusAccepted, decision.Justification, extra)
	})
	if err != nil {
		return View{}, err
	}
	return d.Get(ctx, principal, decision.ProjectID, decision.Finding)
}

// AuthorizeExtraRound is authorizeFindingExtraRound (US-32 CA-6): escalated → in_rework for one more
// round; the worker creates the rework wave.
func (d *Decisions) AuthorizeExtraRound(ctx context.Context, principal identity.Principal, session identity.Session, decision Decision) (View, error) {
	if err := d.critical(ctx, principal, session, decision.ProjectID, decision.CSRFToken); err != nil {
		return View{}, err
	}
	if err := validJustification(decision.Justification); err != nil {
		return View{}, err
	}
	err := d.tx.InTx(ctx, func(ctx context.Context) error {
		f, err := d.load(ctx, decision.ProjectID, decision.Finding)
		if err != nil {
			return err
		}
		if f.Status != domainfinding.StatusEscalated {
			return d.conflict(f)
		}
		if err := d.store.DecideEscalation(ctx, principal, decision.ProjectID, f.Code, domainfinding.DecisionNewRework, decision.Justification, "", nil,
			domainfinding.StatusInRework); err != nil {
			return err
		}
		return d.record(ctx, principal, f, "br.easycode.finding.rework_started.v1", "finding.authorize_new_rework", domainfinding.StatusInRework,
			decision.Justification, map[string]any{"cycle": len(f.Cycles) + 1, "rework_wave": nil, "internal_cycle": f.DestinationStage == "implementation",
				"extra_round_authorized": true})
	})
	if err != nil {
		return View{}, err
	}
	return d.Get(ctx, principal, decision.ProjectID, decision.Finding)
}

// DecideScopeChange is decideFindingScopeChange (US-32 CA-7, PD-06): escalated → awaiting_scope; the
// worker reopens the requirements and the starting point artifacts.
func (d *Decisions) DecideScopeChange(ctx context.Context, principal identity.Principal, session identity.Session, decision Decision) (View, error) {
	if err := d.critical(ctx, principal, session, decision.ProjectID, decision.CSRFToken); err != nil {
		return View{}, err
	}
	if err := validJustification(decision.Justification); err != nil {
		return View{}, err
	}
	if decision.ModifiedBy != "requirements-agent" && decision.ModifiedBy != "human" {
		return View{}, failure.Invalid(failure.FieldError{Field: "modified_by", Message: "enum"})
	}
	if len(decision.StartingPoint) == 0 {
		return View{}, failure.Invalid(failure.FieldError{Field: "starting_point_artifacts", Message: "required"})
	}
	err := d.tx.InTx(ctx, func(ctx context.Context) error {
		f, err := d.load(ctx, decision.ProjectID, decision.Finding)
		if err != nil {
			return err
		}
		if f.Status != domainfinding.StatusEscalated {
			return d.conflict(f)
		}
		types, err := d.store.ArtifactTypes(ctx, decision.ProjectID, decision.StartingPoint)
		if err != nil {
			return err
		}
		for _, code := range decision.StartingPoint {
			switch types[code] {
			case "VISAO", "US", "RNF", "BACKLOG":
			default:
				return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", f.Status).
					With("available_actions", Actions(f.Status)).With("artifact", code)
			}
		}
		if err := d.store.DecideEscalation(ctx, principal, decision.ProjectID, f.Code, domainfinding.DecisionScopeChange, decision.Justification,
			decision.ModifiedBy, decision.StartingPoint, domainfinding.StatusAwaitingScope); err != nil {
			return err
		}
		return d.record(ctx, principal, f, "br.easycode.finding.scope_change_decided.v1", "finding.decide_scope_change", domainfinding.StatusAwaitingScope,
			decision.Justification, map[string]any{"modified_by": decision.ModifiedBy, "starting_point_artifacts": decision.StartingPoint})
	})
	if err != nil {
		return View{}, err
	}
	return d.Get(ctx, principal, decision.ProjectID, decision.Finding)
}

// Discard is discardFinding: only without evidence or duplicate (with the original), by an approver, by
// the web session with the anti-CSRF token like the other decisions on findings (no recent
// authentication: discarding keeps the finding in the repository and cancels no decision of risk).
func (d *Decisions) Discard(ctx context.Context, principal identity.Principal, session identity.Session, decision Decision) (View, error) {
	if _, err := appproject.Authorize(ctx, d.projects, principal, decision.ProjectID, identity.RoleApprover); err != nil {
		return View{}, err
	}
	if session.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(decision.CSRFToken)) != 1 {
		return View{}, failure.New(failure.KindForbidden, CodeInvalidCSRF, nil)
	}
	if err := validJustification(decision.Justification); err != nil {
		return View{}, err
	}
	if decision.Reason != "no_evidence" && decision.Reason != "duplicate" {
		return View{}, failure.Invalid(failure.FieldError{Field: "reason", Message: "enum"})
	}
	if decision.Reason == "duplicate" && (decision.DuplicateOf == "" || decision.DuplicateOf == decision.Finding) {
		return View{}, failure.Invalid(failure.FieldError{Field: "duplicate_of", Message: "required"})
	}
	err := d.tx.InTx(ctx, func(ctx context.Context) error {
		f, err := d.load(ctx, decision.ProjectID, decision.Finding)
		if err != nil {
			return err
		}
		if !contains(Actions(f.Status), "discard") {
			return d.conflict(f)
		}
		if decision.Reason == "duplicate" {
			original, err := d.store.Find(ctx, decision.ProjectID, decision.DuplicateOf, false)
			if err != nil {
				return err
			}
			if original == nil {
				return failure.Invalid(failure.FieldError{Field: "duplicate_of", Message: "not found"})
			}
		}
		reason := "sem evidência: " + strings.TrimSpace(decision.Justification)
		if decision.Reason == "duplicate" {
			reason = "duplicado de " + decision.DuplicateOf + ": " + strings.TrimSpace(decision.Justification)
		}
		if err := d.store.Discard(ctx, decision.ProjectID, f.Code, reason, decision.DuplicateOf); err != nil {
			return err
		}
		extra := map[string]any{"reason": decision.Reason, "canceled_waves": []string{}}
		if decision.DuplicateOf != "" {
			extra["duplicate_of"] = decision.DuplicateOf
		}
		return d.record(ctx, principal, f, "br.easycode.finding.discarded.v1", "finding.discard", domainfinding.StatusDiscarded, decision.Justification, extra)
	})
	if err != nil {
		return View{}, err
	}
	return d.Get(ctx, principal, decision.ProjectID, decision.Finding)
}
