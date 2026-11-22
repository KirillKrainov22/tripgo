package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const serviceName = "trip-service"

type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
}

func Setup(ctx context.Context, logger *slog.Logger) (*Telemetry, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName), semconv.ServiceVersion(version())),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, err
	}
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("telemetry export failed", "error", err)
	}))
	return &Telemetry{TracerProvider: tracerProvider, MeterProvider: meterProvider}, nil
}

func (t *Telemetry) Meter() metric.Meter {
	return t.MeterProvider.Meter("github.com/KirillKrainov22/tripgo")
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	return errors.Join(t.TracerProvider.Shutdown(ctx), t.MeterProvider.Shutdown(ctx))
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
			return setting.Value[:7]
		}
	}
	return info.Main.Version
}
