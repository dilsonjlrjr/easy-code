package install

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Step statuses (ESP-31 section 6).
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// StepState is the state of one step of Apply. Reason is a stable code or a message without secrets.
type StepState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Status is what the wizard shows.
type Status struct {
	Installed bool          `json:"installed"`
	Running   bool          `json:"running"`
	Steps     []StepState   `json:"steps"`
	Skills    []SkillResult `json:"skills"`
	// Administrator is the person who signs in first (no credential).
	Administrator *DirectoryUser `json:"administrator,omitempty"`
	PublicOrigin  string         `json:"public_origin,omitempty"`
}

// Config wires the service.
type Config struct {
	Database DatabaseAdmin
	Open     PlatformOpener
	SSO      Directory
	Store    ConfigStore
	Skills   SkillCatalog
	Logger   *slog.Logger
	Version  string
	// Secret returns a random secret; nil uses crypto/rand.
	Secret func() string
	Now    func() time.Time
}

// Service holds the wizard state. One installation at a time.
type Service struct {
	cfg Config

	mu        sync.Mutex
	installed bool
	run       *run
}

type run struct {
	plan        Plan
	logins      Logins
	integration Integration
	session     string
	steps       []StepState
	running     bool
	admin       DirectoryUser
	user        *DirectoryUser
	skills      []SkillResult
}

// NewService builds the service and reads the lock of the configuration volume.
func NewService(cfg Config) (*Service, error) {
	if cfg.Secret == nil {
		cfg.Secret = randomSecret
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	installed, err := cfg.Store.Installed()
	if err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, installed: installed}, nil
}

func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand never fails on Linux
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// State is getInstallation.
func (s *Service) State() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := Status{Installed: s.installed}
	if s.run != nil {
		status.Running = s.run.running
		status.Steps = append([]StepState(nil), s.run.steps...)
		status.Skills = append([]SkillResult(nil), s.run.skills...)
		status.PublicOrigin = s.run.plan.Organization.PublicOrigin
		if s.run.admin.ID != "" {
			admin := s.run.admin
			status.Administrator = &admin
		}
	}
	return status
}

// guard refuses every write after the lock and while Apply runs or has started.
func (s *Service) guard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.installed {
		return ErrInstalled
	}
	return nil
}

// ErrApplying refuses a check or a second Apply once the steps started (ESP-31: no way back after Install).
var ErrApplying = errors.New("the installation is already running")

func (s *Service) guardBeforeApply() error {
	if err := s.guard(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != nil {
		return ErrApplying
	}
	return nil
}

// CheckDatabase is the test of step 2 (ER-31-3).
func (s *Service) CheckDatabase(ctx context.Context, database Database) (DatabaseProbe, error) {
	if err := s.guardBeforeApply(); err != nil {
		return DatabaseProbe{}, err
	}
	if err := database.Validate(); err != nil {
		return DatabaseProbe{}, err
	}
	probe, err := s.cfg.Database.Probe(ctx, database)
	if err != nil {
		return DatabaseProbe{}, err
	}
	if probe.HasPlatformTables {
		return probe, Invalids{{"database.name", "has_platform_tables"}}
	}
	if probe.OtherPlatformDatabase != "" {
		return probe, Invalids{{"database.host", "server_has_platform"}}
	}
	if !probe.CanCreate {
		return probe, Invalids{{"database.admin_user", "cannot_create"}}
	}
	return probe, nil
}

// CheckOrganization is the test of step 3.
func (s *Service) CheckOrganization(organization Organization) error {
	if err := s.guardBeforeApply(); err != nil {
		return err
	}
	return organization.Validate()
}

// CheckSSO is the test of step 4 (ER-31-4, ER-31-5).
func (s *Service) CheckSSO(ctx context.Context, sso SSO, organization Organization) (SSOProbe, error) {
	if err := s.guardBeforeApply(); err != nil {
		return SSOProbe{}, err
	}
	if err := sso.Validate(); err != nil {
		return SSOProbe{}, err
	}
	probe, err := s.cfg.SSO.Probe(ctx, sso, organization)
	if err != nil {
		return SSOProbe{}, err
	}
	switch {
	case sso.Mode == ModeNewRealm && probe.RealmExists:
		return probe, Invalids{{"sso.realm", "realm_exists"}}
	case sso.Mode == ModeExistingRealm && !probe.RealmExists:
		return probe, Invalids{{"sso.realm", "realm_not_found"}}
	}
	return probe, nil
}

// SearchUsers finds people of an existing realm for steps 5 and 6.
func (s *Service) SearchUsers(ctx context.Context, sso SSO, query string) ([]DirectoryUser, error) {
	if err := s.guardBeforeApply(); err != nil {
		return nil, err
	}
	if err := sso.Validate(); err != nil {
		return nil, err
	}
	if sso.Mode != ModeExistingRealm {
		return nil, Invalids{{"sso.mode", "only_in_existing_realm"}}
	}
	return s.cfg.SSO.Search(ctx, sso, strings.TrimSpace(query))
}

// CheckPerson is the test of steps 5 and 6; field is administrator or user.
func (s *Service) CheckPerson(ctx context.Context, sso SSO, person Person, field string) error {
	if err := s.guardBeforeApply(); err != nil {
		return err
	}
	if field != "administrator" && field != "user" {
		return Invalids{{"field", "invalid"}}
	}
	if err := person.Validate(field, sso.Mode); err != nil {
		return err
	}
	if sso.Mode == ModeNewRealm {
		return nil // the realm does not exist yet: nothing can be taken
	}
	return s.cfg.SSO.CheckPerson(ctx, sso, person, field)
}

// NativeSkills lists the catalog that comes with the platform (RN-5).
func (s *Service) NativeSkills() ([]SkillSummary, error) { return s.cfg.Skills.Native() }

// CheckSkill is the test of each package of step 7 (US-55 CA-7).
func (s *Service) CheckSkill(pkg SkillPackage) (SkillSummary, error) {
	if err := s.guardBeforeApply(); err != nil {
		return SkillSummary{}, err
	}
	return s.cfg.Skills.Check(pkg)
}

// Apply starts the steps after the review (US-55 CA-8). It returns once they started; State follows them.
func (s *Service) Apply(ctx context.Context, plan Plan) error {
	if err := s.guardBeforeApply(); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	r := &run{
		plan:        plan,
		logins:      Logins{Migrator: s.cfg.Secret(), API: s.cfg.Secret(), Worker: s.cfg.Secret()},
		integration: Integration{APISecret: s.cfg.Secret(), EventsSecret: s.cfg.Secret()},
		session:     s.cfg.Secret() + s.cfg.Secret(),
	}
	for _, name := range StepOrder {
		r.steps = append(r.steps, StepState{Name: name, Status: StatusPending})
	}
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		return ErrApplying
	}
	s.run = r
	r.running = true
	s.mu.Unlock()
	go s.execute(context.WithoutCancel(ctx))
	return nil
}

