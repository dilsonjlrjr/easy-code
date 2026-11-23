package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"easycode/internal/application/localclient"
)

// accessClaims are the claims of an access token of the desktop app the api reads.
type accessClaims struct {
	idClaims
	Scope        string `json:"scope"`
	SessionState string `json:"session_state"` // older Keycloak name of sid
}

// LocalClientVerifier implements localclient.Verifier: RS256 signature by the realm keys, public issuer,
// audience = the api client, authorized party = easycode-desktop, scope easycode-read and expiry (ADR-12,
// openapi localClientToken).
type LocalClientVerifier struct {
	provider *Provider
}

var _ localclient.Verifier = LocalClientVerifier{}

// NewLocalClientVerifier checks tokens with the keys and settings of the provider.
func NewLocalClientVerifier(provider *Provider) LocalClientVerifier {
	return LocalClientVerifier{provider: provider}
}

// VerifyAccessToken implements localclient.Verifier.
func (v LocalClientVerifier) VerifyAccessToken(ctx context.Context, raw string) (localclient.Token, error) {
	p := v.provider
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headerJSON, &header) != nil || header.Alg != "RS256" {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	key, err := p.keys.key(ctx, header.Kid)
	if err != nil {
		if errors.Is(err, errInvalidToken) {
			return localclient.Token{}, localclient.ErrInvalidToken
		}
		return localclient.Token{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	var claims accessClaims
	if json.Unmarshal(payload, &claims) != nil {
		return localclient.Token{}, localclient.ErrInvalidToken
	}
	sessionID := claims.SessionID
	if sessionID == "" {
		sessionID = claims.SessionState
	}
	now := p.now()
	switch {
	case claims.Issuer != p.Issuer(), !slices.Contains(claims.audiences(), p.settings.ClientID), claims.Subject == "",
		claims.AuthorizedParty != localclient.ClientID, !slices.Contains(strings.Fields(claims.Scope), localclient.Scope),
		sessionID == "" || len(sessionID) > 128, len(claims.Subject) > 128,
		claims.IssuedAt != 0 && time.Unix(claims.IssuedAt, 0).After(now.Add(clockSkew)):
		return localclient.Token{}, localclient.ErrInvalidToken
	case claims.Expires == 0 || now.After(time.Unix(claims.Expires, 0).Add(clockSkew)):
		return localclient.Token{}, localclient.ErrExpiredToken
	}
	name := claims.Name
	if strings.TrimSpace(name) == "" {
		name = claims.PreferredUsername
	}
	if strings.TrimSpace(name) == "" {
		name = claims.Subject
	}
	token := localclient.Token{
		Subject: claims.Subject, Name: name, SessionID: sessionID, ExpiresAt: time.Unix(claims.Expires, 0).UTC(),
		Administrator: p.settings.AdministratorRole != "" && claims.hasRole(p.settings.AdministratorRole),
	}
	if claims.AuthTime > 0 {
		token.AuthTime = time.Unix(claims.AuthTime, 0).UTC()
	}
	return token, nil
}

// AppSessions implements localclient.Directory over the admin API (view-users): the SSO session of the app
// holds while the person has a session with that id and the client easycode-desktop in it.
type AppSessions struct {
	admin *Admin
}

var _ localclient.Directory = AppSessions{}

// NewAppSessions builds the directory.
func NewAppSessions(admin *Admin) AppSessions { return AppSessions{admin: admin} }

// AppSessionAlive implements localclient.Directory.
func (d AppSessions) AppSessionAlive(ctx context.Context, subject, sessionID string) (bool, error) {
	var sessions []struct {
		ID      string            `json:"id"`
		Clients map[string]string `json:"clients"`
	}
	err := d.admin.get(ctx, "/users/"+url.PathEscape(subject)+"/sessions", nil, &sessions)
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if s.ID != sessionID {
			continue
		}
		for _, client := range s.Clients {
			if client == localclient.ClientID {
				return true, nil
			}
		}
	}
	return false, nil
}
