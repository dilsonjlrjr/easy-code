package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	appgateway "easycode/internal/application/gateway"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/permission"
	"easycode/internal/domain/provider"
)

// Forwarder is the part of the easycode-gateway client the model proxy needs (ADR-23): a session per model
// of the pair and the raw forward of one Messages API request, answered unchanged (status, headers, body
// streamed). Implemented by *gatewayapi.Client.
type Forwarder interface {
	OpenSession(ctx context.Context, scope appgateway.Scope, sealedKey []byte) (string, error)
	CloseSession(ctx context.Context, id string) error
	Forward(ctx context.Context, sessionID, path, rawQuery string, header http.Header, body []byte) (*http.Response, error)
}

const (
	maxProxyRequestBytes = 32 << 20 // a Claude Code request carries the whole conversation
	maxProxyJSONBytes    = 8 << 20  // a non streamed answer read for its usage
	maxToolInputBytes    = 64 << 10 // input of one tool_use kept to find its path for the summary
	proxyEventBuffer     = 256
	proxySocketName      = "model.sock"
	messagesPath         = "/v1/messages"
	countTokensPath      = "/v1/messages/count_tokens" //nolint:gosec // G101: an API path, not a credential
	heldMessage          = "easycode: dispatch held (budget or project paused)"
	closeTimeout         = 5 * time.Second                 // closing a gateway session never blocks the end of a run
	deniedPathMarker     = "is not allowed for this agent" // message of the PreToolUse hook of agent-bridge
)

// safeSocketDir keeps only the characters the runner accepts in the socket directory of a wave.
var safeSocketDir = regexp.MustCompile(`[^A-Za-z0-9-]`)

// ProxyEvent is one step the model proxy saw, in order: a finished model call or a tool the agent ran (a
// tool_use of an answer closed by the tool_result of the next request).
type ProxyEvent struct {
	Call *CallRecord
	Tool *ToolRecord
}

// ModelProxy is the model proxy of the worker (ADR-23): one Unix socket per Claude Code run, in the volume
// shared with the sandbox runner, that checks the run token, opens the model_calls row before each call
// (price in force; a held dispatch refuses the call), forwards the request to easycode-gateway, which holds
// the credential, and reads the usage of the answer while it streams back. It never logs a body or a token.
type ModelProxy struct {
	dir     string
	gateway Forwarder
	keys    KeyVault
	prices  PriceLookup
	calls   CallLog
	now     func() time.Time
}

// NewModelProxy builds the proxy over the volume directory dir.
func NewModelProxy(dir string, gateway Forwarder, keys KeyVault, prices PriceLookup, calls CallLog) *ModelProxy {
	return &ModelProxy{dir: dir, gateway: gateway, keys: keys, prices: prices, calls: calls, now: time.Now}
}

// ProxySession is the socket of one run: its directory name in the volume, the token the run presents and
// the events it produced.
type ProxySession struct {
	SocketDir string
	Token     string

	proxy    *ModelProxy
	wave     WaveContext
	path     string
	listener net.Listener
	server   *http.Server
	events   chan ProxyEvent
	closed   chan struct{}
	signal   chan struct{} // closed once when the run must stop: dispatch held or a fatal error
	once     sync.Once
	stopOnce sync.Once

	mu      sync.Mutex // serializes the messages requests: steps stay ordered
	step    int        // checkpoint number of the last event
	pending map[string]pendingTool

	sessionMu sync.Mutex
	sessions  map[string]string // model → gateway session

	stateMu sync.Mutex
	held    bool
	fatal   error
}

type pendingTool struct {
	name    string
	summary string
}

