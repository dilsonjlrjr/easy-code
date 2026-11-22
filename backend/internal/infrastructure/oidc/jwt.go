package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// errInvalidToken marks an ID token that fails any check; the reason stays out of logs and answers.
var errInvalidToken = errors.New("invalid id token")

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// keySet caches the signing keys of the realm (JWKS) and refetches on an unknown kid, at most
// once per minute (key rotation without a restart).
type keySet struct {
	url    string
	client *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func (k *keySet) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if key, ok := k.keys[kid]; ok {
		return key, nil
	}
	if time.Since(k.fetchedAt) < time.Minute && k.keys != nil {
		return nil, errInvalidToken
	}
	keys, err := k.fetch(ctx)
	if err != nil {
		return nil, err
	}
	k.keys, k.fetchedAt = keys, time.Now()
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, errInvalidToken
}

func (k *keySet) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	var body struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(limited(resp)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, candidate := range body.Keys {
		if candidate.Kty != "RSA" || (candidate.Use != "" && candidate.Use != "sig") {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(candidate.N)
		e, errE := base64.RawURLEncoding.DecodeString(candidate.E)
		if errN != nil || errE != nil || len(e) > 4 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		keys[candidate.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	return keys, nil
}

// idClaims are the ID token claims the api reads.
type idClaims struct {
	Issuer            string          `json:"iss"`
	Subject           string          `json:"sub"`
	Audience          json.RawMessage `json:"aud"`
	AuthorizedParty   string          `json:"azp"`
	Expires           int64           `json:"exp"`
	IssuedAt          int64           `json:"iat"`
	AuthTime          int64           `json:"auth_time"`
	Nonce             string          `json:"nonce"`
	SessionID         string          `json:"sid"`
	Name              string          `json:"name"`
	PreferredUsername string          `json:"preferred_username"`
	Roles             []string        `json:"roles"`
	RealmAccess       struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

func (c idClaims) audiences() []string {
	var single string
	if json.Unmarshal(c.Audience, &single) == nil {
		return []string{single}
	}
	var many []string
	_ = json.Unmarshal(c.Audience, &many)
	return many
}

func (c idClaims) hasRole(role string) bool {
	for _, list := range [][]string{c.Roles, c.RealmAccess.Roles} {
		for _, candidate := range list {
			if candidate == role {
				return true
			}
		}
	}
	return false
}

const clockSkew = time.Minute

// verify checks signature (RS256), issuer, audience, expiry and nonce of an ID token.
func verify(ctx context.Context, keys *keySet, raw, issuer, clientID, nonce string, now time.Time) (idClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return idClaims{}, errInvalidToken
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return idClaims{}, errInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headerJSON, &header) != nil || header.Alg != "RS256" {
		return idClaims{}, errInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return idClaims{}, errInvalidToken
	}
	key, err := keys.key(ctx, header.Kid)
	if err != nil {
		return idClaims{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return idClaims{}, errInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return idClaims{}, errInvalidToken
	}
	var claims idClaims
	if json.Unmarshal(payload, &claims) != nil {
		return idClaims{}, errInvalidToken
	}
	audienceOK := false
	for _, audience := range claims.audiences() {
		audienceOK = audienceOK || audience == clientID
	}
	switch {
	case claims.Issuer != issuer, !audienceOK, claims.Subject == "",
		claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID,
		now.After(time.Unix(claims.Expires, 0).Add(clockSkew)),
		claims.IssuedAt != 0 && time.Unix(claims.IssuedAt, 0).After(now.Add(clockSkew)),
		nonce == "" || claims.Nonce != nonce:
		return idClaims{}, errInvalidToken
	}
	return claims, nil
}
