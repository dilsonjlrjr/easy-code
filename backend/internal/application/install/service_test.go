package install_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/application/install"
)

type recorder struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]int // step name -> failures left
}

func (r *recorder) call(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
	if r.fail[name] > 0 {
		r.fail[name]--
		return errors.New(name + " unavailable")
	}
	return nil
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c == name {
			n++
		}
	}
	return n
}

type fakeDatabase struct {
	rec   *recorder
	probe install.DatabaseProbe
}

func (f fakeDatabase) Probe(context.Context, install.Database) (install.DatabaseProbe, error) {
	return f.probe, nil
}
func (f fakeDatabase) Prepare(context.Context, install.Database, install.Logins) error {
	return f.rec.call(install.StepDatabaseCreate)
}
func (f fakeDatabase) Migrate(context.Context, install.Database, install.Logins) (int, error) {
	return 17, f.rec.call(install.StepDatabaseMigrate)
}
func (fakeDatabase) URL(_ install.Database, user, password string) string {
	return "postgres://" + user + ":" + password + "@db/easycode"
}

type fakePlatform struct{ rec *recorder }

func (f fakePlatform) Register(context.Context, install.Registration) error {
	return f.rec.call(install.StepPlatformRegister)
}
func (f fakePlatform) ImportSkills(_ context.Context, _ install.Registration, packages []install.SkillPackage) ([]install.SkillResult, error) {
	results := make([]install.SkillResult, len(packages))
	for i, p := range packages {
		results[i] = install.SkillResult{FileName: p.FileName, Status: install.SkillReceived}
	}
	return results, f.rec.call(install.StepSkillsImport)
}
func (f fakePlatform) Lock(context.Context, install.Installation) error {
	return f.rec.call("platform_lock")
}
func (fakePlatform) Close() {}

type fakeDirectory struct {
	rec   *recorder
	probe install.SSOProbe
}

func (f fakeDirectory) Probe(context.Context, install.SSO, install.Organization) (install.SSOProbe, error) {
	return f.probe, nil
}
func (fakeDirectory) Search(context.Context, install.SSO, string) ([]install.DirectoryUser, error) {
	return nil, nil
}
func (fakeDirectory) CheckPerson(context.Context, install.SSO, install.Person, string) error {
	return nil
}
func (f fakeDirectory) Configure(_ context.Context, _ install.SSO, _ install.Organization, in install.Integration) (install.Integration, error) {
	return in, f.rec.call(install.StepSSOConfigure)
}
func (f fakeDirectory) EnsurePerson(_ context.Context, _ install.SSO, p install.Person, _ bool) (install.DirectoryUser, error) {
	return install.DirectoryUser{ID: "id-" + p.Username, Username: p.Username}, f.rec.call(install.StepSSOUsers)
}

type fakeStore struct {
	rec       *recorder
	mu        sync.Mutex
	installed bool
	settings  install.Settings
}

func (f *fakeStore) Installed() (bool, error) { return f.installed, nil }
func (f *fakeStore) Write(s install.Settings) error {
	f.mu.Lock()
	f.settings = s
	f.mu.Unlock()
	return f.rec.call(install.StepConfigWrite)
}
func (f *fakeStore) Lock(install.Installation) error {
	f.installed = true
	return f.rec.call(install.StepLock)
}

type fakeSkills struct{}

func (fakeSkills) Native() ([]install.SkillSummary, error) { return nil, nil }
func (fakeSkills) Check(install.SkillPackage) (install.SkillSummary, error) {
	return install.SkillSummary{Name: "x"}, nil
}

