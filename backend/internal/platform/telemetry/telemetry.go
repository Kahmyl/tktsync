package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/tktsync/tktsync/backend/internal/platform/config"
)

type Runtime struct {
	provider *sdktrace.TracerProvider
	logger   *slog.Logger
}

func Start(ctx context.Context, service string, cfg config.Telemetry, logger *slog.Logger) (*Runtime, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	runtime := &Runtime{logger: logger}
	if !cfg.Enabled {
		logger.Info("telemetry tracing disabled", "operation", "telemetry.start")
		return runtime, nil
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	serviceResource, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(service)))
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}
	runtime.provider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(serviceResource), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))))
	otel.SetTracerProvider(runtime.provider)
	logger.Info("telemetry tracing enabled", "operation", "telemetry.start", "sample_ratio", cfg.SampleRatio)
	return runtime, nil
}

func (r *Runtime) HTTPHandler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server", otelhttp.WithSpanNameFormatter(func(_ string, request *http.Request) string {
		return request.Method + " " + routeClass(request.URL.Path)
	}))
}
func routeClass(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for index, part := range parts {
		if (strings.Contains(part, "_") || strings.Contains(part, "-")) && len(part) > 20 {
			parts[index] = "{id}"
		}
	}
	return "/" + strings.Join(parts, "/")
}
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.provider == nil {
		return nil
	}
	if err := r.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown telemetry: %w", err)
	}
	r.logger.Info("telemetry flushed", "operation", "telemetry.shutdown")
	return nil
}
