package observability

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetClientIPFunc covers the Medium #13 consolidated client-IP path.
// Forwarded-For trust is gated on the request originating from a configured
// trusted-proxy CIDR; without that gate, any client can spoof their source IP
// and bypass per-IP rate limiting.
func TestGetClientIPFunc(t *testing.T) {
	_, lan, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	trusted := []*net.IPNet{lan}

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
			name:    "trusted proxy: XFF first IP wins",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "5.5.5.5, 6.6.6.6, 7.7.7.7",
			want:    "5.5.5.5",
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
			name:    "trusted proxy: XFF whitespace trimmed",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			xff:     "  5.5.5.5  , 6.6.6.6",
			want:    "5.5.5.5",
		},
		{
			name:   "malformed remote (no port) is returned verbatim",
			remote: "weird",
			want:   "weird",
		},
		{
			name:    "trusted proxy with empty XFF falls through to remote",
			proxies: trusted,
			remote:  "10.0.0.1:80",
			want:    "10.0.0.1",
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
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