// Retry runs again the failed step and the ones after it; done steps do not run again (CA-31-8).
func (s *Service) Retry(ctx context.Context) error {
	if err := s.guard(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.running {
		return ErrApplying
	}
	failed := false
	for _, step := range s.run.steps {
		failed = failed || step.Status == StatusFailed
	}
	if !failed {
		return ErrApplying
	}
	s.run.running = true
	go s.execute(context.WithoutCancel(ctx))
	return nil
}

func (s *Service) execute(ctx context.Context) {
	for i := range StepOrder {
		s.mu.Lock()
		step := s.run.steps[i]
		if step.Status == StatusDone || step.Status == StatusSkipped {
			s.mu.Unlock()
			continue
		}
		s.run.steps[i] = StepState{Name: step.Name, Status: StatusRunning}
		s.mu.Unlock()

		skipped, err := s.runStep(ctx, step.Name)

		s.mu.Lock()
		switch {
		case err != nil:
			s.run.steps[i] = StepState{Name: step.Name, Status: StatusFailed, Reason: reason(err)}
			s.run.running = false
			s.mu.Unlock()
			s.cfg.Logger.Warn("installation step failed", slog.String("step", step.Name), slog.String("reason", reason(err)))
			return
		case skipped:
			s.run.steps[i] = StepState{Name: step.Name, Status: StatusSkipped}
		default:
			s.run.steps[i] = StepState{Name: step.Name, Status: StatusDone}
		}
		s.mu.Unlock()
		s.cfg.Logger.Info("installation step finished", slog.String("step", step.Name))
	}
	s.mu.Lock()
	s.run.running = false
	s.installed = true
	s.mu.Unlock()
	s.cfg.Logger.Info("installation finished")
}

// reason keeps only messages that carry no secret: the adapters build their errors without
// credentials, and an Invalid gives its stable code.
func reason(err error) string {
	var many Invalids
	if errors.As(err, &many) && len(many) > 0 {
		return many[0].Field + ": " + many[0].Reason
	}
	var one Invalid
	if errors.As(err, &one) {
		return one.Field + ": " + one.Reason
	}
	return err.Error()
}

func (s *Service) runStep(ctx context.Context, name string) (bool, error) {
	r := s.snapshot()
	plan := r.plan
	switch name {
	case StepDatabaseCreate:
		return false, s.cfg.Database.Prepare(ctx, plan.Database, r.logins)
	case StepDatabaseMigrate:
		_, err := s.cfg.Database.Migrate(ctx, plan.Database, r.logins)
		return false, err
	case StepSSOConfigure:
		integration, err := s.cfg.SSO.Configure(ctx, plan.SSO, plan.Organization, r.integration)
		if err == nil {
			s.update(func(r *run) { r.integration = integration })
		}
		return false, err
	case StepSSOUsers:
		return false, s.ensurePeople(ctx, plan)
	case StepConfigWrite:
		return false, s.cfg.Store.Write(s.settings(r))
	case StepPlatformRegister:
		return false, s.withPlatform(ctx, r, func(p Platform) error { return p.Register(ctx, registration(r)) })
	case StepSkillsImport:
		if len(plan.Skills) == 0 {
			return true, nil
		}
		return false, s.withPlatform(ctx, r, func(p Platform) error {
			results, err := p.ImportSkills(ctx, registration(r), plan.Skills)
			s.update(func(r *run) { r.skills = results })
			return err
		})
	case StepLock:
		installation := Installation{
			InstalledAt: s.cfg.Now().UTC(), Mode: plan.SSO.Mode, Realm: plan.SSO.Realm,
			AdministratorSub: r.admin.ID, Version: s.cfg.Version,
		}
		if err := s.withPlatform(ctx, r, func(p Platform) error { return p.Lock(ctx, installation) }); err != nil {
			return false, err
		}
		return false, s.cfg.Store.Lock(installation)
	}
	return false, fmt.Errorf("unknown step %s", name)
}

func (s *Service) ensurePeople(ctx context.Context, plan Plan) error {
	admin, err := s.cfg.SSO.EnsurePerson(ctx, plan.SSO, plan.Administrator, true)
	if err != nil {
		return err
	}
	s.update(func(r *run) { r.admin = admin })
	if plan.User == nil {
		return nil
	}
	user, err := s.cfg.SSO.EnsurePerson(ctx, plan.SSO, *plan.User, false)
	if err != nil {
		return err
	}
	s.update(func(r *run) { r.user = &user })
	return nil
}

func (s *Service) withPlatform(ctx context.Context, r run, do func(Platform) error) error {
	platform, err := s.cfg.Open(ctx, r.plan.Database, r.logins)
	if err != nil {
		return err
	}
	defer platform.Close()
	return do(platform)
}

func (s *Service) snapshot() run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.run
}

