package mcp

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/oleiade/goagain/internal/data"
)

func newProbabilityTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := data.NewStore(nil)
	if err != nil {
		t.Fatalf("data.NewStore: %v", err)
	}
	return NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func callDrawProbability(t *testing.T, s *Server, args map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	tool := s.MCPServer().GetTool("draw_probability")
	if tool == nil {
		t.Fatalf("draw_probability tool not registered")
	}
	req := mcpgo.CallToolRequest{}
	req.Params.Arguments = args
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func TestHypergeomAtLeast_KnownValue(t *testing.T) {
	got, _ := hypergeomAtLeast(60, 3, 4, 1).Float64()
	want := 1 - (57.0*56.0*55.0*54.0)/(60.0*59.0*58.0*57.0)
	if math.Abs(got-want) >= 1e-9 {
		t.Errorf("hypergeomAtLeast(60, 3, 4, 1) = %v, want %v", got, want)
	}
}

func TestHypergeomAtLeast_Certainty(t *testing.T) {
	got, _ := hypergeomAtLeast(60, 60, 4, 1).Float64()
	if got != 1 {
		t.Errorf("hypergeomAtLeast with copies == deckSize = %v, want 1", got)
	}
}

func TestHypergeomAtLeast_Impossible(t *testing.T) {
	got, _ := hypergeomAtLeast(60, 3, 4, 4).Float64()
	if got != 0 {
		t.Errorf("hypergeomAtLeast with k > copies = %v, want 0", got)
	}
}

func TestHypergeomAtLeast_KZero(t *testing.T) {
	got, _ := hypergeomAtLeast(60, 3, 4, 0).Float64()
	if got != 1 {
		t.Errorf("hypergeomAtLeast with k = 0 = %v, want 1", got)
	}
}

func TestDrawProbability_MissingDeckSizeIsError(t *testing.T) {
	s := newProbabilityTestServer(t)
	result := callDrawProbability(t, s, map[string]any{"copies": float64(3), "draws": float64(4)})
	if !result.IsError {
		t.Errorf("expected IsError for missing deck_size, got success")
	}
}

func TestDrawProbability_CopiesExceedsDeckSizeIsError(t *testing.T) {
	s := newProbabilityTestServer(t)
	result := callDrawProbability(t, s, map[string]any{
		"deck_size": float64(60),
		"copies":    float64(61),
		"draws":     float64(4),
	})
	if !result.IsError {
		t.Errorf("expected IsError for copies > deck_size, got success")
	}
}
