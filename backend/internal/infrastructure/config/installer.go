package config

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Variables of easycode-installer (ADR-22). The EASYCODE_INSTALLER_DEFAULT_* values only prefill the
// wizard (the bundled PostgreSQL and Keycloak of install/compose.yaml); the person may change them.
const (
	EnvInstallerAddr       = "EASYCODE_INSTALLER_ADDR"
	EnvInstallerConfigDir  = "EASYCODE_INSTALLER_CONFIG_DIR"
	EnvInstallerWebDir     = "EASYCODE_INSTALLER_WEB_DIR"
	EnvInstallerPublicURL  = "EASYCODE_INSTALLER_PUBLIC_URL"
	EnvInstallerBundled    = "EASYCODE_INSTALLER_BUNDLED"
	EnvInstallerCheckTime  = "EASYCODE_INSTALLER_CHECK_TIMEOUT"
	EnvDefaultDatabaseHost = "EASYCODE_INSTALLER_DEFAULT_DATABASE_HOST"
	EnvDefaultDatabasePort = "EASYCODE_INSTALLER_DEFAULT_DATABASE_PORT"
	EnvDefaultDatabaseUser = "EASYCODE_INSTALLER_DEFAULT_DATABASE_USER"
	EnvDefaultDatabasePass = "EASYCODE_INSTALLER_DEFAULT_DATABASE_PASSWORD"
	EnvDefaultSSOPublicURL = "EASYCODE_INSTALLER_DEFAULT_SSO_PUBLIC_URL"
	EnvDefaultSSOInternal  = "EASYCODE_INSTALLER_DEFAULT_SSO_INTERNAL_URL"
	EnvDefaultSSOAdminUser = "EASYCODE_INSTALLER_DEFAULT_SSO_ADMIN_USER"
	EnvDefaultSSOAdminPass = "EASYCODE_INSTALLER_DEFAULT_SSO_ADMIN_PASSWORD"
	EnvDefaultPublicOrigin = "EASYCODE_INSTALLER_DEFAULT_PUBLIC_ORIGIN"
	EnvDefaultOrganization = "EASYCODE_INSTALLER_DEFAULT_ORGANIZATION_NAME"
)

// Installer configures easycode-installer.
type Installer struct {
	Addr      string
	ConfigDir string
	WebDir    string
	// PublicURL is the address the person opens, shown next to the token ("" shows only the port).
	PublicURL    string
	Bundled      bool
	CheckTimeout time.Duration
	Defaults     InstallerDefaults
}

// InstallerDefaults prefill the wizard.
type InstallerDefaults struct {
	DatabaseHost, DatabaseUser, DatabasePassword string
	DatabasePort                                 int
	SSOPublicURL, SSOInternalURL                 string
	SSOAdminUser, SSOAdminPassword               string
	PublicOrigin, OrganizationName               string
}

// LoadInstaller reads the installer variables.
func LoadInstaller(lookup LookupFunc) (Installer, error) {
	r := reader{lookup: lookup}
	port := r.integer(EnvDefaultDatabasePort, 5432)
	bundled := r.boolean(EnvInstallerBundled, false)
	cfg := Installer{
		Addr:         r.text(EnvInstallerAddr, ":8080"),
		ConfigDir:    r.text(EnvInstallerConfigDir, "/etc/easycode"),
		WebDir:       r.text(EnvInstallerWebDir, ""),
		PublicURL:    strings.TrimRight(r.text(EnvInstallerPublicURL, ""), "/"),
		Bundled:      bundled,
		CheckTimeout: r.duration(EnvInstallerCheckTime, 20*time.Second),
		Defaults: InstallerDefaults{
			DatabaseHost: r.text(EnvDefaultDatabaseHost, ""), DatabasePort: port,
			DatabaseUser: r.text(EnvDefaultDatabaseUser, ""), DatabasePassword: r.secret(EnvDefaultDatabasePass),
			SSOPublicURL: r.text(EnvDefaultSSOPublicURL, ""), SSOInternalURL: r.text(EnvDefaultSSOInternal, ""),
			SSOAdminUser: r.text(EnvDefaultSSOAdminUser, ""), SSOAdminPassword: r.secret(EnvDefaultSSOAdminPass),
			PublicOrigin: r.text(EnvDefaultPublicOrigin, ""), OrganizationName: r.text(EnvDefaultOrganization, ""),
		},
	}
	if port > 65535 {
		r.fail(EnvDefaultDatabasePort, strconv.Itoa(port), errors.New("must be a port"))
	}
	return cfg, errors.Join(r.problems...)
}
