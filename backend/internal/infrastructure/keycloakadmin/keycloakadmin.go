// Package keycloakadmin is the Keycloak admin API client of easycode-installer (ADR-22): it creates
// the realm of the platform or the missing parts of an existing realm, and creates or promotes the
// people of the install. It is used only while the wizard runs; the credential stays in memory and
// no token, password or secret goes into an error message.
package keycloakadmin

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"easycode/internal/application/install"
)

//go:embed realm.json
var realmTemplate []byte

// Directory implements install.Directory.
type Directory struct {
	client *http.Client
}

var _ install.Directory = Directory{}

// New builds the client with a timeout per call.
func New(timeout time.Duration) Directory {
	return Directory{client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// session is a logged-in admin API client for one realm.
type session struct {
	client *http.Client
	base   string // .../auth
	realm  string
	token  string
}

// StatusError is an unexpected answer of the admin API (the body is not kept: it may echo input).
type StatusError struct {
	Operation string
	Status    int
}

func (e StatusError) Error() string {
	return fmt.Sprintf("keycloak %s answered %d", e.Operation, e.Status)
}

// login uses the admin-cli client with the password grant: of the master realm when a realm is
// created, of the realm itself when an existing realm is used.
func (d Directory) login(ctx context.Context, sso install.SSO) (*session, error) {
	loginRealm := "master"
	if sso.Mode == install.ModeExistingRealm {
		loginRealm = sso.Realm
	}
	base := sso.AdminURL()
	form := url.Values{"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {sso.AdminUser}, "password": {sso.AdminPassword}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/realms/"+url.PathEscape(loginRealm)+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, install.Invalids{{Field: "sso.public_url", Reason: "invalid_url"}}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, install.Invalids{{Field: "sso.public_url", Reason: "unreachable"}}
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest:
		return nil, install.Invalids{{Field: "sso.admin_password", Reason: "auth_failed"}}
	case resp.StatusCode == http.StatusNotFound:
		if sso.Mode == install.ModeExistingRealm {
			return nil, install.Invalids{{Field: "sso.realm", Reason: "realm_not_found"}}
		}
		return nil, install.Invalids{{Field: "sso.public_url", Reason: "not_keycloak"}}
	case resp.StatusCode != http.StatusOK:
		return nil, StatusError{"login", resp.StatusCode}
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil || token.AccessToken == "" {
		return nil, install.Invalids{{Field: "sso.public_url", Reason: "not_keycloak"}}
	}
	return &session{client: d.client, base: base, realm: sso.Realm, token: token.AccessToken}, nil
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}

// do calls the admin API under /admin/realms; out may be nil. It returns the status and the
// Location header.
func (s *session) do(ctx context.Context, operation, method, route string, query url.Values, body, out any) (int, string, error) {
	target := s.base + "/admin/realms" + route
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, "", err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("keycloak %s: unreachable", operation)
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, "", install.Invalids{{Field: "sso.admin_user", Reason: "not_allowed"}}
	}
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
			return resp.StatusCode, "", fmt.Errorf("keycloak %s: unexpected answer", operation)
		}
	}
	return resp.StatusCode, resp.Header.Get("Location"), nil
}

func (s *session) realmPath(parts ...string) string {
	escaped := []string{"/" + url.PathEscape(s.realm)}
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return strings.Join(escaped, "/")
}

// expect fails unless status is one of want.
func expect(operation string, status int, err error, want ...int) error {
	if err != nil {
		return err
	}
	for _, w := range want {
		if status == w {
			return nil
		}
	}
	return StatusError{operation, status}
}

