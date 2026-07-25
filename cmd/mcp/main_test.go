package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestMcpPathNormalizer covers the allowlist behavior: the two paths actually
// registered on the mux in runHTTP (/health and the streamable-http endpoint
// at /mcp) pass through unchanged; everything else -- unknown paths and
// invalid UTF-8 alike -- collapses to "/other". Before the fix both branches
// of mcpPathNormalizer returned the path unchanged, a no-op that let arbitrary
// path values reach the shared metrics middleware.
func TestMcpPathNormalizer(t *testing.T) {
	normalize := mcpPathNormalizer()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"mcp endpoint", "/mcp", "/mcp"},
		{"health", "/health", "/health"},
		{"unknown mcp submessage", "/mcp/message", "/other"},
		{"unknown scanner path", "/wp-admin/setup.php", "/other"},
		{"root", "/", "/other"},
		{"invalid utf8", "/mcp/\xff\xfe", "/other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.path); got != tc.want {
				t.Errorf("mcpPathNormalizer()(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestMcpPathNormalizer_AlwaysBounded is the same property check as the API
// normalizer's: for hostile input, the output must always be valid UTF-8 and
// always a member of the finite expected set.
func TestMcpPathNormalizer_AlwaysBounded(t *testing.T) {
	normalize := mcpPathNormalizer()

	expected := map[string]bool{"/other": true}
	for route := range mcpRoutes {
		expected[route] = true
	}

	hostile := []string{
		"",
		"/",
		"/mcp/\xff\xfe",
		string([]byte{0x00, 0x01, 0xff, 0xfe, 0xfd}),
		"/" + strings.Repeat("a", 100_000),
		"/mcp/" + strings.Repeat("\U0001F4A5", 1000),
		"/../../etc/passwd",
		"/mcp/\xed\xa0\x80", // CESU-8 encoded surrogate half, invalid UTF-8
	}

	for _, in := range hostile {
		got := normalize(in)
		if !utf8.ValidString(got) {
			t.Errorf("mcpPathNormalizer()(%q) = %q, not valid UTF-8", in, got)
		}
		if !expected[got] {
			t.Errorf("mcpPathNormalizer()(%q) = %q, not a member of the finite expected set", in, got)
		}
	}
}
