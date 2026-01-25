// Package api provides HTTP handlers for the Flesh and Blood Cards REST API.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/oleiade/goagain/internal/data"
	"github.com/oleiade/goagain/internal/domain"
)

// Handler holds the dependencies for HTTP handlers.
type Handler struct {
	store *data.Store
}

// NewHandler creates a new Handler with the given data store.
func NewHandler(store *data.Store) *Handler {
	return &Handler{store: store}
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

	// Check if client wants JSON
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		info := map[string]any{
			"name":    "goagain - Flesh and Blood Cards API",
			"version": "1.0.0",
			"endpoints": map[string]string{
				"GET /":                    "Landing page (HTML) or API info (JSON with Accept: application/json)",
				"GET /health":              "Health check with stats",
				"GET /docs":                "Interactive API documentation (Swagger UI)",
				"GET /openapi.yaml":        "OpenAPI 3.0 specification",
				"GET /cards":               "List/search cards (params: name, type, class, set, pitch, keyword, q, legal_in, limit, offset)",
				"GET /cards/{id}":          "Get card by unique_id or name",
				"GET /cards/{id}/legality": "Get card legality across all formats",
				"GET /sets":                "List/search sets (params: name, id, q)",
				"GET /sets/{id}":           "Get set details with cards",
				"GET /keywords":            "List all keywords",
				"GET /keywords/{name}":     "Get keyword description",
				"GET /abilities":           "List all abilities",
			},
			"stats": h.store.Stats(),
		}
		writeJSON(w, http.StatusOK, info)
		return
	}

	// Serve landing page HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(landingPage)
}

// Health returns the health status of the API.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status: "ok",
		Stats:  h.store.Stats(),
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

	cards := h.store.SearchCards(filter)

	// Get total count (without pagination) for response
	filterNoLimit := filter
	filterNoLimit.Limit = 0
	filterNoLimit.Offset = 0
	total := len(h.store.SearchCards(filterNoLimit))

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