func newService(t *testing.T, rec *recorder, store *fakeStore) *install.Service {
	t.Helper()
	n := 0
	svc, err := install.NewService(install.Config{
		Database: fakeDatabase{rec: rec, probe: install.DatabaseProbe{CanCreate: true}},
		Open: func(context.Context, install.Database, install.Logins) (install.Platform, error) {
			return fakePlatform{rec: rec}, nil
		},
		SSO:     fakeDirectory{rec: rec},
		Store:   store,
		Skills:  fakeSkills{},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version: "test",
		Secret: func() string {
			n++
			return "generated-secret-" + strings.Repeat("x", n)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func validPlan() install.Plan {
	return install.Plan{
		Database:      install.Database{Host: "postgres", Port: 5432, AdminUser: "postgres", AdminPassword: "db-admin-password", Name: "easycode", SSLMode: "disable"},
		Organization:  install.Organization{Name: "Empresa", Currency: "BRL", PublicOrigin: "https://easycode.example", Locale: "pt-BR"},
		SSO:           install.SSO{PublicURL: "https://sso.example/auth", Mode: install.ModeNewRealm, Realm: "easycode", AdminUser: "admin", AdminPassword: "kc-admin-password"},
		Administrator: install.Person{Username: "ana", Email: "ana@example.com", FirstName: "Ana", Password: "temporary-password-1"},
	}
}

func waitIdle(t *testing.T, svc *install.Service) install.Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if state := svc.State(); !state.Running {
			return state
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("installation did not stop")
	return install.Status{}
}

func TestApplyRunsTheStepsInOrderAndLocks(t *testing.T) {
	rec := &recorder{}
	store := &fakeStore{rec: rec}
	svc := newService(t, rec, store)
	if err := svc.Apply(context.Background(), validPlan()); err != nil {
		t.Fatal(err)
	}
	state := waitIdle(t, svc)
	if !state.Installed {
		t.Fatalf("not installed: %+v", state.Steps)
	}
	want := []string{install.StepDatabaseCreate, install.StepDatabaseMigrate, install.StepSSOConfigure, install.StepSSOUsers,
		install.StepConfigWrite, install.StepPlatformRegister, "platform_lock", install.StepLock}
	if strings.Join(rec.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v", rec.calls)
	}
	for _, step := range state.Steps {
		wantStatus := install.StatusDone
		if step.Name == install.StepSkillsImport {
			wantStatus = install.StatusSkipped
		}
		if step.Status != wantStatus {
			t.Fatalf("%s = %s", step.Name, step.Status)
		}
	}
	if state.Administrator == nil || state.Administrator.ID != "id-ana" {
		t.Fatalf("administrator = %+v", state.Administrator)
	}
	// After the lock every write answers "already installed" (CA-31-9).
	if err := svc.Apply(context.Background(), validPlan()); !errors.Is(err, install.ErrInstalled) {
		t.Fatalf("Apply after lock: %v", err)
	}
	if _, err := svc.CheckDatabase(context.Background(), validPlan().Database); !errors.Is(err, install.ErrInstalled) {
		t.Fatalf("CheckDatabase after lock: %v", err)
	}
}

func TestSettingsCarryNoCredentialOfTheWizard(t *testing.T) {
	rec := &recorder{}
	store := &fakeStore{rec: rec}
	svc := newService(t, rec, store)
	if err := svc.Apply(context.Background(), validPlan()); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, svc)
	all := ""
	for k, v := range store.settings.Values {
		all += k + "=" + v + "\n"
	}
	for k, v := range store.settings.Secrets {
		all += k + "=" + v + "\n"
	}
	for _, secret := range []string{"db-admin-password", "kc-admin-password", "temporary-password-1"} {
		if strings.Contains(all, secret) {
			t.Fatalf("settings carry %q (ER-31-6)", secret)
		}
	}
	if store.settings.Values["EASYCODE_OIDC_ISSUER"] != "https://sso.example/auth/realms/easycode" {
		t.Fatalf("issuer = %q", store.settings.Values["EASYCODE_OIDC_ISSUER"])
	}
	if !strings.HasPrefix(store.settings.Secrets[install.SecretAPIDatabaseURL], "postgres://easycode_api_login:") {
		t.Fatalf("api url = %q", store.settings.Secrets[install.SecretAPIDatabaseURL])
	}
}

func TestRetryRunsOnlyTheFailedStepAndTheNextOnes(t *testing.T) {
	rec := &recorder{fail: map[string]int{install.StepSSOConfigure: 1}}
	store := &fakeStore{rec: rec}
	svc := newService(t, rec, store)
	if err := svc.Apply(context.Background(), validPlan()); err != nil {
		t.Fatal(err)
	}
	state := waitIdle(t, svc)
	if state.Installed {
		t.Fatal("installed despite the failure")
	}
	if state.Steps[2].Status != install.StatusFailed || state.Steps[2].Reason != "sso_configure unavailable" || state.Steps[3].Status != install.StatusPending {
		t.Fatalf("steps = %+v", state.Steps)
	}
	if err := svc.Apply(context.Background(), validPlan()); !errors.Is(err, install.ErrApplying) {
		t.Fatalf("second Apply: %v", err)
	}
	if err := svc.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state = waitIdle(t, svc); !state.Installed {
		t.Fatalf("not installed after retry: %+v", state.Steps)
	}
	if rec.count(install.StepDatabaseCreate) != 1 || rec.count(install.StepDatabaseMigrate) != 1 || rec.count(install.StepSSOConfigure) != 2 {
		t.Fatalf("calls = %v", rec.calls)
	}
}

func TestSkillsAreImportedWhenSent(t *testing.T) {
	rec := &recorder{}
	svc := newService(t, rec, &fakeStore{rec: rec})
	plan := validPlan()
	plan.Skills = []install.SkillPackage{{FileName: "padrao.zip", Content: []byte("zip")}}
	if err := svc.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	state := waitIdle(t, svc)
	if len(state.Skills) != 1 || state.Skills[0].Status != install.SkillReceived || rec.count(install.StepSkillsImport) != 1 {
		t.Fatalf("skills = %+v, calls = %v", state.Skills, rec.calls)
	}
}

func TestCheckDatabaseRefusesASchemaAlreadyThereAndAWeakCredential(t *testing.T) {
	rec := &recorder{}
	for _, tc := range []struct {
		probe  install.DatabaseProbe
		reason string
	}{
		{install.DatabaseProbe{CanCreate: true, Exists: true, HasPlatformTables: true}, "has_platform_tables"},
		{install.DatabaseProbe{CanCreate: false}, "cannot_create"},
		{install.DatabaseProbe{CanCreate: true, OtherPlatformDatabase: "easycode"}, "server_has_platform"},
	} {
		svc, err := install.NewService(install.Config{
			Database: fakeDatabase{rec: rec, probe: tc.probe}, Store: &fakeStore{rec: rec},
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CheckDatabase(context.Background(), validPlan().Database)
		var invalid install.Invalids
		if !errors.As(err, &invalid) || invalid[0].Reason != tc.reason {
			t.Fatalf("want %s, got %v", tc.reason, err)
		}
	}
}

func TestCheckSSORefusesTheWrongRealmState(t *testing.T) {
	rec := &recorder{}
	svc, err := install.NewService(install.Config{
		SSO: fakeDirectory{rec: rec, probe: install.SSOProbe{RealmExists: true}}, Store: &fakeStore{rec: rec},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	_, err = svc.CheckSSO(context.Background(), plan.SSO, plan.Organization)
	var invalid install.Invalids
	if !errors.As(err, &invalid) || invalid[0].Reason != "realm_exists" {
		t.Fatalf("got %v", err)
	}
	plan.SSO.Mode = install.ModeExistingRealm
	if _, err := svc.CheckSSO(context.Background(), plan.SSO, plan.Organization); err != nil {
		t.Fatal(err)
	}
}

func TestPlanValidation(t *testing.T) {
	plan := validPlan()
	plan.Organization.PublicOrigin = "ftp://x"
	plan.SSO.Realm = "master"
	plan.Administrator.Password = "short"
	plan.Administrator.ExistingID = ""
	user := plan.Administrator
	plan.User = &user
	var invalid install.Invalids
	if err := plan.Validate(); !errors.As(err, &invalid) {
		t.Fatalf("got %v", err)
	}
	fields := map[string]bool{}
	for _, item := range invalid {
		fields[item.Field] = true
	}
	for _, want := range []string{"organization.public_origin", "sso.realm", "administrator.password", "user.username"} {
		if !fields[want] {
			t.Fatalf("missing %s in %v", want, invalid)
		}
	}
	existing := validPlan()
	existing.Administrator = install.Person{ExistingID: "abc"}
	if err := existing.Validate(); err == nil {
		t.Fatal("existing user accepted in a new realm")
	}
}

func TestInstalledVolumeRefusesEverything(t *testing.T) {
	rec := &recorder{}
	svc := newService(t, rec, &fakeStore{rec: rec, installed: true})
	if !svc.State().Installed {
		t.Fatal("state not installed")
	}
	if err := svc.Apply(context.Background(), validPlan()); !errors.Is(err, install.ErrInstalled) {
		t.Fatalf("got %v", err)
	}
}
