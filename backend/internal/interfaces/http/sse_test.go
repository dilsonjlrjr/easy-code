package http_test

import (
	"bufio"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/event"
	"easycode/internal/domain/identity"
	httpapi "easycode/internal/interfaces/http"
)

type fakeHistory struct{ events []event.Envelope }

func (f fakeHistory) ProjectEvents(_ context.Context, _ string, after int64, limit int) ([]event.Envelope, error) {
	var out []event.Envelope
	for _, e := range f.events {
		if e.Sequence > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f fakeHistory) FirstSequenceSince(context.Context, string, time.Time) (int64, bool, error) {
	return f.events[0].Sequence, true, nil
}

func (f fakeHistory) LastSequence(context.Context, string) (int64, error) {
	return f.events[len(f.events)-1].Sequence, nil
}

type allowAll struct{}

func (allowAll) Authorize(context.Context, identity.Principal, string) error { return nil }

// US-09 CA-3, sse.md 5.3: a starting point older than the 24 h window gets easycode.gap first,
// then the replay from the first available event, in order.
func TestStreamSendsGapThenReplayInOrder(t *testing.T) {
	history := fakeHistory{}
	for seq := int64(5); seq <= 7; seq++ {
		history.events = append(history.events, event.Envelope{Sequence: seq, Type: "br.easycode.wave.started.v1", Data: []byte(`{}`)})
	}
	shutdown := make(chan struct{})
	h := productHarness(t, func(d *httpapi.Dependencies) {
		d.Events, d.History, d.Access, d.Shutdown = appevent.NewBroker(), history, allowAll{}, shutdown
	})
	rc := h.do(fasthttp.MethodGet, "/api/v1/projects/0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30/events?since=1",
		map[string]string{"Cookie": sessionCookie(t, h)})
	if rc.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("stream: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	reader := bufio.NewReader(rc.Response.BodyStream())
	var lines []string
	for len(lines) < 30 {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		lines = append(lines, strings.TrimSpace(line))
		if line == "id: 7\n" {
			break
		}
	}
	close(shutdown)
	text := strings.Join(lines, "|")
	gap := strings.Index(text, "event: easycode.gap")
	if gap < 0 || !strings.Contains(text, `"first_available":5`) || gap > strings.Index(text, "id: 5") ||
		strings.Index(text, "id: 5") > strings.Index(text, "id: 6") || strings.Index(text, "id: 6") > strings.Index(text, "id: 7") {
		t.Fatalf("gap then ordered replay: %s", text)
	}
}
