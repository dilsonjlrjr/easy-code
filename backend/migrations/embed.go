// Package migrations embeds the numbered SQL migrations (NNNN_name.sql) applied by
// "easycode-api migrate". The SQL files are owned by the database design work.
package migrations

import (
	"embed"
	"io/fs"
)

// files must be package level: go:embed only applies to package variables.
//
//go:embed *
var files embed.FS

// Files returns the embedded migration directory.
func Files() fs.FS { return files }
