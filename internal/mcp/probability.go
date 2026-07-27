package mcp

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// hypergeomAtLeast returns P(X >= k) when drawing draws cards from a deck of
// deckSize cards containing successes copies, as an exact rational.
func hypergeomAtLeast(deckSize, successes, draws, k int) *big.Rat {
	total := new(big.Int).Binomial(int64(deckSize), int64(draws))

	maxI := min(draws, successes)

	sum := new(big.Rat)
	for i := k; i <= maxI; i++ {
		ways := new(big.Int).Mul(
			new(big.Int).Binomial(int64(successes), int64(i)),
			new(big.Int).Binomial(int64(deckSize-successes), int64(draws-i)),
		)
		sum.Add(sum, new(big.Rat).SetFrac(ways, total))
	}
	return sum
}

func (s *Server) registerDrawProbability(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("draw_probability",
		mcp.WithDescription("Calculate the exact probability of drawing specific cards. Answers questions like: what are the odds of seeing at least one copy of a 3-of in my opening hand? Uses the hypergeometric distribution. Typical Flesh and Blood numbers: 60-card Classic Constructed deck, 40-card Blitz deck, 4-card hand refilled each turn."),
		mcp.WithNumber("deck_size", mcp.Required(), mcp.Description("Total cards in the deck")),
		mcp.WithNumber("copies", mcp.Required(), mcp.Description("Copies of the desired card(s) in the deck")),
		mcp.WithNumber("draws", mcp.Required(), mcp.Description("Number of cards drawn or seen")),
		mcp.WithNumber("min_copies", mcp.Description("Success means drawing at least this many (default 1)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		deckSize := getIntArg(args, "deck_size", -1)
		copies := getIntArg(args, "copies", -1)
		draws := getIntArg(args, "draws", -1)
		minCopies := getIntArg(args, "min_copies", 1)

		if deckSize < 1 {
			return mcp.NewToolResultError("deck_size must be >= 1"), nil
		}
		if copies < 0 || copies > deckSize {
			return mcp.NewToolResultError("copies must be between 0 and deck_size"), nil
		}
		if draws < 0 || draws > deckSize {
			return mcp.NewToolResultError("draws must be between 0 and deck_size"), nil
		}
		if minCopies < 0 || minCopies > draws {
			return mcp.NewToolResultError("min_copies must be between 0 and draws"), nil
		}

		p, _ := hypergeomAtLeast(deckSize, copies, draws, minCopies).Float64()
		expected := math.Round(float64(draws)*float64(copies)/float64(deckSize)*10000) / 10000

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"deck_size":             deckSize,
			"copies":                copies,
			"draws":                 draws,
			"min_copies":            minCopies,
			"probability":           p,
			"probability_pct":       fmt.Sprintf("%.2f%%", p*100),
			"expected_copies_drawn": expected,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("draw_probability", handler))
}
