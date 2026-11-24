// Package install holds the use cases of the one-time installation wizard (US-55, ESP-31, ADR-22):
// the checks of each step, which only read, and the ordered, idempotent steps that run after the
// review. Passwords and secrets entered in the wizard stay in memory: they never go to a log, an
// error message, the audit trail or the summary (US-55 RN-3).
package install

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

// SSO modes (platform_installation.sso_mode).
const (
	ModeNewRealm      = "new_realm"
	ModeExistingRealm = "existing_realm"
)

// Fixed names of the realm integration (ADR-12).
const (
	AdministratorRole = "easycode-administrator"
	APIClientID       = "easycode-api"
	EventsClientID    = "easycode-events"
	DesktopClientID   = "easycode-desktop"
)

// Database is the PostgreSQL server and the administrator credential used only during the install.
type Database struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
	Name          string `json:"name"`
	// SSLMode is a libpq sslmode: disable, require or verify-full.
	SSLMode string `json:"ssl_mode"`
}

// Organization is the organization of the realm and the public address of the web.
type Organization struct {
	Name         string `json:"name"`
	Currency     string `json:"currency"`
	PublicOrigin string `json:"public_origin"`
	Locale       string `json:"locale"`
}

// SSO is the Keycloak server and the credential used only during the install: of the master realm
// (new realm) or of an account with realm-management in the existing realm.
type SSO struct {
	PublicURL     string `json:"public_url"`
	InternalURL   string `json:"internal_url"`
	Mode          string `json:"mode"`
	Realm         string `json:"realm"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
}

// Person is a person of the realm: a new user, or an existing user (ExistingID) of an existing realm.
type Person struct {
	ExistingID string `json:"existing_id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	// Password is temporary: the SSO asks for a new one at the first login (ER-31-6).
	Password string `json:"password"`
}

// DisplayName is how the platform names the person.
func (p Person) DisplayName() string {
	name := strings.TrimSpace(strings.TrimSpace(p.FirstName) + " " + strings.TrimSpace(p.LastName))
	if name == "" {
		return strings.TrimSpace(p.Username)
	}
	return name
}

// SkillPackage is a zipped organization skill (US-44).
type SkillPackage struct {
	FileName string `json:"file_name"`
	Content  []byte `json:"content"`
}

// Plan is everything the wizard collected; nothing is written before Apply (US-55 RN-2).
type Plan struct {
	Database      Database       `json:"database"`
	Organization  Organization   `json:"organization"`
	SSO           SSO            `json:"sso"`
	Administrator Person         `json:"administrator"`
	User          *Person        `json:"user"`
	Skills        []SkillPackage `json:"skills"`
}

// Issuer is the public issuer of the realm.
func (s SSO) Issuer() string { return strings.TrimRight(s.PublicURL, "/") + "/realms/" + s.Realm }

// InternalIssuer is the realm as reached from the containers ("" = Issuer).
func (s SSO) InternalIssuer() string {
	if strings.TrimSpace(s.InternalURL) == "" {
		return ""
	}
	return strings.TrimRight(s.InternalURL, "/") + "/realms/" + s.Realm
}

// AdminURL is where the installer reaches the Keycloak admin API.
func (s SSO) AdminURL() string {
	if strings.TrimSpace(s.InternalURL) != "" {
		return strings.TrimRight(s.InternalURL, "/")
	}
	return strings.TrimRight(s.PublicURL, "/")
}

// Invalid is a field the person must fix; Field names the input, Reason is a stable code the
// interface translates.
type Invalid struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (i Invalid) Error() string { return fmt.Sprintf("invalid %s: %s", i.Field, i.Reason) }

// Invalids joins the problems of one step.
type Invalids []Invalid

func (i Invalids) Error() string {
	parts := make([]string, len(i))
	for n, item := range i {
		parts[n] = item.Error()
	}
	return strings.Join(parts, "; ")
}

func (i Invalids) err() error {
	if len(i) == 0 {
		return nil
	}
	return i
}

