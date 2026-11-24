package keycloakadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"easycode/internal/application/install"
)

// CallbackPath is where the SSO returns to the api (Identity.PublicOrigin + this path).
const CallbackPath = "/api/v1/session/oidc/callback"

// readScope is the client scope of the read-only desktop token (ADR-20).
const readScope = "easycode-read"

// template is the realm template (realm.json): the integration of the development realm without its
// people and secrets. TestTemplateFollowsTheDevelopmentRealm keeps both equal.
type template struct {
	raw map[string]any
}

func loadTemplate() (template, error) {
	var raw map[string]any
	if err := json.Unmarshal(realmTemplate, &raw); err != nil {
		return template{}, fmt.Errorf("realm template: %w", err)
	}
	return template{raw: raw}, nil
}

func (t template) clients() []map[string]any { return objects(t.raw["clients"]) }

func (t template) client(clientID string) map[string]any {
	for _, c := range t.clients() {
		if c["clientId"] == clientID {
			return c
		}
	}
	return nil
}

func (t template) scope(name string) map[string]any {
	for _, s := range objects(t.raw["clientScopes"]) {
		if s["name"] == name {
			return s
		}
	}
	return nil
}

func (t template) role(name string) map[string]any {
	roles, _ := t.raw["roles"].(map[string]any)
	for _, r := range objects(roles["realm"]) {
		if r["name"] == name {
			return r
		}
	}
	return nil
}

// serviceRoles lists the realm-management roles of the service account of each client.
func (t template) serviceRoles() map[string][]string {
	out := map[string][]string{}
	for _, u := range objects(t.raw["users"]) {
		clientID, _ := u["serviceAccountClientId"].(string)
		roles, _ := u["clientRoles"].(map[string]any)
		for _, r := range anySlice(roles["realm-management"]) {
			if name, ok := r.(string); ok && clientID != "" {
				out[clientID] = append(out[clientID], name)
			}
		}
	}
	return out
}

