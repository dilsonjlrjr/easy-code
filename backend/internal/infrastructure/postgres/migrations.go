package postgres

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// noTransactionMarker lets a migration run outside a transaction, for CREATE INDEX CONCURRENTLY.
const noTransactionMarker = "-- migrate:no-transaction"

var migrationFileName = regexp.MustCompile(`^(\d+)_([A-Za-z0-9_-]+)\.sql$`)

// Migration is one numbered SQL file.
type Migration struct {
	Version       int64
	Name          string
	SQL           string
	Checksum      string
	InTransaction bool
}

// LoadMigrations reads NNNN_name.sql files from the root of files, ordered by version.
// Other files are ignored. Two files with the same version are an error.
func LoadMigrations(files fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	var migrations []Migration
	for _, entry := range entries {
		match := migrationFileName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			continue
		}
		migration, err := readMigration(files, entry.Name(), match)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration)
	}
	slices.SortFunc(migrations, func(a, b Migration) int { return cmp.Compare(a.Version, b.Version) })
	return migrations, rejectDuplicateVersions(migrations)
}

func readMigration(files fs.FS, fileName string, match []string) (Migration, error) {
	version, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return Migration{}, fmt.Errorf("migration %s: invalid version: %w", fileName, err)
	}
	content, err := fs.ReadFile(files, fileName)
	if err != nil {
		return Migration{}, fmt.Errorf("read migration %s: %w", fileName, err)
	}
	sum := sha256.Sum256(content)
	sql := string(content)
	return Migration{
		Version:       version,
		Name:          match[2],
		SQL:           sql,
		Checksum:      hex.EncodeToString(sum[:]),
		InTransaction: !strings.Contains(sql, noTransactionMarker),
	}, nil
}

func rejectDuplicateVersions(migrations []Migration) error {
	var problems []error
	for i := 1; i < len(migrations); i++ {
		if migrations[i].Version == migrations[i-1].Version {
			problems = append(problems, fmt.Errorf("duplicate migration version %d", migrations[i].Version))
		}
	}
	return errors.Join(problems...)
}
