package websession_test

import (
	"strings"
	"testing"
	"time"

	"easycode/internal/domain/identity"
	"easycode/internal/infrastructure/websession"
)

const secret = "0123456789abcdef0123456789abcdef"

func TestSealOpenRoundTrip(t *testing.T) {
	codec := websession.NewCodec(secret)
	now := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	want := identity.Session{Subject: "dev", OrganizationID: "0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30", AuthenticatedAt: now, IssuedAt: now, LastSeenAt: now, CSRFToken: "abc"}
	token, err := codec.Seal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Open(token)
	if err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

func TestOpenRefusesTamperedOrForeignCookies(t *testing.T) {
	codec := websession.NewCodec(secret)
	token, _ := codec.Seal(identity.Session{Subject: "dev"})
	payload, signature, _ := strings.Cut(token, ".")
	forged, _ := websession.NewCodec(strings.Repeat("x", 32)).Seal(identity.Session{Subject: "admin"})
	for name, candidate := range map[string]string{
		"payload changed": payload + "A." + signature,
		"other secret":    forged,
		"no signature":    payload,
		"garbage":         "garbage.garbage",
	} {
		if _, err := codec.Open(candidate); err == nil {
			t.Errorf("%s: expected refusal", name)
		}
	}
}
