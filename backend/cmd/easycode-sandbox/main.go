// Command easycode-sandbox will be the only process with access to the Docker daemon (ADR-06) (ADR-02).
// It is a stub: it starts, logs and exits cleanly.
package main

import (
	"os"

	"easycode/internal/infrastructure/process"
)

const serviceName = "easycode-sandbox"

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	proc, err := process.Start(serviceName, version)
	if err != nil {
		os.Exit(1)
	}
	proc.Logger.Info("stub binary: nothing to run yet, exiting")
}