var (
	identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	realmName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	username   = regexp.MustCompile(`^[a-z0-9][a-z0-9._@-]{1,254}$`)
	currency   = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Validate checks the database fields.
func (d Database) Validate() error {
	var problems Invalids
	if strings.TrimSpace(d.Host) == "" {
		problems = append(problems, Invalid{"database.host", "required"})
	}
	if d.Port < 1 || d.Port > 65535 {
		problems = append(problems, Invalid{"database.port", "out_of_range"})
	}
	if strings.TrimSpace(d.AdminUser) == "" {
		problems = append(problems, Invalid{"database.admin_user", "required"})
	}
	if !identifier.MatchString(d.Name) {
		problems = append(problems, Invalid{"database.name", "invalid_identifier"})
	}
	switch d.SSLMode {
	case "disable", "require", "verify-full":
	default:
		problems = append(problems, Invalid{"database.ssl_mode", "invalid"})
	}
	return problems.err()
}

// Validate checks the organization fields.
func (o Organization) Validate() error {
	var problems Invalids
	if name := strings.TrimSpace(o.Name); name == "" || len(name) > 120 {
		problems = append(problems, Invalid{"organization.name", "required"})
	}
	if !currency.MatchString(o.Currency) {
		problems = append(problems, Invalid{"organization.currency", "invalid"})
	}
	if !httpURL(o.PublicOrigin, false) {
		problems = append(problems, Invalid{"organization.public_origin", "invalid_url"})
	}
	if o.Locale != "pt-BR" && o.Locale != "en" {
		problems = append(problems, Invalid{"organization.locale", "invalid"})
	}
	return problems.err()
}

// Validate checks the SSO fields.
func (s SSO) Validate() error {
	var problems Invalids
	if !httpURL(s.PublicURL, true) {
		problems = append(problems, Invalid{"sso.public_url", "invalid_url"})
	}
	if strings.TrimSpace(s.InternalURL) != "" && !httpURL(s.InternalURL, true) {
		problems = append(problems, Invalid{"sso.internal_url", "invalid_url"})
	}
	if s.Mode != ModeNewRealm && s.Mode != ModeExistingRealm {
		problems = append(problems, Invalid{"sso.mode", "invalid"})
	}
	if !realmName.MatchString(s.Realm) || s.Realm == "master" {
		problems = append(problems, Invalid{"sso.realm", "invalid"})
	}
	if strings.TrimSpace(s.AdminUser) == "" {
		problems = append(problems, Invalid{"sso.admin_user", "required"})
	}
	if s.AdminPassword == "" {
		problems = append(problems, Invalid{"sso.admin_password", "required"})
	}
	return problems.err()
}

// Validate checks a person; field prefixes the field names (administrator or user).
func (p Person) Validate(field string, mode string) error {
	if p.ExistingID != "" {
		if mode != ModeExistingRealm {
			return Invalids{{field + ".existing_id", "only_in_existing_realm"}}
		}
		return nil
	}
	var problems Invalids
	if !username.MatchString(p.Username) {
		problems = append(problems, Invalid{field + ".username", "invalid"})
	}
	if address, err := mail.ParseAddress(p.Email); err != nil || address.Address != p.Email {
		problems = append(problems, Invalid{field + ".email", "invalid"})
	}
	if strings.TrimSpace(p.FirstName) == "" {
		problems = append(problems, Invalid{field + ".first_name", "required"})
	}
	if len(p.Password) < 12 {
		problems = append(problems, Invalid{field + ".password", "too_short"})
	}
	return problems.err()
}

// Validate checks the whole plan, as the review does before Apply.
func (p Plan) Validate() error {
	var problems Invalids
	collect := func(err error) {
		var many Invalids
		if errors.As(err, &many) {
			problems = append(problems, many...)
		}
	}
	collect(p.Database.Validate())
	collect(p.Organization.Validate())
	collect(p.SSO.Validate())
	collect(p.Administrator.Validate("administrator", p.SSO.Mode))
	if p.User != nil {
		collect(p.User.Validate("user", p.SSO.Mode))
		if p.User.ExistingID == "" && p.Administrator.ExistingID == "" &&
			(p.User.Username == p.Administrator.Username || strings.EqualFold(p.User.Email, p.Administrator.Email)) {
			problems = append(problems, Invalid{"user.username", "same_as_administrator"})
		}
	}
	return problems.err()
}

func httpURL(raw string, allowPath bool) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return false
	}
	return allowPath || parsed.Path == "" || parsed.Path == "/"
}

// ErrInstalled is returned by every write once the lock exists (US-55 CA-9, ER-31-10).
var ErrInstalled = errors.New("the platform is already installed")

// Steps of Apply, in order (ESP-31 section 3).
const (
	StepDatabaseCreate   = "database_create"
	StepDatabaseMigrate  = "database_migrate"
	StepSSOConfigure     = "sso_configure"
	StepSSOUsers         = "sso_users"
	StepConfigWrite      = "config_write"
	StepPlatformRegister = "platform_register"
	StepSkillsImport     = "skills_import"
	StepLock             = "lock"
)

// StepOrder is the order of Apply.
var StepOrder = []string{
	StepDatabaseCreate, StepDatabaseMigrate, StepSSOConfigure, StepSSOUsers,
	StepConfigWrite, StepPlatformRegister, StepSkillsImport, StepLock,
}
