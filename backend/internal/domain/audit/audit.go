// Package audit holds the append-only audit trail record (RNF-13, US-35): who did what,
// when, from where, on which target and why. Records are never changed or deleted by the
// application; only retention expires them (audit_period).
package audit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ActorType says who acted.
type ActorType string

// Actor types of the common payload.
const (
	ActorHuman        ActorType = "human"
	ActorAgent        ActorType = "agent"
	ActorOrchestrator ActorType = "orchestrator"
)

// Origin says where a human action came from.
type Origin string

// Origins of a human action (US-12/RN-1).
const (
	OriginWeb         Origin = "web"
	OriginLocalClient Origin = "local_client"
)

// ErrInvalid marks a record the trail refuses.
var ErrInvalid = errors.New("invalid audit record")

var (
	actionPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	policyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	decimalPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,8})?$`)
)

// Actor is who acted, by opaque id only (OIDC sub, agent id or "orchestrator"; never name or e-mail).
type Actor struct {
	Type ActorType
	ID   string
}

// Record is one audit entry. Ids are public (uuid) ids; the adapter resolves internal keys.
type Record struct {
	OrganizationID  string // required unless ProjectID is set (then it is the project's organization)
	ProjectID       string // empty for organization actions (key, price, skill)
	Actor           Actor
	Origin          Origin // required for human actions, empty otherwise
	Policy          string // policy that drove an orchestrator action
	Action          string // entity.action, e.g. gate.approve, wave.pause_now
	Target          string // subject or id: project, waves/DBA-01, gates/gate.data/rounds/2
	Decision        string
	Justification   string // free human text: personal data, anonymized with the project
	References      []string
	LostCost        string // exact decimal, e.g. "0.4127"; empty when not applicable
	EventID         string // public id of the matching domain event, written in the same transaction
	CorrelationID   string
	ExternalEventID string    // Keycloak event id of session actions (PL-36)
	RecordedAt      time.Time // zero means "now" in the database; set only for external events
}

// Entry is a stored record.
type Entry struct {
	ID         string // public id
	RecordedAt time.Time
}

// Validate mirrors the audit_records constraints so a bad record fails before the database.
func (r Record) Validate() error {
	var problems []string
	check := func(ok bool, problem string) {
		if !ok {
			problems = append(problems, problem)
		}
	}
	check(uuidPattern.MatchString(r.OrganizationID) || uuidPattern.MatchString(r.ProjectID), "organization or project id")
	check(r.OrganizationID == "" || uuidPattern.MatchString(r.OrganizationID), "organization id")
	check(r.ProjectID == "" || uuidPattern.MatchString(r.ProjectID), "project id")
	switch r.Actor.Type {
	case ActorHuman:
		check(r.Origin == OriginWeb || r.Origin == OriginLocalClient, "origin required for human actions")
	case ActorAgent, ActorOrchestrator:
		check(r.Origin == "", "origin only on human actions")
	default:
		problems = append(problems, "actor type")
	}
	check(len(r.Actor.ID) >= 1 && len([]rune(r.Actor.ID)) <= 128, "actor id")
	check(r.Policy == "" || (r.Actor.Type == ActorOrchestrator && policyPattern.MatchString(r.Policy)), "policy only on orchestrator actions")
	check(actionPattern.MatchString(r.Action), "action")
	check(strings.TrimSpace(r.Target) != "", "target")
	check(r.LostCost == "" || decimalPattern.MatchString(r.LostCost), "lost cost")
	check(r.EventID == "" || uuidPattern.MatchString(r.EventID), "event id")
	check(r.CorrelationID == "" || uuidPattern.MatchString(r.CorrelationID), "correlation id")
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalid, strings.Join(problems, ", "))
	}
	return nil
}
