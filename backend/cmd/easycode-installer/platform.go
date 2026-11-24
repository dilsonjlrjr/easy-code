package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"easycode/internal/application/install"
	appskill "easycode/internal/application/skill"
	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/specs"
)

// platform implements install.Platform over the migrated database, as the migrator.
type platform struct {
	pool         *pgxpool.Pool
	installation postgres.Installation
	importer     *appskill.Importer
}

func openPlatform(logger *slog.Logger) install.PlatformOpener {
	return func(ctx context.Context, database install.Database, logins install.Logins) (install.Platform, error) {
		url := postgres.InstallDatabase{}.URL(database, install.MigratorLogin, logins.Migrator)
		pool, err := postgres.NewPool(ctx, postgres.PoolSettings{URL: url, MaxConns: 2}, logger)
		if err != nil {
			return nil, errors.New("connect as the migrator failed")
		}
		trail := postgres.NewAuditTrail(pool)
		return &platform{
			pool:         pool,
			installation: postgres.NewInstallation(pool),
			importer: appskill.NewImporter(postgres.NewTxRunner(pool), postgres.NewSkillImports(pool), postgres.NewSkills(pool),
				trail, specs.YAMLToJSON),
		}, nil
	}
}

func (p *platform) Register(ctx context.Context, registration install.Registration) error {
	_, err := p.installation.Register(ctx, registration)
	return err
}

// ImportSkills sends each package through the import of US-44; the worker scans them when the platform
// starts. A package already received by an earlier try (same content) is not sent again.
func (p *platform) ImportSkills(ctx context.Context, registration install.Registration, packages []install.SkillPackage) ([]install.SkillResult, error) {
	principal, err := p.installation.Principal(ctx, registration.Issuer, registration.Administrator.ID)
	if err != nil {
		return nil, err
	}
	results := make([]install.SkillResult, 0, len(packages))
	for _, pkg := range packages {
		sum := sha256.Sum256(pkg.Content)
		key := "install-" + hex.EncodeToString(sum[:16])
		received, err := p.installation.ImportReceived(ctx, principal, key)
		if err != nil {
			return results, err
		}
		if received {
			results = append(results, install.SkillResult{FileName: pkg.FileName, Status: install.SkillReceived})
			continue
		}
		imported, err := p.importer.Upload(ctx, principal, pkg.Content, key)
		var typed *failure.Error
		switch {
		case err == nil:
			results = append(results, install.SkillResult{FileName: pkg.FileName, Name: imported.Skill, Status: install.SkillReceived})
		case errors.As(err, &typed) && typed.Kind == failure.KindInvalidInput:
			reason := typed.Code
			if r, ok := typed.Extensions["reason"].(string); ok {
				reason = r
			}
			results = append(results, install.SkillResult{FileName: pkg.FileName, Status: install.SkillRefused, Reason: reason})
		default:
			return results, err
		}
	}
	return results, nil
}

func (p *platform) Lock(ctx context.Context, installation install.Installation) error {
	return p.installation.Lock(ctx, installation)
}

func (p *platform) Close() { p.pool.Close() }
