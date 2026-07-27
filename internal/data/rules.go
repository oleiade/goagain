package data

import (
	"bufio"
	"embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed rules/comprehensive-rules.txt
var embeddedRules embed.FS

const minParsedRules = 300

// Rule is a single numbered entry from the Comprehensive Rules: a chapter
// ("1"), section ("1.0"), rule ("1.0.1"), sub-rule ("1.0.1a"), or glossary
// entry.
type Rule struct {
	ID   string
	Text string
}

// ruleIDLine matches a line that starts a new Rule. It covers chapters
// ("1"), sections ("1.0"), rules and sub-rules ("1.0.1", "1.0.1a"), and the
// glossary's irregular "N.-1.M" numbering (e.g. "1.-1.1"). Glossary entries
// are deliberately included as Rules: their definitions are useful for
// search and the ID pattern never collides with normal rule numbers.
var ruleIDLine = regexp.MustCompile(`^(\d+(?:\.-?\d+)*[a-z]?)[ \t]+(.*)$`)

// parseRules splits the Comprehensive Rules plain text into Rule entries in
// document order. A line matching ruleIDLine starts a new Rule; any line
// that doesn't append to the current rule's Text.
//
// A handful of appendix headings reuse earlier chapter numbers verbatim
// (e.g. "2 Acknowledgments" reuses chapter "2", "1 Glossary" reuses chapter
// "1"). All occurrences are kept in the returned slice; callers building an
// ID index should expect the last occurrence of a duplicate ID to win.
func parseRules(text string) []Rule {
	var rules []Rule
	current := -1

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if m := ruleIDLine.FindStringSubmatch(line); m != nil {
			rules = append(rules, Rule{ID: m[1], Text: m[2]})
			current = len(rules) - 1
			continue
		}
		if current < 0 || strings.TrimSpace(line) == "" {
			continue
		}
		rules[current].Text += "\n" + line
	}

	return rules
}

func (s *Store) loadRules() error {
	data, err := embeddedRules.ReadFile("rules/comprehensive-rules.txt")
	if err != nil {
		return fmt.Errorf("reading comprehensive-rules.txt: %w", err)
	}

	rules := parseRules(string(data))
	if len(rules) < minParsedRules {
		return fmt.Errorf("parsed only %d rules from comprehensive-rules.txt, expected at least %d", len(rules), minParsedRules)
	}

	s.Rules = rules
	s.RulesByID = make(map[string]*Rule, len(rules))
	for i := range s.Rules {
		s.RulesByID[s.Rules[i].ID] = &s.Rules[i]
	}

	return nil
}
