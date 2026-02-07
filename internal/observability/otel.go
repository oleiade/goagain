package observability

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// OTelConfig holds OpenTelemetry configuration.
type OTelConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string

	// OTLP endpoint (e.g., "localhost:4318" for HTTP).
	// If empty, stdout exporters are used (development mode).
	OTLPEndpoint string

	// Export intervals
	MetricInterval    time.Duration
	TraceBatchTimeout time.Duration
}

// LoadOTelConfig loads OpenTelemetry configuration from environment variables.
func LoadOTelConfig(serviceName string) OTelConfig {
	config := OTelConfig{
		ServiceName:       serviceName,
		ServiceVersion:    "0.1.0",
		Environment:       "development",
		MetricInterval:    30 * time.Second,
		TraceBatchTimeout: 5 * time.Second,
	}

	if name := os.Getenv("OTEL_SERVICE_NAME"); name != "" {
		config.ServiceName = name
	}

	if version := os.Getenv("OTEL_SERVICE_VERSION"); version != "" {
		config.ServiceVersion = version
	}

	if env := os.Getenv("OTEL_ENVIRONMENT"); env != "" {
		config.Environment = env
	}

	// Standard OTel env var for OTLP endpoint
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		config.OTLPEndpoint = endpoint
	}

	return config
}

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context, config OTelConfig) (func(context.Context) error, error) {
	var shutdownFuncs []func(context.Context) error
	var err error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set up resource with service information
	res, err := newResource(config)
	if err != nil {
		return shutdown, err
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	tracerProvider, err := newTracerProvider(ctx, config, res)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up meter provider.
	meterProvider, err := newMeterProvider(ctx, config, res)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	// Set up logger provider.
	loggerProvider, err := newLoggerProvider(ctx, config, res)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return shutdown, nil
}

func newResource(config OTelConfig) (*resource.Resource, error) {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(config.ServiceName),
		semconv.ServiceVersion(config.ServiceVersion),
		semconv.DeploymentEnvironment(config.Environment),
	), nil
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTracerProvider(ctx context.Context, config OTelConfig, res *resource.Resource) (*trace.TracerProvider, error) {
	var exporter trace.SpanExporter
	var err error

	if config.OTLPEndpoint != "" {
		// Use OTLP HTTP exporter for production
		exporter, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(config.OTLPEndpoint),
			otlptracehttp.WithInsecure(), // Use WithInsecure for non-TLS endpoints
		)
	} else {
		// Use stdout exporter for development
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	if err != nil {
		return nil, err
	}

	tracerProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(exporter,
			trace.WithBatchTimeout(config.TraceBatchTimeout)),
	)
	return tracerProvider, nil
}

func newMeterProvider(ctx context.Context, config OTelConfig, res *resource.Resource) (*metric.MeterProvider, error) {
	var exporter metric.Exporter
	var err error

	if config.OTLPEndpoint != "" {
		// Use OTLP HTTP exporter for production
		exporter, err = otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpoint(config.OTLPEndpoint),
			otlpmetrichttp.WithInsecure(),
		)
	} else {
		// Use stdout exporter for development
		exporter, err = stdoutmetric.New(stdoutmetric.WithPrettyPrint())
	}
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter,
			metric.WithInterval(config.MetricInterval))),
	)
	return meterProvider, nil
}

func newLoggerProvider(ctx context.Context, config OTelConfig, res *resource.Resource) (*log.LoggerProvider, error) {
	var exporter log.Exporter
	var err error

	if config.OTLPEndpoint != "" {
		// Use OTLP HTTP exporter for production
		exporter, err = otlploghttp.New(ctx,
			otlploghttp.WithEndpoint(config.OTLPEndpoint),
			otlploghttp.WithInsecure(),
		)
	} else {
		// Use stdout exporter for development
		exporter, err = stdoutlog.New(stdoutlog.WithPrettyPrint())
	}
	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(exporter)),
	)
	return loggerProvider, nil
}
