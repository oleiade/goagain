// Package api provides HTTP handlers for the Flesh and Blood Cards REST API.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/domain"
)

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store      *data.Store
	apiBaseURL string
	mcpBaseURL string
}

// NewHandler creates a new Handler with the given data store.
func NewHandler(store *data.Store, apiBaseURL, mcpBaseURL string) *Handler {
	return &Handler{
		store:      store,
		apiBaseURL: apiBaseURL,
		mcpBaseURL: mcpBaseURL,
	}
}

// Response types

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// PaginatedResponse wraps paginated results.
type PaginatedResponse struct {
	Data   any `json:"data"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status string         `json:"status"`
	Stats  map[string]int `json:"stats"`
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

func getIntParam(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	intVal, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return intVal
}

// Handlers

// Index serves the landing page (HTML) or API info (JSON).
// Returns JSON if Accept header contains "application/json".
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	// Only handle exact root path
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Add Link headers for agent discovery (RFC 8288)
	w.Header().Add("Link", `</.well-known/api-catalog>; rel="api-catalog"`)
	w.Header().Add("Link", `</docs>; rel="service-doc"`)
	w.Header().Add("Link", `</openapi.yaml>; rel="service-desc"`)

	// Check Accept header for content negotiation
	accept := r.Header.Get("Accept")

	// Serve markdown for agents requesting it
	if strings.Contains(accept, "text/markdown") {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		md := string(landingMarkdown)
		md = strings.ReplaceAll(md, "https://api.goagain.dev", h.apiBaseURL)
		md = strings.ReplaceAll(md, "https://mcp.goagain.dev", h.mcpBaseURL)
		_, _ = w.Write([]byte(md))
		return
	}

	if strings.Contains(accept, "application/json") {
		dataStats, _ := h.store.Stats()
		info := map[string]any{
			"name":        "goagain - Flesh and Blood Cards API",
			"version":     "1.0.0",
			"api_version": "v1",
			"endpoints": map[string]string{
				"GET /":                       "Landing page (HTML) or API info (JSON with Accept: application/json)",
				"GET /health":                 "Health check with stats",
				"GET /docs":                   "Interactive API documentation (Swagger UI)",
				"GET /openapi.yaml":           "OpenAPI 3.0 specification",
				"GET /v1/cards":               "List/search cards (params: name, type, class, set, pitch, keyword, q, legal_in, limit, offset)",
				"GET /v1/cards/{id}":          "Get card by unique_id or name",
				"GET /v1/cards/{id}/legality": "Get card legality across all formats",
				"GET /v1/sets":                "List/search sets (params: name, id, q)",
				"GET /v1/sets/{id}":           "Get set details with cards",
				"GET /v1/keywords":            "List all keywords",
				"GET /v1/keywords/{name}":     "Get keyword description",
				"GET /v1/abilities":           "List all abilities",
			},
			"stats": dataStats,
		}
		writeJSON(w, http.StatusOK, info)
		return
	}

	// Serve landing page HTML with configured URLs
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := string(landingPage)
	html = strings.ReplaceAll(html, "https://api.goagain.dev", h.apiBaseURL)
	html = strings.ReplaceAll(html, "https://mcp.goagain.dev", h.mcpBaseURL)
	_, _ = w.Write([]byte(html))
}

// Health returns the health status of the API.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	dataStats, _ := h.store.Stats()
	writeJSON(w, http.StatusOK, HealthResponse{
		Status: "ok",
		Stats:  dataStats,
	})
}

// ListCards returns a list of cards matching query parameters.
func (h *Handler) ListCards(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	filter := data.CardFilter{
		Name:      query.Get("name"),
		Type:      query.Get("type"),
		Class:     query.Get("class"),
		SetID:     query.Get("set"),
		Pitch:     query.Get("pitch"),
		Keyword:   query.Get("keyword"),
		TextQuery: query.Get("q"),
		Limit:     getIntParam(r, "limit", 50),
		Offset:    getIntParam(r, "offset", 0),
	}

	// Parse format legality filter
	if legalIn := query.Get("legal_in"); legalIn != "" {
		filter.LegalIn = domain.Format(legalIn)
	}

	// Cap limit at 100
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	cards, total := h.store.SearchCards(filter)
	if cards == nil {
		// Ensure we send back an empty array instead of null
		cards = make([]*domain.Card, 0)
	}

	writeJSON(w, http.StatusOK, PaginatedResponse{
		Data:   cards,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetCard returns a single card by ID.
func (h *Handler) GetCard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "card ID required")
		return
	}

	card := h.store.GetCardByID(id)
	if card == nil {
		// Try by name
		cards := h.store.GetCardsByName(id)
		if len(cards) == 0 {
			writeError(w, http.StatusNotFound, "card not found")
			return
		}
		// Return first match if searching by name
		card = cards[0]
	}

	writeJSON(w, http.StatusOK, card)
}

// ListSets returns sets, optionally filtered by query parameters.
func (h *Handler) ListSets(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	filter := data.SetFilter{
		Name:  query.Get("name"),
		ID:    query.Get("id"),
		Query: query.Get("q"),
	}

	// If no filters provided, return all sets
	if filter.Name == "" && filter.ID == "" && filter.Query == "" {
		writeJSON(w, http.StatusOK, h.store.Sets)
		return
	}

	sets := h.store.SearchSets(filter)
	writeJSON(w, http.StatusOK, sets)
}

// GetSet returns a single set by ID with its cards.
func (h *Handler) GetSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "set ID required")
		return
	}

	set := h.store.GetSetByID(id)
	if set == nil {
		writeError(w, http.StatusNotFound, "set not found")
		return
	}

	// Include cards in this set
	type SetWithCards struct {
		*domain.Set
		Cards []*domain.Card `json:"cards"`
	}

	cards := h.store.GetCardsInSet(id)
	if cards == nil {
		cards = []*domain.Card{}
	}

	writeJSON(w, http.StatusOK, SetWithCards{
		Set:   set,
		Cards: cards,
	})
}

// ListKeywords returns all keywords.
func (h *Handler) ListKeywords(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Keywords)
}

// GetKeyword returns a single keyword by name.
func (h *Handler) GetKeyword(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "keyword name required")
		return
	}

	keyword := h.store.GetKeywordByName(name)
	if keyword == nil {
		writeError(w, http.StatusNotFound, "keyword not found")
		return
	}

	writeJSON(w, http.StatusOK, keyword)
}

// ListAbilities returns all abilities.
func (h *Handler) ListAbilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Abilities)
}

// RobotsTxt serves the robots.txt file with AI crawler rules and Content Signals.
func (h *Handler) RobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, `# Robots.txt for goagain API
# https://www.rfc-editor.org/rfc/rfc9309

User-agent: *
Allow: /

# AI crawlers: allow access, disallow training
User-agent: GPTBot
Allow: /

User-agent: OAI-SearchBot
Allow: /

User-agent: Claude-Web
Allow: /

User-agent: anthropic-ai
Allow: /

User-agent: Google-Extended
Disallow: /

User-agent: CCBot
Disallow: /

Sitemap: %s/sitemap.xml

# Content Signals (https://contentsignals.org/)
# https://datatracker.ietf.org/doc/draft-romm-aipref-contentsignals/
Content-Signal: ai-train=no, search=yes, ai-input=yes
`, h.apiBaseURL)
}

// AgentSkillsIndex serves the agent skills discovery index.
func (h *Handler) AgentSkillsIndex(w http.ResponseWriter, r *http.Request) {
	index := map[string]any{
		"$schema": "https://agentskills.io/schema/v0.2.0/index.json",
		"skills": []map[string]string{
			{
				"name":        "api-catalog",
				"type":        "api-catalog",
				"description": "API catalog for automated discovery (RFC 9727)",
				"url":         h.apiBaseURL + "/.well-known/api-catalog",
			},
			{
				"name":        "mcp-server-card",
				"type":        "mcp-server-card",
				"description": "MCP Server Card for agent tool discovery",
				"url":         h.apiBaseURL + "/.well-known/mcp/server-card.json",
			},
			{
				"name":        "sitemap",
				"type":        "sitemap",
				"description": "XML sitemap with canonical URLs",
				"url":         h.apiBaseURL + "/sitemap.xml",
			},
			{
				"name":        "robots-txt",
				"type":        "robots-txt",
				"description": "Robots.txt with AI crawler rules and Content Signals",
				"url":         h.apiBaseURL + "/robots.txt",
			},
			{
				"name":        "openapi",
				"type":        "openapi",
				"description": "OpenAPI 3.0 specification for the REST API",
				"url":         h.apiBaseURL + "/openapi.yaml",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(index)
}

// MCPServerCard serves the MCP Server Card for agent discovery (SEP-1649).
func (h *Handler) MCPServerCard(w http.ResponseWriter, r *http.Request) {
	card := map[string]any{
		"serverInfo": map[string]string{
			"name":        "goagain-mcp",
			"version":     "1.0.0",
			"description": "Flesh and Blood card game data MCP server",
		},
		"transport": map[string]string{
			"type": "http",
			"url":  h.mcpBaseURL + "/",
		},
		"capabilities": map[string]bool{
			"tools": true,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(card)
}

// APICatalog serves the API catalog (RFC 9727) as application/linkset+json.
func (h *Handler) APICatalog(w http.ResponseWriter, r *http.Request) {
	catalog := map[string]any{
		"linkset": []map[string]any{
			{
				"anchor":       h.apiBaseURL + "/",
				"service-desc": []map[string]string{{"href": h.apiBaseURL + "/openapi.yaml", "type": "application/yaml"}},
				"service-doc":  []map[string]string{{"href": h.apiBaseURL + "/docs"}},
				"status":       []map[string]string{{"href": h.apiBaseURL + "/health"}},
			},
		},
	}

	w.Header().Set("Content-Type", "application/linkset+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(catalog)
}

// SitemapXML serves a sitemap.xml listing canonical URLs for all public endpoints.
func (h *Handler) SitemapXML(w http.ResponseWriter, r *http.Request) {
	urls := []string{
		"/",
		"/docs",
		"/openapi.yaml",
		"/health",
		"/v1/cards",
		"/v1/sets",
		"/v1/keywords",
		"/v1/abilities",
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`)
	for _, u := range urls {
		_, _ = fmt.Fprintf(w, "  <url>\n    <loc>%s%s</loc>\n  </url>\n", h.apiBaseURL, u)
	}
	_, _ = fmt.Fprint(w, "</urlset>\n")
}

// GetCardLegality returns legality info for a card across all formats.
func (h *Handler) GetCardLegality(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "card ID required")
		return
	}

	card := h.store.GetCardByID(id)
	if card == nil {
		writeError(w, http.StatusNotFound, "card not found")
		return
	}

	formats := []domain.Format{
		domain.FormatBlitz,
		domain.FormatCC,
		domain.FormatCommoner,
		domain.FormatLL,
		domain.FormatSilverAge,
		domain.FormatUPF,
	}

	legalities := make([]domain.Legality, len(formats))
	for i, format := range formats {
		legalities[i] = card.GetLegality(format)
	}

	type LegalityResponse struct {
		CardID     string            `json:"card_id"`
		CardName   string            `json:"card_name"`
		Legalities []domain.Legality `json:"legalities"`
	}

	writeJSON(w, http.StatusOK, LegalityResponse{
		CardID:     card.UniqueID,
		CardName:   card.Name,
		Legalities: legalities,
	})
}
