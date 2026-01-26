package data

import (
	"testing"

	"github.com/oleiade/goagain/internal/domain"
)

func TestNewStore(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	dataStats, indexStats := store.Stats()

	if dataStats["cards"] == 0 {
		t.Error("Expected cards to be loaded")
	}
	if dataStats["sets"] == 0 {
		t.Error("Expected sets to be loaded")
	}
	if dataStats["keywords"] == 0 {
		t.Error("Expected keywords to be loaded")
	}
	if dataStats["abilities"] == 0 {
		t.Error("Expected abilities to be loaded")
	}
	if dataStats["types"] == 0 {
		t.Error("Expected types to be loaded")
	}

	t.Logf("Loaded data: %d cards, %d sets, %d keywords, %d abilities, %d types",
		dataStats["cards"], dataStats["sets"], dataStats["keywords"], dataStats["abilities"], dataStats["types"])

	if indexStats["cards_by_id"] == 0 {
		t.Error("Expected cards_by_id index to be built")
	}
	if indexStats["cards_by_class"] == 0 {
		t.Error("Expected cards_by_class index to be built")
	}
	t.Logf("Loaded indexes: %d cards_by_id, %d cards_by_name, %d cards_by_set_id, %d sets_by_id, %d keywords_by_name, %d types_by_name, %d cards_by_class, %d cards_by_type, %d cards_by_keyword",
		indexStats["cards_by_id"], indexStats["cards_by_name"], indexStats["cards_by_set_id"], indexStats["sets_by_id"], indexStats["keywords_by_name"], indexStats["types_by_name"], indexStats["cards_by_class"], indexStats["cards_by_type"], indexStats["cards_by_keyword"])
}

func TestGetCardByID(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Get a known card ID from the data
	if len(store.Cards) == 0 {
		t.Skip("No cards loaded")
	}

	expectedCard := store.Cards[0]
	card := store.GetCardByID(expectedCard.UniqueID)

	if card == nil {
		t.Fatalf("GetCardByID(%q) returned nil", expectedCard.UniqueID)
	}

	if card.Name != expectedCard.Name {
		t.Errorf("GetCardByID() name = %q, want %q", card.Name, expectedCard.Name)
	}
}

func TestGetCardsByName(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Search for a card that should exist
	cards := store.GetCardsByName("Enlightened Strike")
	if len(cards) == 0 {
		t.Error("Expected to find 'Enlightened Strike'")
	}

	// Verify case insensitivity
	cardsLower := store.GetCardsByName("enlightened strike")
	if len(cardsLower) != len(cards) {
		t.Errorf("Case insensitive search failed: got %d, want %d", len(cardsLower), len(cards))
	}
}

func TestSearchCards(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	tests := []struct {
		name      string
		filter    CardFilter
		want      func(cards []*domain.Card, total int) bool
		wantTotal int
	}{
		{
			name:   "filter by name",
			filter: CardFilter{Name: "Strike", Limit: 10},
			want: func(cards []*domain.Card, total int) bool {
				return len(cards) > 0 && len(cards) <= 10 && total > 0
			},
		},
		{
			name:   "filter by class",
			filter: CardFilter{Class: "Warrior", Limit: 10},
			want: func(cards []*domain.Card, total int) bool {
				for _, c := range cards {
					if c.GetClass() != "Warrior" {
						return false
					}
				}
				return len(cards) > 0 && total > 0
			},
		},
		{
			name:   "filter by pitch",
			filter: CardFilter{Pitch: "1", Limit: 10},
			want: func(cards []*domain.Card, total int) bool {
				for _, c := range cards {
					if c.Pitch != "1" {
						return false
					}
				}
				return len(cards) > 0 && total > 0
			},
		},
		{
			name:   "filter by text",
			filter: CardFilter{TextQuery: "go again", Limit: 10},
			want: func(cards []*domain.Card, total int) bool {
				return len(cards) > 0 && total > 0
			},
		},
		{
			name:   "filter by format legality",
			filter: CardFilter{LegalIn: domain.FormatBlitz, Limit: 10},
			want: func(cards []*domain.Card, total int) bool {
				for _, c := range cards {
					leg := c.GetLegality(domain.FormatBlitz)
					if !leg.Legal {
						return false
					}
				}
				return len(cards) > 0 && total > 0
			},
		},
		{
			name:   "pagination",
			filter: CardFilter{Limit: 5, Offset: 0},
			want: func(cards []*domain.Card, total int) bool {
				return len(cards) == 5 && total > 5
			},
		},
		{
			name:   "pagination out of bounds",
			filter: CardFilter{Limit: 5, Offset: 100000},
			want: func(cards []*domain.Card, total int) bool {
				return len(cards) == 0 && total > 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cards, total := store.SearchCards(tt.filter)
			if !tt.want(cards, total) {
				t.Errorf("SearchCards(%+v) did not meet expectations, got %d cards and total %d", tt.filter, len(cards), total)
			}
		})
	}
}

func TestGetSetByID(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// WTR is the first set
	set := store.GetSetByID("WTR")
	if set == nil {
		t.Fatal("Expected to find set WTR")
	}

	if set.Name != "Welcome to Rathe" {
		t.Errorf("GetSetByID(WTR) name = %q, want 'Welcome to Rathe'", set.Name)
	}

	// Test case insensitivity
	setLower := store.GetSetByID("wtr")
	if setLower == nil {
		t.Fatal("Case insensitive lookup failed for 'wtr'")
	}
}

func TestGetKeywordByName(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	kw := store.GetKeywordByName("Go again")
	if kw == nil {
		t.Fatal("Expected to find keyword 'Go again'")
	}

	if kw.DescriptionPlain == "" {
		t.Error("Expected keyword to have a description")
	}

	// Test case insensitivity
	kwLower := store.GetKeywordByName("go again")
	if kwLower == nil {
		t.Fatal("Case insensitive lookup failed for 'go again'")
	}
}

func TestCardLegality(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Find a card that's legal in blitz
	filter := CardFilter{LegalIn: domain.FormatBlitz, Limit: 1}
	cards, _ := store.SearchCards(filter)

	if len(cards) == 0 {
		t.Skip("No blitz-legal cards found")
	}

	card := cards[0]
	legality := card.GetLegality(domain.FormatBlitz)

	if !legality.Legal {
		t.Errorf("Card %q should be legal in Blitz", card.Name)
	}
}