// Package websession signs the web session cookie of the marco1 profile with HMAC-SHA256.
// The session lives in the cookie (no server table yet); MARCO-2 replaces it by the server-side
// session of the Keycloak login (ADR-12) without changing application/session.
package websession

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"easycode/internal/application/session"
	"easycode/internal/domain/identity"
)

// ErrInvalid marks a cookie that was not issued by this codec or was changed.
var ErrInvalid = errors.New("invalid session cookie")

// Codec implements session.Codec.
type Codec struct {
	secret []byte
}

var _ session.Codec = Codec{}

// NewCodec builds the codec. secret must be at least 32 bytes (checked by config).
func NewCodec(secret string) Codec {
	return Codec{secret: []byte(secret)}
}

type claims struct {
	Subject         string    `json:"sub"`
	OrganizationID  string    `json:"org"`
	AuthenticatedAt time.Time `json:"auth"`
	IssuedAt        time.Time `json:"iat"`
	LastSeenAt      time.Time `json:"seen"`
	CSRFToken       string    `json:"csrf"`
}

// Seal implements session.Codec.
func (c Codec) Seal(s identity.Session) (string, error) {
	payload, err := json.Marshal(claims{
		Subject: s.Subject, OrganizationID: s.OrganizationID, AuthenticatedAt: s.AuthenticatedAt,
		IssuedAt: s.IssuedAt, LastSeenAt: s.LastSeenAt, CSRFToken: s.CSRFToken,
	})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(c.mac(encoded)), nil
}

// Open implements session.Codec.
func (c Codec) Open(token string) (identity.Session, error) {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return identity.Session{}, ErrInvalid
	}
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(given, c.mac(encoded)) {
		return identity.Session{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return identity.Session{}, ErrInvalid
	}
	var decoded claims
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded.Subject == "" {
		return identity.Session{}, ErrInvalid
	}
	return identity.Session{
		Subject: decoded.Subject, OrganizationID: decoded.OrganizationID, AuthenticatedAt: decoded.AuthenticatedAt,
		IssuedAt: decoded.IssuedAt, LastSeenAt: decoded.LastSeenAt, CSRFToken: decoded.CSRFToken,
	}, nil
}

func (c Codec) mac(encoded string) []byte {
	h := hmac.New(sha256.New, c.secret)
	h.Write([]byte(encoded))
	return h.Sum(nil)
}
