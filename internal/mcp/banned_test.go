package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/domain"
)

func newBannedTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	return NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func callGetBannedList(t *testing.T, s *Server, args map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	tool := s.MCPServer().GetTool("get_banned_list")
	if tool == nil {
		t.Fatalf("get_banned_list tool not registered")
	}
	req := mcpgo.CallToolRequest{}
	req.Params.Arguments = args
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func TestGetBannedList_CCHasBans(t *testing.T) {
	s := newBannedTestServer(t)
	result := callGetBannedList(t, s, map[string]any{"format": "cc"})
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}

	text := result.Content[0].(mcpgo.TextContent).Text
	var payload struct {
		Banned struct {
			Count int `json:"count"`
		} `json:"banned"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, text)
	}
	if payload.Banned.Count == 0 {
		t.Errorf("expected banned.count > 0 for cc, got 0")
	}
}

func TestGetBannedList_CountMatchesStore(t *testing.T) {
	s := newBannedTestServer(t)
	result := callGetBannedList(t, s, map[string]any{"format": "cc"})
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result)
	}

	text := result.Content[0].(mcpgo.TextContent).Text
	var payload struct {
		Banned struct {
			Count int `json:"count"`
		} `json:"banned"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, text)
	}

	want := 0
	for _, card := range s.store.Cards {
		if card.GetLegality(domain.FormatCC).Banned {
			want++
		}
	}

	if payload.Banned.Count != want {
		t.Errorf("banned.count = %d, want %d (independent count)", payload.Banned.Count, want)
	}
}

func TestGetBannedList_UnknownFormatIsError(t *testing.T) {
	s := newBannedTestServer(t)
	result := callGetBannedList(t, s, map[string]any{"format": "vintage"})
	if !result.IsError {
		t.Errorf("expected IsError for unknown format, got success")
	}
}

func TestGetBannedList_MissingFormatIsError(t *testing.T) {
	s := newBannedTestServer(t)
	result := callGetBannedList(t, s, map[string]any{})
	if !result.IsError {
		t.Errorf("expected IsError for missing format, got success")
	}
}
