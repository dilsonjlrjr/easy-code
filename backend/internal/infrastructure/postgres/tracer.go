package postgres

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"easycode/internal/infrastructure/logging"
)

type operationKey struct{}

type queryStartKey struct{}

type queryStart struct {
	operation string
	startedAt time.Time
}

// WithOperation names the queries run with ctx so logs identify them without SQL text or values.
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey{}, operation)
}

// QueryTracer logs every query by operation name, duration, rows and error. It never logs
// SQL arguments, which may hold user content (RNF-20).
type QueryTracer struct {
	logger *slog.Logger
}

// NewQueryTracer builds the pgx tracer.
func NewQueryTracer(logger *slog.Logger) *QueryTracer {
	return &QueryTracer{logger: logger}
}

// TraceQueryStart implements pgx.QueryTracer.
func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{
		operation: operationName(ctx, data.SQL),
		startedAt: time.Now(),
	})
}

// TraceQueryEnd implements pgx.QueryTracer.
func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, _ := ctx.Value(queryStartKey{}).(queryStart)
	attrs := []slog.Attr{
		slog.String("operation", start.operation),
		slog.Float64("duration_ms", milliseconds(time.Since(start.startedAt))),
		slog.Int64("rows", data.CommandTag.RowsAffected()),
	}
	if data.Err != nil {
		attrs = append(attrs, logging.ErrorAttr(data.Err))
		t.logger.LogAttrs(ctx, slog.LevelError, "db query failed", attrs...)
		return
	}
	t.logger.LogAttrs(ctx, slog.LevelDebug, "db query", attrs...)
}

// TraceConnectStart implements pgx.ConnectTracer.
func (t *QueryTracer) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{operation: "connect", startedAt: time.Now()})
}

// TraceConnectEnd implements pgx.ConnectTracer.
func (t *QueryTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	start, _ := ctx.Value(queryStartKey{}).(queryStart)
	attrs := []slog.Attr{slog.Float64("duration_ms", milliseconds(time.Since(start.startedAt)))}
	if data.Err != nil {
		t.logger.LogAttrs(ctx, slog.LevelError, "db connect failed", append(attrs, logging.ErrorAttr(data.Err))...)
		return
	}
	t.logger.LogAttrs(ctx, slog.LevelDebug, "db connect", attrs...)
}

// operationName prefers the name set by WithOperation and falls back to the SQL verb.
func operationName(ctx context.Context, sql string) string {
	if operation, ok := ctx.Value(operationKey{}).(string); ok && operation != "" {
		return operation
	}
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "unnamed"
	}
	return "unnamed." + strings.ToLower(fields[0])
}

func milliseconds(elapsed time.Duration) float64 {
	return float64(elapsed.Microseconds()) / 1000
}
