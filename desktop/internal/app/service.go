// Package app is the core of easycode-desktop, bound to the interface through Wails (ADR-20). Only this Go
// process talks to the api and to Keycloak; the WebView receives data through the bound methods and the
// Wails event channel and never opens a connection of its own (cliente-local.md section 4). The app is
// read-only: there is no control or decision here, every action opens the web screen (PD-07).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"strconv"
	"sync"
	"time"

	"easycode-desktop/internal/api"
	"easycode-desktop/internal/config"
	"easycode-desktop/internal/keychain"
	"easycode-desktop/internal/notice"
	"easycode-desktop/internal/oidc"
	"easycode-desktop/internal/session"
	"easycode-desktop/internal/sse"
	"easycode-desktop/internal/weblink"
)

// Names of the events sent to the interface.
const (
	EventSession         = "easycode:session"
	EventConnection      = "easycode:connection"
	EventNotice          = "easycode:notice"
	EventWave            = "easycode:wave-event"
	EventGap             = "easycode:gap"
	EventWaveClosed      = "easycode:wave-closed"
	EventNavigate        = "easycode:navigate"
	EventProjectsChanged = "easycode:projects-changed"
)

// Phases of the session shown by the interface (ESP-28 section 6).
const (
	PhaseStarting         = "starting"
	PhaseSignedOut        = "signed_out"
	PhaseAwaitingApproval = "awaiting_approval"
	PhaseSignedIn         = "signed_in"
)

// Reasons for being signed out.
const (
	ReasonCodeExpired         = "code_expired"
	ReasonDenied              = "denied"
	ReasonKeychainUnavailable = "keychain_unavailable"
	ReasonSessionRevoked      = "session_revoked"
	ReasonSessionExpired      = "session_expired"
	ReasonSignInFailed        = "sign_in_failed"
)

// Platform is what the app needs from Wails (replaced by a fake in tests).
type Platform interface {
	Emit(name string, data any)
	OpenURL(address string) error
	Notify(id, title, body string, silent bool) error
	Show()
}

// Deps are the collaborators of the service.
type Deps struct {
	Version  string
	Prefs    *config.Store
	Keychain func(server string) keychain.Store
	Now      func() time.Time
	Logger   *slog.Logger
	// Tune and TuneIdentity shorten timings in tests (nil in production).
	Tune         func(*sse.Options)
	TuneIdentity func(*oidc.Client)
}

// DeviceView is the code the user types in the browser (ESP-28 section 3, Login).
type DeviceView struct {
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	ExpiresAt               time.Time `json:"expires_at"`
}

// Status is the state of the app shown by every view.
type Status struct {
	Phase           string          `json:"phase"`
	Reason          string          `json:"reason,omitempty"`
	Server          string          `json:"server,omitempty"`
	ServerFromEnv   bool            `json:"server_from_env"`
	Locale          string          `json:"locale"`
	MuteSound       bool            `json:"mute_sound"`
	AppVersion      string          `json:"app_version"`
	ServerVersion   string          `json:"server_version,omitempty"`
	VersionMismatch bool            `json:"version_mismatch"`
	User            json.RawMessage `json:"user,omitempty"`
	Device          *DeviceView     `json:"device,omitempty"`
}

// ProjectView feeds the project view.
type ProjectView struct {
	Project  json.RawMessage `json:"project"`
	Workflow json.RawMessage `json:"workflow"`
	Waves    json.RawMessage `json:"waves"`
}

// WaveView feeds the live wave view.
type WaveView struct {
	Project json.RawMessage `json:"project"`
	Wave    json.RawMessage `json:"wave"`
	Calls   json.RawMessage `json:"calls"`
}

// Service is bound to the interface.
type Service struct {
	deps     Deps
	log      *slog.Logger
	platform Platform

	mu            sync.Mutex
	root          context.Context
	prefs         config.Preferences
	server        string
	serverFromEnv bool
	phase         string
	reason        string
	user          json.RawMessage
	serverVersion string
	device        *DeviceView

	tokens   *session.Tokens
	client   *api.Client
	identity *oidc.Client

	signInCancel  context.CancelFunc
	noticesCancel context.CancelFunc
	wave          *waveWatch
	waveSeq       int
	followed      map[string]bool
	projectNames  map[string]string
	notifications map[string]notice.Notification
	notifyOrder   []string
}

