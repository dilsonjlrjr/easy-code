package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Stream settings of sse.md.
const (
	replayWindow     = 24 * time.Hour
	heartbeatEvery   = 15 * time.Second
	subscriberBuffer = 512
	replayBatch      = 500
)

// EventFeed is the in-process fan-out of event notifications (one LISTEN per api process, ADR-11).
type EventFeed interface {
	Subscribe(projectID string, buffer int) *appevent.Subscription
	Unsubscribe(subscription *appevent.Subscription)
}

// EventHistory reads committed domain events of a project.
type EventHistory interface {
	ProjectEvents(ctx context.Context, projectID string, afterSequence int64, limit int) ([]event.Envelope, error)
	FirstSequenceSince(ctx context.Context, projectID string, since time.Time) (int64, bool, error)
	LastSequence(ctx context.Context, projectID string) (int64, error)
}

// ProjectAccess authorizes a person on a project (404 without role, RNF-04).
type ProjectAccess interface {
	Authorize(ctx context.Context, principal identity.Principal, projectID string) error
}

type streamFilter struct {
	wave, stage, class string
	types              []string
}

func (f streamFilter) accepts(e event.Envelope, telemetry bool) bool {
	if (f.class == "domain" && telemetry) || (f.class == "telemetry" && !telemetry) {
		return false
	}
	if f.wave != "" && e.WaveID != f.wave {
		return false
	}
	if f.stage != "" {
		var data struct {
			Stage string `json:"stage"`
		}
		if json.Unmarshal(e.Data, &data) != nil || data.Stage != f.stage {
			return false
		}
	}
	if len(f.types) == 0 {
		return true
	}
	for _, prefix := range f.types {
		if strings.HasPrefix(e.Type, prefix) {
			return true
		}
	}
	return false
}

func parseFilter(rc *fasthttp.RequestCtx) (streamFilter, error) {
	f := streamFilter{wave: string(rc.QueryArgs().Peek("waveId")), stage: string(rc.QueryArgs().Peek("stage")), class: string(rc.QueryArgs().Peek("class"))}
	if f.class == "" {
		f.class = "all"
	}
	if f.class != "all" && f.class != "domain" && f.class != "telemetry" {
		return f, failure.Invalid(failure.FieldError{Field: "class", Message: "enum"})
	}
	f.types = queryList(rc, "type")
	if len(f.types) > 20 {
		return f, failure.Invalid(failure.FieldError{Field: "type", Message: "too_many"})
	}
	for _, prefix := range f.types {
		if !strings.HasPrefix(prefix, "br.easycode.") {
			return f, failure.Invalid(failure.FieldError{Field: "type", Message: "pattern"})
		}
	}
	return f, nil
}

// watchProjectEvents is the SSE stream of a project (sse.md, US-09, RNF-08): subscribe first,
// then replay after Last-Event-ID or since (24 h window, easycode.gap when older), then live.
func (a *api) watchProjectEvents(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	if err := a.deps.Access.Authorize(RequestContext(rc), principal, projectID); err != nil {
		return err
	}
	filter, err := parseFilter(rc)
	if err != nil {
		return err
	}
	ctx := context.WithoutCancel(RequestContext(rc))
	start, replay, err := a.startingPoint(rc, projectID)
	if err != nil {
		return err
	}
	session := currentSession(rc)
	ends := session.IssuedAt.Add(identity.SessionLifetime)
	subscription := a.deps.Events.Subscribe(projectID, subscriberBuffer)
	rc.Response.Header.Set(fasthttp.HeaderCacheControl, "no-cache, no-transform")
	rc.Response.Header.Set("X-Accel-Buffering", "no")
	rc.SetContentType("text/event-stream; charset=utf-8")
	rc.SetStatusCode(fasthttp.StatusOK)
	rc.SetBodyStreamWriter(func(w *bufio.Writer) {
		defer a.deps.Events.Unsubscribe(subscription)
		stream := &sseStream{w: w, filter: filter, last: start}
		stream.raw("retry: 2000\n\n")
		if replay != "" {
			stream.raw(replay)
		}
		if !stream.catchUp(ctx, a.deps.History, projectID) || !stream.flush() {
			return
		}
		heartbeat := time.NewTicker(heartbeatEvery)
		defer heartbeat.Stop()
		for {
			select {
			case <-a.deps.Shutdown:
				stream.ended("offboarding")
				return
			case <-heartbeat.C:
				if time.Now().After(ends) {
					stream.ended("session_expired")
					return
				}
				if reason := a.streamEnd(ctx, principal, session, projectID); reason != "" {
					stream.ended(reason)
					return
				}
				stream.raw(": hb " + time.Now().Format(time.RFC3339) + "\n\n")
			case n, open := <-subscription.C():
				if !open {
					stream.ended("slow_consumer")
					return
				}
				if n.Telemetry != nil {
					stream.send(*n.Telemetry, true)
				} else if n.Sequence > stream.last && !stream.catchUp(ctx, a.deps.History, projectID) {
					return
				}
			}
			if !stream.flush() {
				return
			}
		}
	})
	return nil
}

