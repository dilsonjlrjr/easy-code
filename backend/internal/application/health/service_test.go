package health_test

import (
	"context"
	"errors"
	"testing"
	"time"

	apphealth "easycode/internal/application/health"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/health"
)

type fakeProbe struct{ err error }

func (fakeProbe) Dependency() health.Dependency { return health.DependencyDatabase }
func (p fakeProbe) Probe(context.Context) error { return p.err }

func TestReadinessSucceedsWhenProbesPass(t *testing.T) {
	service := apphealth.NewService("1.0.0", time.Second, fakeProbe{})

	report, err := service.Readiness(context.Background())

	if err != nil || !report.IsReady() {
		t.Fatalf("expected ready, got %+v, %v", report, err)
	}
}

func TestReadinessReturnsUnavailableFailureWithCause(t *testing.T) {
	cause := errors.New("connection refused")
	service := apphealth.NewService("1.0.0", time.Second, fakeProbe{err: cause})

	report, err := service.Readiness(context.Background())

	if report.IsReady() {
		t.Fatal("expected not ready")
	}
	if got := failure.From(err); got.Kind != failure.KindUnavailable {
		t.Fatalf("unexpected kind %v", got.Kind)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause chain lost")
	}
}

func TestLivenessReportsVersion(t *testing.T) {
	report := apphealth.NewService("1.2.3", time.Second).Liveness()

	if report.Status != health.StatusOK || report.Version != "1.2.3" {
		t.Fatalf("unexpected report %+v", report)
	}
}
