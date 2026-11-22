// Package health holds the liveness and readiness use cases.
package health

import (
	"context"
	"errors"
	"fmt"
	"time"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/health"
)

// DependencyProbe is the port an adapter implements to report whether a dependency answers.
type DependencyProbe interface {
	Dependency() health.Dependency
	Probe(ctx context.Context) error
}

// Service evaluates the health of the running process.
type Service struct {
	version      string
	probeTimeout time.Duration
	probes       []DependencyProbe
}

// NewService builds the health use cases.
func NewService(version string, probeTimeout time.Duration, probes ...DependencyProbe) *Service {
	return &Service{version: version, probeTimeout: probeTimeout, probes: probes}
}

// Liveness reports that the process is up. It never touches dependencies.
func (s *Service) Liveness() health.Report {
	return health.Report{Status: health.StatusOK, Version: s.version}
}

// Readiness probes every dependency. When any probe fails it returns the report and
// an unavailable failure whose cause chain holds every probe error.
func (s *Service) Readiness(ctx context.Context) (health.Report, error) {
	checks := make([]health.Check, 0, len(s.probes))
	var probeErrors []error
	for _, probe := range s.probes {
		err := s.runProbe(ctx, probe)
		checks = append(checks, health.Check{Dependency: probe.Dependency(), Healthy: err == nil})
		if err != nil {
			probeErrors = append(probeErrors, fmt.Errorf("probe %s: %w", probe.Dependency(), err))
		}
	}
	report := health.NewReadinessReport(s.version, checks)
	if report.IsReady() {
		return report, nil
	}
	return report, failure.New(failure.KindUnavailable, failure.CodeServiceUnavailable, errors.Join(probeErrors...))
}

func (s *Service) runProbe(ctx context.Context, probe DependencyProbe) error {
	probeCtx, cancel := context.WithTimeout(ctx, s.probeTimeout)
	defer cancel()
	return probe.Probe(probeCtx)
}
