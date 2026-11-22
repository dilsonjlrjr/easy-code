// Package sealing seals provider keys with the platform public key and opens them with the
// private key (ADR-13, option B). The envelope is an ephemeral X25519 key agreement, HKDF-SHA256
// and AES-256-GCM, like a sealed box: the api seals but cannot open; only easycode-gateway,
// which holds the private key, opens. Format: version (1) | ephemeral public key (32) | nonce
// (12) | ciphertext with tag.
package sealing

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"easycode/internal/domain/provider"
)

const (
	formatVersion = 1
	info          = "easycode provider key v1"
	publicSize    = 32
	nonceSize     = 12
)

// ErrOpen is returned for any envelope that does not open (wrong key, tampered, other format).
var ErrOpen = errors.New("sealed key does not open")

// Sealer seals with the public key; it cannot open anything.
type Sealer struct {
	public *ecdh.PublicKey
}

// Opener opens with the private key (easycode-gateway only).
type Opener struct {
	private *ecdh.PrivateKey
}

// GenerateKeyPair writes a new key pair as PEM files (PKCS #8 private, 0600; PKIX public).
// An existing private key is kept, so running it again is safe.
func GenerateKeyPair(privatePath, publicPath string) (bool, error) {
	if _, err := os.Stat(privatePath); err == nil {
		return false, nil
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return false, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return false, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(private.PublicKey())
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		return false, err
	}
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644); err != nil { //nolint:gosec // G306: the public key is meant to be read by the api
		return false, err
	}
	return true, nil
}

// LoadSealer reads the PEM public key.
func LoadSealer(path string) (Sealer, error) {
	block, err := readPEM(path, "PUBLIC KEY")
	if err != nil {
		return Sealer{}, err
	}
	parsed, err := x509.ParsePKIXPublicKey(block)
	if err != nil {
		return Sealer{}, fmt.Errorf("parse sealing public key: %w", err)
	}
	public, ok := parsed.(*ecdh.PublicKey)
	if !ok || public.Curve() != ecdh.X25519() {
		return Sealer{}, errors.New("sealing public key is not X25519")
	}
	return Sealer{public: public}, nil
}

// LoadOpener reads the PEM private key.
func LoadOpener(path string) (Opener, error) {
	block, err := readPEM(path, "PRIVATE KEY")
	if err != nil {
		return Opener{}, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block)
	if err != nil {
		return Opener{}, fmt.Errorf("parse platform private key: %w", err)
	}
	private, ok := parsed.(*ecdh.PrivateKey)
	if !ok || private.Curve() != ecdh.X25519() {
		return Opener{}, errors.New("platform private key is not X25519")
	}
	return Opener{private: private}, nil
}

func readPEM(path, kind string) ([]byte, error) {
	content, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from deployment configuration
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", kind, err)
	}
	block, _ := pem.Decode(content)
	if block == nil || block.Type != kind {
		return nil, fmt.Errorf("%s: not a PEM %s", path, kind)
	}
	return block.Bytes, nil
}

// KeyRef identifies the platform key pair (provider_keys.encryption_key_ref), for rotation.
func (s Sealer) KeyRef() string { return keyRef(s.public) }

func keyRef(public *ecdh.PublicKey) string {
	sum := sha256.Sum256(public.Bytes())
	return "x25519:" + hex.EncodeToString(sum[:8])
}

// Seal implements the api port: it encrypts plain for the platform private key.
func (s Sealer) Seal(plain []byte) ([]byte, string, error) {
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	shared, err := ephemeral.ECDH(s.public)
	if err != nil {
		return nil, "", err
	}
	aead, err := newAEAD(shared, ephemeral.PublicKey().Bytes(), s.public.Bytes())
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}
	header := append([]byte{formatVersion}, ephemeral.PublicKey().Bytes()...)
	header = append(header, nonce...)
	return aead.Seal(header, nonce, plain, header[:1+publicSize]), s.KeyRef(), nil
}

// Open implements gateway.KeyOpener.
func (o Opener) Open(_ context.Context, sealed []byte) (provider.Secret, error) {
	if len(sealed) < 1+publicSize+nonceSize+16 || sealed[0] != formatVersion {
		return provider.Secret{}, ErrOpen
	}
	ephemeralBytes := sealed[1 : 1+publicSize]
	nonce := sealed[1+publicSize : 1+publicSize+nonceSize]
	ephemeral, err := ecdh.X25519().NewPublicKey(ephemeralBytes)
	if err != nil {
		return provider.Secret{}, ErrOpen
	}
	shared, err := o.private.ECDH(ephemeral)
	if err != nil {
		return provider.Secret{}, ErrOpen
	}
	aead, err := newAEAD(shared, ephemeralBytes, o.private.PublicKey().Bytes())
	if err != nil {
		return provider.Secret{}, ErrOpen
	}
	plain, err := aead.Open(nil, nonce, sealed[1+publicSize+nonceSize:], sealed[:1+publicSize])
	if err != nil {
		return provider.Secret{}, ErrOpen
	}
	return provider.NewSecret(plain), nil
}

// KeyRef identifies the key pair of this opener.
func (o Opener) KeyRef() string { return keyRef(o.private.PublicKey()) }

func newAEAD(shared, ephemeralPublic, recipientPublic []byte) (cipher.AEAD, error) {
	salt := append(append([]byte{}, ephemeralPublic...), recipientPublic...)
	key, err := hkdf.Key(sha256.New, shared, salt, info, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
