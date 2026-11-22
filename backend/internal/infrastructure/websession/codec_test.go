package websession_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"easycode/internal/application/session"
	"easycode/internal/infrastructure/websession"
)

const secret = "0123456789abcdef0123456789abcdef"

func TestFlowSealOpenRoundTrip(t *testing.T) {
	codec := websession.NewFlowCodec(secret)
	want := session.LoginFlow{State: "s", Nonce: "n", Verifier: "v", Callback: "/projects", Reauthenticate: true,
		ExpiresAt: time.Date(2026, 11, 22, 8, 10, 0, 0, time.UTC)}
	value, err := codec.Seal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Open(value)
	if err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

func TestFlowOpenRefusesTamperedOrForeignValues(t *testing.T) {
	codec := websession.NewFlowCodec(secret)
	value, _ := codec.Seal(session.LoginFlow{State: "s", Verifier: "v"})
	payload, signature, _ := strings.Cut(value, ".")
	foreign, _ := websession.NewFlowCodec(strings.Repeat("x", 32)).Seal(session.LoginFlow{State: "s", Verifier: "v"})
	for name, candidate := range map[string]string{
		"tampered payload": payload + "x." + signature,
		"other key":        foreign,
		"no signature":     payload,
		"empty":            "",
	} {
		if _, err := codec.Open(candidate); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
}

// The refresh token is never stored in clear (ADR-12, RNF-20): sealing is randomized and only
// the same key opens it.
func TestTokenSealerHidesAndRestoresTheRefreshToken(t *testing.T) {
	sealer := websession.NewTokenSealer(secret)
	plain := []byte("refresh-token-value")
	first, err := sealer.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := sealer.Seal(plain)
	if bytes.Contains(first, plain) || bytes.Equal(first, second) {
		t.Fatal("sealed value must not contain the token and must be randomized")
	}
	if got, err := sealer.Open(first); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := websession.NewTokenSealer(strings.Repeat("x", 32)).Open(first); err == nil {
		t.Fatal("another key must not open the token")
	}
	first[len(first)-1] ^= 1
	if _, err := sealer.Open(first); err == nil {
		t.Fatal("tampered value must not open")
	}
}
