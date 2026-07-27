package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/oleiade/goagain/internal/data"
)

func newRulesTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	return NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func callTool(t *testing.T, s *Server, name string, args map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	tool := s.MCPServer().GetTool(name)
	if tool == nil {
		t.Fatalf("%s tool not registered", name)
	}
	req := mcpgo.CallToolRequest{}
	req.Params.Arguments = args
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

type ruleResultPayload struct {
	Count   int `json:"count"`
	Results []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"results"`
	Hint string `json:"hint"`
}

func decodeRuleResult(t *testing.T, result *mcpgo.CallToolResult) ruleResultPayload {
	t.Helper()
	text := result.Content[0].(mcpgo.TextContent).Text
	var payload ruleResultPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, text)
	}
	return payload
}

func TestSearchRules_GoAgainReturnsMatches(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "search_rules", map[string]any{"query": "go again"})
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}

	payload := decodeRuleResult(t, result)
	if payload.Count == 0 {
		t.Fatalf("expected count > 0 for query %q", "go again")
	}
	if len(payload.Results) != payload.Count {
		t.Fatalf("count %d does not match number of results %d", payload.Count, len(payload.Results))
	}
	for _, r := range payload.Results {
		haystack := strings.ToLower(r.ID + " " + r.Text)
		if !strings.Contains(haystack, "go") || !strings.Contains(haystack, "again") {
			t.Errorf("rule %s does not contain both terms: %q", r.ID, r.Text)
		}
	}
}

func TestSearchRules_MissingQueryIsError(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "search_rules", map[string]any{})
	if !result.IsError {
		t.Errorf("expected IsError for missing query, got success")
	}
}

func TestSearchRules_NoMatchesReturnsHint(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "search_rules", map[string]any{"query": "xyzzy_no_such_term_qqq"})
	if result.IsError {
		t.Fatalf("expected a normal result for zero matches, got error: %v", result)
	}

	payload := decodeRuleResult(t, result)
	if payload.Count != 0 {
		t.Errorf("expected count 0, got %d", payload.Count)
	}
	if payload.Hint == "" {
		t.Errorf("expected a hint field for zero matches")
	}
}

func TestSearchRules_LimitRespectedAndCapped(t *testing.T) {
	s := newRulesTestServer(t)

	result := callTool(t, s, "search_rules", map[string]any{"query": "attack", "limit": float64(3)})
	payload := decodeRuleResult(t, result)
	if payload.Count > 3 {
		t.Errorf("expected at most 3 results, got %d", payload.Count)
	}

	result = callTool(t, s, "search_rules", map[string]any{"query": "attack", "limit": float64(100)})
	payload = decodeRuleResult(t, result)
	if payload.Count > searchRulesMaxLimit {
		t.Errorf("expected at most %d results (capped), got %d", searchRulesMaxLimit, payload.Count)
	}
}

func TestGetRule_ChapterReturnsMultipleEntries(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "get_rule", map[string]any{"id": "8.3"})
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}

	payload := decodeRuleResult(t, result)
	if payload.Count <= 1 {
		t.Fatalf("expected more than one entry for section 8.3, got %d", payload.Count)
	}
}

func TestGetRule_IncludesLetteredSubRules(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "get_rule", map[string]any{"id": "1.0.2"})
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}

	payload := decodeRuleResult(t, result)
	ids := make(map[string]bool)
	for _, r := range payload.Results {
		ids[r.ID] = true
	}
	if !ids["1.0.2"] || !ids["1.0.2a"] {
		t.Fatalf("expected 1.0.2 and 1.0.2a in results, got %v", ids)
	}
	if ids["1.0.20"] {
		t.Fatalf("1.0.20 must not match a 1.0.2 lookup")
	}
}

func TestGetRule_UnknownIDIsError(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "get_rule", map[string]any{"id": "999.999.999"})
	if !result.IsError {
		t.Errorf("expected IsError for unknown rule id, got success")
	}
}

func TestGetRule_MissingIDIsError(t *testing.T) {
	s := newRulesTestServer(t)
	result := callTool(t, s, "get_rule", map[string]any{})
	if !result.IsError {
		t.Errorf("expected IsError for missing id, got success")
	}
}
