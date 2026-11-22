package sealing_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"easycode/internal/infrastructure/sealing"
)

func pair(t *testing.T) (sealing.Sealer, sealing.Opener) {
	t.Helper()
	dir := t.TempDir()
	private, public := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if created, err := sealing.GenerateKeyPair(private, public); err != nil || !created {
		t.Fatalf("generate: %v %v", created, err)
	}
	if created, err := sealing.GenerateKeyPair(private, public); err != nil || created {
		t.Fatalf("an existing private key must be kept: %v %v", created, err)
	}
	sealer, err := sealing.LoadSealer(public)
	if err != nil {
		t.Fatal(err)
	}
	opener, err := sealing.LoadOpener(private)
	if err != nil {
		t.Fatal(err)
	}
	return sealer, opener
}

func TestSealedKeyOpensOnlyWithThePrivateKey(t *testing.T) {
	sealer, opener := pair(t)
	plain := []byte("sk-ant-canary-0123456789")
	sealed, ref, err := sealer.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) || ref != opener.KeyRef() {
		t.Fatalf("sealed value must not contain the key (RNF-01); ref %q", ref)
	}
	secret, err := opener.Open(context.Background(), sealed)
	if err != nil || string(secret.Reveal()) != string(plain) {
		t.Fatalf("open: %v", err)
	}
	again, _, _ := sealer.Seal(plain)
	if bytes.Equal(again, sealed) {
		t.Fatal("each seal uses a fresh ephemeral key")
	}
}

func TestTamperedOrForeignEnvelopeDoesNotOpen(t *testing.T) {
	sealer, opener := pair(t)
	_, otherOpener := pair(t)
	sealed, _, _ := sealer.Seal([]byte("sk-ant-canary-0123456789"))
	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 1
	for name, candidate := range map[string][]byte{"tampered": tampered, "short": sealed[:10], "plain": []byte("sk-ant-plain")} {
		if _, err := opener.Open(context.Background(), candidate); err == nil {
			t.Errorf("%s: must not open", name)
		}
	}
	if _, err := otherOpener.Open(context.Background(), sealed); err == nil {
		t.Error("another platform key must not open it")
	}
}
