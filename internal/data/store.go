// Package data handles loading and indexing of Flesh and Blood card data.
package data

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/oleiade/goagain/internal/domain"
	"github.com/oleiade/goagain/internal/observability"
)

//go:embed english/*.json
var embeddedData embed.FS

// Store holds all loaded card data with indexes for efficient lookup.
//
// Concurrency: a Store is read-only after NewStore returns. All methods and exported
// fields are safe for concurrent use as long as callers do not mutate the slices or
// maps. There is no mechanism for reloading data; if one is added, it must coordinate
// with readers (e.g. via a sync.RWMutex or an atomic.Pointer[Store]) — Go panics on
// concurrent map writes.
type Store struct {
	Cards     []*domain.Card
	Sets      []*domain.Set
	Keywords  []*domain.Keyword
	Abilities []*domain.Ability
	Types     []*domain.Type
	Rules     []Rule

	// Indexes
	CardsByID      map[string]*domain.Card
	CardsByName    map[string][]*domain.Card // Multiple cards can share a name (different pitches)
	CardsBySetID   map[string][]*domain.Card
	SetsByID       map[string]*domain.Set
	KeywordsByName map[string]*domain.Keyword
	TypesByName    map[string]*domain.Type
	RulesByID      map[string]*Rule

	// New indexes for filtering
	CardsByClass   map[string][]*domain.Card
	CardsByType    map[string][]*domain.Card
	CardsByKeyword map[string][]*domain.Card
}

// NewStore creates and initializes a new data store from embedded JSON files.
func NewStore(metrics *observability.Metrics) (*Store, error) {
	s := &Store{
		CardsByID:      make(map[string]*domain.Card),
		CardsByName:    make(map[string][]*domain.Card),
		CardsBySetID:   make(map[string][]*domain.Card),
		SetsByID:       make(map[string]*domain.Set),
		KeywordsByName: make(map[string]*domain.Keyword),
		TypesByName:    make(map[string]*domain.Type),
		CardsByClass:   make(map[string][]*domain.Card),
		CardsByType:    make(map[string][]*domain.Card),
		CardsByKeyword: make(map[string][]*domain.Card),
	}

	if err := s.loadTypes(); err != nil {
		return nil, fmt.Errorf("loading types: %w", err)
	}

	if err := s.loadCards(); err != nil {
		return nil, fmt.Errorf("loading cards: %w", err)
	}

	if err := s.loadSets(); err != nil {
		return nil, fmt.Errorf("loading sets: %w", err)
	}

	if err := s.loadKeywords(); err != nil {
		return nil, fmt.Errorf("loading keywords: %w", err)
	}

	if err := s.loadAbilities(); err != nil {
		return nil, fmt.Errorf("loading abilities: %w", err)
	}

	if err := s.loadRules(); err != nil {
		return nil, fmt.Errorf("loading rules: %w", err)
	}

	// After all data is loaded and indexed, set the metrics
	if metrics != nil {
		stats, indexStats := s.Stats()
		metrics.SetDataStats(stats)
		metrics.SetIndexStats(indexStats)
	}

	return s, nil
}

func (s *Store) loadCards() error {
	data, err := embeddedData.ReadFile("english/card.json")
	if err != nil {
		return fmt.Errorf("reading card.json: %w", err)
	}

	var cards []*domain.Card
	if err := json.Unmarshal(data, &cards); err != nil {
		return fmt.Errorf("parsing card.json: %w", err)
	}

	s.Cards = cards

	// Build indexes
	for _, card := range cards {
		s.CardsByID[card.UniqueID] = card

		nameLower := strings.ToLower(card.Name)
		s.CardsByName[nameLower] = append(s.CardsByName[nameLower], card)

		for _, printing := range card.Printings {
			s.CardsBySetID[printing.SetID] = append(s.CardsBySetID[printing.SetID], card)
		}

		// Build new indexes
		if cardClass := card.GetClass(); cardClass != "" {
			s.CardsByClass[cardClass] = append(s.CardsByClass[cardClass], card)
		}

		for _, cardType := range card.Types {
			s.CardsByType[cardType] = append(s.CardsByType[cardType], card)
		}

		for _, keyword := range card.CardKeywords {
			s.CardsByKeyword[keyword] = append(s.CardsByKeyword[keyword], card)
		}
	}

	return nil
}

func (s *Store) loadSets() error {
	data, err := embeddedData.ReadFile("english/set.json")
	if err != nil {
		return fmt.Errorf("reading set.json: %w", err)
	}

	var sets []*domain.Set
	if err := json.Unmarshal(data, &sets); err != nil {
		return fmt.Errorf("parsing set.json: %w", err)
	}

	s.Sets = sets

	for _, set := range sets {
		s.SetsByID[set.ID] = set
	}

	return nil
}

