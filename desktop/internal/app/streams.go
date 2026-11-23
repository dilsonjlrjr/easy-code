package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"easycode-desktop/internal/notice"
	"easycode-desktop/internal/sse"
)

// Streams of the app (cliente-local.md section 4): watchNotices while signed in, watchProjectEvents filtered
// by the wave while its view is open.
const (
	StreamNotices = "notices"
	StreamWave    = "wave"
)

// ConnectionState is sent with EventConnection; the interface shows it in text (ESP-28 section 6).
type ConnectionState struct {
	Stream      string `json:"stream"`
	State       string `json:"state"`
	Reconnected bool   `json:"reconnected"`
}

// NoticeSignal tells the dashboard that something it shows may have changed.
type NoticeSignal struct {
	Type      string `json:"type"`
	ProjectID string `json:"project_id,omitempty"`
	WaveID    string `json:"wave_id,omitempty"`
}

// StreamSignal names the stream of a gap or a closed wave view.
type StreamSignal struct {
	Stream string `json:"stream"`
	Reason string `json:"reason,omitempty"`
}

type ended struct {
	Reason string `json:"reason"`
}

func (s *Service) streamOptions(path string, query url.Values, since, stream string) sse.Options {
	tokens, client := s.current()
	opts := sse.Options{
		URL:       client.URL(path, query),
		Since:     since,
		Token:     tokens.AccessToken,
		Renew:     tokens.Renew,
		UserAgent: s.userAgent(),
		OnState: func(state sse.State, reconnected bool) {
			s.emit(EventConnection, ConnectionState{Stream: stream, State: string(state), Reconnected: reconnected})
		},
	}
	if s.deps.Tune != nil {
		s.deps.Tune(&opts)
	}
	return opts
}

func (s *Service) startNotices() {
	s.mu.Lock()
	if s.noticesCancel != nil {
		s.noticesCancel()
	}
	ctx, cancel := context.WithCancel(s.root)
	s.noticesCancel = cancel
	s.mu.Unlock()
	// No since: a new session starts at the next notice, old ones are already in Pending items.
	opts := s.streamOptions("/me/events", nil, "", StreamNotices)
	go func() {
		err := sse.Run(ctx, opts, s.handleNotice)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.log.Warn("notices stream ended", "error", err)
			s.handleSessionError(err)
		}
	}()
}

func (s *Service) handleNotice(ev sse.Event) sse.Action {
	switch ev.Name {
	case sse.EventGap:
		s.emit(EventGap, StreamSignal{Stream: StreamNotices})
		return sse.Continue
	case sse.EventEnded:
		return s.onEnded(ev, StreamNotices)
	}
	var ce notice.CloudEvent
	if err := json.Unmarshal(ev.Data, &ce); err != nil {
		s.log.Warn("unreadable notice", "type", ev.Name)
		return sse.Continue
	}
	s.emit(EventNotice, NoticeSignal{Type: notice.ShortType(ce.Type), ProjectID: ce.ProjectID, WaveID: ce.WaveID})
	builder := notice.Builder{ProjectName: s.projectName, Followed: s.isFollowedOrOpen}
	n, ok := builder.Build(ce, s.locale())
	if !ok {
		return sse.Continue
	}
	s.rememberNotification(n)
	if s.platform != nil {
		s.mu.Lock()
		mute := s.prefs.MuteSound
		s.mu.Unlock()
		if err := s.platform.Notify(n.ID, n.Title, n.Body, mute); err != nil {
			s.log.Warn("native notification failed", "error", err)
		}
	}
	return sse.Continue
}

// isFollowedOrOpen says whether a finished wave notifies: its view is open or the user follows it.
func (s *Service) isFollowedOrOpen(projectID, waveID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wave != nil && s.wave.projectID == projectID && s.wave.waveID == waveID {
		return true
	}
	return s.followed[projectID+"/"+waveID]
}

const maxRememberedNotifications = 200

func (s *Service) rememberNotification(n notice.Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notifications[n.ID]; !ok {
		s.notifyOrder = append(s.notifyOrder, n.ID)
	}
	s.notifications[n.ID] = n
	for len(s.notifyOrder) > maxRememberedNotifications {
		delete(s.notifications, s.notifyOrder[0])
		s.notifyOrder = s.notifyOrder[1:]
	}
}

