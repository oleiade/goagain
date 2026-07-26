package observability

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
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

// Version is the application version, set at build time via ldflags:
//
//	-ldflags "-X github.com/oleiade/goagain/internal/observability.Version=0.7.0"
var Version = "dev"

// OTelConfig holds OpenTelemetry configuration.
type OTelConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string

	// OTLP endpoint (e.g., "localhost:4318" for HTTP).
	// If empty, stdout exporters are used (development mode).
	OTLPEndpoint string

	// OTLPInsecure disables TLS on the OTLP exporters. Defaults to false (TLS on)
	// when an OTLP endpoint is configured. Set OTEL_EXPORTER_OTLP_INSECURE=true
	// for plaintext loopback collectors during development.
	OTLPInsecure bool

	// Export intervals
	MetricInterval    time.Duration
	TraceBatchTimeout time.Duration
}

// LoadOTelConfig loads OpenTelemetry configuration from environment variables.
func LoadOTelConfig(serviceName string) OTelConfig {
	config := OTelConfig{
		ServiceName:       serviceName,
		ServiceVersion:    Version,
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

	// Honor the standard OTEL_EXPORTER_OTLP_INSECURE env var. Default is TLS-on; users
	// who want plaintext (e.g. a local collector on loopback) must opt in explicitly.
	if v := strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")); v == "true" || v == "1" || v == "yes" {
		config.OTLPInsecure = true
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

	// Start Go runtime metrics instrumentation (goroutines, memory, GC).
	if err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(15 * time.Second)); err != nil {
		handleErr(err)
		return shutdown, err
	}

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
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(config.OTLPEndpoint)}
		if config.OTLPInsecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	} else {
		// Use stdout exporter for development
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	if err != nil {
		return nil, err
	}

	tracerProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(sanitizingExporter{SpanExporter: exporter},
			trace.WithBatchTimeout(config.TraceBatchTimeout)),
	)
	return tracerProvider, nil
}

func newMeterProvider(ctx context.Context, config OTelConfig, res *resource.Resource) (*metric.MeterProvider, error) {
	var exporter metric.Exporter
	var err error

	if config.OTLPEndpoint != "" {
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(config.OTLPEndpoint)}
		if config.OTLPInsecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
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
		opts := []otlploghttp.Option{otlploghttp.WithEndpoint(config.OTLPEndpoint)}
		if config.OTLPInsecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		exporter, err = otlploghttp.New(ctx, opts...)
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