func objects(value any) []map[string]any {
	var out []map[string]any
	for _, item := range anySlice(value) {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}
	return out
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

// fill sets the addresses and secrets of the deployment in a copy of the clients.
func fill(client map[string]any, origin string, integration install.Integration) map[string]any {
	encoded, _ := json.Marshal(client)
	var out map[string]any
	_ = json.Unmarshal(encoded, &out)
	origin = strings.TrimRight(origin, "/")
	switch out["clientId"] {
	case install.APIClientID:
		out["secret"] = integration.APISecret
		out["redirectUris"] = []string{origin + CallbackPath}
		out["webOrigins"] = []string{origin}
		attributes, _ := out["attributes"].(map[string]any)
		if attributes == nil {
			attributes = map[string]any{}
		}
		attributes["post.logout.redirect.uris"] = origin + "/login"
		out["attributes"] = attributes
	case install.EventsClientID:
		out["secret"] = integration.EventsSecret
	case install.DesktopClientID:
		out["name"] = fmt.Sprintf("%v — %s", out["name"], origin)
	}
	return out
}

// newRealm is the representation sent to create the realm of the platform.
func (t template) newRealm(sso install.SSO, organization install.Organization, integration install.Integration) map[string]any {
	encoded, _ := json.Marshal(t.raw)
	var realm map[string]any
	_ = json.Unmarshal(encoded, &realm)
	realm["realm"] = sso.Realm
	realm["displayName"] = strings.TrimSpace(organization.Name)
	realm["defaultLocale"] = organization.Locale
	clients := make([]any, 0, len(t.clients()))
	for _, c := range t.clients() {
		clients = append(clients, fill(c, organization.PublicOrigin, integration))
	}
	realm["clients"] = clients
	return realm
}

// Configure implements install.Directory.
func (d Directory) Configure(ctx context.Context, sso install.SSO, organization install.Organization, integration install.Integration) (install.Integration, error) {
	t, err := loadTemplate()
	if err != nil {
		return install.Integration{}, err
	}
	s, err := d.login(ctx, sso)
	if err != nil {
		return install.Integration{}, err
	}
	if sso.Mode == install.ModeNewRealm {
		status, _, err := s.do(ctx, "create realm", http.MethodPost, "", nil, t.newRealm(sso, organization, integration), nil)
		if status == http.StatusConflict {
			return install.Integration{}, install.Invalids{{Field: "sso.realm", Reason: "realm_exists"}}
		}
		return integration, expect("create realm", status, err, http.StatusCreated)
	}
	plan, err := s.planExisting(ctx, organization)
	if err != nil {
		return install.Integration{}, err
	}
	if err := plan.conflicts.err(); err != nil {
		return install.Integration{}, err
	}
	return s.applyExisting(ctx, t, plan, organization, integration)
}

// existingPlan is what an existing realm lacks (ER-31-4: nothing that exists is changed).
type existingPlan struct {
	createScope    bool
	createRole     bool
	createClients  []string
	reusedClients  map[string]string // clientId -> internal id
	enableEvents   bool
	eventsConfig   map[string]any
	conflicts      conflictList
	serviceClients []string
}

type conflictList install.Invalids

func (c conflictList) err() error {
	if len(c) == 0 {
		return nil
	}
	return install.Invalids(c)
}

func (p existingPlan) actions() []string {
	var out []string
	if p.createRole {
		out = append(out, "create_role")
	}
	if p.createScope {
		out = append(out, "create_client_scope:"+readScope)
	}
	for _, c := range p.createClients {
		out = append(out, "create_client:"+c)
	}
	for _, c := range p.serviceClients {
		out = append(out, "grant_service_roles:"+c)
	}
	if p.enableEvents {
		out = append(out, "enable_login_events")
	}
	return out
}

type clientRep struct {
	ID                     string   `json:"id"`
	ClientID               string   `json:"clientId"`
	PublicClient           bool     `json:"publicClient"`
	StandardFlowEnabled    bool     `json:"standardFlowEnabled"`
	ServiceAccountsEnabled bool     `json:"serviceAccountsEnabled"`
	RedirectURIs           []string `json:"redirectUris"`
}

func (s *session) findClient(ctx context.Context, clientID string) (*clientRep, error) {
	var clients []clientRep
	status, _, err := s.do(ctx, "find client", http.MethodGet, s.realmPath("clients"), url.Values{"clientId": {clientID}}, nil, &clients)
	if err := expect("find client", status, err, http.StatusOK); err != nil {
		return nil, err
	}
	for i := range clients {
		if clients[i].ClientID == clientID {
			return &clients[i], nil
		}
	}
	return nil, nil
}

func (s *session) planExisting(ctx context.Context, organization install.Organization) (existingPlan, error) {
	t, err := loadTemplate()
	if err != nil {
		return existingPlan{}, err
	}
	plan := existingPlan{reusedClients: map[string]string{}}
	status, _, err := s.do(ctx, "read role", http.MethodGet, s.realmPath("roles", install.AdministratorRole), nil, nil, nil)
	if err != nil {
		return plan, err
	}
	plan.createRole = status == http.StatusNotFound
	var scopes []struct {
		Name string `json:"name"`
	}
	status, _, err = s.do(ctx, "list scopes", http.MethodGet, s.realmPath("client-scopes"), nil, nil, &scopes)
	if err := expect("list scopes", status, err, http.StatusOK); err != nil {
		return plan, err
	}
	plan.createScope = !slices.ContainsFunc(scopes, func(sc struct {
		Name string `json:"name"`
	}) bool {
		return sc.Name == readScope
	})
	callback := strings.TrimRight(organization.PublicOrigin, "/") + CallbackPath
	for _, want := range t.clients() {
		clientID, _ := want["clientId"].(string)
		found, err := s.findClient(ctx, clientID)
		if err != nil {
			return plan, err
		}
		if found == nil {
			plan.createClients = append(plan.createClients, clientID)
			continue
		}
		public, _ := want["publicClient"].(bool)
		standard, _ := want["standardFlowEnabled"].(bool)
		service, _ := want["serviceAccountsEnabled"].(bool)
		same := found.PublicClient == public && found.StandardFlowEnabled == standard && found.ServiceAccountsEnabled == service
		if clientID == install.APIClientID && !slices.Contains(found.RedirectURIs, callback) {
			same = false
		}
		if !same {
			plan.conflicts = append(plan.conflicts, install.Invalid{Field: "sso.realm", Reason: "client_conflict:" + clientID})
			continue
		}
		plan.reusedClients[clientID] = found.ID
	}
	for clientID := range t.serviceRoles() {
		plan.serviceClients = append(plan.serviceClients, clientID)
	}
	slices.Sort(plan.serviceClients)
	var events map[string]any
	status, _, err = s.do(ctx, "read events", http.MethodGet, s.realmPath("events", "config"), nil, nil, &events)
	if err := expect("read events", status, err, http.StatusOK); err != nil {
		return plan, err
	}
	plan.eventsConfig = events
	enabled, _ := events["eventsEnabled"].(bool)
	types := stringSet(events["enabledEventTypes"])
	for _, want := range anySlice(t.raw["enabledEventTypes"]) {
		if name, _ := want.(string); !types[name] {
			enabled = false
		}
	}
	plan.enableEvents = !enabled
	return plan, nil
}

func stringSet(value any) map[string]bool {
	out := map[string]bool{}
	for _, item := range anySlice(value) {
		if name, ok := item.(string); ok {
			out[name] = true
		}
	}
	return out
}

func (s *session) applyExisting(ctx context.Context, t template, plan existingPlan, organization install.Organization, integration install.Integration) (install.Integration, error) {
	if plan.createScope {
		status, _, err := s.do(ctx, "create scope", http.MethodPost, s.realmPath("client-scopes"), nil, t.scope(readScope), nil)
		if err := expect("create scope", status, err, http.StatusCreated, http.StatusConflict); err != nil {
			return install.Integration{}, err
		}
	}
	if plan.createRole {
		status, _, err := s.do(ctx, "create role", http.MethodPost, s.realmPath("roles"), nil, t.role(install.AdministratorRole), nil)
		if err := expect("create role", status, err, http.StatusCreated, http.StatusConflict); err != nil {
			return install.Integration{}, err
		}
	}
	for _, clientID := range plan.createClients {
		status, location, err := s.do(ctx, "create client", http.MethodPost, s.realmPath("clients"), nil, fill(t.client(clientID), organization.PublicOrigin, integration), nil)
		if err := expect("create client", status, err, http.StatusCreated); err != nil {
			return install.Integration{}, err
		}
		plan.reusedClients[clientID] = location[strings.LastIndex(location, "/")+1:]
	}
	// An existing confidential client keeps its secret: the platform uses it.
	for clientID, target := range map[string]*string{install.APIClientID: &integration.APISecret, install.EventsClientID: &integration.EventsSecret} {
		if slices.Contains(plan.createClients, clientID) {
			continue
		}
		var secret struct {
			Value string `json:"value"`
		}
		status, _, err := s.do(ctx, "read client secret", http.MethodGet, s.realmPath("clients", plan.reusedClients[clientID], "client-secret"), nil, nil, &secret)
		if err := expect("read client secret", status, err, http.StatusOK); err != nil {
			return install.Integration{}, err
		}
		*target = secret.Value
	}
	if err := s.grantServiceRoles(ctx, t, plan.reusedClients); err != nil {
		return install.Integration{}, err
	}
	if plan.enableEvents {
		events := plan.eventsConfig
		events["eventsEnabled"] = true
		types := stringSet(events["enabledEventTypes"])
		list := anySlice(events["enabledEventTypes"])
		for _, want := range anySlice(t.raw["enabledEventTypes"]) {
			if name, _ := want.(string); !types[name] {
				list = append(list, name)
			}
		}
		events["enabledEventTypes"] = list
		if _, ok := events["eventsExpiration"]; !ok {
			events["eventsExpiration"] = t.raw["eventsExpiration"]
		}
		status, _, err := s.do(ctx, "enable events", http.MethodPut, s.realmPath("events", "config"), nil, events, nil)
		if err := expect("enable events", status, err, http.StatusNoContent, http.StatusOK); err != nil {
			return install.Integration{}, err
		}
	}
	return integration, nil
}

func (s *session) grantServiceRoles(ctx context.Context, t template, clients map[string]string) error {
	management, err := s.findClient(ctx, "realm-management")
	if err != nil {
		return err
	}
	if management == nil {
		return StatusError{"find realm-management", http.StatusNotFound}
	}
	for clientID, names := range t.serviceRoles() {
		var account userRep
		status, _, err := s.do(ctx, "service account", http.MethodGet, s.realmPath("clients", clients[clientID], "service-account-user"), nil, nil, &account)
		if err := expect("service account", status, err, http.StatusOK); err != nil {
			return err
		}
		roles := make([]roleRep, 0, len(names))
		for _, name := range names {
			var role roleRep
			status, _, err := s.do(ctx, "read client role", http.MethodGet, s.realmPath("clients", management.ID, "roles", name), nil, nil, &role)
			if err := expect("read client role", status, err, http.StatusOK); err != nil {
				return err
			}
			roles = append(roles, role)
		}
		status, _, err = s.do(ctx, "grant client roles", http.MethodPost, s.realmPath("users", account.ID, "role-mappings", "clients", management.ID), nil, roles, nil)
		if err := expect("grant client roles", status, err, http.StatusNoContent, http.StatusOK); err != nil {
			return err
		}
	}
	return nil
}
