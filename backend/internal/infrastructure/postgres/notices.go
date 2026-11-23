package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/identity"
)

// Event types of watchNotices by audience (sse.md section 3).
var (
	noticeTypesForAll = []string{
		"br.easycode.wave.question_opened.v1", "br.easycode.wave.question_answered.v1",
		"br.easycode.wave.created.v1", "br.easycode.wave.started.v1", "br.easycode.wave.paused.v1", "br.easycode.wave.resumed.v1",
		"br.easycode.wave.completed.v1", "br.easycode.wave.failed.v1", "br.easycode.wave.canceled.v1",
		"br.easycode.wave.pause_requested.v1", "br.easycode.wave.restarted.v1", "br.easycode.release.published.v1",
		"br.easycode.stage.unblocked.v1", "br.easycode.stage.approved.v1", "br.easycode.stage.returned.v1",
		"br.easycode.stage.reopened.v1", "br.easycode.stage.blocked.v1",
	}
	noticeTypesForDeciders = []string{
		"br.easycode.gate.awaiting_approval.v1", "br.easycode.gate.approved.v1", "br.easycode.gate.rejected.v1", "br.easycode.gate.invalidated.v1",
		"br.easycode.finding.escalated.v1", "br.easycode.finding.scope_change_decided.v1", "br.easycode.finding.accepted.v1", "br.easycode.finding.closed.v1",
	}
)

// Notices reads the published domain events after a global position that the person receives now:
// projects where the person has a role at delivery time, gate and finding events only for approver
// or lead (US-48 CA-3), member.role_assigned only for the lead and the person (PL-52). since (the
// replay window) bounds the first load; projects narrows to some projects (empty = all).
func (o Outbox) Notices(ctx context.Context, p identity.Principal, after int64, since *time.Time, projects []string, limit int) ([]appevent.Notice, error) {
	if projects == nil {
		projects = []string{}
	}
	rows, err := conn(ctx, o.pool).Query(WithOperation(ctx, "outbox.notices"), `
WITH me AS (`+principalUserIDSQL+`),
mine AS (
  SELECT pr.id, bool_or(m.role IN ('approver','lead')) AS decides, bool_or(m.role = 'lead') AS lead
    FROM easycode.projects pr JOIN easycode.project_members m ON m.project_id = pr.id
   WHERE m.user_id = (SELECT * FROM me) AND pr.deletion_requested_at IS NULL
     AND (cardinality($6::text[]) = 0 OR pr.public_id::text = ANY ($6::text[]))
   GROUP BY pr.id
)
SELECT e.global_position, e.envelope FROM easycode.events e JOIN mine p ON p.id = e.project_id
 WHERE e.global_position IS NOT NULL AND e.global_position > $3 AND ($4::timestamptz IS NULL OR e.occurred_at >= $4)
   AND (e.type = ANY ($7::text[]) OR (p.decides AND e.type = ANY ($8::text[])) OR e.type LIKE 'br.easycode.project.%'
        OR (e.type = 'br.easycode.member.role_assigned.v1' AND (p.lead OR e.subject = 'members/' || $2)))
 ORDER BY e.global_position
 LIMIT $5`, p.OrganizationID, p.Subject, after, since, limit, projects, noticeTypesForAll, noticeTypesForDeciders)
	if err != nil {
		return nil, fmt.Errorf("read notices: %w", classify(err))
	}
	notices, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appevent.Notice, error) {
		var n appevent.Notice
		var raw []byte
		if err := row.Scan(&n.Position, &raw); err != nil {
			return n, err
		}
		return n, json.Unmarshal(raw, &n.Envelope)
	})
	if err != nil {
		return nil, fmt.Errorf("scan notices: %w", err)
	}
	return notices, nil
}

// LastPosition is the latest global outbox position (start of a live-only notices stream).
func (o Outbox) LastPosition(ctx context.Context) (int64, error) {
	var last int64
	err := conn(ctx, o.pool).QueryRow(WithOperation(ctx, "outbox.last_position"),
		"SELECT coalesce(max(global_position), 0) FROM easycode.events WHERE global_position IS NOT NULL").Scan(&last)
	if err != nil {
		return 0, fmt.Errorf("last position: %w", classify(err))
	}
	return last, nil
}