func (s *Store) loadKeywords() error {
	data, err := embeddedData.ReadFile("english/keyword.json")
	if err != nil {
		return fmt.Errorf("reading keyword.json: %w", err)
	}

	var keywords []*domain.Keyword
	if err := json.Unmarshal(data, &keywords); err != nil {
		return fmt.Errorf("parsing keyword.json: %w", err)
	}

	s.Keywords = keywords

	for _, kw := range keywords {
		s.KeywordsByName[strings.ToLower(kw.Name)] = kw
	}

	return nil
}

func (s *Store) loadAbilities() error {
	data, err := embeddedData.ReadFile("english/ability.json")
	if err != nil {
		return fmt.Errorf("reading ability.json: %w", err)
	}

	var abilities []*domain.Ability
	if err := json.Unmarshal(data, &abilities); err != nil {
		return fmt.Errorf("parsing ability.json: %w", err)
	}

	s.Abilities = abilities
	return nil
}

func (s *Store) loadTypes() error {
	data, err := embeddedData.ReadFile("english/type.json")
	if err != nil {
		return fmt.Errorf("reading type.json: %w", err)
	}

	var types []*domain.Type
	if err := json.Unmarshal(data, &types); err != nil {
		return fmt.Errorf("parsing type.json: %w", err)
	}

	s.Types = types
	for _, t := range types {
		s.TypesByName[t.Name] = t
	}
	return nil
}

// GetCardByID returns a card by its unique ID.
func (s *Store) GetCardByID(id string) *domain.Card {
	return s.CardsByID[id]
}

// GetCardsByName returns all cards matching the exact name (case-insensitive).
func (s *Store) GetCardsByName(name string) []*domain.Card {
	return s.CardsByName[strings.ToLower(name)]
}

// ResolveCard returns the card denoted by idOrName, matching UniqueID first and
// falling back to an exact (case-insensitive) name lookup.
//
// Any handler or tool taking a caller-supplied card reference should use this
// instead of GetCardByID, so the same reference resolves identically everywhere.
// GET /v1/cards/{id}/legality once accepted only unique IDs while GET
// /v1/cards/{id} accepted both, so an agent that looked a card up by name got a
// 404 asking for that same card's legality.
//
// Several cards can share a name (pitch-1/2/3 variants); the first match wins.
// Callers wanting every variant should use GetCardsByName directly.
func (s *Store) ResolveCard(idOrName string) *domain.Card {
	if card := s.GetCardByID(idOrName); card != nil {
		return card
	}
	if cards := s.GetCardsByName(idOrName); len(cards) > 0 {
		return cards[0]
	}
	return nil
}

// GetSetByID returns a set by its ID code (e.g., "WTR", "ARC").
func (s *Store) GetSetByID(id string) *domain.Set {
	return s.SetsByID[strings.ToUpper(id)]
}

// GetKeywordByName returns a keyword by its name (case-insensitive).
func (s *Store) GetKeywordByName(name string) *domain.Keyword {
	return s.KeywordsByName[strings.ToLower(name)]
}

// CardFilter defines filtering criteria for card searches.
type CardFilter struct {
	Name      string
	Type      string
	Class     string
	SetID     string
	Pitch     string
	Cost      string
	Power     string
	Defense   string
	Rarity    string
	Keyword   string
	TextQuery string
	LegalIn   domain.Format
	Limit     int
	Offset    int
}

// SearchCards searches for cards matching the given filter criteria.
// It returns the paginated results and the total number of matches.
func (s *Store) SearchCards(filter CardFilter) ([]*domain.Card, int) {
	var results []*domain.Card

	// Use indexes to get an initial, smaller set of candidates
	var candidates []*domain.Card
	var usingIndex bool

	if filter.Class != "" {
		candidates = s.CardsByClass[filter.Class]
		usingIndex = true
	} else if filter.Type != "" {
		candidates = s.CardsByType[filter.Type]
		usingIndex = true
	} else if filter.Keyword != "" {
		// Partial keyword match: union all keywords whose name contains the query,
		// deduplicated by card UniqueID. Map iteration order is random, so deduping
		// is required for determinism as well as correctness.
		needle := strings.ToLower(filter.Keyword)
		seen := make(map[string]struct{})
		for kw, cards := range s.CardsByKeyword {
			if !strings.Contains(strings.ToLower(kw), needle) {
				continue
			}
			for _, c := range cards {
				if _, dup := seen[c.UniqueID]; dup {
					continue
				}
				seen[c.UniqueID] = struct{}{}
				candidates = append(candidates, c)
			}
			usingIndex = true
		}
	} else if filter.SetID != "" {
		candidates = s.CardsBySetID[strings.ToUpper(filter.SetID)]
		usingIndex = true
	}

	// If no index was used, fall back to a full scan
	if !usingIndex {
		candidates = s.Cards
	}

	// Now, filter the candidates
	for _, card := range candidates {
		if !s.matchesFilter(card, filter) {
			continue
		}
		results = append(results, card)
	}

	total := len(results)

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(results) {
			return nil, total // Page is out of bounds
		}
		results = results[filter.Offset:]
	}

	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results, total
}