// onEnded applies sse.md section 5.4 for both streams.
func (s *Service) onEnded(ev sse.Event, stream string) sse.Action {
	var reason ended
	_ = json.Unmarshal(ev.Data, &reason)
	switch reason.Reason {
	case "slow_consumer":
		return sse.ReconnectNow
	case "offboarding":
		return sse.ReconnectWithBackoff
	case "session_revoked":
		// Revoked on the web or account disabled: back to sign-in (US-45 CA-2).
		go func() {
			s.clearSession()
			s.setSignedOut(ReasonSessionRevoked)
		}()
		return sse.Stop
	case "session_expired":
		// The session passed its limit: renew with the refresh token and reconnect.
		go s.verifySession(reason.Reason)
		return sse.Stop
	case "access_removed", "project_deleted":
		s.emit(EventProjectsChanged, StreamSignal{Stream: stream, Reason: reason.Reason})
		if stream == StreamWave {
			s.emit(EventWaveClosed, StreamSignal{Stream: stream, Reason: reason.Reason})
			return sse.Stop
		}
		return sse.ReconnectWithBackoff
	}
	return sse.ReconnectWithBackoff
}

// verifySession renews the access token after the server ended a stream for the session: a refused refresh
// signs out with "session revoked" (US-45 CA-2); otherwise the streams start again.
func (s *Service) verifySession(reason string) {
	tokens, _ := s.current()
	if tokens == nil {
		return
	}
	ctx := s.rootCtx()
	stale, err := tokens.AccessToken(ctx)
	if err == nil {
		_, err = tokens.Renew(ctx, stale)
	}
	if err != nil {
		if reason == "session_expired" && !errors.Is(err, context.Canceled) {
			s.setSignedOut(ReasonSessionExpired)
			_ = tokens.Clear()
			return
		}
		s.handleSessionError(err)
		return
	}
	s.mu.Lock()
	wave := s.wave
	s.mu.Unlock()
	s.startNotices()
	if wave != nil {
		// The wave stream restarts at the next event: the view reloads by REST as after a gap.
		s.startWave(wave.projectID, wave.waveID, "")
		s.emit(EventGap, StreamSignal{Stream: StreamWave})
	}
}

func (s *Service) startWave(projectID, waveID, since string) {
	s.mu.Lock()
	if s.wave != nil {
		s.wave.cancel()
	}
	s.waveSeq++
	ctx, cancel := context.WithCancel(s.root)
	watch := &waveWatch{id: s.waveSeq, projectID: projectID, waveID: waveID, cancel: cancel}
	s.wave = watch
	s.mu.Unlock()
	path := "/projects/" + url.PathEscape(projectID) + "/events"
	opts := s.streamOptions(path, url.Values{"waveId": {waveID}}, since, StreamWave)
	go func() {
		err := sse.Run(ctx, opts, func(ev sse.Event) sse.Action { return s.handleWaveEvent(watch, ev) })
		if ctx.Err() != nil {
			return
		}
		switch {
		case errors.Is(err, sse.ErrNotFound):
			s.emit(EventWaveClosed, StreamSignal{Stream: StreamWave, Reason: "not_found"})
		case err != nil:
			s.log.Warn("wave stream ended", "error", err)
			s.handleSessionError(err)
		}
	}()
}

func (s *Service) handleWaveEvent(watch *waveWatch, ev sse.Event) sse.Action {
	s.mu.Lock()
	currentWatch := s.wave != nil && s.wave.id == watch.id
	s.mu.Unlock()
	if !currentWatch {
		return sse.Stop
	}
	switch ev.Name {
	case sse.EventGap:
		s.emit(EventGap, StreamSignal{Stream: StreamWave})
		return sse.Continue
	case sse.EventEnded:
		return s.onEnded(ev, StreamWave)
	}
	if !json.Valid(ev.Data) {
		return sse.Continue
	}
	s.emit(EventWave, json.RawMessage(ev.Data))
	return sse.Continue
}