type waveWatch struct {
	id        int
	projectID string
	waveID    string
	cancel    context.CancelFunc
}

// Host is the side of the service used by main (lifecycle, platform, notification clicks). It is a separate
// type so these methods are not bound to the interface.
type Host struct{ s *Service }

// New returns the service bound to the interface and its host for main.
func New(deps Deps) (*Service, *Host) {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Keychain == nil {
		deps.Keychain = func(server string) keychain.Store { return keychain.NewOS(server) }
	}
	s := &Service{
		deps: deps, log: deps.Logger, phase: PhaseStarting, root: context.Background(),
		followed: map[string]bool{}, projectNames: map[string]string{}, notifications: map[string]notice.Notification{},
	}
	return s, &Host{s: s}
}

// SetPlatform connects the service to Wails.
func (h *Host) SetPlatform(p Platform) { h.s.platform = p }

// Startup loads the preferences and restores a stored session (refresh token in the keychain).
func (h *Host) Startup(ctx context.Context) error {
	s := h.s
	prefs, err := s.deps.Prefs.Load()
	if err != nil {
		s.log.Warn("preferences unreadable, using defaults", "error", err)
		prefs = config.Preferences{Locale: config.LocalePtBR}
	}
	server, fromEnv, err := s.deps.Prefs.Server(prefs)
	if err != nil {
		s.log.Warn("configured server address is invalid", "error", err)
		server = ""
	}
	s.mu.Lock()
	s.root, s.prefs, s.serverFromEnv = ctx, prefs, fromEnv
	s.useServerLocked(server)
	s.mu.Unlock()
	if server == "" {
		s.setSignedOut("")
		return nil
	}
	go s.restore()
	return nil
}

// Shutdown stops the streams.
func (h *Host) Shutdown() {
	s := h.s
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopStreamsLocked()
	if s.signInCancel != nil {
		s.signInCancel()
	}
}

// userAgent is shown as the device in Minha conta › Cliente local: easycode-desktop/<version> (<os>; <arch>).
func (s *Service) userAgent() string {
	return fmt.Sprintf("easycode-desktop/%s (%s; %s)", s.deps.Version, runtime.GOOS, runtime.GOARCH)
}

func (s *Service) locale() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefs.Locale
}

// useServerLocked rebuilds the clients for a server address.
func (s *Service) useServerLocked(server string) {
	s.server = server
	s.identity = nil
	if server == "" {
		s.tokens, s.client = nil, nil
		return
	}
	s.tokens = session.New(s.deps.Keychain(server), refresher{s}, s.deps.Now)
	s.client = api.New(server, s.tokens, s.userAgent(), s.locale)
}

type refresher struct{ s *Service }

func (r refresher) Refresh(ctx context.Context, refreshToken string) (oidc.Tokens, error) {
	identity, err := r.s.identityClient(ctx)
	if err != nil {
		return oidc.Tokens{}, err
	}
	return identity.Refresh(ctx, refreshToken)
}

