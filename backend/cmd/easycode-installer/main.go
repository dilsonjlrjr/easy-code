// Command easycode-installer is the one-time installation wizard (US-55, ESP-31, ADR-22). It prints a
// one-time token, serves the wizard until the installation is locked and the person closes it, and
// stops. "easycode-installer status" exits 0 when the configuration volume is locked, 3 otherwise.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"easycode/internal/application/install"
	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/installconfig"
	"easycode/internal/infrastructure/keycloakadmin"
	"easycode/internal/infrastructure/logging"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/process"
	"easycode/internal/infrastructure/specs"
	"easycode/internal/interfaces/installer"
	"easycode/migrations"
)

const serviceName = "easycode-installer"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// exitNotInstalled is the status of "status" while the volume has no lock.
const exitNotInstalled = 3

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		os.Exit(1)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	logger := logging.New(os.Stdout, slog.LevelInfo, serviceName, version)
	cfg, err := config.LoadInstaller(os.LookupEnv)
	if err != nil {
		logger.Error("invalid configuration", logging.ErrorAttr(err))
		return 1, err
	}
	store := installconfig.New(cfg.ConfigDir)
	if len(args) > 0 && args[0] == "status" {
		installed, err := store.Installed()
		if err != nil {
			logger.Error("cannot read the configuration volume", logging.ErrorAttr(err))
			return 1, err
		}
		if installed {
			fmt.Println("installed")
			return 0, nil
		}
		fmt.Println("not installed")
		return exitNotInstalled, nil
	}
	if len(args) > 0 {
		err := fmt.Errorf("unknown command %q (use status or no command)", args[0])
		logger.Error("invalid command", logging.ErrorAttr(err))
		return 1, err
	}
	if err := store.Writable(); err != nil {
		logger.Error("configuration volume", logging.ErrorAttr(err))
		return 1, err
	}
	return 0, serve(cfg, store, logger)
}

func serve(cfg config.Installer, store installconfig.Store, logger *slog.Logger) error {
	ctx, stop := process.SignalContext()
	defer stop()
	ctx, finish := context.WithCancel(ctx)
	defer finish()

	service, err := install.NewService(install.Config{
		Database: postgres.InstallDatabase{Migrations: migrations.Files(), Logger: logger},
		Open:     openPlatform(logger),
		SSO:      keycloakadmin.New(cfg.CheckTimeout),
		Store:    store,
		Skills:   specs.InstallCatalog{},
		Logger:   logger,
		Version:  version,
	})
	if err != nil {
		logger.Error("cannot read the installation lock", logging.ErrorAttr(err))
		return err
	}
	token := newToken()
	handler := installer.NewHandler(installer.Settings{
		UseCases: service, Logger: logger, Token: token, Version: version, Defaults: defaults(cfg),
		WebDir: cfg.WebDir, ConfigWritable: store.Writable, Finish: finish, Timeout: cfg.CheckTimeout,
	})
	server := &fasthttp.Server{
		Handler: handler, Name: serviceName, MaxRequestBodySize: installer.MaxBodyBytes,
		ReadTimeout: time.Minute, WriteTimeout: time.Minute, IdleTimeout: 2 * time.Minute,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe(cfg.Addr) }()
	banner(cfg, token, service.State().Installed)
	logger.Info("installer listening", slog.String("addr", cfg.Addr))

	select {
	case err := <-errs:
		logger.Error("installer stopped", logging.ErrorAttr(err))
		return err
	case <-ctx.Done():
	}
	// A short pause lets the last answer reach the browser.
	time.Sleep(500 * time.Millisecond)
	if err := server.ShutdownWithContext(context.Background()); err != nil {
		return err
	}
	if !service.State().Installed {
		logger.Warn("installer stopped before the installation finished")
		return errors.New("installation not finished")
	}
	logger.Info("installer finished")
	return nil
}

// newToken is the one-time installation token: 128 random bits, in groups for reading aloud.
func newToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	raw := hex.EncodeToString(buf)
	return strings.Join([]string{raw[0:8], raw[8:16], raw[16:24], raw[24:32]}, "-")
}

// banner prints the address and the token outside the JSON log (the logger redacts tokens).
func banner(cfg config.Installer, token string, installed bool) {
	address := cfg.PublicURL
	if address == "" {
		address = "http://<this server>" + cfg.Addr
	}
	line := strings.Repeat("=", 64)
	if installed {
		fmt.Printf("\n%s\n Easy Code is already installed. The wizard accepts no change.\n %s\n%s\n\n", line, address, line)
		return
	}
	fmt.Printf("\n%s\n Easy Code installation wizard\n\n   Open:   %s\n   Token:  %s\n\n The token works until the installation finishes.\n%s\n\n",
		line, address, token, line)
}

func defaults(cfg config.Installer) installer.Defaults {
	d := cfg.Defaults
	out := installer.Defaults{
		Bundled:      cfg.Bundled,
		Database:     install.Database{Host: d.DatabaseHost, Port: d.DatabasePort, AdminUser: d.DatabaseUser, AdminPassword: d.DatabasePassword, Name: "easycode", SSLMode: "disable"},
		SSO:          install.SSO{PublicURL: d.SSOPublicURL, InternalURL: d.SSOInternalURL, Mode: install.ModeNewRealm, Realm: "easycode", AdminUser: d.SSOAdminUser, AdminPassword: d.SSOAdminPassword},
		Organization: install.Organization{Name: d.OrganizationName, Currency: "BRL", PublicOrigin: d.PublicOrigin, Locale: "pt-BR"},
	}
	if !cfg.Bundled {
		// External servers: nothing of the bundled services is a good guess.
		out.Database = install.Database{Port: 5432, Name: "easycode", SSLMode: "require"}
		out.SSO = install.SSO{Mode: install.ModeNewRealm, Realm: "easycode"}
	}
	return out
}