func (s *session) realmExists(ctx context.Context) (bool, error) {
	status, _, err := s.do(ctx, "read realm", http.MethodGet, s.realmPath(), nil, nil, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, StatusError{"read realm", status}
}

// Probe implements install.Directory.
func (d Directory) Probe(ctx context.Context, sso install.SSO, organization install.Organization) (install.SSOProbe, error) {
	s, err := d.login(ctx, sso)
	if err != nil {
		return install.SSOProbe{}, err
	}
	exists, err := s.realmExists(ctx)
	if err != nil || !exists {
		return install.SSOProbe{RealmExists: exists, Actions: newRealmActions()}, err
	}
	if sso.Mode == install.ModeNewRealm {
		return install.SSOProbe{RealmExists: true}, nil
	}
	plan, err := s.planExisting(ctx, organization)
	if err != nil {
		return install.SSOProbe{RealmExists: true}, err
	}
	admins, err := s.roleHolders(ctx)
	if err != nil {
		return install.SSOProbe{RealmExists: true}, err
	}
	return install.SSOProbe{RealmExists: true, Actions: plan.actions(), Administrators: admins}, plan.conflicts.err()
}

func newRealmActions() []string {
	return []string{"create_realm", "create_role", "create_client:" + install.APIClientID, "create_client:" + install.EventsClientID,
		"create_client:" + install.DesktopClientID, "create_client_scope:easycode-read", "enable_login_events"}
}

// Search implements install.Directory.
func (d Directory) Search(ctx context.Context, sso install.SSO, query string) ([]install.DirectoryUser, error) {
	s, err := d.login(ctx, sso)
	if err != nil {
		return nil, err
	}
	var users []userRep
	status, _, err := s.do(ctx, "search users", http.MethodGet, s.realmPath("users"), url.Values{"search": {query}, "max": {"20"}, "briefRepresentation": {"true"}}, nil, &users)
	if err := expect("search users", status, err, http.StatusOK); err != nil {
		return nil, err
	}
	out := make([]install.DirectoryUser, 0, len(users))
	for _, u := range users {
		if u.ServiceAccountClientID == "" {
			out = append(out, u.directory())
		}
	}
	return out, nil
}

type userRep struct {
	ID                     string           `json:"id,omitempty"`
	Username               string           `json:"username"`
	Email                  string           `json:"email,omitempty"`
	FirstName              string           `json:"firstName,omitempty"`
	LastName               string           `json:"lastName,omitempty"`
	Enabled                bool             `json:"enabled"`
	EmailVerified          bool             `json:"emailVerified,omitempty"`
	ServiceAccountClientID string           `json:"serviceAccountClientId,omitempty"`
	Credentials            []credentialRep  `json:"credentials,omitempty"`
	RequiredActions        []string         `json:"requiredActions,omitempty"`
	Attributes             map[string][]any `json:"attributes,omitempty"`
}

type credentialRep struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Temporary bool   `json:"temporary"`
}

func (u userRep) directory() install.DirectoryUser {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return install.DirectoryUser{ID: u.ID, Username: u.Username, Email: u.Email, Name: name, Enabled: u.Enabled}
}

func (s *session) findUser(ctx context.Context, key, value string) (*userRep, error) {
	var users []userRep
	status, _, err := s.do(ctx, "find user", http.MethodGet, s.realmPath("users"), url.Values{key: {value}, "exact": {"true"}}, nil, &users)
	if err := expect("find user", status, err, http.StatusOK); err != nil {
		return nil, err
	}
	for i := range users {
		if strings.EqualFold(key, "username") && strings.EqualFold(users[i].Username, value) ||
			strings.EqualFold(key, "email") && strings.EqualFold(users[i].Email, value) {
			return &users[i], nil
		}
	}
	return nil, nil
}

func (s *session) user(ctx context.Context, id string) (*userRep, error) {
	var user userRep
	status, _, err := s.do(ctx, "read user", http.MethodGet, s.realmPath("users", id), nil, nil, &user)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	return &user, expect("read user", status, nil, http.StatusOK)
}