// identityClient reads getLocalClientConfiguration once per server (RNF-17: no Keycloak address in code).
func (s *Service) identityClient(ctx context.Context) (*oidc.Client, error) {
	s.mu.Lock()
	identity, client := s.identity, s.client
	s.mu.Unlock()
	if identity != nil {
		return identity, nil
	}
	if client == nil {
		return nil, errNoServer
	}
	var cfg oidc.Configuration
	if err := client.GetPublic(ctx, "/session/local-client", &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	identity = oidc.NewClient(cfg, s.userAgent())
	if s.deps.TuneIdentity != nil {
		s.deps.TuneIdentity(identity)
	}
	s.mu.Lock()
	if s.client == client {
		s.identity = identity
	}
	s.mu.Unlock()
	return identity, nil
}

var errNoServer = errors.New("no server configured")

// restore turns a stored refresh token into a session at start-up.
func (s *Service) restore() {
	ctx := s.rootCtx()
	tokens, client := s.current()
	ok, err := tokens.HasStoredSession()
	if err != nil {
		s.log.Warn("keychain unavailable at start-up", "error", err)
		s.setSignedOut(ReasonKeychainUnavailable)
		return
	}
	if !ok {
		s.setSignedOut("")
		return
	}
	if err := s.loadUser(ctx, client); err != nil {
		s.handleSessionError(err)
		return
	}
	s.enterSignedIn()
}

func (s *Service) rootCtx() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

func (s *Service) current() (*session.Tokens, *api.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens, s.client
}

func (s *Service) loadUser(ctx context.Context, client *api.Client) error {
	user, err := client.Get(ctx, "/me", nil)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.user = user
	s.mu.Unlock()
	return nil
}

func (s *Service) enterSignedIn() {
	s.checkVersion()
	s.mu.Lock()
	s.phase, s.reason, s.device = PhaseSignedIn, "", nil
	s.mu.Unlock()
	s.startNotices()
	s.emitStatus()
}

// checkVersion compares the app with verifyHealth (cliente-local.md section 6).
func (s *Service) checkVersion() {
	_, client := s.current()
	if client == nil {
		return
	}
	var health api.Health
	if err := client.GetPublic(s.rootCtx(), "/health", &health); err != nil {
		s.log.Warn("health check failed", "error", err)
		return
	}
	s.mu.Lock()
	s.serverVersion = health.Version
	s.mu.Unlock()
}

// Status returns the current state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() Status {
	status := Status{
		Phase: s.phase, Reason: s.reason, Server: s.server, ServerFromEnv: s.serverFromEnv,
		Locale: s.prefs.Locale, MuteSound: s.prefs.MuteSound, AppVersion: s.deps.Version,
		ServerVersion: s.serverVersion, Device: s.device,
	}
	// No warning when either side is a development build ("dev").
	status.VersionMismatch = s.serverVersion != "" && s.serverVersion != "dev" && s.deps.Version != "dev" &&
		s.serverVersion != s.deps.Version
	if s.phase == PhaseSignedIn {
		status.User = s.user
	}
	return status
}

func (s *Service) emitStatus() { s.emit(EventSession, s.Status()) }

func (s *Service) emit(name string, data any) {
	if s.platform != nil {
		s.platform.Emit(name, data)
	}
}

func (s *Service) setSignedOut(reason string) {
	s.mu.Lock()
	s.stopStreamsLocked()
	s.phase, s.reason, s.user, s.device = PhaseSignedOut, reason, nil, nil
	s.projectNames = map[string]string{}
	s.followed = map[string]bool{}
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Service) stopStreamsLocked() {
	if s.noticesCancel != nil {
		s.noticesCancel()
		s.noticesCancel = nil
	}
	if s.wave != nil {
		s.wave.cancel()
		s.wave = nil
	}
}

// handleSessionError signs out when err means the session is over and returns the error for the view.
func (s *Service) handleSessionError(err error) error {
	switch {
	case errors.Is(err, session.ErrRevoked), errors.Is(err, api.ErrSessionEnded), errors.Is(err, sse.ErrUnauthorized):
		s.log.Info("session ended by the server")
		s.clearSession()
		s.setSignedOut(ReasonSessionRevoked)
	case errors.Is(err, session.ErrSignedOut):
		s.setSignedOut("")
	case errors.Is(err, keychain.ErrUnavailable):
		s.setSignedOut(ReasonKeychainUnavailable)
	default:
		s.mu.Lock()
		starting := s.phase == PhaseStarting
		s.mu.Unlock()
		if starting {
			// Server unreachable at start-up: keep the stored token and let the user try again.
			s.setSignedOut(ReasonSignInFailed)
		}
	}
	return toUIError(err)
}

// clearSession forgets the tokens and clears the keychain after the server ended the session.
func (s *Service) clearSession() {
	if tokens, _ := s.current(); tokens != nil {
		if err := tokens.Clear(); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			s.log.Warn("could not clear the keychain", "error", err)
		}
	}
}

// SetServer stores the server address typed on the first sign-in (cliente-local.md section 1).
func (s *Service) SetServer(address string) (Status, error) {
	normalized, err := config.NormalizeServer(address)
	if err != nil {
		return s.Status(), toUIError(err)
	}
	s.mu.Lock()
	if s.serverFromEnv || s.phase == PhaseSignedIn || s.phase == PhaseAwaitingApproval {
		s.mu.Unlock()
		return s.Status(), &UIError{Kind: KindNotAllowed}
	}
	s.prefs.Server = normalized
	prefs := s.prefs
	s.useServerLocked(normalized)
	s.mu.Unlock()
	if err := s.deps.Prefs.Save(prefs); err != nil {
		s.log.Warn("could not save preferences", "error", err)
	}
	s.emitStatus()
	return s.Status(), nil
}

