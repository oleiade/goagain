package observability

import (
	"testing"
)

// TestLoadOTelConfig_Defaults verifies the zero-env baseline. We don't compare
// the whole struct because durations/version may shift; we just pin the values
// that matter for the review's TLS finding.
func TestLoadOTelConfig_Defaults(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_SERVICE_VERSION", "")
	t.Setenv("OTEL_ENVIRONMENT", "")

	cfg := LoadOTelConfig("default-svc")
	if cfg.ServiceName != "default-svc" {
		t.Errorf("ServiceName = %q, want default-svc", cfg.ServiceName)
	}
	if cfg.OTLPEndpoint != "" {
		t.Errorf("OTLPEndpoint should default to empty (stdout exporters), got %q", cfg.OTLPEndpoint)
	}
	if cfg.OTLPInsecure {
		t.Errorf("OTLPInsecure must default to false so TLS is the default for remote endpoints")
	}
	if cfg.Environment != "development" {
		t.Errorf("Environment = %q, want development", cfg.Environment)
	}
}

func TestLoadOTelConfig_Overrides(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "override-svc")
	t.Setenv("OTEL_SERVICE_VERSION", "9.9.9")
	t.Setenv("OTEL_ENVIRONMENT", "production")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example.com:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "")

	cfg := LoadOTelConfig("ignored")
	if cfg.ServiceName != "override-svc" {
		t.Errorf("ServiceName = %q, want override-svc", cfg.ServiceName)
	}
	if cfg.ServiceVersion != "9.9.9" {
		t.Errorf("ServiceVersion = %q, want 9.9.9", cfg.ServiceVersion)
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want production", cfg.Environment)
	}
	if cfg.OTLPEndpoint != "https://collector.example.com:4318" {
		t.Errorf("OTLPEndpoint = %q", cfg.OTLPEndpoint)
	}
	if cfg.OTLPInsecure {
		t.Errorf("OTLPInsecure should remain false without explicit opt-in")
	}
}

// TestLoadOTelConfig_InsecureFlag tightens the Medium #11 fix. The set of
// accepted truthy values must include lower/upper/mixed case so operators
// can use whichever convention they like, but unknown strings must NOT
// flip the bit (fail safe → TLS on).
func TestLoadOTelConfig_InsecureFlag(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"bogus", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", tc.val)
			cfg := LoadOTelConfig("svc")
			if cfg.OTLPInsecure != tc.want {
				t.Errorf("OTEL_EXPORTER_OTLP_INSECURE=%q → OTLPInsecure=%v, want %v", tc.val, cfg.OTLPInsecure, tc.want)
			}
		})
	}
}
