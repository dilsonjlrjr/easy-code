package installconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/application/install"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/installconfig"
)

func TestWrittenDirectoryIsReadByTheProcesses(t *testing.T) {
	dir := t.TempDir()
	store := installconfig.New(dir)
	if err := store.Writable(); err != nil {
		t.Fatal(err)
	}
	err := store.Write(install.Settings{
		Values:  map[string]string{"EASYCODE_OIDC_ISSUER": "https://sso.example/auth/realms/easycode", "EASYCODE_SESSION_SECRET_FILE": "secrets/session-secret"},
		Secrets: map[string]string{"session-secret": strings.Repeat("k", 43)},
	})
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := config.Environment(func(key string) (string, bool) {
		if key == config.EnvConfigDir {
			return dir, true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identity.Issuer != "https://sso.example/auth/realms/easycode" || len(cfg.Identity.SessionSecret) != 43 {
		t.Fatalf("identity = %+v", cfg.Identity)
	}
	info, err := os.Stat(filepath.Join(dir, config.SecretsDir, "session-secret"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file mode = %v, %v", info, err)
	}
	env, _ := os.ReadFile(filepath.Join(dir, config.FileName))
	if strings.Contains(string(env), strings.Repeat("k", 43)) {
		t.Fatal("secret written into easycode.env")
	}
}

func TestLock(t *testing.T) {
	store := installconfig.New(t.TempDir())
	if installed, err := store.Installed(); err != nil || installed {
		t.Fatalf("installed = %v, %v", installed, err)
	}
	if err := store.Lock(install.Installation{InstalledAt: time.Now(), Mode: install.ModeNewRealm, Realm: "easycode", AdministratorSub: "sub", Version: "v"}); err != nil {
		t.Fatal(err)
	}
	if installed, err := store.Installed(); err != nil || !installed {
		t.Fatalf("installed = %v, %v", installed, err)
	}
}

func TestSecretNameCannotLeaveTheDirectory(t *testing.T) {
	if err := installconfig.New(t.TempDir()).Write(install.Settings{Secrets: map[string]string{"../x": "v"}}); err == nil {
		t.Fatal("expected an error")
	}
}
