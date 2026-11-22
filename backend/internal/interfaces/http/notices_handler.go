package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/valyala/fasthttp"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// NoticeHistory reads the notices of a person by global outbox position (sse.md section 3).
type NoticeHistory interface {
	Notices(ctx context.Context, principal identity.Principal, after int64, since *time.Time, projects []string, limit int) ([]appevent.Notice, error)
	LastPosition(ctx context.Context) (int64, error)
}

const maxNoticeProjects = 20

// watchNotices is watchNotices (/me/events): the domain events of the projects where the person has
// a role, for Pendências, dashboard and notices (US-48, US-10, US-02 CA-7). The SSE id is the global
// outbox position; since=0 replays the 24 h window; no position starts live.
func (a *api) watchNotices(rc *fasthttp.RequestCtx, principal identity.Principal) error {
	projects := queryList(rc, "projectId")
	if len(projects) > maxNoticeProjects {
		return failure.Invalid(failure.FieldError{Field: "projectId", Message: "too_many"})
	}
	for _, id := range projects {
		if !uuidPattern.MatchString(id) {
			return failure.Invalid(failure.FieldError{Field: "projectId", Message: "invalid"})
		}
	}
	ctx := context.WithoutCancel(RequestContext(rc))
	raw := string(rc.Request.Header.Peek("Last-Event-ID"))
	if raw == "" {
		raw = string(rc.QueryArgs().Peek("since"))
	}
	var after int64
	var window *time.Time
	if raw == "" {
		last, err := a.deps.Notices.LastPosition(ctx)
		if err != nil {
			return err
		}
		after = last
	} else {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return failure.Invalid(failure.FieldError{Field: "since", Message: "invalid"})
		}
		start := time.Now().Add(-replayWindow)
		after, window = parsed, &start
	}
	current := currentSession(rc)
	ends := current.IssuedAt.Add(identity.SessionLifetime)
	subscription := a.deps.Events.Subscribe("", subscriberBuffer)
	rc.Response.Header.Set(fasthttp.HeaderCacheControl, "no-cache, no-transform")
	rc.Response.Header.Set("X-Accel-Buffering", "no")
	rc.SetContentType("text/event-stream; charset=utf-8")
	rc.SetStatusCode(fasthttp.StatusOK)
	rc.SetBodyStreamWriter(func(w *bufio.Writer) {
		defer a.deps.Events.Unsubscribe(subscription)
		stream := &noticeStream{w: w, last: after}
		stream.raw("retry: 2000\n\n")
		if !stream.catchUp(ctx, a.deps.Notices, principal, window, projects) || !stream.flush() {
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
				if active, err := a.deps.Sessions.Active(ctx, current); err == nil && !active {
					stream.ended("session_revoked")
					return
				}
				stream.raw(": hb " + time.Now().Format(time.RFC3339) + "\n\n")
			case n, open := <-subscription.C():
				if !open {
					stream.ended("slow_consumer")
					return
				}
				if n.Telemetry == nil && n.Position > stream.last && !stream.catchUp(ctx, a.deps.Notices, principal, nil, projects) {
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

type noticeStream struct {
	w      *bufio.Writer
	last   int64
	failed bool
}

func (s *noticeStream) raw(text string) {
	if _, err := s.w.WriteString(text); err != nil {
		s.failed = true
	}
}

func (s *noticeStream) flush() bool {
	if s.failed {
		return false
	}
	return s.w.Flush() == nil
}

func (s *noticeStream) ended(reason string) {
	data, _ := json.Marshal(map[string]string{"reason": reason})
	s.raw("event: easycode.ended\ndata: " + string(data) + "\n\n")
	_ = s.w.Flush()
}

// catchUp sends every notice after the last position sent, in position order.
func (s *noticeStream) catchUp(ctx context.Context, history NoticeHistory, principal identity.Principal, window *time.Time, projects []string) bool {
	for {
		notices, err := history.Notices(ctx, principal, s.last, window, projects, replayBatch)
		if err != nil {
			return false
		}
		for _, n := range notices {
			if data, err := json.Marshal(n.Envelope); err == nil && len(data) <= 64<<10 {
				s.raw(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", n.Position, n.Envelope.Type, data))
			}
			s.last = n.Position
		}
		if len(notices) < replayBatch || s.failed {
			return !s.failed
		}
	}
}