// SignIn starts the device flow: it shows the code, opens the browser and waits for the approval
// (US-45 CA-1). The result arrives as EventSession.
func (s *Service) SignIn() (Status, error) {
	ctx := s.rootCtx()
	tokens, _ := s.current()
	if tokens == nil {
		return s.Status(), toUIError(errNoServer)
	}
	// Without a keychain there is no point in asking for the approval (ESP-28 ER-28-1).
	if _, err := tokens.HasStoredSession(); errors.Is(err, keychain.ErrUnavailable) {
		s.setSignedOut(ReasonKeychainUnavailable)
		return s.Status(), toUIError(err)
	}
	identity, err := s.identityClient(ctx)
	if err != nil {
		return s.Status(), toUIError(err)
	}
	auth, err := identity.Start(ctx)
	if err != nil {
		return s.Status(), toUIError(err)
	}
	pollCtx, cancel := context.WithDeadline(ctx, auth.ExpiresAt.Add(5*time.Second))
	s.mu.Lock()
	if s.signInCancel != nil {
		s.signInCancel()
	}
	s.signInCancel = cancel
	s.phase, s.reason = PhaseAwaitingApproval, ""
	s.device = &DeviceView{
		UserCode: auth.UserCode, VerificationURI: auth.VerificationURI,
		VerificationURIComplete: auth.VerificationURIComplete, ExpiresAt: auth.ExpiresAt,
	}
	s.mu.Unlock()
	s.emitStatus()
	_ = s.OpenVerificationPage()
	go s.completeSignIn(pollCtx, cancel, identity, tokens, auth)
	return s.Status(), nil
}

func (s *Service) completeSignIn(ctx context.Context, cancel context.CancelFunc, identity *oidc.Client, tokens *session.Tokens, auth oidc.DeviceAuthorization) {
	defer cancel()
	result, err := identity.Poll(ctx, auth)
	s.mu.Lock()
	stillWaiting := s.phase == PhaseAwaitingApproval && s.tokens == tokens
	s.mu.Unlock()
	if !stillWaiting {
		return
	}
	switch {
	case errors.Is(err, oidc.ErrExpired), errors.Is(err, context.DeadlineExceeded):
		s.setSignedOut(ReasonCodeExpired)
		return
	case errors.Is(err, oidc.ErrDenied):
		s.setSignedOut(ReasonDenied)
		return
	case errors.Is(err, context.Canceled):
		return
	case err != nil:
		s.log.Warn("device flow failed", "error", err)
		s.setSignedOut(ReasonSignInFailed)
		return
	}
	if err := tokens.Establish(result); err != nil {
		s.log.Warn("could not store the session", "error", err)
		reason := ReasonSignInFailed
		if errors.Is(err, keychain.ErrUnavailable) {
			reason = ReasonKeychainUnavailable
		}
		s.setSignedOut(reason)
		return
	}
	_, client := s.current()
	if err := s.loadUser(s.rootCtx(), client); err != nil {
		s.log.Warn("signed in but getCurrentUser failed", "error", err)
		_ = tokens.Clear()
		s.setSignedOut(ReasonSignInFailed)
		return
	}
	s.enterSignedIn()
}

// CancelSignIn abandons a pending device flow.
func (s *Service) CancelSignIn() Status {
	s.mu.Lock()
	if s.signInCancel != nil {
		s.signInCancel()
		s.signInCancel = nil
	}
	waiting := s.phase == PhaseAwaitingApproval
	s.mu.Unlock()
	if waiting {
		s.setSignedOut("")
	}
	return s.Status()
}

// OpenVerificationPage opens the Keycloak page where the user approves the code.
func (s *Service) OpenVerificationPage() error {
	s.mu.Lock()
	device := s.device
	s.mu.Unlock()
	if device == nil || s.platform == nil {
		return nil
	}
	address := device.VerificationURIComplete
	if address == "" {
		address = device.VerificationURI
	}
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && config.IsLoopback(parsed.Hostname()))) {
		return &UIError{Kind: KindInvalidTarget}
	}
	return s.platform.OpenURL(address)
}

