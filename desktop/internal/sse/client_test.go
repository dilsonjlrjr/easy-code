package sse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type request struct {
	since, lastEventID, auth string
}

// fakeAPI serves one scripted answer per connection.
type fakeAPI struct {
	mu       sync.Mutex
	requests []request
	answers  []func(w http.ResponseWriter, flush func())
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, request{
		since: r.URL.Query().Get("since"), lastEventID: r.Header.Get("Last-Event-ID"), auth: r.Header.Get("Authorization"),
	})
	n := len(f.requests)
	var answer func(http.ResponseWriter, func())
	if n <= len(f.answers) {
		answer = f.answers[n-1]
	}
	f.mu.Unlock()
	if answer == nil {
		<-r.Context().Done()
		return
	}
	flusher := w.(http.Flusher)
	answer(w, flusher.Flush)
}

func stream(frames ...string) func(http.ResponseWriter, func()) {
	return func(w http.ResponseWriter, flush func()) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, frame := range frames {
			fmt.Fprint(w, frame)
			flush()
		}
	}
}

func status(code int, header ...string) func(http.ResponseWriter, func()) {
	return func(w http.ResponseWriter, _ func()) {
		if len(header) == 2 {
			w.Header().Set(header[0], header[1])
		}
		w.WriteHeader(code)
	}
}

func options(url string) (Options, *[]time.Duration, *[]string) {
	var waits []time.Duration
	var states []string
	var mu sync.Mutex
	return Options{
		URL:   url,
		Token: func(context.Context) (string, error) { return "a1", nil },
		Renew: func(context.Context, string) (string, error) { return "a2", nil },
		OnState: func(state State, reconnected bool) {
			mu.Lock()
			defer mu.Unlock()
			states = append(states, fmt.Sprintf("%s/%v", state, reconnected))
		},
		Wait: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			return ctx.Err()
		},
	}, &waits, &states
}

