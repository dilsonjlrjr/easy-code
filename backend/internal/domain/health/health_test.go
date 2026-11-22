package health_test

import (
	"testing"

	"easycode/internal/domain/health"
)

func TestReadinessReportIsNotReadyWhenAnyDependencyFails(t *testing.T) {
	report := health.NewReadinessReport("dev", []health.Check{
		{Dependency: health.DependencyDatabase, Healthy: false},
	})

	if report.IsReady() {
		t.Fatal("expected not ready")
	}
}

func TestReadinessReportIsReadyWhenAllDependenciesPass(t *testing.T) {
	report := health.NewReadinessReport("dev", []health.Check{
		{Dependency: health.DependencyDatabase, Healthy: true},
	})

	if !report.IsReady() {
		t.Fatal("expected ready")
	}
}