// SignOut ends the session on the server (endSession revokes the Keycloak session) and clears the keychain.
func (s *Service) SignOut() (Status, error) {
	ctx, cancel := context.WithTimeout(s.rootCtx(), 10*time.Second)
	defer cancel()
	tokens, client := s.current()
	s.mu.Lock()
	s.stopStreamsLocked()
	s.mu.Unlock()
	if tokens != nil {
		if err := client.EndSession(ctx); err != nil {
			s.log.Warn("endSession failed; the keychain is cleared anyway", "error", err)
		}
		if refresh, err := tokens.RefreshToken(); err == nil {
			if identity, err := s.identityClient(ctx); err == nil {
				_ = identity.Revoke(ctx, refresh)
			}
		}
		if err := tokens.Clear(); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			s.log.Warn("could not clear the keychain", "error", err)
		}
	}
	s.setSignedOut("")
	return s.Status(), nil
}

// Projects is listProjects for the dashboard (US-46 CA-1: only projects where I have a role).
func (s *Service) Projects() (json.RawMessage, error) {
	_, client := s.current()
	if client == nil {
		return nil, toUIError(errNoServer)
	}
	body, err := client.Get(s.rootCtx(), "/projects", url.Values{"limit": {"100"}})
	if err != nil {
		return nil, s.handleSessionError(err)
	}
	var page api.ProjectsPage
	if json.Unmarshal(body, &page) == nil {
		s.mu.Lock()
		for _, p := range page.Items {
			s.projectNames[p.ID] = p.Name
		}
		s.mu.Unlock()
	}
	return body, nil
}

// PendingItems is listPendingItems (read-only; each item opens the web).
func (s *Service) PendingItems() (json.RawMessage, error) {
	return s.get("/pending-items", url.Values{"limit": {"50"}})
}

// Project reads the project, its workflow and its waves.
func (s *Service) Project(projectID string) (ProjectView, error) {
	if err := checkID(projectID); err != nil {
		return ProjectView{}, err
	}
	base := "/projects/" + url.PathEscape(projectID)
	project, err := s.get(base, nil)
	if err != nil {
		return ProjectView{}, err
	}
	s.rememberName(project)
	workflow, err := s.get(base+"/workflow", nil)
	if err != nil {
		return ProjectView{}, err
	}
	waves, err := s.get(base+"/waves", url.Values{"limit": {"50"}})
	if err != nil {
		return ProjectView{}, err
	}
	return ProjectView{Project: project, Workflow: workflow, Waves: waves}, nil
}

// GateRound is getGateRound (summary only, the decision is on the web).
func (s *Service) GateRound(projectID, gateID string, round int) (json.RawMessage, error) {
	if err := checkID(projectID); err != nil {
		return nil, err
	}
	if err := checkID(gateID); err != nil || round < 1 {
		return nil, &UIError{Kind: KindInvalidTarget}
	}
	return s.get(fmt.Sprintf("/projects/%s/gates/%s/rounds/%d", url.PathEscape(projectID), url.PathEscape(gateID), round), nil)
}

// WaveSnapshot reads the wave by REST (used again after a gap or a reconnection, US-46 CA-5).
func (s *Service) WaveSnapshot(projectID, waveID string) (WaveView, error) {
	if err := checkID(projectID); err != nil {
		return WaveView{}, err
	}
	if err := checkID(waveID); err != nil {
		return WaveView{}, err
	}
	base := "/projects/" + url.PathEscape(projectID)
	project, err := s.get(base, nil)
	if err != nil {
		return WaveView{}, err
	}
	s.rememberName(project)
	wave, err := s.get(base+"/waves/"+url.PathEscape(waveID), nil)
	if err != nil {
		return WaveView{}, err
	}
	calls, err := s.get(base+"/waves/"+url.PathEscape(waveID)+"/calls", url.Values{"limit": {"100"}})
	if err != nil {
		return WaveView{}, err
	}
	return WaveView{Project: project, Wave: wave, Calls: calls}, nil
}