// Open starts the socket of a run of the wave; base is the checkpoint the run starts from (the proxy
// numbers the calls it opens after it).
func (p *ModelProxy) Open(wave WaveContext, run, base int) (*ProxySession, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	name := safeSocketDir.ReplaceAllString(fmt.Sprintf("%s-%d-%d", wave.WaveID, wave.Attempt, run), "")
	dir := filepath.Join(p.dir, name)
	_ = os.RemoveAll(dir)                           // left by a worker that fell during a run
	if err := os.MkdirAll(dir, 0o777); err != nil { //nolint:gosec // G301: the sandbox user (65532) must reach the socket
		return nil, fmt.Errorf("model proxy directory: %w", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil { //nolint:gosec // G302: the sandbox user is not the worker user
		return nil, fmt.Errorf("model proxy directory: %w", err)
	}
	socket := filepath.Join(dir, proxySocketName)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("model proxy socket: %w", err)
	}
	if err := os.Chmod(socket, 0o666); err != nil { //nolint:gosec // G302: the run token, not the mode, guards the socket
		_ = listener.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("model proxy socket: %w", err)
	}
	s := &ProxySession{SocketDir: name, Token: token, proxy: p, wave: wave, path: dir, listener: listener,
		events: make(chan ProxyEvent, proxyEventBuffer), closed: make(chan struct{}), signal: make(chan struct{}),
		step: base, pending: map[string]pendingTool{}, sessions: map[string]string{}}
	s.server = &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = s.server.Serve(listener) }()
	return s, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ecs-" + hex.EncodeToString(b), nil
}

// Events are the steps of the run, in order.
func (s *ProxySession) Events() <-chan ProxyEvent { return s.events }

// Signal is closed when the run must stop (Stopped tells why).
func (s *ProxySession) Signal() <-chan struct{} { return s.signal }

// Stopped reports why the run must stop: ErrDispatchHeld, a provider authentication error or the failure
// that prevented a call (no price, no key); nil while it may go on.
func (s *ProxySession) Stopped() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.held {
		return ErrDispatchHeld
	}
	return s.fatal
}

func (s *ProxySession) stop(held bool, err error) {
	s.stateMu.Lock()
	if held {
		s.held = true
	} else if s.fatal == nil {
		s.fatal = err
	}
	s.stateMu.Unlock()
	s.stopOnce.Do(func() { close(s.signal) })
}

// shutdown waits for the requests in flight to end (the run exited), so their events are queued.
func (s *ProxySession) shutdown(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

// Close stops the socket, aborts the requests in flight, ends the gateway sessions and removes the directory.
func (s *ProxySession) Close() {
	s.once.Do(func() {
		close(s.closed)
		_ = s.server.Close()
		s.sessionMu.Lock()
		ids := make([]string, 0, len(s.sessions))
		for _, id := range s.sessions {
			ids = append(ids, id)
		}
		s.sessions = map[string]string{}
		s.sessionMu.Unlock()
		for _, id := range ids {
			closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
			_ = s.proxy.gateway.CloseSession(closeCtx, id)
			cancel()
		}
		_ = os.RemoveAll(s.path)
	})
}

// emit queues an event; the caller holds s.mu, so the numbers follow the order of the events.
func (s *ProxySession) emit(event ProxyEvent) {
	s.step++
	select {
	case s.events <- event:
	case <-s.closed:
	}
}

// anthropicError writes an error in the format of the Anthropic API, which Claude Code understands.
func anthropicError(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoded, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": kind, "message": message}})
	_, _ = w.Write(encoded)
}

func (s *ProxySession) authorized(r *http.Request) bool {
	got := r.Header.Get("X-Api-Key")
	if got == "" {
		got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func (s *ProxySession) serve(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		anthropicError(w, http.StatusUnauthorized, "authentication_error", "easycode: invalid session token")
		return
	}
	if r.Method != http.MethodPost || (r.URL.Path != messagesPath && r.URL.Path != countTokensPath) {
		anthropicError(w, http.StatusNotFound, "not_found_error", "easycode: path not allowed")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProxyRequestBytes+1))
	if err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "easycode: unreadable request")
		return
	}
	if len(body) > maxProxyRequestBytes {
		anthropicError(w, http.StatusRequestEntityTooLarge, "request_too_large", "easycode: request too large")
		return
	}
	var request proxyRequest
	if err := json.Unmarshal(body, &request); err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "easycode: request is not JSON")
		return
	}
	role, model, ok := s.modelOf(request.Model)
	if !ok {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "easycode: model not in the wave pair")
		return
	}
	if r.URL.Path == countTokensPath {
		if s.Stopped() != nil { // a held or failed run makes no more provider calls, free ones included
			anthropicError(w, http.StatusBadRequest, "invalid_request_error", heldMessage)
			return
		}
		s.countTokens(w, r, model, body) // no model_calls row: counting is free
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeTools(request)
	s.call(w, r, role, model, body)
}

