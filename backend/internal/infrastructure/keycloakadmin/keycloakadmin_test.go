package keycloakadmin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/application/install"
	"easycode/internal/infrastructure/keycloakadmin"
)

// fakeKeycloak keeps just enough of the admin API in memory.
type fakeKeycloak struct {
	mu          sync.Mutex
	realms      map[string]map[string]any
	clients     map[string]map[string]any // clientId -> client of the existing realm
	users       map[string]map[string]any // id -> user
	roles       map[string]bool
	scopes      []string
	events      map[string]any
	grants      map[string][]string // user id -> realm roles
	clientRoles map[string][]string // user id -> realm-management roles
	password    string
}

func newFake() *fakeKeycloak {
	return &fakeKeycloak{
		realms:  map[string]map[string]any{},
		clients: map[string]map[string]any{"realm-management": {"id": "rm", "clientId": "realm-management"}},
		users:   map[string]map[string]any{}, roles: map[string]bool{}, scopes: []string{"roles", "profile", "basic"},
		events: map[string]any{"eventsEnabled": false, "enabledEventTypes": []any{}, "eventsListeners": []any{"jboss-logging"}},
		grants: map[string][]string{}, clientRoles: map[string][]string{}, password: "right",
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeKeycloak) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/auth")
	if strings.HasSuffix(p, "/protocol/openid-connect/token") {
		_ = r.ParseForm()
		if r.Form.Get("password") != f.password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]string{"access_token": "token"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var body any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(p, "/admin/realms"), "/"), "/")
	switch {
	case r.Method == http.MethodPost && parts[0] == "":
		rep := body.(map[string]any)
		name := rep["realm"].(string)
		if _, ok := f.realms[name]; ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.realms[name] = rep
		w.WriteHeader(http.StatusCreated)
	case len(parts) == 1 && r.Method == http.MethodGet:
		if _, ok := f.realms[parts[0]]; ok {
			writeJSON(w, map[string]any{"realm": parts[0]})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case parts[1] == "roles" && len(parts) == 3 && r.Method == http.MethodGet:
		if !f.roles[parts[2]] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"id": "role-" + parts[2], "name": parts[2]})
	case parts[1] == "roles" && len(parts) == 4 && parts[3] == "users":
		var out []any
		for id, roles := range f.grants {
			for _, role := range roles {
				if role == parts[2] {
					out = append(out, f.users[id])
				}
			}
		}
		writeJSON(w, out)
	case parts[1] == "roles" && r.Method == http.MethodPost:
		f.roles[body.(map[string]any)["name"].(string)] = true
		w.WriteHeader(http.StatusCreated)
	case parts[1] == "client-scopes" && r.Method == http.MethodGet:
		var out []any
		for _, s := range f.scopes {
			out = append(out, map[string]string{"name": s})
		}
		writeJSON(w, out)
	case parts[1] == "client-scopes" && r.Method == http.MethodPost:
		f.scopes = append(f.scopes, body.(map[string]any)["name"].(string))
		w.WriteHeader(http.StatusCreated)
	case parts[1] == "clients" && len(parts) == 2 && r.Method == http.MethodGet:
		var out []any
		if c, ok := f.clients[r.URL.Query().Get("clientId")]; ok {
			out = append(out, c)
		}
		writeJSON(w, out)
	case parts[1] == "clients" && len(parts) == 2 && r.Method == http.MethodPost:
		rep := body.(map[string]any)
		id := "c-" + rep["clientId"].(string)
		rep["id"] = id
		f.clients[rep["clientId"].(string)] = rep
		if rep["serviceAccountsEnabled"] == true {
			f.users["sa-"+id] = map[string]any{"id": "sa-" + id, "username": "service-account-" + rep["clientId"].(string), "serviceAccountClientId": rep["clientId"]}
		}
		w.Header().Set("Location", "http://kc/admin/realms/x/clients/"+id)
		w.WriteHeader(http.StatusCreated)
	case parts[1] == "clients" && len(parts) == 4 && parts[3] == "client-secret":
		for _, c := range f.clients {
			if c["id"] == parts[2] {
				writeJSON(w, map[string]any{"value": c["secret"]})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	case parts[1] == "clients" && len(parts) == 4 && parts[3] == "service-account-user":
		writeJSON(w, f.users["sa-"+parts[2]])
	case parts[1] == "clients" && len(parts) == 5 && parts[3] == "roles":
		writeJSON(w, map[string]any{"id": "r-" + parts[4], "name": parts[4]})
	case parts[1] == "events" && r.Method == http.MethodGet:
		writeJSON(w, f.events)
	case parts[1] == "events" && r.Method == http.MethodPut:
		f.events = body.(map[string]any)
		w.WriteHeader(http.StatusNoContent)
	case parts[1] == "users" && len(parts) == 2 && r.Method == http.MethodGet:
		var out []any
		q := r.URL.Query()
		for _, u := range f.users {
			if q.Get("username") != "" && u["username"] == q.Get("username") || q.Get("email") != "" && u["email"] == q.Get("email") ||
				q.Get("search") != "" && strings.Contains(u["username"].(string), q.Get("search")) {
				out = append(out, u)
			}
		}
		writeJSON(w, out)
	case parts[1] == "users" && len(parts) == 2 && r.Method == http.MethodPost:
		rep := body.(map[string]any)
		id := "u-" + rep["username"].(string)
		rep["id"] = id
		delete(rep, "credentials")
		f.users[id] = rep
		w.Header().Set("Location", "http://kc/admin/realms/x/users/"+id)
		w.WriteHeader(http.StatusCreated)
	case parts[1] == "users" && len(parts) == 3:
		u, ok := f.users[parts[2]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, u)
	case parts[1] == "users" && len(parts) == 5 && parts[4] == "realm":
		for _, role := range body.([]any) {
			f.grants[parts[2]] = append(f.grants[parts[2]], role.(map[string]any)["name"].(string))
		}
		w.WriteHeader(http.StatusNoContent)
	case parts[1] == "users" && len(parts) == 6 && parts[4] == "clients":
		for _, role := range body.([]any) {
			f.clientRoles[parts[2]] = append(f.clientRoles[parts[2]], role.(map[string]any)["name"].(string))
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected "+r.Method+" "+p, http.StatusTeapot)
	}
}

func sso(server *httptest.Server, mode string) install.SSO {
	return install.SSO{PublicURL: server.URL + "/auth", Mode: mode, Realm: "easycode", AdminUser: "admin", AdminPassword: "right"}
}

var organization = install.Organization{Name: "Empresa", Currency: "BRL", PublicOrigin: "https://easycode.example", Locale: "pt-BR"}

func TestNewRealmIsCreatedFromTheTemplate(t *testing.T) {
	fake := newFake()
	server := httptest.NewServer(fake)
	defer server.Close()
	d := keycloakadmin.New(5 * time.Second)
	ctx := context.Background()

	probe, err := d.Probe(ctx, sso(server, install.ModeNewRealm), organization)
	if err != nil || probe.RealmExists {
		t.Fatalf("probe = %+v, %v", probe, err)
	}
	got, err := d.Configure(ctx, sso(server, install.ModeNewRealm), organization, install.Integration{APISecret: "api-s", EventsSecret: "ev-s"})
	if err != nil || got.APISecret != "api-s" {
		t.Fatalf("configure = %+v, %v", got, err)
	}
	realm := fake.realms["easycode"]
	if realm["displayName"] != "Empresa" {
		t.Fatalf("displayName = %v", realm["displayName"])
	}
	encoded, _ := json.Marshal(realm)
	text := string(encoded)
	for _, want := range []string{`"secret":"api-s"`, `"secret":"ev-s"`, "https://easycode.example/api/v1/session/oidc/callback", "https://easycode.example/login"} {
		if !strings.Contains(text, want) {
			t.Fatalf("realm misses %s", want)
		}
	}
	if strings.Contains(text, "dev-only") || strings.Contains(text, "admin.dev") {
		t.Fatal("realm carries development values")
	}
	if _, err := d.Configure(ctx, sso(server, install.ModeNewRealm), organization, install.Integration{}); !isReason(err, "realm_exists") {
		t.Fatalf("second create: %v", err)
	}
}

func TestWrongPasswordIsAFieldProblem(t *testing.T) {
	server := httptest.NewServer(newFake())
	defer server.Close()
	s := sso(server, install.ModeNewRealm)
	s.AdminPassword = "wrong"
	if _, err := keycloakadmin.New(time.Second).Probe(context.Background(), s, organization); !isReason(err, "auth_failed") {
		t.Fatalf("got %v", err)
	}
}

func isReason(err error, reason string) bool {
	var invalid install.Invalids
	return errors.As(err, &invalid) && len(invalid) > 0 && invalid[0].Reason == reason
}

func TestExistingRealmGetsOnlyWhatIsMissing(t *testing.T) {
	fake := newFake()
	fake.realms["easycode"] = map[string]any{"realm": "easycode"}
	// The events client already exists with the expected shape: it is reused with its secret.
	fake.clients[install.EventsClientID] = map[string]any{"id": "c-ev", "clientId": install.EventsClientID, "publicClient": false,
		"standardFlowEnabled": false, "serviceAccountsEnabled": true, "secret": "kept-secret"}
	fake.users["sa-c-ev"] = map[string]any{"id": "sa-c-ev", "username": "service-account-easycode-events", "serviceAccountClientId": install.EventsClientID}
	server := httptest.NewServer(fake)
	defer server.Close()
	d := keycloakadmin.New(5 * time.Second)
	ctx := context.Background()
	s := sso(server, install.ModeExistingRealm)

	probe, err := d.Probe(ctx, s, organization)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create_role", "create_client_scope:easycode-read", "create_client:easycode-api", "create_client:easycode-desktop",
		"grant_service_roles:easycode-api", "grant_service_roles:easycode-events", "enable_login_events"}
	if !reflect.DeepEqual(probe.Actions, want) {
		t.Fatalf("actions = %v", probe.Actions)
	}
	got, err := d.Configure(ctx, s, organization, install.Integration{APISecret: "new-api", EventsSecret: "new-ev"})
	if err != nil {
		t.Fatal(err)
	}
	if got.APISecret != "new-api" || got.EventsSecret != "kept-secret" {
		t.Fatalf("integration = %+v", got)
	}
	if !fake.roles[install.AdministratorRole] || fake.events["eventsEnabled"] != true {
		t.Fatal("role or events not configured")
	}
	if !reflect.DeepEqual(fake.clientRoles["sa-c-ev"], []string{"view-events", "view-users"}) {
		t.Fatalf("events service roles = %v", fake.clientRoles["sa-c-ev"])
	}

	admin, err := d.EnsurePerson(ctx, s, install.Person{Username: "ana", Email: "ana@example.com", FirstName: "Ana", Password: "temporary-password"}, true)
	if err != nil || admin.ID != "u-ana" || admin.Name != "Ana" {
		t.Fatalf("admin = %+v, %v", admin, err)
	}
	// A retry reuses the user created before.
	again, err := d.EnsurePerson(ctx, s, install.Person{Username: "ana", Email: "ana@example.com", FirstName: "Ana", Password: "temporary-password"}, true)
	if err != nil || again.ID != admin.ID {
		t.Fatalf("retry = %+v, %v", again, err)
	}
	if err := d.CheckPerson(ctx, s, install.Person{Username: "ana", Email: "other@example.com"}, "user"); !isReason(err, "taken") {
		t.Fatalf("check taken: %v", err)
	}
	probe, err = d.Probe(ctx, s, organization)
	if err != nil || len(probe.Administrators) == 0 || probe.Administrators[0].Username != "ana" {
		t.Fatalf("holders = %+v, %v", probe.Administrators, err)
	}
}

