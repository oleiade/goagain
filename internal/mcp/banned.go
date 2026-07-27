package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/domain"
)

func (s *Server) registerGetBannedList(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("get_banned_list",
		mcp.WithDescription("List all cards that are banned, suspended, restricted, or gone Living Legend in a given format"),
		mcp.WithString("format", mcp.Required(), mcp.Description("The format to check (blitz, cc, commoner, ll, silver_age, upf)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		format := strings.ToLower(getStringArg(request.Params.Arguments, "format"))
		if !domain.IsKnownFormat(domain.Format(format)) {
			return mcp.NewToolResultError(fmt.Sprintf("unknown format %q, must be one of: blitz, cc, commoner, ll, silver_age, upf", format)), nil
		}

		var banned, suspended, restricted, livingLegend []map[string]any
		for _, card := range s.store.Cards {
			leg := card.GetLegality(domain.Format(format))
			if leg.Banned {
				banned = append(banned, formatCardSummary(card))
			}
			if leg.Suspended {
				suspended = append(suspended, formatCardSummary(card))
			}
			if leg.Restricted {
				restricted = append(restricted, formatCardSummary(card))
			}
			if leg.LivingLegend {
				livingLegend = append(livingLegend, formatCardSummary(card))
			}
		}

		total := len(banned) + len(suspended) + len(restricted) + len(livingLegend)
		recordResultCount(ctx, total)

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"format": format,
			"banned": map[string]any{
				"count": len(banned),
				"cards": banned,
			},
			"suspended": map[string]any{
				"count": len(suspended),
				"cards": suspended,
			},
			"restricted": map[string]any{
				"count": len(restricted),
				"cards": restricted,
			},
			"living_legend": map[string]any{
				"count": len(livingLegend),
				"cards": livingLegend,
			},
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("get_banned_list", handler))
}