func (s *Service) update(change func(*run)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s.run)
}

func registration(r run) Registration {
	return Registration{Organization: r.plan.Organization, Issuer: r.plan.SSO.Issuer(), Administrator: r.admin, User: r.user}
}

// Names of the secret files (config.SecretsDir) and the variables that point at them.
//
//nolint:gosec // G101: these are file names, not credentials.
const (
	SecretMigratorDatabaseURL = "migrator-database-url"
	SecretAPIDatabaseURL      = "api-database-url"
	SecretWorkerDatabaseURL   = "worker-database-url"
	SecretSession             = "session-secret"
	SecretOIDCClient          = "oidc-client-secret"
	SecretEventsClient        = "events-client-secret"
)

// settings are the files of the configuration directory. The database URL of each process is its own
// secret file, chosen by the compose service (EASYCODE_DATABASE_URL_FILE); the file names a default.
func (s *Service) settings(r run) Settings {
	plan := r.plan
	values := map[string]string{
		"EASYCODE_PUBLIC_ORIGIN":                 strings.TrimRight(plan.Organization.PublicOrigin, "/"),
		"EASYCODE_ORGANIZATION_NAME":             strings.TrimSpace(plan.Organization.Name),
		"EASYCODE_OIDC_ISSUER":                   plan.SSO.Issuer(),
		"EASYCODE_OIDC_CLIENT_ID":                APIClientID,
		"EASYCODE_OIDC_ADMINISTRATOR_ROLE":       AdministratorRole,
		"EASYCODE_SSO_EVENTS_CLIENT_ID":          EventsClientID,
		"EASYCODE_DATABASE_URL_FILE":             "secrets/" + SecretAPIDatabaseURL,
		"EASYCODE_MIGRATION_DATABASE_URL_FILE":   "secrets/" + SecretMigratorDatabaseURL,
		"EASYCODE_SESSION_SECRET_FILE":           "secrets/" + SecretSession,
		"EASYCODE_OIDC_CLIENT_SECRET_FILE":       "secrets/" + SecretOIDCClient,
		"EASYCODE_SSO_EVENTS_CLIENT_SECRET_FILE": "secrets/" + SecretEventsClient,
	}
	if internal := plan.SSO.InternalIssuer(); internal != "" {
		values["EASYCODE_OIDC_INTERNAL_URL"] = internal
	}
	db := s.cfg.Database
	return Settings{
		Values: values,
		Secrets: map[string]string{
			SecretMigratorDatabaseURL: db.URL(plan.Database, MigratorLogin, r.logins.Migrator),
			SecretAPIDatabaseURL:      db.URL(plan.Database, APILogin, r.logins.API),
			SecretWorkerDatabaseURL:   db.URL(plan.Database, WorkerLogin, r.logins.Worker),
			SecretSession:             r.session,
			SecretOIDCClient:          r.integration.APISecret,
			SecretEventsClient:        r.integration.EventsSecret,
		},
	}
}
