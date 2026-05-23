package observability

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetClientIPFunc asserts the rightmost-untrusted contract for X-Forwarded-For.
// Leftmost selection is unsafe: the leftmost entry is client-supplied (the proxy
// appends, it does not overwrite), so picking it would let any client spoof its
// source IP and bypass per-IP rate limiting by rotating the spoofed value.
// The function must therefore walk the chain right-to-left and return the first
// IP that is itself not part of the trusted-proxy set.
func TestGetClientIPFunc(t *testing.T) {
	_, lan, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	_, edge, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	trusted := []*net.IPNet{lan}
	multiTrusted := []*net.IPNet{lan, edge}

	cases := []struct {
		name    string
		proxies []*net.IPNet
		remote  string
		xff     string
		xri     string
		want    string
	}{
		{
			name: "no proxies configured returns raw remote",
			// Without any trusted-proxy config, headers must be ignored entirely.
			remote: "1.2.3.4:5678",
			xff:    "9.9.9.9",
			xri:    "8.8.8.8",
			want:   "1.2.3.4",
		},
		{
			name:    "trusted proxy: rightmost untrusted IP wins",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "5.5.5.5, 6.6.6.6, 7.7.7.7",
			want:    "7.7.7.7",
		},
		{
			name:    "trusted proxy: rightmost untrusted skips trailing trusted hop",
			proxies: multiTrusted,
			remote:  "10.0.0.1:80",
			// Chain left to right: client → edge proxy → app proxy.
			// 7.7.7.7 is the only entry not in a trusted CIDR.
			xff:  "7.7.7.7, 192.168.1.10",
			want: "7.7.7.7",
		},
		{
			name:    "trusted proxy: X-Real-IP when no XFF",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xri:     "8.8.8.8",
			want:    "8.8.8.8",
		},
		{
			name:    "trusted proxy: XFF takes precedence over X-Real-IP",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "5.5.5.5",
			xri:     "8.8.8.8",
			want:    "5.5.5.5",
		},
		{
			name:    "untrusted remote: spoofed XFF ignored",
			proxies: trusted,
			remote:  "1.2.3.4:5678",
			xff:     "9.9.9.9",
			want:    "1.2.3.4",
		},
		{
			name:    "trusted proxy: whitespace trimmed",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "  5.5.5.5  ",
			want:    "5.5.5.5",
		},
		{
			name:    "trusted proxy: IPv4 with port stripped",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "1.2.3.4:5678",
			want:    "1.2.3.4",
		},
		{
			name:    "trusted proxy: IPv6 in brackets with port stripped",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "[2001:db8::1]:443",
			want:    "2001:db8::1",
		},
		{
			name:    "trusted proxy: garbage mid-chain skipped, valid rightmost-untrusted wins",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "9.9.9.9, not-an-ip, 8.8.8.8",
			want:    "8.8.8.8",
		},
		{
			name:    "trusted proxy: whole chain inside trusted CIDR falls back to remote",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "10.1.1.1, 10.2.2.2",
			want:    "10.0.0.1",
		},
		{
			name:    "trusted proxy: empty XFF falls back to X-Real-IP",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xri:     "8.8.8.8",
			want:    "8.8.8.8",
		},
		{
			name:    "trusted proxy: X-Real-IP rejected when unparseable",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xri:     "not-an-ip",
			want:    "10.0.0.1",
		},
		{
			name:    "trusted proxy: XFF cap honours last 10 entries — earlier client IP ignored",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			// 11 entries: the first (8.8.8.8) falls outside the 10-entry window
			// and is not considered. The next-most-recent untrusted entry wins.
			xff:  "8.8.8.8, 9.9.9.1, 9.9.9.2, 9.9.9.3, 9.9.9.4, 9.9.9.5, 9.9.9.6, 9.9.9.7, 9.9.9.8, 9.9.9.9, 10.1.1.1",
			want: "9.9.9.9",
		},
		{
			name:    "trusted proxy with empty XFF falls through to remote",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			want:    "10.0.0.1",
		},
		{
			name:   "malformed remote (no port) is returned verbatim",
			remote: "weird",
			want:   "weird",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				r.Header.Set("X-Real-IP", tc.xri)
			}

			got := GetClientIPFunc(tc.proxies)(r)
			if normalizeIP(got) != normalizeIP(tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// normalizeIP returns the canonical net.IP.String() form of s, or s itself when
// it cannot be parsed (so fixtures like "weird" still compare exactly).
func normalizeIP(s string) string {
	if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
		return ip.String()
	}
	return s
}
