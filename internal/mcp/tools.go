// Package mcp provides MCP server tools for Flesh and Blood card data.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/domain"
	"github.com/oleiade/goagain/internal/observability"
)

// Server wraps the MCP server with card data access.
type Server struct {
	mcpServer *server.MCPServer
	store     *data.Store
	logger    *slog.Logger
	metrics   *observability.Metrics
}

// NewServer creates a new MCP server with all tools registered.
func NewServer(store *data.Store, logger *slog.Logger, metrics *observability.Metrics) *Server {
	s := &Server{
		store:   store,
		logger:  logger,
		metrics: metrics,
	}

	mcpServer := server.NewMCPServer(
		"fab-cards",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tools
	s.registerSearchCards(mcpServer)
	s.registerGetCard(mcpServer)
	s.registerListSets(mcpServer)
	s.registerSearchSets(mcpServer)
	s.registerGetSet(mcpServer)
	s.registerSearchCardText(mcpServer)
	s.registerGetFormatLegality(mcpServer)
	s.registerListKeywords(mcpServer)
	s.registerGetKeyword(mcpServer)

	s.mcpServer = mcpServer
	return s
}

// MCPServer returns the underlying MCP server for running.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcpServer
}

// instrumentTool wraps a tool handler with metrics and logging.
func (s *Server) instrumentTool(toolName string, handler func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()

		if s.metrics != nil {
			s.metrics.ToolInFlightInc(toolName)
			defer s.metrics.ToolInFlightDec(toolName)
		}

		result, err := handler(ctx, request)

		duration := time.Since(start)

		// Determine result count (if applicable)
		resultCount := 0
		if result != nil && !result.IsError {
			resultCount = 1 // Default to 1 for single results
		}

		// Record metrics
		if s.metrics != nil {
			s.metrics.RecordToolInvocation(toolName, duration, resultCount, err)
		}

		// Log the invocation
		if s.logger != nil {
			observability.LogToolInvocation(ctx, s.logger, toolName, duration, resultCount, err)
		}

		return result, err
	}
}