// modelOf finds the model of the pair with the requested name (execution first when both roles share it).
func (s *ProxySession) modelOf(name string) (string, ModelRef, bool) {
	roles := make([]string, 0, len(s.wave.Models))
	for role := range s.wave.Models {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool {
		if (roles[i] == "execution") != (roles[j] == "execution") {
			return roles[i] == "execution"
		}
		return roles[i] < roles[j]
	})
	for _, role := range roles {
		if m := s.wave.Models[role]; name != "" && m.Name == name {
			return role, m, true
		}
	}
	return "", ModelRef{}, false
}

func (s *ProxySession) session(ctx context.Context, model ModelRef) (string, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if id, ok := s.sessions[model.Name]; ok {
		return id, nil
	}
	sealed, err := s.proxy.keys.ActiveSealedKey(ctx, s.wave.ProjectID, model.Provider)
	if err != nil {
		return "", err
	}
	id, err := s.proxy.gateway.OpenSession(ctx, appgateway.Scope{ProjectID: s.wave.ProjectID, WaveID: s.wave.WaveID, Provider: model.Provider, Model: model.Name}, sealed)
	if err != nil {
		return "", err
	}
	s.sessions[model.Name] = id
	return id, nil
}

func (s *ProxySession) forget(model ModelRef) {
	s.sessionMu.Lock()
	id, ok := s.sessions[model.Name]
	delete(s.sessions, model.Name)
	s.sessionMu.Unlock()
	if ok {
		closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		_ = s.proxy.gateway.CloseSession(closeCtx, id)
		cancel()
	}
}

