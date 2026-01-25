// Package observability provides shared logging, metrics, and tracing utilities
// for the goagain API and MCP servers.
package observability

import (
	"os"
	"strings"
)

// Config holds observability configuration.
type Config struct {
	// Logging configuration
	LogLevel  string // debug, info, warn, error
	LogFormat string // json or text

	// Service identification
	ServiceName string

	// Metrics configuration
	MetricsEnabled bool
	MetricsPath    string
}

// LoadConfig loads observability configuration from environment variables.
func LoadConfig(defaultServiceName string) Config {
	config := Config{
		LogLevel:       "info",
		LogFormat:      "json",
		ServiceName:    defaultServiceName,
		MetricsEnabled: true,
		MetricsPath:    "/metrics",
	}

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		level = strings.ToLower(level)
		switch level {
		case "debug", "info", "warn", "error":
			config.LogLevel = level
		}
	}

	if format := os.Getenv("LOG_FORMAT"); format != "" {
		format = strings.ToLower(format)
		if format == "json" || format == "text" {
			config.LogFormat = format
		}
	}

	if name := os.Getenv("SERVICE_NAME"); name != "" {
		config.ServiceName = name
	}

	if enabled := os.Getenv("METRICS_ENABLED"); enabled != "" {
		enabled = strings.ToLower(enabled)
		config.MetricsEnabled = enabled != "false" && enabled != "0" && enabled != "no"
	}

	if path := os.Getenv("METRICS_PATH"); path != "" {
		config.MetricsPath = path
	}

	return config
}
