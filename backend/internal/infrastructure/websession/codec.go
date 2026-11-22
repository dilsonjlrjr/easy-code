// Package websession protects what the web session keeps outside the database: the transient
// login flow cookie (ec_login, HMAC-SHA256) and the refresh token at rest (AES-256-GCM). Both keys
// derive from the session key of the api (ARQ section 8.1), each with its own label.
package websession

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"easycode/internal/application/session"
)

// ErrInvalid marks a value that was not produced with this key or was changed.
var ErrInvalid = errors.New("invalid sealed value")

func derive(secret, label string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// FlowCodec implements session.FlowCodec.
type FlowCodec struct {
	key []byte
}

var _ session.FlowCodec = FlowCodec{}

// NewFlowCodec builds the codec. secret must have at least 32 bytes (checked by config).
func NewFlowCodec(secret string) FlowCodec {
	return FlowCodec{key: derive(secret, "easycode login flow v1")}
}

type flowClaims struct {
	State          string    `json:"st"`
	Nonce          string    `json:"no"`
	Verifier       string    `json:"cv"`
	Callback       string    `json:"cb"`
	Reauthenticate bool      `json:"re,omitempty"`
	ExpiresAt      time.Time `json:"exp"`
}

// Seal implements session.FlowCodec.
func (c FlowCodec) Seal(f session.LoginFlow) (string, error) {
	payload, err := json.Marshal(flowClaims(f))
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(c.mac(encoded)), nil
}

// Open implements session.FlowCodec.
func (c FlowCodec) Open(value string) (session.LoginFlow, error) {
	encoded, signature, ok := strings.Cut(value, ".")
	if !ok {
		return session.LoginFlow{}, ErrInvalid
	}
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(given, c.mac(encoded)) {
		return session.LoginFlow{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return session.LoginFlow{}, ErrInvalid
	}
	var decoded flowClaims
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded.State == "" || decoded.Verifier == "" {
		return session.LoginFlow{}, ErrInvalid
	}
	return session.LoginFlow(decoded), nil
}

func (c FlowCodec) mac(encoded string) []byte {
	h := hmac.New(sha256.New, c.key)
	h.Write([]byte(encoded))
	return h.Sum(nil)
}

// TokenSealer implements session.Sealer with AES-256-GCM (random nonce prefixed to the output).
type TokenSealer struct {
	aead cipher.AEAD
}

var _ session.Sealer = TokenSealer{}

// NewTokenSealer builds the sealer from the session key.
func NewTokenSealer(secret string) TokenSealer {
	block, err := aes.NewCipher(derive(secret, "easycode refresh token v1"))
	if err != nil {
		panic(err) // a 32-byte key always builds a cipher
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return TokenSealer{aead: aead}
}

// Seal implements session.Sealer.
func (s TokenSealer) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open implements session.Sealer.
func (s TokenSealer) Open(sealed []byte) ([]byte, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return nil, ErrInvalid
	}
	plain, err := s.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		return nil, ErrInvalid
	}
	return plain, nil
}
