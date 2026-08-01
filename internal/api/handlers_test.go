package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oleiade/goagain/internal/api"
	"github.com/oleiade/goagain/internal/data"
)

func newTestHandler(t *testing.T) *api.Handler {
	t.Helper()
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	return api.NewHandler(store, "http://api.test", "http://mcp.test")
}

// TestListCards_LegalInValidation covers the Low #17 fix. Unknown formats used to
// silently return an empty result set because GetLegality treats them as illegal;
// they now produce 400 with a hint listing the valid values.
func TestListCards_LegalInValidation(t *testing.T) {
	h := newTestHandler(t)

	cases := []struct {
		name       string
		legal      string
		wantStatus int
	}{
		{"valid blitz", "blitz", http.StatusOK},
		{"valid cc", "cc", http.StatusOK},
		{"empty omits filter", "", http.StatusOK},
		{"unknown rejected", "bogus", http.StatusBadRequest},
		{"case-sensitive: BLITZ rejected", "BLITZ", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/v1/cards?limit=1"
			if tc.legal != "" {
				url += "&legal_in=" + tc.legal
			}
			r := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			h.ListCards(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus == http.StatusBadRequest {
				var resp map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unparseable error body: %v (%s)", err, w.Body.String())
				}
				if !strings.Contains(resp["error"], "legal_in") {
					t.Errorf("error body should mention legal_in; got %q", resp["error"])
				}
			}
		})
	}
}

// TestRobotsTxt_AIRules guards the shape scanners and RFC 9309 parsers rely on:
// every AI crawler needs its own group, and the file must contain real newlines.
// Production once served the whole file as a single line with a literal "\n",
// which silently voids every directive in it.
func TestRobotsTxt_AIRules(t *testing.T) {
	h := newTestHandler(t)

	w := httptest.NewRecorder()
	h.RobotsTxt(w, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()

	if strings.Contains(body, `\n`) {
		t.Errorf("body contains a literal backslash-n, directives will not parse:\n%s", body)
	}
	if !strings.Contains(body, "User-agent: *\nAllow: /\n") {
		t.Error("missing wildcard group")
	}
	for _, ua := range api.AICrawlers {
		if !strings.Contains(body, "User-agent: "+ua+"\nAllow: /\n") {
			t.Errorf("missing group for %q", ua)
		}
	}
	if !strings.Contains(body, "Sitemap: http://api.test/sitemap.xml\n") {
		t.Error("sitemap missing or not using the configured base URL")
	}
	if !strings.Contains(body, "Content-Signal: ai-train=yes, search=yes, ai-input=yes\n") {
		t.Error("Content-Signal does not match the allow-everything policy")
	}
}

// TestAuthMd covers the two things that make /auth.md useful: the H1 the
// agent-readiness scanners key on, and base-URL substitution so a non-production
// deployment does not document goagain.dev's endpoints as its own.
func TestAuthMd(t *testing.T) {
	h := newTestHandler(t)

	w := httptest.NewRecorder()
	h.AuthMd(w, httptest.NewRequest(http.MethodGet, "/auth.md", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}

	body := w.Body.String()
	if !strings.HasPrefix(body, "# auth.md\n") {
		t.Errorf("must open with an H1 containing \"auth.md\"; got %.40q", body)
	}
	if strings.Contains(body, "https://api.goagain.dev") || strings.Contains(body, "https://mcp.goagain.dev") {
		t.Error("hardcoded production URLs survived substitution")
	}
	for _, want := range []string{"http://api.test", "http://mcp.test"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing substituted base URL %q", want)
		}
	}
}