// streamEnd tells why a stream must end now: the session ended (logout in another tab, SSO
// revocation) or the person lost the role in the project (sse.md 5.4). A failed check keeps the
// stream; the next heartbeat tries again.
func (a *api) streamEnd(ctx context.Context, principal identity.Principal, current identity.Session, projectID string) string {
	if active, err := a.sessionActive(ctx, current); err == nil && !active {
		return "session_revoked"
	}
	if err := a.deps.Access.Authorize(ctx, principal, projectID); err != nil && failure.From(err).Kind == failure.KindNotFound {
		return "access_removed"
	}
	return ""
}

// startingPoint resolves Last-Event-ID, then since, then "now" (sse.md 5.1) and the gap frame.
func (a *api) startingPoint(rc *fasthttp.RequestCtx, projectID string) (int64, string, error) {
	raw := string(rc.Request.Header.Peek("Last-Event-ID"))
	if raw == "" {
		raw = string(rc.QueryArgs().Peek("since"))
	}
	ctx := RequestContext(rc)
	if raw == "" {
		last, err := a.deps.History.LastSequence(ctx, projectID)
		return last, "", err
	}
	since, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || since < 0 {
		return 0, "", failure.Invalid(failure.FieldError{Field: "since", Message: "invalid"})
	}
	first, ok, err := a.deps.History.FirstSequenceSince(ctx, projectID, time.Now().Add(-replayWindow))
	if err != nil {
		return 0, "", err
	}
	if !ok {
		last, err := a.deps.History.LastSequence(ctx, projectID)
		if err != nil {
			return 0, "", err
		}
		if last > since {
			return last, gapFrame(since, last+1), nil
		}
		return since, "", nil
	}
	if since < first-1 {
		return first - 1, gapFrame(since, first), nil
	}
	return since, "", nil
}

func gapFrame(request, first int64) string {
	data, _ := json.Marshal(map[string]any{"request": request, "first_available": first, "action": "reload_state"})
	return "event: easycode.gap\ndata: " + string(data) + "\n\n"
}

type sseStream struct {
	w      *bufio.Writer
	filter streamFilter
	last   int64
	failed bool
}

func (s *sseStream) raw(text string) {
	if _, err := s.w.WriteString(text); err != nil {
		s.failed = true
	}
}

func (s *sseStream) send(e event.Envelope, telemetry bool) {
	if !s.filter.accepts(e, telemetry) {
		return
	}
	data, err := json.Marshal(e)
	if err != nil || len(data) > 64<<10 {
		return
	}
	if telemetry {
		s.raw(fmt.Sprintf("event: %s\ndata: %s\n\n", e.Type, data))
		return
	}
	s.raw(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.Sequence, e.Type, data))
}

// catchUp sends every committed domain event after the last one sent, in sequence order.
func (s *sseStream) catchUp(ctx context.Context, history EventHistory, projectID string) bool {
	for {
		events, err := history.ProjectEvents(ctx, projectID, s.last, replayBatch)
		if err != nil {
			return false
		}
		for _, e := range events {
			s.send(e, false)
			s.last = e.Sequence
		}
		if len(events) < replayBatch || s.failed {
			return !s.failed
		}
	}
}

func (s *sseStream) ended(reason string) {
	data, _ := json.Marshal(map[string]string{"reason": reason})
	s.raw("event: easycode.ended\ndata: " + string(data) + "\n\n")
	_ = s.flush()
}

func (s *sseStream) flush() bool {
	if s.failed {
		return false
	}
	if err := s.w.Flush(); err != nil {
		s.failed = true
	}
	return !s.failed
}

type eventsPageJSON struct {
	Items        []event.Envelope `json:"items"`
	LastSequence int64            `json:"last_sequence"`
	More         bool             `json:"more"`
}

// listProjectEvents is the domain event history (US-09 CA-4).
func (a *api) listProjectEvents(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projectID, err := pathUUID(rc, "projectId")
	if err != nil {
		return err
	}
	if err := a.deps.Access.Authorize(RequestContext(rc), principal, projectID); err != nil {
		return err
	}
	since, limit := int64(0), 100
	if raw := string(rc.QueryArgs().Peek("since_sequence")); raw != "" {
		if since, err = strconv.ParseInt(raw, 10, 64); err != nil || since < 0 {
			return failure.Invalid(failure.FieldError{Field: "since_sequence", Message: "invalid"})
		}
	}
	if raw := string(rc.QueryArgs().Peek("limit")); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 1 || limit > 500 {
			return failure.Invalid(failure.FieldError{Field: "limit", Message: "range"})
		}
	}
	filter := streamFilter{wave: string(rc.QueryArgs().Peek("waveId")), stage: string(rc.QueryArgs().Peek("stage")), class: "domain"}
	page := eventsPageJSON{Items: []event.Envelope{}, LastSequence: since}
	for len(page.Items) < limit {
		events, err := a.deps.History.ProjectEvents(RequestContext(rc), projectID, page.LastSequence, replayBatch)
		if err != nil {
			return err
		}
		for _, e := range events {
			if len(page.Items) == limit {
				page.More = true
				break
			}
			page.LastSequence = e.Sequence
			if filter.accepts(e, false) {
				page.Items = append(page.Items, e)
			}
		}
		if len(events) < replayBatch || page.More {
			break
		}
	}
	a.responder.JSON(rc, fasthttp.StatusOK, page)
	return nil
}