func (s *Store) matchesFilter(card *domain.Card, filter CardFilter) bool {
	// Name filter (partial match)
	if filter.Name != "" {
		if !strings.Contains(strings.ToLower(card.Name), strings.ToLower(filter.Name)) {
			return false
		}
	}

	// Type filter (if not already handled by index)
	if filter.Type != "" && s.CardsByType[filter.Type] == nil {
		if !card.HasType(filter.Type) {
			return false
		}
	}

	// Class filter (if not already handled by index)
	if filter.Class != "" && s.CardsByClass[filter.Class] == nil {
		if !strings.EqualFold(card.GetClass(), filter.Class) {
			return false
		}
	}

	// Set filter (if not already handled by index)
	if filter.SetID != "" && s.CardsBySetID[strings.ToUpper(filter.SetID)] == nil {
		found := false
		for _, printing := range card.Printings {
			if strings.EqualFold(printing.SetID, filter.SetID) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Pitch filter
	if filter.Pitch != "" && card.Pitch != filter.Pitch {
		return false
	}

	// Cost filter
	if filter.Cost != "" && card.Cost != filter.Cost {
		return false
	}

	// Power filter
	if filter.Power != "" && card.Power != filter.Power {
		return false
	}

	// Defense filter
	if filter.Defense != "" && card.Defense != filter.Defense {
		return false
	}

	// Rarity filter: matches if any printing has the rarity
	if filter.Rarity != "" {
		found := false
		for _, printing := range card.Printings {
			if strings.EqualFold(printing.Rarity, filter.Rarity) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Keyword filter (more thorough than the index check)
	if filter.Keyword != "" {
		hasKeyword := slices.ContainsFunc(card.CardKeywords, func(k string) bool {
			return strings.Contains(strings.ToLower(k), strings.ToLower(filter.Keyword))
		})
		if !hasKeyword {
			return false
		}
	}

	// Text search
	if filter.TextQuery != "" {
		query := strings.ToLower(filter.TextQuery)
		text := strings.ToLower(card.FunctionalTextPlain)
		if !strings.Contains(text, query) {
			return false
		}
	}

	// Format legality filter
	if filter.LegalIn != "" {
		legality := card.GetLegality(filter.LegalIn)
		if !legality.Legal {
			return false
		}
	}

	return true
}

// SetFilter defines filtering criteria for set searches.
type SetFilter struct {
	Name  string // Partial match on set name
	ID    string // Partial match on set code
	Query string // Search both name and ID
}

// SearchSets searches for sets matching the given filter criteria.
func (s *Store) SearchSets(filter SetFilter) []*domain.Set {
	var results []*domain.Set

	for _, set := range s.Sets {
		if !s.matchesSetFilter(set, filter) {
			continue
		}
		results = append(results, set)
	}

	return results
}

func (s *Store) matchesSetFilter(set *domain.Set, filter SetFilter) bool {
	// Name filter (partial match, case-insensitive)
	if filter.Name != "" {
		if !strings.Contains(strings.ToLower(set.Name), strings.ToLower(filter.Name)) {
			return false
		}
	}

	// ID filter (partial match, case-insensitive)
	if filter.ID != "" {
		if !strings.Contains(strings.ToLower(set.ID), strings.ToLower(filter.ID)) {
			return false
		}
	}

	// Query filter (searches both name and ID)
	if filter.Query != "" {
		query := strings.ToLower(filter.Query)
		nameMatch := strings.Contains(strings.ToLower(set.Name), query)
		idMatch := strings.Contains(strings.ToLower(set.ID), query)
		if !nameMatch && !idMatch {
			return false
		}
	}

	return true
}

// GetCardsInSet returns all cards in a given set.
func (s *Store) GetCardsInSet(setID string) []*domain.Card {
	// Deduplicate cards (a card might have multiple printings in same set)
	seen := make(map[string]bool)
	var results []*domain.Card

	for _, card := range s.CardsBySetID[strings.ToUpper(setID)] {
		if !seen[card.UniqueID] {
			seen[card.UniqueID] = true
			results = append(results, card)
		}
	}

	return results
}

// Stats returns basic statistics about the loaded data and indexes.
func (s *Store) Stats() (map[string]int, map[string]int) {
	dataStats := map[string]int{
		"cards":     len(s.Cards),
		"sets":      len(s.Sets),
		"keywords":  len(s.Keywords),
		"abilities": len(s.Abilities),
		"types":     len(s.Types),
		"rules":     len(s.Rules),
	}

	indexStats := map[string]int{
		"cards_by_id":      len(s.CardsByID),
		"cards_by_name":    len(s.CardsByName),
		"cards_by_set_id":  len(s.CardsBySetID),
		"sets_by_id":       len(s.SetsByID),
		"keywords_by_name": len(s.KeywordsByName),
		"types_by_name":    len(s.TypesByName),
		"cards_by_class":   len(s.CardsByClass),
		"cards_by_type":    len(s.CardsByType),
		"cards_by_keyword": len(s.CardsByKeyword),
		"rules_by_id":      len(s.RulesByID),
	}

	return dataStats, indexStats
}