func TestExistingClientWithAnotherShapeIsRefused(t *testing.T) {
	fake := newFake()
	fake.realms["easycode"] = map[string]any{"realm": "easycode"}
	fake.clients[install.APIClientID] = map[string]any{"id": "c-api", "clientId": install.APIClientID, "publicClient": true}
	server := httptest.NewServer(fake)
	defer server.Close()
	_, err := keycloakadmin.New(time.Second).Probe(context.Background(), sso(server, install.ModeExistingRealm), organization)
	if !isReason(err, "client_conflict:easycode-api") {
		t.Fatalf("got %v", err)
	}
}

// TestTemplateFollowsTheDevelopmentRealm keeps realm.json equal to infra/keycloak/easycode-realm.json
// except people, secrets and addresses. make test mounts the development realm (EASYCODE_TEST_DEV_REALM).
func TestTemplateFollowsTheDevelopmentRealm(t *testing.T) {
	devPath := os.Getenv("EASYCODE_TEST_DEV_REALM")
	if devPath == "" {
		t.Skip("EASYCODE_TEST_DEV_REALM not set")
	}
	read := func(p string) map[string]any {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	dev, tmpl := read(devPath), read("realm.json")
	normalize := func(realm map[string]any) {
		delete(realm, "displayName")
		var users []any
		for _, u := range realm["users"].([]any) {
			if u.(map[string]any)["serviceAccountClientId"] != nil {
				users = append(users, u)
			}
		}
		realm["users"] = users
		for _, c := range realm["clients"].([]any) {
			client := c.(map[string]any)
			for _, key := range []string{"secret", "redirectUris", "webOrigins", "name", "description"} {
				delete(client, key)
			}
			if attributes, ok := client["attributes"].(map[string]any); ok {
				delete(attributes, "post.logout.redirect.uris")
			}
		}
	}
	normalize(dev)
	normalize(tmpl)
	if !reflect.DeepEqual(dev, tmpl) {
		t.Fatal("realm.json drifted from infra/keycloak/easycode-realm.json: regenerate it without people, secrets and addresses")
	}
}
