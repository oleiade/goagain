package data

import (
	"strings"
	"testing"
)

const fixtureRules = `1 Game Concepts
1.0 General
1.0.1 The rules in this document apply to any game of Flesh and Blood.
1.0.1a If an effect directly contradicts a rule, the effect wins.
This is a continuation line of 1.0.1a.
1.0.2 A restriction takes precedence over a requirement.
1.-1.1 Go again
An ability keyword that gives an action point.
Second line of the glossary entry.
`

func TestParseRules_Fixture(t *testing.T) {
	rules := parseRules(fixtureRules)

	wantIDs := []string{"1", "1.0", "1.0.1", "1.0.1a", "1.0.2", "1.-1.1"}
	if len(rules) != len(wantIDs) {
		t.Fatalf("got %d rules, want %d: %+v", len(rules), len(wantIDs), rules)
	}
	for i, id := range wantIDs {
		if rules[i].ID != id {
			t.Errorf("rules[%d].ID = %q, want %q", i, rules[i].ID, id)
		}
	}
}

func TestParseRules_TextJoining(t *testing.T) {
	rules := parseRules(fixtureRules)

	byID := make(map[string]Rule, len(rules))
	for _, r := range rules {
		byID[r.ID] = r
	}

	if got, want := byID["1"].Text, "Game Concepts"; got != want {
		t.Errorf("rule 1 Text = %q, want %q", got, want)
	}
	if got, want := byID["1.0.1"].Text, "The rules in this document apply to any game of Flesh and Blood."; got != want {
		t.Errorf("rule 1.0.1 Text = %q, want %q", got, want)
	}

	wantSubText := "If an effect directly contradicts a rule, the effect wins.\nThis is a continuation line of 1.0.1a."
	if got := byID["1.0.1a"].Text; got != wantSubText {
		t.Errorf("rule 1.0.1a Text = %q, want %q", got, wantSubText)
	}

	wantGlossaryText := "Go again\nAn ability keyword that gives an action point.\nSecond line of the glossary entry."
	if got := byID["1.-1.1"].Text; got != wantGlossaryText {
		t.Errorf("glossary rule 1.-1.1 Text = %q, want %q", got, wantGlossaryText)
	}
}

func TestParseRules_DocumentOrder(t *testing.T) {
	rules := parseRules(fixtureRules)
	for i := 1; i < len(rules); i++ {
		if rules[i].ID == rules[i-1].ID {
			t.Errorf("duplicate adjacent ID %q at index %d", rules[i].ID, i)
		}
	}
}

func TestLoadRules_RealFile(t *testing.T) {
	store, err := NewStore(nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if len(store.Rules) < minParsedRules {
		t.Fatalf("parsed %d rules, want at least %d", len(store.Rules), minParsedRules)
	}
	if len(store.RulesByID) == 0 {
		t.Fatalf("RulesByID index is empty")
	}

	rule, ok := store.RulesByID["8.3.5"]
	if !ok {
		t.Fatalf("expected rule 8.3.5 (Go again) to exist")
	}
	if !strings.Contains(rule.Text, "Gain 1 action point") {
		t.Errorf("rule 8.3.5 Text = %q, want it to mention %q", rule.Text, "Gain 1 action point")
	}
}