func TestResumesFromLastEventIDWithoutLossOrDuplicate(t *testing.T) {
	api := &fakeAPI{answers: []func(http.ResponseWriter, func()){
		stream("retry: 2000\n\n", "id: 1\nevent: br.easycode.wave.started.v1\ndata: {\"id\":\"e1\"}\n\n",
			": hb\n\n", "event: br.easycode.agent.progress_reported.v1\ndata: {\"id\":\"t1\"}\n\n",
			"id: 2\nevent: br.easycode.wave.paused.v1\ndata: {\"id\":\"e2\"}\n\n"),
		stream("id: 3\nevent: br.easycode.wave.resumed.v1\ndata: {\"id\":\"e3\"}\n\n"),
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	opts, waits, states := options(server.URL + "/api/v1/me/events")
	opts.Since = "0"
	var got []string
	err := Run(context.Background(), opts, func(ev Event) Action {
		got = append(got, ev.ID+"|"+ev.Name+"|"+string(ev.Data))
		if ev.ID == "3" {
			return Stop
		}
		return Continue
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`1|br.easycode.wave.started.v1|{"id":"e1"}`,
		`|br.easycode.agent.progress_reported.v1|{"id":"t1"}`,
		`2|br.easycode.wave.paused.v1|{"id":"e2"}`,
		`3|br.easycode.wave.resumed.v1|{"id":"e3"}`,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events\n got %v\nwant %v", got, want)
	}
	if api.requests[0].since != "0" || api.requests[0].lastEventID != "" || api.requests[0].auth != "Bearer a1" {
		t.Fatalf("first request %+v", api.requests[0])
	}
	// Telemetry has no id: the resume stays anchored on the last domain event (sse.md section 2).
	if api.requests[1].since != "" || api.requests[1].lastEventID != "2" {
		t.Fatalf("second request %+v", api.requests[1])
	}
	if len(*waits) != 1 || (*waits)[0] < 50*time.Millisecond || (*waits)[0] > 100*time.Millisecond {
		t.Fatalf("backoff after a live connection must restart at 100 ms with jitter: %v", *waits)
	}
	wantStates := "[connecting/false live/false reconnecting/false live/true stopped/false]"
	if fmt.Sprint(*states) != wantStates {
		t.Fatalf("states %v, want %s", *states, wantStates)
	}
}

func TestDeadConnectionIsDetectedByIdleTimeout(t *testing.T) {
	api := &fakeAPI{answers: []func(http.ResponseWriter, func()){
		func(w http.ResponseWriter, flush func()) {
			stream("id: 7\nevent: x\ndata: {}\n\n")(w, flush)
			time.Sleep(2 * time.Second) // silent: no heartbeat
		},
		stream("id: 8\nevent: x\ndata: {}\n\n"),
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	opts, _, _ := options(server.URL)
	opts.IdleTimeout = 150 * time.Millisecond
	start := time.Now()
	err := Run(context.Background(), opts, func(ev Event) Action {
		if ev.ID == "8" {
			return Stop
		}
		return Continue
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("idle connection was not dropped")
	}
	if api.requests[1].lastEventID != "7" {
		t.Fatalf("resume %+v", api.requests[1])
	}
}

func TestRenewsTokenAfter401(t *testing.T) {
	api := &fakeAPI{answers: []func(http.ResponseWriter, func()){
		status(401),
		stream("id: 1\nevent: x\ndata: {}\n\n"),
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	opts, _, _ := options(server.URL)
	if err := Run(context.Background(), opts, func(Event) Action { return Stop }); err != nil {
		t.Fatal(err)
	}
	if api.requests[1].auth != "Bearer a2" {
		t.Fatalf("renewed token not used: %+v", api.requests)
	}
}

func TestRefusalsEndTheStream(t *testing.T) {
	cases := map[string]struct {
		answers []func(http.ResponseWriter, func())
		renew   func(context.Context, string) (string, error)
		want    error
	}{
		"401 after renewal": {answers: []func(http.ResponseWriter, func()){status(401), status(401)}, want: ErrUnauthorized},
		"renewal refused": {
			answers: []func(http.ResponseWriter, func()){status(401)},
			renew:   func(context.Context, string) (string, error) { return "", errors.New("revoked") },
			want:    ErrUnauthorized,
		},
		"404": {answers: []func(http.ResponseWriter, func()){status(404)}, want: ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(&fakeAPI{answers: tc.answers})
			defer server.Close()
			opts, _, _ := options(server.URL)
			if tc.renew != nil {
				opts.Renew = tc.renew
			}
			if err := Run(context.Background(), opts, func(Event) Action { return Continue }); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
	server := httptest.NewServer(&fakeAPI{answers: []func(http.ResponseWriter, func()){status(400)}})
	defer server.Close()
	opts, _, _ := options(server.URL)
	if err := Run(context.Background(), opts, func(Event) Action { return Continue }); err == nil {
		t.Fatal("400 must end the stream with an error")
	}
}

func TestBackoffGrowsToFiveSecondsAndHonoursRetryAfter(t *testing.T) {
	answers := []func(http.ResponseWriter, func()){status(429, "Retry-After", "7")}
	for range 8 {
		answers = append(answers, status(503))
	}
	answers = append(answers, stream("id: 1\nevent: x\ndata: {}\n\n"))
	server := httptest.NewServer(&fakeAPI{answers: answers})
	defer server.Close()
	opts, waits, _ := options(server.URL)
	if err := Run(context.Background(), opts, func(Event) Action { return Stop }); err != nil {
		t.Fatal(err)
	}
	if (*waits)[0] != 7*time.Second {
		t.Fatalf("Retry-After ignored: %v", *waits)
	}
	last := (*waits)[len(*waits)-1]
	if last < 2500*time.Millisecond || last > 5*time.Second {
		t.Fatalf("backoff must cap at 5 s: %v", *waits)
	}
}

func TestControlFramesReachTheHandler(t *testing.T) {
	api := &fakeAPI{answers: []func(http.ResponseWriter, func()){
		stream("event: easycode.gap\ndata: {\"request\":40,\"first_available\":401,\"action\":\"reload_state\"}\n\n",
			"event: easycode.ended\ndata: {\"reason\":\"slow_consumer\"}\n\n"),
		stream("event: easycode.ended\ndata: {\"reason\":\"session_revoked\"}\n\n"),
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	opts, waits, _ := options(server.URL)
	var names []string
	err := Run(context.Background(), opts, func(ev Event) Action {
		names = append(names, ev.Name)
		switch {
		case ev.Name == EventEnded && string(ev.Data) == `{"reason":"slow_consumer"}`:
			return ReconnectNow
		case ev.Name == EventEnded:
			return Stop
		}
		return Continue
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(names) != "[easycode.gap easycode.ended easycode.ended]" {
		t.Fatalf("names %v", names)
	}
	if len(*waits) != 0 {
		t.Fatalf("slow_consumer must reconnect at once: %v", *waits)
	}
}

func TestMultiLineDataAndCancel(t *testing.T) {
	api := &fakeAPI{answers: []func(http.ResponseWriter, func()){stream("data: a\ndata: b\n\n")}}
	server := httptest.NewServer(api)
	defer server.Close()
	opts, _, _ := options(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	var data string
	err := Run(ctx, opts, func(ev Event) Action {
		data = string(ev.Data)
		cancel()
		return Continue
	})
	if err != nil || data != "a\nb" {
		t.Fatalf("%q %v", data, err)
	}
}
