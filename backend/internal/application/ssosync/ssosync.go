// Package ssosync is the worker job that brings the SSO into the platform (interno.md section 7):
// login, logout and login failure events of the realm become audit records of the organization
// (PL-36, US-01 CA-5, RNF-13), and people removed or disabled in the realm are deactivated, with the
// display name anonymized for whoever left the organization (anonymize_offboarded_user, D-31, RNF-20).
// A person only disabled in the realm keeps the record: the SSO already refuses the login and the
// api refuses the open sessions within 60 s (RNF-06); enabling the account again restores access.
package ssosync

import (
	"context"
	"time"

	appaudit "easycode/internal/application/audit"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
)

// Event types read from the realm.
const (
	EventLogin       = "LOGIN"
	EventLoginError  = "LOGIN_ERROR"
	EventLogout      = "LOGOUT"
	EventCodeToToken = "CODE_TO_TOKEN"
)

// DesktopClientID is the OIDC client of the read-only desktop app (ADR-20).
const DesktopClientID = "easycode-desktop"

// Event is a user event of the realm.
type Event struct {
	ID       string
	Type     string
	Time     time.Time
	ClientID string
	Subject  string
	Error    string
}

// Directory reads the realm with the read-only service account.
type Directory interface {
	Events(ctx context.Context, types []string, since time.Time, first, limit int) ([]Event, error)
	// Exists reports whether the realm still has the person.
	Exists(ctx context.Context, subject string) (bool, error)
}

// Store is what the job reads and changes in the platform.
type Store interface {
	// Organization returns the public id of the organization of the issuer (not-found if none).
	Organization(ctx context.Context, issuer string) (string, error)
	// Cursor is the latest moment already recorded from the SSO for the organization.
	Cursor(ctx context.Context, organizationID string) (time.Time, error)
	// ActiveSubjects lists the active people of the organization.
	ActiveSubjects(ctx context.Context, organizationID string) ([]string, error)
	// Offboard deactivates the person, anonymizes the display name (anonymized_at recorded; the sub
	// stays as the opaque id of the trail) and ends the open sessions, in one transaction.
	Offboard(ctx context.Context, organizationID, subject string) error
}

// Job runs one synchronization pass.
type Job struct {
	issuer    string
	directory Directory
	store     Store
	trail     appaudit.Trail
	pageSize  int
}

// NewJob builds the job for the realm of issuer.
func NewJob(issuer string, directory Directory, store Store, trail appaudit.Trail) *Job {
	return &Job{issuer: issuer, directory: directory, store: store, trail: trail, pageSize: 100}
}

// Result counts what a pass did.
type Result struct {
	Recorded   int
	Offboarded int
}

// Run records the new session events and applies offboarding.
func (j *Job) Run(ctx context.Context) (Result, error) {
	var result Result
	organizationID, err := j.store.Organization(ctx, j.issuer)
	if err != nil {
		return result, err
	}
	recorded, err := j.recordEvents(ctx, organizationID)
	result.Recorded = recorded
	if err != nil {
		return result, err
	}
	result.Offboarded, err = j.offboard(ctx, organizationID)
	return result, err
}

func (j *Job) recordEvents(ctx context.Context, organizationID string) (int, error) {
	cursor, err := j.store.Cursor(ctx, organizationID)
	if err != nil {
		return 0, err
	}
	since := cursor
	if since.IsZero() {
		since = time.Now().Add(-7 * 24 * time.Hour) // Keycloak keeps events for 7 days
	}
	recorded := 0
	types := []string{EventLogin, EventLoginError, EventLogout, EventCodeToToken}
	for first := 0; ; first += j.pageSize {
		events, err := j.directory.Events(ctx, types, since, first, j.pageSize)
		if err != nil {
			return recorded, err
		}
		for _, e := range events {
			record, ok := recordOf(organizationID, e)
			if !ok || e.Time.Before(cursor) {
				continue
			}
			if _, err := j.trail.Append(ctx, record); err != nil {
				if failure.From(err).Kind == failure.KindConflict {
					continue // already recorded: reading again never duplicates (external_event_id)
				}
				return recorded, err
			}
			recorded++
		}
		if len(events) < j.pageSize {
			return recorded, nil
		}
	}
}

// recordOf maps an SSO event to the audit record of interno.md section 7. A failure without an
// identified person is not recorded (only the security metric, never the typed user name); the
// web code exchange repeats the login and is skipped.
func recordOf(organizationID string, e Event) (domainaudit.Record, bool) {
	if e.Subject == "" || e.ID == "" {
		return domainaudit.Record{}, false
	}
	origin := domainaudit.OriginWeb
	if e.ClientID == DesktopClientID {
		origin = domainaudit.OriginLocalClient
	}
	record := domainaudit.Record{
		OrganizationID: organizationID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: e.Subject}, Origin: origin,
		Target: "session", ExternalEventID: e.ID, RecordedAt: e.Time,
	}
	switch e.Type {
	case EventLogin:
		record.Action = "session.login"
	case EventCodeToToken:
		if origin != domainaudit.OriginLocalClient {
			return domainaudit.Record{}, false
		}
		record.Action = "session.login"
	case EventLogout:
		record.Action = "session.logout"
	case EventLoginError:
		record.Action, record.Decision = "session.login_failed", summarize(e.Error)
	default:
		return domainaudit.Record{}, false
	}
	return record, true
}

// summarize keeps only the short reason code of the SSO (never free text).
func summarize(reason string) string {
	if reason == "" {
		return "login_error"
	}
	out := make([]rune, 0, len(reason))
	for _, r := range reason {
		if (r >= 'a' && r <= 'z') || r == '_' {
			out = append(out, r)
		}
	}
	if len(out) == 0 || len(out) > 64 {
		return "login_error"
	}
	return string(out)
}

// offboard applies D-31 to whoever the realm no longer has.
func (j *Job) offboard(ctx context.Context, organizationID string) (int, error) {
	subjects, err := j.store.ActiveSubjects(ctx, organizationID)
	if err != nil {
		return 0, err
	}
	offboarded := 0
	for _, subject := range subjects {
		exists, err := j.directory.Exists(ctx, subject)
		if err != nil {
			return offboarded, err
		}
		if exists {
			continue
		}
		if err := j.store.Offboard(ctx, organizationID, subject); err != nil {
			return offboarded, err
		}
		offboarded++
	}
	return offboarded, nil
}
