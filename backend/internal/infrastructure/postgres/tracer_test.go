package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"easycode/internal/infrastructure/logging"
)

func TestQueryTracerLogsOperationWithoutArguments(t *testing.T) {
	var buffer bytes.Buffer
	tracer := NewQueryTracer(logging.New(&buffer, slog.LevelDebug, "test", "dev"))
	ctx := WithOperation(context.Background(), "user.find_by_email")

	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL:  "SELECT id FROM users WHERE email = $1",
		Args: []any{"person@example.com"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})

	output := buffer.String()
	if !strings.Contains(output, `"operation":"user.find_by_email"`) || !strings.Contains(output, `"rows":1`) {
		t.Fatalf("operation or rows missing: %s", output)
	}
	if strings.Contains(output, "person@example.com") || strings.Contains(output, "FROM users") {
		t.Fatalf("query text or arguments leaked: %s", output)
	}
}

func TestQueryTracerLogsFailureAsError(t *testing.T) {
	var buffer bytes.Buffer
	tracer := NewQueryTracer(logging.New(&buffer, slog.LevelInfo, "test", "dev"))

	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "DELETE FROM x"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("deadlock")})

	output := buffer.String()
	if !strings.Contains(output, `"level":"ERROR"`) || !strings.Contains(output, `"operation":"unnamed.delete"`) {
		t.Fatalf("unexpected log: %s", output)
	}
}