func (s *ProxySession) countTokens(w http.ResponseWriter, r *http.Request, model ModelRef, body []byte) {
	session, err := s.session(r.Context(), model)
	if err != nil {
		anthropicError(w, http.StatusServiceUnavailable, "api_error", "easycode: model session unavailable")
		return
	}
	resp, err := s.proxy.gateway.Forward(r.Context(), session, strings.TrimPrefix(countTokensPath, "/"), r.URL.RawQuery, r.Header, body)
	if err != nil {
		anthropicError(w, http.StatusServiceUnavailable, "api_error", "easycode: gateway unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// call is one model call: row opened before the forward, usage read from the answer, event after it.
func (s *ProxySession) call(w http.ResponseWriter, r *http.Request, role string, model ModelRef, body []byte) {
	ctx := r.Context()
	started := s.proxy.now()
	priceID, err := s.proxy.prices.CurrentPriceID(ctx, s.wave.ProjectID, model.ID, started)
	if err != nil {
		s.stop(false, err) // model-without-current-price: no call
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "easycode: model call not allowed")
		return
	}
	session, err := s.session(ctx, model)
	if err != nil {
		s.stop(false, err) // provider-without-key: no call (US-15 CA-4)
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "easycode: model call not allowed")
		return
	}
	sum := sha256.Sum256(body)
	id, err := s.proxy.calls.OpenCall(ctx, s.wave.WaveID, s.wave.Attempt, s.step+1, model.ID, priceID, role, hex.EncodeToString(sum[:]), (len(body)+3)/4)
	if errors.Is(err, ErrDispatchHeld) {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", heldMessage)
		s.stop(true, nil) // RNF-16: no call after the budget is reached
		return
	}
	if err != nil {
		anthropicError(w, http.StatusServiceUnavailable, "api_error", "easycode: call not recorded")
		return
	}
	record := CallRecord{ID: id, Role: role, Provider: model.Provider, Model: model.Name}
	answer := newAnswerReader()
	resp, err := s.proxy.gateway.Forward(ctx, session, strings.TrimPrefix(messagesPath, "/"), r.URL.RawQuery, r.Header, body)
	if err != nil {
		record.Result, record.Error = "error", errorClass(err)
		var providerErr *provider.Error
		switch {
		case errors.As(err, &providerErr) && providerErr.Class == provider.ErrorAborted:
			record.Result = "aborted"
			anthropicError(w, http.StatusServiceUnavailable, "api_error", "easycode: call aborted")
		case errors.As(err, &providerErr) && providerErr.Class == provider.ErrorAuthentication:
			s.forget(model)
			s.stop(false, &provider.Error{Class: provider.ErrorAuthentication, Message: "provider refused the credential"})
			anthropicError(w, http.StatusUnauthorized, "authentication_error", "easycode: provider refused the credential")
		default:
			s.forget(model) // a gateway that restarted lost the session: the retry of Claude Code opens another
			anthropicError(w, http.StatusServiceUnavailable, "api_error", "easycode: gateway unavailable")
		}
		record.Duration = s.proxy.now().Sub(started).Milliseconds()
		s.emit(ProxyEvent{Call: &record})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	streamed := strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, 32<<10)
	var copyErr error
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if streamed {
				answer.feedStream(buffer[:n])
			} else {
				answer.feedJSON(buffer[:n])
			}
			if _, err := w.Write(buffer[:n]); err != nil {
				copyErr = err
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}
	if !streamed {
		answer.endJSON()
	}
	record.Usage, record.Duration = answer.usage, s.proxy.now().Sub(started).Milliseconds()
	switch {
	case resp.StatusCode != http.StatusOK:
		record.Result, record.Error = "error", string(statusClass(resp.StatusCode))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			s.forget(model)
			s.stop(false, &provider.Error{Class: provider.ErrorAuthentication, Message: "provider refused the credential"})
		}
	case copyErr != nil || ctx.Err() != nil || (streamed && !answer.stopped):
		record.Result, record.Error = "aborted", string(provider.ErrorAborted)
	case answer.failed:
		record.Result, record.Error = "error", string(provider.ErrorTransient)
	default:
		record.Result = "success"
	}
	record.Summary = answer.summary()
	if record.Result == "success" {
		for _, tool := range answer.tools {
			if platformTool(tool.name) != ToolFinish { // the structured result is the end of the run, not a step
				s.pending[tool.id] = pendingTool{name: tool.name, summary: tool.summary}
			}
		}
	}
	s.emit(ProxyEvent{Call: &record})
}

func errorClass(err error) string {
	var providerErr *provider.Error
	if errors.As(err, &providerErr) {
		return string(providerErr.Class)
	}
	return failure.From(err).Code
}

// statusClass classifies an upstream error status (model_calls.error_summary).
func statusClass(status int) provider.ErrorClass {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return provider.ErrorAuthentication
	case status == http.StatusTooManyRequests:
		return provider.ErrorRateLimit
	case status == http.StatusRequestEntityTooLarge:
		return provider.ErrorContextExceeded
	case status >= http.StatusInternalServerError || status == http.StatusRequestTimeout:
		return provider.ErrorTransient
	}
	return provider.ErrorRequest
}

var hopHeaders = map[string]bool{"Connection": true, "Keep-Alive": true, "Transfer-Encoding": true, "Upgrade": true, "Content-Length": true}

