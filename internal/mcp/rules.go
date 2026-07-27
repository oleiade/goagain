package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/data"
)

const (
	searchRulesDefaultLimit = 10
	searchRulesMaxLimit     = 25
)

func (s *Server) registerSearchRules(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("search_rules",
		mcp.WithDescription("Search the official Flesh and Blood Comprehensive Rules. Returns matching numbered rules. Use this to answer any rules question instead of relying on memory."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search terms to match against rule text and rule numbers")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default 10, max 25)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := strings.TrimSpace(getStringArg(request.Params.Arguments, "query"))
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		terms := strings.Fields(strings.ToLower(query))

		limit := getIntArg(request.Params.Arguments, "limit", searchRulesDefaultLimit)
		if limit <= 0 {
			limit = searchRulesDefaultLimit
		}
		if limit > searchRulesMaxLimit {
			limit = searchRulesMaxLimit
		}

		type scoredRule struct {
			rule  data.Rule
			score int
		}

		var matches []scoredRule
		for _, rule := range s.store.Rules {
			haystack := strings.ToLower(rule.ID + " " + rule.Text)
			score := 0
			matchedAll := true
			for _, term := range terms {
				count := strings.Count(haystack, term)
				if count == 0 {
					matchedAll = false
					break
				}
				score += count
			}
			if matchedAll {
				matches = append(matches, scoredRule{rule: rule, score: score})
			}
		}

		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i].score > matches[j].score
		})

		if len(matches) > limit {
			matches = matches[:limit]
		}

		recordResultCount(ctx, len(matches))

		if len(matches) == 0 {
			return mcp.NewToolResultText(formatJSON(map[string]any{
				"count":   0,
				"results": []map[string]any{},
				"hint":    "try fewer or different terms; get_rule fetches a rule by number",
			})), nil
		}

		var results []map[string]any
		for _, m := range matches {
			results = append(results, map[string]any{
				"id":   m.rule.ID,
				"text": m.rule.Text,
			})
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"count":   len(results),
			"results": results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("search_rules", handler))
}

func (s *Server) registerGetRule(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("get_rule",
		mcp.WithDescription("Get a specific rule from the Comprehensive Rules by its number, including all of its sub-rules (e.g. 1.2 also returns 1.2.1, 1.2.2, ...)"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The rule number to look up, e.g. \"1.2\" or \"8.3.5\"")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(getStringArg(request.Params.Arguments, "id"))
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}
		id = strings.TrimSuffix(id, ".")

		// A rule's descendants are ids extended with ".N" segments, plus lettered
		// sub-rules like "1.0.2a" directly under "1.0.2".
		isUnder := func(ruleID string) bool {
			if !strings.HasPrefix(ruleID, id) {
				return false
			}
			rest := ruleID[len(id):]
			return rest == "" || rest[0] == '.' || (len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z')
		}

		var results []map[string]any
		for _, rule := range s.store.Rules {
			if isUnder(rule.ID) {
				results = append(results, map[string]any{
					"id":   rule.ID,
					"text": rule.Text,
				})
			}
		}

		if len(results) == 0 {
			return mcp.NewToolResultError(fmt.Sprintf("no rule found with id %q", id)), nil
		}

		recordResultCount(ctx, len(results))
		return mcp.NewToolResultText(formatJSON(map[string]any{
			"count":   len(results),
			"results": results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("get_rule", handler))
}
