// Package sse is the Server-Sent Events client of easycode-desktop (sse.md sections 5, 10 and 12), grown
// from spikes/eventos-ao-vivo/client/sse.go: Bearer token, Last-Event-ID of the last processed event,
// exponential backoff from 100 ms to 5 s with jitter, reconnection without loss, a dead connection detected
// after 45 s without any frame, and the control frames easycode.gap and easycode.ended handed to the caller.
package sse

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Control frames of the api (sse.md sections 5.3 and 5.4).
const (
	EventGap   = "easycode.gap"
	EventEnded = "easycode.ended"
)

// Defaults of sse.md sections 10 and 12.
const (
	DefaultMinBackoff  = 100 * time.Millisecond
	DefaultMaxBackoff  = 5 * time.Second
	DefaultIdleTimeout = 45 * time.Second
)

var (
	// ErrUnauthorized means the api answered 401 even with a renewed token.
	ErrUnauthorized = errors.New("stream: unauthorized")
	// ErrNotFound means the project does not exist or the user has no role in it (404, RNF-04).
	ErrNotFound = errors.New("stream: not found")
)

// Event is one SSE frame with data.
type Event struct {
	ID   string // empty for telemetry and control frames
	Name string // the `event:` line, the CloudEvent type or a control name
	Data []byte
}

// Action tells the client what to do after an event.
type Action int

const (
	// Continue reading the stream.
	Continue Action = iota
	// ReconnectNow closes the stream and reconnects at once (slow_consumer).
	ReconnectNow
	// ReconnectWithBackoff closes the stream and reconnects after the backoff (offboarding).
	ReconnectWithBackoff
	// Stop closes the stream for good (session ended, access removed, project deleted).
	Stop
)

// State is the connection state shown in text by the interface (ESP-28 section 6).
type State string

const (
	StateConnecting   State = "connecting"
	StateLive         State = "live"
	StateReconnecting State = "reconnecting"
	StateStopped      State = "stopped"
)

// Options configures one stream.
type Options struct {
	// URL of the stream, without since (it is added on the first connection only).
	URL string
	// Since is the starting point of the first connection (sse.md section 5.1); empty = next event.
	Since string
	// Token returns the Bearer token; Renew is called after a 401 with the token that got it.
	Token func(ctx context.Context) (string, error)
	Renew func(ctx context.Context, stale string) (string, error)
	// OnState reports connection changes; reconnected is true on a successful connection after a drop.
	OnState func(state State, reconnected bool)

	HTTP        *http.Client
	UserAgent   string
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
	IdleTimeout time.Duration
	// Wait sleeps (replaced in tests).
	Wait func(ctx context.Context, d time.Duration) error
}

// Run reads the stream until ctx ends, handle returns Stop, or the api refuses the connection for good.
// handle is called in order, from a single goroutine. It returns nil when stopped by ctx or by handle.
func Run(ctx context.Context, opts Options, handle func(Event) Action) error {
	opts = withDefaults(opts)
	var lastID string
	backoff := opts.MinBackoff
	connectedBefore := false
	notify := func(state State, reconnected bool) {
		if opts.OnState != nil {
			opts.OnState(state, reconnected)
		}
	}
	defer notify(StateStopped, false)
	notify(StateConnecting, false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		action, live, err := connect(ctx, opts, lastID, func() {
			notify(StateLive, connectedBefore)
			connectedBefore = true
		}, func(ev Event) Action {
			if ev.ID != "" {
				lastID = ev.ID
			}
			return handle(ev)
		})
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrNotFound) {
			return err
		}
		var refused *refusedError
		if errors.As(err, &refused) && refused.permanent {
			return err
		}
		if action == Stop {
			return nil
		}
		if live {
			backoff = opts.MinBackoff
		}
		notify(StateReconnecting, false)
		if action == ReconnectNow {
			continue
		}
		delay := jitter(backoff)
		if refused != nil && refused.retryAfter > delay {
			delay = refused.retryAfter
		}
		if err := opts.Wait(ctx, delay); err != nil {
			return nil
		}
		backoff = min(backoff*2, opts.MaxBackoff)
	}
}

func withDefaults(opts Options) Options {
	if opts.HTTP == nil {
		// No overall timeout: the stream has no planned end; the idle timer closes a dead one.
		opts.HTTP = &http.Client{}
	}
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = DefaultMinBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = DefaultMaxBackoff
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = DefaultIdleTimeout
	}
	if opts.Wait == nil {
		opts.Wait = func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	return opts
}

// jitter returns a random delay between half and all of d.
func jitter(d time.Duration) time.Duration {
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(half+1)
}

type refusedError struct {
	status     int
	permanent  bool
	retryAfter time.Duration
}

func (e *refusedError) Error() string { return fmt.Sprintf("stream: status %d", e.status) }

// connect opens one connection and reads it to the end. live says whether the api answered 200.
func connect(ctx context.Context, opts Options, lastID string, onLive func(), handle func(Event) Action) (Action, bool, error) {
	token, err := opts.Token(ctx)
	if err != nil {
		return Stop, false, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	resp, err := open(ctx, opts, lastID, token)
	if err != nil {
		return Continue, false, err
	}
	if resp.StatusCode == http.StatusUnauthorized && opts.Renew != nil {
		resp.Body.Close()
		token, err = opts.Renew(ctx, token)
		if err != nil {
			return Stop, false, fmt.Errorf("%w: %v", ErrUnauthorized, err)
		}
		if resp, err = open(ctx, opts, lastID, token); err != nil {
			return Continue, false, err
		}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return Stop, false, ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return Stop, false, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return Continue, false, &refusedError{status: resp.StatusCode, retryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	default:
		return Stop, false, &refusedError{status: resp.StatusCode, permanent: true}
	}
	onLive()
	return read(ctx, resp.Body, opts.IdleTimeout, handle), true, nil
}

func open(ctx context.Context, opts Options, lastID, token string) (*http.Response, error) {
	address := opts.URL
	if lastID == "" && opts.Since != "" {
		address = withQuery(address, "since", opts.Since)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Authorization", "Bearer "+token)
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	return opts.HTTP.Do(req)
}

func withQuery(address, key, value string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return address
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// read parses frames until the body ends, handle asks to leave, or no line arrives within idle.
func read(ctx context.Context, body io.ReadCloser, idle time.Duration, handle func(Event) Action) Action {
	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { body.Close() }) }
	timer := time.AfterFunc(idle, closeBody)
	defer timer.Stop()
	stop := context.AfterFunc(ctx, closeBody)
	defer stop()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var ev Event
	var data []string
	for scanner.Scan() {
		timer.Reset(idle)
		line := scanner.Text()
		if line == "" {
			if data != nil {
				ev.Data = []byte(strings.Join(data, "\n"))
				if action := handle(ev); action != Continue {
					return action
				}
			}
			ev, data = Event{}, nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // heartbeat comment
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			ev.ID = value
		case "event":
			ev.Name = value
		case "data":
			data = append(data, value)
		}
	}
	return ReconnectWithBackoff
}
