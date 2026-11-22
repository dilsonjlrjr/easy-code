// Package process holds the startup steps every binary shares: configuration, logger and
// signal handling.
package process

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"easycode/internal/infrastructure/config"
	"easycode/internal/infrastructure/logging"
)

// Process is the started runtime of one binary.
type Process struct {
	Config config.Config
	Logger *slog.Logger
}

// Start loads the configuration and builds the logger. A configuration error is logged
// and returned, so main can exit non-zero.
func Start(service, version string) (Process, error) {
	cfg, err := config.Load(os.LookupEnv)
	logger := logging.New(os.Stdout, cfg.LogLevel, service, version)
	if err != nil {
		logger.Error("invalid configuration", logging.ErrorAttr(err))
		return Process{}, err
	}
	logger.Info("process starting", slog.Int("pid", os.Getpid()))
	return Process{Config: cfg, Logger: logger}, nil
}

// SignalContext is canceled on SIGINT or SIGTERM.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
