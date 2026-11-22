package telemetry_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"easycode/internal/infrastructure/telemetry"
)

func TestSetupDisabledReturnsNoopProvider(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tracing, err := telemetry.Setup(context.Background(), telemetry.Settings{Service: "test"}, logger)
	if err != nil {
		t.Fatal(err)
	}

	_, span := tracing.Provider.Tracer("test").Start(context.Background(), "op")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Fatal("disabled telemetry must not record spans")
	}
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
