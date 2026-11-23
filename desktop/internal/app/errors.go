package app

import (
	"errors"

	"easycode-desktop/internal/api"
	"easycode-desktop/internal/config"
	"easycode-desktop/internal/keychain"
	"easycode-desktop/internal/oidc"
	"easycode-desktop/internal/session"
	"easycode-desktop/internal/weblink"
)

// Kinds of UIError; the interface maps each to a message of its catalog.
const (
	KindProblem             = "problem"
	KindSessionEnded        = "session_ended"
	KindSignedOut           = "signed_out"
	KindKeychainUnavailable = "keychain_unavailable"
	KindInvalidServer       = "invalid_server"
	KindNoServer            = "no_server"
	KindInsecureIdentity    = "insecure_identity"
	KindInvalidTarget       = "invalid_target"
	KindNotAllowed          = "not_allowed"
	KindNetwork             = "network"
)

// UIError is what a bound method returns to the interface; Wails sends it as the `cause` of the rejected
// call. The interface decides by Kind and ProblemType, never by text (ESP-28 ER-28-9), and shows Title and
// Detail of the Problem Details when present.
type UIError struct {
	Kind        string `json:"kind"`
	ProblemType string `json:"problem_type,omitempty"`
	Status      int    `json:"status,omitempty"`
	Title       string `json:"title,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

func (e *UIError) Error() string {
	if e.ProblemType != "" {
		return e.Kind + ": " + e.ProblemType
	}
	return e.Kind
}

func toUIError(err error) error {
	if err == nil {
		return nil
	}
	var ui *UIError
	if errors.As(err, &ui) {
		return ui
	}
	var problem *api.Problem
	switch {
	case errors.As(err, &problem):
		return &UIError{Kind: KindProblem, ProblemType: problem.Type, Status: problem.Status, Title: problem.Title, Detail: problem.Detail}
	case errors.Is(err, session.ErrRevoked), errors.Is(err, api.ErrSessionEnded):
		return &UIError{Kind: KindSessionEnded}
	case errors.Is(err, session.ErrSignedOut):
		return &UIError{Kind: KindSignedOut}
	case errors.Is(err, keychain.ErrUnavailable):
		return &UIError{Kind: KindKeychainUnavailable}
	case errors.Is(err, config.ErrInvalidServer):
		return &UIError{Kind: KindInvalidServer}
	case errors.Is(err, errNoServer):
		return &UIError{Kind: KindNoServer}
	case errors.Is(err, oidc.ErrInsecureEndpoint):
		return &UIError{Kind: KindInsecureIdentity}
	case errors.Is(err, weblink.ErrInvalidTarget):
		return &UIError{Kind: KindInvalidTarget}
	}
	return &UIError{Kind: KindNetwork}
}