func (s *Server) registerSearchCards(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("search_cards",
		mcp.WithDescription("Search for Flesh and Blood cards by name, type, class, keywords, or other attributes"),
		mcp.WithString("name", mcp.Description("Filter by card name (partial match)")),
		mcp.WithString("type", mcp.Description("Filter by card type (e.g., 'Action', 'Attack', 'Equipment')")),
		mcp.WithString("class", mcp.Description("Filter by class (e.g., 'Warrior', 'Ninja', 'Wizard')")),
		mcp.WithString("set", mcp.Description("Filter by set code (e.g., 'WTR', 'ARC', 'MON')")),
		mcp.WithString("pitch", mcp.Description("Filter by pitch value ('1', '2', or '3')")),
		mcp.WithString("keyword", mcp.Description("Filter by keyword (e.g., 'Go again', 'Dominate')")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default 20, max 50)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		filter := data.CardFilter{
			Name:    getStringArg(args, "name"),
			Type:    getStringArg(args, "type"),
			Class:   getStringArg(args, "class"),
			SetID:   getStringArg(args, "set"),
			Pitch:   getStringArg(args, "pitch"),
			Keyword: getStringArg(args, "keyword"),
			Limit:   getIntArg(args, "limit", 20),
		}

		if filter.Limit > 50 {
			filter.Limit = 50
		}

		cards, _ := s.store.SearchCards(filter)

		// Format results for display
		var results []map[string]any
		for _, card := range cards {
			results = append(results, formatCardSummary(card))
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"count":   len(results),
			"results": results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("search_cards", handler))
}

func (s *Server) registerGetCard(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("get_card",
		mcp.WithDescription("Get full details of a specific Flesh and Blood card by unique ID or name"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The unique_id or exact name of the card")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := getStringArg(request.Params.Arguments, "id")
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}

		card := s.store.GetCardByID(id)
		if card == nil {
			// Try by name
			cards := s.store.GetCardsByName(id)
			if len(cards) > 0 {
				card = cards[0]
			}
		}

		if card == nil {
			return mcp.NewToolResultError(fmt.Sprintf("card not found: %s", id)), nil
		}

		return mcp.NewToolResultText(formatJSON(formatCardFull(card))), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("get_card", handler))
}

func (s *Server) registerListSets(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("list_sets",
		mcp.WithDescription("List all Flesh and Blood card sets"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var results []map[string]any
		for _, set := range s.store.Sets {
			results = append(results, map[string]any{
				"id":   set.ID,
				"name": set.Name,
			})
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"count": len(results),
			"sets":  results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("list_sets", handler))
}

func (s *Server) registerSearchSets(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("search_sets",
		mcp.WithDescription("Search for Flesh and Blood card sets by name or code"),
		mcp.WithString("name", mcp.Description("Filter by set name (partial match, case-insensitive)")),
		mcp.WithString("id", mcp.Description("Filter by set code (partial match, case-insensitive)")),
		mcp.WithString("q", mcp.Description("Search both name and code")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		filter := data.SetFilter{
			Name:  getStringArg(args, "name"),
			ID:    getStringArg(args, "id"),
			Query: getStringArg(args, "q"),
		}

		sets := s.store.SearchSets(filter)

		var results []map[string]any
		for _, set := range sets {
			results = append(results, map[string]any{
				"id":   set.ID,
				"name": set.Name,
			})
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"count": len(results),
			"sets":  results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("search_sets", handler))
}

func (s *Server) registerGetSet(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("get_set",
		mcp.WithDescription("Get details of a specific set including its cards"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The set code (e.g., 'WTR', 'ARC')")),
		mcp.WithBoolean("include_cards", mcp.Description("Whether to include the list of cards in this set (default false)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := getStringArg(request.Params.Arguments, "id")
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}

		set := s.store.GetSetByID(id)
		if set == nil {
			return mcp.NewToolResultError(fmt.Sprintf("set not found: %s", id)), nil
		}

		result := map[string]any{
			"id":        set.ID,
			"name":      set.Name,
			"printings": set.Printings,
		}

		if getBoolArg(request.Params.Arguments, "include_cards") {
			cards := s.store.GetCardsInSet(id)
			var cardSummaries []map[string]any
			for _, card := range cards {
				cardSummaries = append(cardSummaries, formatCardSummary(card))
			}
			result["cards"] = cardSummaries
			result["card_count"] = len(cardSummaries)
		}

		return mcp.NewToolResultText(formatJSON(result)), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("get_set", handler))
}

func (s *Server) registerSearchCardText(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("search_card_text",
		mcp.WithDescription("Search for cards by text in their abilities or effects"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Text to search for in card abilities/effects")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results (default 20, max 50)")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := getStringArg(request.Params.Arguments, "query")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}

		filter := data.CardFilter{
			TextQuery: query,
			Limit:     getIntArg(request.Params.Arguments, "limit", 20),
		}

		if filter.Limit > 50 {
			filter.Limit = 50
		}

		cards, _ := s.store.SearchCards(filter)

		var results []map[string]any
		for _, card := range cards {
			results = append(results, formatCardSummary(card))
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"query":   query,
			"count":   len(results),
			"results": results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("search_card_text", handler))
}

func (s *Server) registerGetFormatLegality(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("get_format_legality",
		mcp.WithDescription("Check a card's legality status across all formats"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The unique_id or name of the card")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := getStringArg(request.Params.Arguments, "id")
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}

		card := s.store.GetCardByID(id)
		if card == nil {
			cards := s.store.GetCardsByName(id)
			if len(cards) > 0 {
				card = cards[0]
			}
		}

		if card == nil {
			return mcp.NewToolResultError(fmt.Sprintf("card not found: %s", id)), nil
		}

		formats := []domain.Format{
			domain.FormatBlitz,
			domain.FormatCC,
			domain.FormatCommoner,
			domain.FormatLL,
			domain.FormatSilverAge,
			domain.FormatUPF,
		}

		legalities := make(map[string]any)
		for _, format := range formats {
			leg := card.GetLegality(format)
			legalities[string(format)] = map[string]any{
				"legal":         leg.Legal,
				"living_legend": leg.LivingLegend,
				"banned":        leg.Banned,
				"suspended":     leg.Suspended,
				"restricted":    leg.Restricted,
			}
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"card_id":    card.UniqueID,
			"card_name":  card.Name,
			"legalities": legalities,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("get_format_legality", handler))
}

func (s *Server) registerListKeywords(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("list_keywords",
		mcp.WithDescription("List all game keywords with their explanations"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var results []map[string]any
		for _, kw := range s.store.Keywords {
			results = append(results, map[string]any{
				"name":        kw.Name,
				"description": kw.DescriptionPlain,
			})
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"count":    len(results),
			"keywords": results,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("list_keywords", handler))
}

func (s *Server) registerGetKeyword(mcpServer *server.MCPServer) {
	tool := mcp.NewTool("get_keyword",
		mcp.WithDescription("Get the description of a specific keyword"),
		mcp.WithString("name", mcp.Required(), mcp.Description("The keyword name (e.g., 'Go again', 'Dominate')")),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := getStringArg(request.Params.Arguments, "name")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}

		kw := s.store.GetKeywordByName(name)
		if kw == nil {
			return mcp.NewToolResultError(fmt.Sprintf("keyword not found: %s", name)), nil
		}

		return mcp.NewToolResultText(formatJSON(map[string]any{
			"name":        kw.Name,
			"description": kw.DescriptionPlain,
		})), nil
	}

	mcpServer.AddTool(tool, s.instrumentTool("get_keyword", handler))
}

// Helper functions

func getStringArg(args any, key string) string {
	m, ok := args.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntArg(args any, key string, defaultVal int) int {
	m, ok := args.(map[string]any)
	if !ok {
		return defaultVal
	}
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return defaultVal
}

func getBoolArg(args any, key string) bool {
	m, ok := args.(map[string]any)
	if !ok {
		return false
	}
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func formatJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func formatCardSummary(card *domain.Card) map[string]any {
	result := map[string]any{
		"unique_id": card.UniqueID,
		"name":      card.Name,
		"type_text": card.TypeText,
	}

	if card.Pitch != "" {
		result["pitch"] = card.Pitch
	}
	if card.Cost != "" {
		result["cost"] = card.Cost
	}
	if card.Power != "" {
		result["power"] = card.Power
	}
	if card.Defense != "" {
		result["defense"] = card.Defense
	}

	return result
}

func formatCardFull(card *domain.Card) map[string]any {
	result := map[string]any{
		"unique_id":       card.UniqueID,
		"name":            card.Name,
		"type_text":       card.TypeText,
		"types":           card.Types,
		"functional_text": card.FunctionalTextPlain,
	}

	if card.Color != "" {
		result["color"] = card.Color
	}
	if card.Pitch != "" {
		result["pitch"] = card.Pitch
	}
	if card.Cost != "" {
		result["cost"] = card.Cost
	}
	if card.Power != "" {
		result["power"] = card.Power
	}
	if card.Defense != "" {
		result["defense"] = card.Defense
	}
	if card.Health != "" {
		result["health"] = card.Health
	}
	if card.Intelligence != "" {
		result["intelligence"] = card.Intelligence
	}
	if len(card.CardKeywords) > 0 {
		result["keywords"] = card.CardKeywords
	}
	if len(card.Traits) > 0 {
		result["traits"] = card.Traits
	}

	// Include first printing's image URL if available
	if len(card.Printings) > 0 && card.Printings[0].ImageURL != nil {
		result["image_url"] = *card.Printings[0].ImageURL
	}

	// Include set info
	var sets []string
	seen := make(map[string]bool)
	for _, p := range card.Printings {
		if !seen[p.SetID] {
			sets = append(sets, p.SetID)
			seen[p.SetID] = true
		}
	}
	if len(sets) > 0 {
		result["sets"] = strings.Join(sets, ", ")
	}

	return result
}
