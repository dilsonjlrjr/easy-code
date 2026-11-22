// Package telemetry configures OpenTelemetry tracing (ADR-15). Export is off unless enabled.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Settings configures the exporter.
type Settings struct {
	Enabled     bool
	Endpoint    string
	Insecure    bool
	SampleRatio float64
	Service     string
	Version     string
}

// Tracing bundles what the adapters need to create and propagate spans.
type Tracing struct {
	Provider   trace.TracerProvider
	Propagator propagation.TextMapPropagator
	Shutdown   func(context.Context) error
}

// Setup builds the tracer provider. Disabled, it returns a no-op provider that still
// propagates incoming trace context, so logs keep the caller's trace id.
func Setup(ctx context.Context, settings Settings, logger *slog.Logger) (Tracing, error) {
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	if !settings.Enabled {
		logger.InfoContext(ctx, "telemetry export disabled")
		return Tracing{Provider: noop.NewTracerProvider(), Propagator: propagator, Shutdown: noShutdown}, nil
	}
	exporter, err := otlptracehttp.New(ctx, exporterOptions(settings)...)
	if err != nil {
		return Tracing{}, fmt.Errorf("create otlp exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", settings.Service),
			attribute.String("service.version", settings.Version),
		)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(settings.SampleRatio))),
	)
	logger.InfoContext(ctx, "telemetry export enabled",
		slog.String("otel_endpoint", settings.Endpoint),
		slog.Float64("sample_ratio", settings.SampleRatio))
	return Tracing{Provider: provider, Propagator: propagator, Shutdown: provider.Shutdown}, nil
}

// exporterOptions leaves an empty endpoint to the standard OTEL_EXPORTER_OTLP_* variables.
func exporterOptions(settings Settings) []otlptracehttp.Option {
	var options []otlptracehttp.Option
	if settings.Endpoint != "" {
		options = append(options, otlptracehttp.WithEndpointURL(settings.Endpoint))
	}
	if settings.Insecure {
		options = append(options, otlptracehttp.WithInsecure())
	}
	return options
}

func noShutdown(context.Context) error { return nil }