func copyHeader(dst, src http.Header) {
	for name, values := range src {
		if hopHeaders[http.CanonicalHeaderKey(name)] {
			continue
		}
		for _, v := range values {
			dst.Add(name, v)
		}
	}
}

// proxyRequest is what the proxy reads of a Messages API request: the model and the tool results.
type proxyRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// closeTools emits a tool event for every tool_use of a previous answer the request answers with its
// tool_result; a refusal of the hook or of the tool list is a refused tool (US-30).
func (s *ProxySession) closeTools(request proxyRequest) {
	if len(s.pending) == 0 {
		return
	}
	for _, message := range request.Messages {
		if message.Role != "user" || len(message.Content) == 0 || message.Content[0] != '[' {
			continue
		}
		var blocks []contentBlock
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, block := range blocks {
			tool, ok := s.pending[block.ToolUseID]
			if block.Type != "tool_result" || !ok {
				continue
			}
			delete(s.pending, block.ToolUseID)
			s.emit(ProxyEvent{Tool: toolRecord(tool, block)})
		}
	}
}

// platformTools maps the Claude Code tools to the tools of the platform (permissoes.yaml).
var platformTools = map[string]string{
	"Read": ToolReadFile, "Glob": ToolListFiles, "Grep": ToolListFiles, "LS": ToolListFiles,
	"Write": ToolWriteFile, "Edit": ToolWriteFile, "MultiEdit": ToolWriteFile, "NotebookEdit": ToolWriteFile,
	"Bash": ToolExecuteCommand, "StructuredOutput": ToolFinish,
}

func platformTool(name string) string {
	if tool, ok := platformTools[name]; ok {
		return tool
	}
	tool := strings.ToLower(safeToolName.ReplaceAllString(name, "_"))
	if tool == "" || tool[0] < 'a' || tool[0] > 'z' {
		tool = "tool_" + tool
	}
	return tool
}

var safeToolName = regexp.MustCompile(`[^A-Za-z0-9_]`)

func toolRecord(tool pendingTool, block contentBlock) *ToolRecord {
	record := &ToolRecord{Tool: platformTool(tool.name), Allowed: true, Success: !block.IsError, Summary: tool.summary}
	if !block.IsError {
		return record
	}
	text := resultText(block.Content)
	switch {
	case strings.Contains(text, deniedPathMarker):
		record.Allowed, record.Success, record.RefusalReason = false, false, permission.RefusedPath
		record.Summary = "escrita negada fora das pastas permitidas"
	case strings.Contains(text, "requested permissions") || strings.Contains(text, "haven't granted") || strings.Contains(text, "No such tool available"):
		reason := permission.RefusedTool
		if record.Tool == ToolExecuteCommand {
			reason = permission.RefusedCommand
		}
		record.Allowed, record.Success, record.RefusalReason = false, false, reason
		record.Summary = "ferramenta ou comando fora da lista do agente"
	}
	return record
}

// resultText is the text of a tool_result (a string or text blocks), bounded; never logged.
func resultText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return clipText(text, 4096)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &blocks)
	var out strings.Builder
	for _, b := range blocks {
		if out.Len() > 4096 {
			break
		}
		out.WriteString(b.Text)
	}
	return clipText(out.String(), 4096)
}

// answerReader reads the usage and the tool calls of an answer while it is copied to Claude Code.
type answerReader struct {
	usage   provider.Usage
	tools   []toolUse
	blocks  map[int]*toolUse
	line    []byte
	raw     []byte
	stopped bool // message_stop seen (stream ended normally)
	failed  bool // error event in the stream
}

type toolUse struct {
	id      string
	name    string
	input   []byte
	summary string
}

func newAnswerReader() *answerReader { return &answerReader{blocks: map[int]*toolUse{}} }

type usageFields struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
}

