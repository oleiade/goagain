// Package data handles loading and indexing of Flesh and Blood card data.
package data

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/oleiade/goagain/internal/domain"
)

//go:embed english/*.json
var embeddedData embed.FS

// Store holds all loaded card data with indexes for efficient lookup.
type Store struct {
	Cards     []*domain.Card
	Sets      []*domain.Set
	Keywords  []*domain.Keyword
	Abilities []*domain.Ability

	// Indexes
	cardsByID      map[string]*domain.Card
	cardsByName    map[string][]*domain.Card // Multiple cards can share a name (different pitches)
	cardsBySetID   map[string][]*domain.Card
	setsByID       map[string]*domain.Set
	keywordsByName map[string]*domain.Keyword
}

// NewStore creates and initializes a new data store from embedded JSON files.
func NewStore() (*Store, error) {
	s := &Store{
		cardsByID:      make(map[string]*domain.Card),
		cardsByName:    make(map[string][]*domain.Card),
		cardsBySetID:   make(map[string][]*domain.Card),
		setsByID:       make(map[string]*domain.Set),
		keywordsByName: make(map[string]*domain.Keyword),
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
		s.cardsByID[card.UniqueID] = card
		nameLower := strings.ToLower(card.Name)
		s.cardsByName[nameLower] = append(s.cardsByName[nameLower], card)

		// Index by set
		for _, printing := range card.Printings {
			s.cardsBySetID[printing.SetID] = append(s.cardsBySetID[printing.SetID], card)
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
		s.setsByID[set.ID] = set
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
		s.keywordsByName[strings.ToLower(kw.Name)] = kw
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

// GetCardByID returns a card by its unique ID.
func (s *Store) GetCardByID(id string) *domain.Card {
	return s.cardsByID[id]
}

// GetCardsByName returns all cards matching the exact name (case-insensitive).
func (s *Store) GetCardsByName(name string) []*domain.Card {
	return s.cardsByName[strings.ToLower(name)]
}

// GetSetByID returns a set by its ID code (e.g., "WTR", "ARC").
func (s *Store) GetSetByID(id string) *domain.Set {
	return s.setsByID[strings.ToUpper(id)]
}

// GetKeywordByName returns a keyword by its name (case-insensitive).
func (s *Store) GetKeywordByName(name string) *domain.Keyword {
	return s.keywordsByName[strings.ToLower(name)]
}

// CardFilter defines filtering criteria for card searches.
type CardFilter struct {
	Name      string
	Type      string
	Class     string
	SetID     string
	Pitch     string
	Keyword   string
	TextQuery string
	LegalIn   domain.Format
	Limit     int
	Offset    int
}

// SearchCards searches for cards matching the given filter criteria.
func (s *Store) SearchCards(filter CardFilter) []*domain.Card {
	var results []*domain.Card

	for _, card := range s.Cards {
		if !s.matchesFilter(card, filter) {
			continue
		}
		results = append(results, card)
	}

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(results) {
			return nil
		}
		results = results[filter.Offset:]
	}

	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results
}

func (s *Store) matchesFilter(card *domain.Card, filter CardFilter) bool {
	// Name filter (partial match)
	if filter.Name != "" {
		if !strings.Contains(strings.ToLower(card.Name), strings.ToLower(filter.Name)) {
			return false
		}
	}

	// Type filter
	if filter.Type != "" {
		if !card.HasType(filter.Type) {
			return false
		}
	}

	// Class filter
	if filter.Class != "" {
		if !strings.EqualFold(card.GetClass(), filter.Class) {
			return false
		}
	}

	// Set filter
	if filter.SetID != "" {
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

	// Keyword filter
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

	for _, card := range s.cardsBySetID[strings.ToUpper(setID)] {
		if !seen[card.UniqueID] {
			seen[card.UniqueID] = true
			results = append(results, card)
		}
	}

	return results
}

// Stats returns basic statistics about the loaded data.
func (s *Store) Stats() map[string]int {
	return map[string]int{
		"cards":     len(s.Cards),
		"sets":      len(s.Sets),
		"keywords":  len(s.Keywords),
		"abilities": len(s.Abilities),
	}
}