// OpenWave reads the wave and opens watchProjectEvents filtered by the wave from the last_sequence read
// (sse.md section 5.1), so nothing between the read and the stream is lost.
func (s *Service) OpenWave(projectID, waveID string) (WaveView, error) {
	view, err := s.WaveSnapshot(projectID, waveID)
	if err != nil {
		return WaveView{}, err
	}
	var ref api.ProjectRef
	_ = json.Unmarshal(view.Project, &ref)
	since := ""
	if ref.LastSequence != nil {
		since = strconv.FormatInt(*ref.LastSequence, 10)
	}
	s.startWave(projectID, waveID, since)
	return view, nil
}

// CloseWave stops the stream of the wave view.
func (s *Service) CloseWave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wave != nil {
		s.wave.cancel()
		s.wave = nil
	}
}

// Follow asks for a notification when the wave ends even with its view closed (cliente-local.md section 4).
// Kept in memory only: the app keeps no wave state (US-46 RN-1).
func (s *Service) Follow(projectID, waveID string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := projectID + "/" + waveID
	if on {
		s.followed[key] = true
	} else {
		delete(s.followed, key)
	}
}

// Following says whether the user follows the wave.
func (s *Service) Following(projectID, waveID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.followed[projectID+"/"+waveID]
}

// OpenWeb opens the web screen of a target in the default browser (ESP-28 ER-28-5).
func (s *Service) OpenWeb(target weblink.Target) error {
	s.mu.Lock()
	server := s.server
	s.mu.Unlock()
	if server == "" {
		return toUIError(errNoServer)
	}
	address, err := weblink.URL(server, target)
	if err != nil {
		return toUIError(err)
	}
	if s.platform == nil {
		return nil
	}
	return s.platform.OpenURL(address)
}

// OpenPendingItem opens the web screen where a pending item is decided or answered.
func (s *Service) OpenPendingItem(itemType, projectID, targetType, targetID string) error {
	return s.OpenWeb(weblink.ForPendingItem(itemType, projectID, targetType, targetID))
}

// SetLocale changes the interface and notification language.
func (s *Service) SetLocale(locale string) Status {
	s.mu.Lock()
	s.prefs.Locale = config.NormalizeLocale(locale)
	prefs := s.prefs
	s.mu.Unlock()
	if err := s.deps.Prefs.Save(prefs); err != nil {
		s.log.Warn("could not save preferences", "error", err)
	}
	return s.Status()
}

// SetMuteSound turns the notification sound off or on (ESP-28 section 4).
func (s *Service) SetMuteSound(mute bool) Status {
	s.mu.Lock()
	s.prefs.MuteSound = mute
	prefs := s.prefs
	s.mu.Unlock()
	if err := s.deps.Prefs.Save(prefs); err != nil {
		s.log.Warn("could not save preferences", "error", err)
	}
	return s.Status()
}

// NotificationClicked is called by main when the user clicks a native notification.
func (h *Host) NotificationClicked(id string) {
	s := h.s
	s.mu.Lock()
	n, ok := s.notifications[id]
	s.mu.Unlock()
	if !ok {
		return
	}
	if n.OpenInApp {
		if s.platform != nil {
			s.platform.Show()
		}
		s.emit(EventNavigate, n.Target)
		return
	}
	if err := s.OpenWeb(n.Target); err != nil {
		s.log.Warn("could not open the web page of a notification", "error", err)
	}
}

func (s *Service) get(path string, query url.Values) (json.RawMessage, error) {
	_, client := s.current()
	if client == nil {
		return nil, toUIError(errNoServer)
	}
	body, err := client.Get(s.rootCtx(), path, query)
	if err != nil {
		return nil, s.handleSessionError(err)
	}
	return body, nil
}

func (s *Service) rememberName(project json.RawMessage) {
	var ref api.ProjectRef
	if json.Unmarshal(project, &ref) == nil && ref.ID != "" {
		s.mu.Lock()
		s.projectNames[ref.ID] = ref.Name
		s.mu.Unlock()
	}
}

func (s *Service) projectName(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectNames[id]
}

func checkID(id string) error {
	if _, err := weblink.URL("http://localhost", weblink.Target{Kind: weblink.KindProject, ProjectID: id}); err != nil {
		return &UIError{Kind: KindInvalidTarget}
	}
	return nil
}