func (a *answerReader) apply(u usageFields) {
	set := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	set(&a.usage.Input, u.InputTokens)
	set(&a.usage.Output, u.OutputTokens)
	set(&a.usage.CacheRead, u.CacheReadInputTokens)
	set(&a.usage.CacheWrite, u.CacheCreationInputTokens)
}

// feedStream reads the server-sent events of a streamed answer, line by line.
func (a *answerReader) feedStream(chunk []byte) {
	a.line = append(a.line, chunk...)
	for {
		i := bytes.IndexByte(a.line, '\n')
		if i < 0 {
			if len(a.line) > maxProxyJSONBytes {
				a.line = a.line[:0] // an oversized line is not an event the proxy needs
			}
			return
		}
		line := bytes.TrimRight(a.line[:i], "\r")
		a.line = a.line[i+1:]
		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			a.event(bytes.TrimSpace(data))
		}
	}
}

type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage usageFields `json:"usage"`
	} `json:"message"`
	Usage        usageFields `json:"usage"`
	ContentBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
}

func (a *answerReader) event(data []byte) {
	var ev streamEvent
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		a.apply(ev.Message.Usage)
	case "message_delta":
		a.apply(ev.Usage)
	case "message_stop":
		a.stopped = true
	case "error":
		a.failed = true
	case "content_block_start":
		if ev.ContentBlock.Type == "tool_use" {
			a.blocks[ev.Index] = &toolUse{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
		}
	case "content_block_delta":
		if block, ok := a.blocks[ev.Index]; ok && ev.Delta.Type == "input_json_delta" && len(block.input) < maxToolInputBytes {
			block.input = append(block.input, ev.Delta.PartialJSON...)
		}
	case "content_block_stop":
		if block, ok := a.blocks[ev.Index]; ok {
			delete(a.blocks, ev.Index)
			block.summary = toolSummary(block.name, block.input)
			block.input = nil
			a.tools = append(a.tools, *block)
		}
	}
}

func (a *answerReader) feedJSON(chunk []byte) {
	if len(a.raw)+len(chunk) <= maxProxyJSONBytes {
		a.raw = append(a.raw, chunk...)
	}
}

func (a *answerReader) endJSON() {
	var answer struct {
		Usage   usageFields `json:"usage"`
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if json.Unmarshal(a.raw, &answer) != nil {
		a.raw = nil
		return
	}
	a.raw = nil
	a.apply(answer.Usage)
	for _, block := range answer.Content {
		if block.Type == "tool_use" {
			a.tools = append(a.tools, toolUse{id: block.ID, name: block.Name, summary: toolSummary(block.Name, block.Input)})
		}
	}
}

func (a *answerReader) summary() string {
	if len(a.tools) == 0 {
		return "respondeu sem usar ferramenta"
	}
	names := make([]string, 0, len(a.tools))
	for _, tool := range a.tools {
		names = append(names, platformTool(tool.name))
	}
	return "pediu " + strings.Join(names, ", ")
}

// toolSummary is the short summary of a tool call (US-09 RN-1: no arguments, only the file name or the
// command name).
func toolSummary(name string, input []byte) string {
	var args struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
		Pattern      string `json:"pattern"`
		Command      string `json:"command"`
	}
	_ = json.Unmarshal(input, &args)
	file := args.FilePath
	if file == "" {
		file = args.NotebookPath
	}
	switch platformTool(name) {
	case ToolReadFile:
		return "leu " + orUnknown(baseName(file))
	case ToolListFiles:
		return "listou arquivos"
	case ToolWriteFile:
		return "gravou " + orUnknown(baseName(file))
	case ToolExecuteCommand:
		fields := strings.Fields(args.Command)
		if len(fields) == 0 {
			return "rodou comando"
		}
		return "rodou " + clipText(baseName(fields[0]), 40)
	case ToolFinish:
		return "entregou o resultado"
	}
	return "usou " + clipText(name, 40)
}

func orUnknown(text string) string {
	if text == "" {
		return "arquivo"
	}
	return clipText(text, 80)
}