// CheckPerson implements install.Directory (existing realm only; a new realm has nobody yet).
func (d Directory) CheckPerson(ctx context.Context, sso install.SSO, person install.Person, field string) error {
	s, err := d.login(ctx, sso)
	if err != nil {
		return err
	}
	if person.ExistingID != "" {
		user, err := s.user(ctx, person.ExistingID)
		switch {
		case err != nil:
			return err
		case user == nil:
			return install.Invalids{{Field: field + ".existing_id", Reason: "user_not_found"}}
		case !user.Enabled:
			return install.Invalids{{Field: field + ".existing_id", Reason: "user_disabled"}}
		}
		return nil
	}
	var problems install.Invalids
	if user, err := s.findUser(ctx, "username", person.Username); err != nil {
		return err
	} else if user != nil {
		problems = append(problems, install.Invalid{Field: field + ".username", Reason: "taken"})
	}
	if user, err := s.findUser(ctx, "email", person.Email); err != nil {
		return err
	} else if user != nil {
		problems = append(problems, install.Invalid{Field: field + ".email", Reason: "taken"})
	}
	if len(problems) > 0 {
		return problems
	}
	return nil
}

// EnsurePerson implements install.Directory. A user created by an earlier try (same username and
// e-mail) is reused, so the step can run again.
func (d Directory) EnsurePerson(ctx context.Context, sso install.SSO, person install.Person, administrator bool) (install.DirectoryUser, error) {
	s, err := d.login(ctx, sso)
	if err != nil {
		return install.DirectoryUser{}, err
	}
	var user *userRep
	if person.ExistingID != "" {
		if user, err = s.user(ctx, person.ExistingID); err != nil {
			return install.DirectoryUser{}, err
		}
		if user == nil {
			return install.DirectoryUser{}, install.Invalids{{Field: "person.existing_id", Reason: "user_not_found"}}
		}
	} else {
		if user, err = s.findUser(ctx, "username", person.Username); err != nil {
			return install.DirectoryUser{}, err
		}
		if user != nil && !strings.EqualFold(user.Email, person.Email) {
			return install.DirectoryUser{}, install.Invalids{{Field: "person.username", Reason: "taken"}}
		}
		if user == nil {
			if user, err = s.createUser(ctx, person); err != nil {
				return install.DirectoryUser{}, err
			}
		}
	}
	if administrator {
		if err := s.grantRealmRole(ctx, user.ID, install.AdministratorRole); err != nil {
			return install.DirectoryUser{}, err
		}
	}
	return user.directory(), nil
}

func (s *session) createUser(ctx context.Context, person install.Person) (*userRep, error) {
	rep := userRep{
		Username: strings.ToLower(person.Username), Email: person.Email, FirstName: strings.TrimSpace(person.FirstName),
		LastName: strings.TrimSpace(person.LastName), Enabled: true, EmailVerified: true,
		Credentials: []credentialRep{{Type: "password", Value: person.Password, Temporary: true}},
	}
	status, location, err := s.do(ctx, "create user", http.MethodPost, s.realmPath("users"), nil, rep, nil)
	if status == http.StatusConflict {
		return nil, install.Invalids{{Field: "person.username", Reason: "taken"}}
	}
	if status == http.StatusBadRequest {
		return nil, install.Invalids{{Field: "person.password", Reason: "refused_by_policy"}}
	}
	if err := expect("create user", status, err, http.StatusCreated); err != nil {
		return nil, err
	}
	rep.ID = path.Base(location)
	rep.Credentials = nil
	return &rep, nil
}

type roleRep struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (s *session) grantRealmRole(ctx context.Context, userID, roleName string) error {
	var role roleRep
	status, _, err := s.do(ctx, "read role", http.MethodGet, s.realmPath("roles", roleName), nil, nil, &role)
	if err := expect("read role", status, err, http.StatusOK); err != nil {
		return err
	}
	status, _, err = s.do(ctx, "grant role", http.MethodPost, s.realmPath("users", userID, "role-mappings", "realm"), nil, []roleRep{role}, nil)
	return expect("grant role", status, err, http.StatusNoContent, http.StatusOK)
}

func (s *session) roleHolders(ctx context.Context) ([]install.DirectoryUser, error) {
	var users []userRep
	status, _, err := s.do(ctx, "role users", http.MethodGet, s.realmPath("roles", install.AdministratorRole, "users"), url.Values{"max": {"50"}}, nil, &users)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err := expect("role users", status, nil, http.StatusOK); err != nil {
		return nil, err
	}
	out := make([]install.DirectoryUser, 0, len(users))
	for _, u := range users {
		out = append(out, u.directory())
	}
	return out, nil
}
