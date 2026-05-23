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
