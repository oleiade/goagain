# MCP Server Expansion Plan

Four independent workstreams that extend the goagain MCP server (`internal/mcp/`).
Each workstream is self-contained, lands as its own jj change, and must pass the
full quality gate before being declared done.

Read this whole document before writing any code. Workstreams A, B, and D are
small and mechanical. Workstream C is larger and has an explicit data-sourcing
phase; do it last.

## Ground rules (apply to every workstream)

### Version control

This repo uses jujutsu (`jj`), not raw git. Before starting each workstream:

1. Run `jj status`. If the working copy (`@`) contains changes or a description
   that is not yours, run `jj new` first.
2. One workstream = one commit. Describe it with a conventional-commit message
   via `jj describe -m "..."` (examples given per workstream).
3. Never mix two workstreams in one commit. If it happens, `jj split`.

### Quality gate (run after every workstream, all must pass)

```
go build -v ./...
go test -race ./...
gofmt -l .        # must print nothing
golangci-lint run
go vet ./...
gosec ./...
govulncheck ./...
```

### Codebase conventions you must follow

- All MCP tools live in `internal/mcp/tools.go` (or a new sibling file in the
  same package). Each tool has a `registerXxx(mcpServer *server.MCPServer)`
  method on `*Server`, called from `NewServer` (tools.go, around line 59-68).
- Every handler is wrapped with `s.instrumentTool("<tool_name>", handler)` when
  registered. This provides tracing, metrics, and logging automatically. Do not
  add your own instrumentation inside handlers.
- Handlers that return a list call `recordResultCount(ctx, n)` before returning
  so the instrumentation records the real result count.
- Tool results are built with `mcp.NewToolResultText(formatJSON(...))` for
  success and `mcp.NewToolResultError("...")` for user-input errors. Return
  `(result, nil)` in both cases; only return a non-nil Go error for internal
  failures.
- Argument extraction uses the existing helpers `getStringArg`, `getIntArg`,
  `getBoolArg` (bottom of tools.go). JSON numbers arrive as `float64`; the
  helpers handle that.
- Card payloads use the existing `formatCardSummary` / `formatCardFull`
  helpers. Do not invent new card serialization shapes.
- The data layer is `internal/data/store.go`. `data.Store` is read-only after
  `NewStore` returns and safe for concurrent reads. Do not add mutable state.
- Domain types are in `internal/domain/types.go`. Note `domain.Format`,
  `domain.IsKnownFormat`, and `Card.GetLegality`.
- Tests use the real embedded dataset: `store, err := data.NewStore(nil)` is
  cheap and is the established pattern in `internal/mcp/tools_test.go` and
  `internal/data/store_test.go`. Prefer property-style assertions against the
  real data over hardcoding specific card names (the dataset changes over
  time).
- Match existing comment density and style. No comments explaining what the
  next line does.
- CLAUDE.md style rules: never use the em-dash character, short active
  sentences in any prose you write (docs, tool descriptions).

### After each workstream

Update the tool inventory in any docs that enumerate MCP tools. Run
`grep -rn "search_card_text\|get_format_legality" README.md docs/ internal/api/landing.md`
to find enumerations; if the new tool belongs there, add it in the same style.

---

## Workstream A: `get_banned_list` tool

**Goal:** let an agent ask "what is banned / suspended / living-legended in
format X" in one call. Today this data exists only per-card via
`get_format_legality`.

**Why it is cheap:** legality flags are denormalized onto every card
(`domain.Card` fields like `CCBanned`, `BlitzSuspended`, `CCLivingLegend`,
`LLRestricted`; see internal/domain/types.go lines 30-62). No new data loading
is needed. Do NOT load the `banned-*.json` files; everything required is
already on the Card structs, and `Card.GetLegality(format)` already interprets
the flags per format.

### Implementation

New registration method in `internal/mcp/tools.go` (or a new file
`internal/mcp/banned.go`, same package):

Tool definition:

- Name: `get_banned_list`
- Description: `List all cards that are banned, suspended, restricted, or gone
  Living Legend in a given format`
- Param `format` (string, required): one of `blitz`, `cc`, `commoner`, `ll`,
  `silver_age`, `upf`.

Handler logic:

1. Read `format` via `getStringArg`; lowercase it with `strings.ToLower`.
2. Validate with `domain.IsKnownFormat(domain.Format(format))`. On failure
   return `mcp.NewToolResultError` listing the valid values.
3. Iterate `s.store.Cards`. For each card call
   `card.GetLegality(domain.Format(format))` and bucket into four slices based
   on the `Banned`, `Suspended`, `Restricted`, `LivingLegend` fields of the
   returned `domain.Legality`. A card can appear in at most one bucket in
   practice, but bucket independently (check each flag separately) so the code
   stays correct if that changes.
4. Serialize each card with `formatCardSummary(card)`.
5. Call `recordResultCount(ctx, total)` where total is the sum of all bucket
   sizes.
6. Return JSON:

```json
{
  "format": "cc",
  "banned":        {"count": N, "cards": [...]},
  "suspended":     {"count": N, "cards": [...]},
  "restricted":    {"count": N, "cards": [...]},
  "living_legend": {"count": N, "cards": [...]}
}
```

Register it in `NewServer` next to `registerGetFormatLegality` and wrap with
`s.instrumentTool("get_banned_list", ...)`.

### Tests (in `internal/mcp/tools_test.go` or `banned_test.go`)

Load the real store (`data.NewStore(nil)`), build the server, invoke the
handler directly (construct `mcp.CallToolRequest` with
`Params.Arguments = map[string]any{"format": "cc"}`).

1. `format=cc` returns a non-error result whose `banned.count` is greater than
   zero (CC has had bans since 2021; the dataset guarantees this).
2. Cross-check: independently count cards where
   `card.GetLegality(domain.FormatCC).Banned` is true and assert it equals the
   reported `banned.count`.
3. Unknown format (`format=vintage`) returns an `IsError` result.
4. Missing format returns an `IsError` result.

### Commit

`feat(mcp): add get_banned_list tool`

### Explicitly out of scope

Ban start dates (`CCBannedStart` etc.). `GetLegality` does not expose them and
adding them is not worth the domain surface right now. Skip.

---

## Workstream B: `draw_probability` tool (hypergeometric calculator)

**Goal:** deterministic deck-math answers ("odds I draw at least one of my 3
copies in my 4-card opening hand"). Models are unreliable at this arithmetic;
the tool makes answers exact.

**Name it `draw_probability`, not `hypergeometric_calculator`.** Agents pick
tools by matching the user's intent to tool names and descriptions; nobody
asks for a hypergeometric distribution by name.

### Implementation

New file `internal/mcp/probability.go` (package `mcp`):

Pure math, stdlib only. Use `math/big`: `(*big.Int).Binomial(n, k)` exists in
the stdlib. Compute exactly with `big.Rat`, convert to float64 only for
output.

```go
// hypergeomAtLeast returns P(X >= k) when drawing draws cards from a deck of
// deckSize cards containing successes copies, as an exact rational.
func hypergeomAtLeast(deckSize, successes, draws, k int) *big.Rat
```

Implementation sketch: P(X = i) = C(successes, i) * C(deckSize-successes,
draws-i) / C(deckSize, draws). Sum P(X = i) for i from k to min(draws,
successes). `big.Int.Binomial(n, k)` returns 0 when k > n, which makes the
edge cases fall out naturally, but validate inputs before computing anyway.

Tool definition:

- Name: `draw_probability`
- Description: `Calculate the exact probability of drawing specific cards.
  Answers questions like: what are the odds of seeing at least one copy of a
  3-of in my opening hand? Uses the hypergeometric distribution. Typical
  Flesh and Blood numbers: 60-card Classic Constructed deck, 40-card Blitz
  deck, 4-card hand refilled each turn.`
- Params (all numbers):
  - `deck_size` (required): total cards in the deck
  - `copies` (required): copies of the desired card(s) in the deck
  - `draws` (required): number of cards drawn or seen
  - `min_copies` (optional, default 1): success means drawing at least this
    many

Handler logic:

1. Extract with `getIntArg` (use -1 defaults to detect missing required
   params).
2. Validate: `deck_size >= 1`, `0 <= copies <= deck_size`,
   `0 <= draws <= deck_size`, `0 <= min_copies <= draws`. On violation return
   `mcp.NewToolResultError` naming the violated constraint.
3. Compute and return:

```json
{
  "deck_size": 60,
  "copies": 3,
  "draws": 4,
  "min_copies": 1,
  "probability": 0.1888,
  "probability_pct": "18.88%",
  "expected_copies_drawn": 0.2
}
```

`probability` is `hypergeomAtLeast(...)` as float64 (use `Rat.Float64()`).
`probability_pct` is `fmt.Sprintf("%.2f%%", p*100)`. `expected_copies_drawn`
is `float64(draws) * float64(copies) / float64(deckSize)` rounded to 4
decimals.

Register in `NewServer` with `s.instrumentTool("draw_probability", ...)`. Do
not call `recordResultCount` (single-result tool; the instrumentation default
of 1 is correct).

### Tests (`internal/mcp/probability_test.go`)

1. Known value: deckSize=60, copies=3, draws=4, k=1. Expected: 1 -
   (57*56*55*54)/(60*59*58*57) = 0.18876... Assert `math.Abs(got-want) < 1e-9`
   with want computed inline from that formula.
2. Certainty: copies=deckSize gives P(>=1)=1 exactly for draws >= 1.
3. Impossible: k > copies gives 0.
4. k=0 gives 1.
5. Handler-level: missing `deck_size` returns IsError; `copies > deck_size`
   returns IsError.

### Commit

`feat(mcp): add draw_probability hypergeometric tool`

---

## Workstream C: Comprehensive Rules search tools

**Goal:** ground agents' FAB rules answers in the official Legend Story
Studios Comprehensive Rules instead of model memory. Delivered as search
tools, not MCP resources (most clients ignore resources).

This workstream has two phases: sourcing the text (verify as you go; do not
trust this document's assumptions about the PDF) and building the tools.

### Phase C1: source the rules text

1. Find the current Comprehensive Rules document at
   https://fabtcg.com/resources/rules-and-policy-center/ (the CR is published
   as a PDF; there may also be an HTML version, prefer HTML if it exists
   because extraction is cleaner). Record the exact URL and the document
   version/date printed on its cover page.
2. Convert to plain text. For PDF use `pdftotext -layout` (from poppler;
   `brew install poppler` if missing). Inspect the output by hand: headers,
   footers, and page numbers must be stripped; ligatures and bullet artifacts
   cleaned.
3. The CR uses hierarchical decimal numbering for rules (chapters like `1.`
   and rules like `1.2.3.`; VERIFY the exact convention against the actual
   document before writing the parser). Normalize the text so every rule
   entry starts on its own line with its number.
4. Commit the cleaned text as `internal/data/rules/comprehensive-rules.txt`.
   Add `internal/data/rules/README.md` stating: source URL, document version,
   retrieval date, conversion command used, and a note that this is
   unmodified Legend Story Studios content redistributed for a non-commercial
   fan project under the LSS fan content policy (read the policy at
   https://fabtcg.com and confirm it permits this; if it does not clearly
   permit redistribution, STOP and report back instead of committing the
   text).
5. Add a refresh script `scripts/sync-rules.sh` that documents/automates
   steps 1-3 (download URL pinned, pdftotext invocation, cleanup). Model it
   on the existing `scripts/sync-data.sh` style.

Commit phase C1 separately: `feat(data): embed LSS Comprehensive Rules text`

### Phase C2: parse, index, and expose

**Parsing (`internal/data/rules.go`, package `data`):**

```go
type Rule struct {
    ID   string // e.g. "1.2.3"
    Text string // full text of the rule entry, without the ID prefix
}
```

- Embed the text file with its own `//go:embed rules/comprehensive-rules.txt`
  directive (the existing embed in store.go covers only `english/*.json`).
- Parse at startup in `NewStore`: scan lines; a line starting with a rule
  number pattern (something like `^\d+(\.\d+)*\.?\s` but verify against the
  real text) starts a new Rule; continuation lines append to the current
  rule's Text.
- Store on `Store`: `Rules []Rule` (document order) and `RulesByID
  map[string]*Rule`. Add both to `Stats()`.
- If the rules file is empty or fails to parse into at least a few hundred
  rules, return an error from `NewStore` (fail loudly at startup, not
  silently at query time).

**Tools (`internal/mcp/rules.go`, package `mcp`):**

Tool 1: `search_rules`

- Description: `Search the official Flesh and Blood Comprehensive Rules.
  Returns matching numbered rules. Use this to answer any rules question
  instead of relying on memory.`
- Params: `query` (string, required), `limit` (number, optional, default 10,
  max 25).
- Logic: case-insensitive AND match; split query on whitespace, a rule
  matches if its Text (or ID) contains every term. Rank by total term
  occurrence count, descending; break ties by document order. Return
  `{count, results: [{id, text}]}`. Call `recordResultCount`.
- If zero matches, return a normal (non-error) result with count 0 and a hint
  field: `"hint": "try fewer or different terms; get_rule fetches a rule by
  number"`.

Tool 2: `get_rule`

- Description: `Get a specific rule from the Comprehensive Rules by its
  number, including all of its sub-rules (e.g. 1.2 also returns 1.2.1,
  1.2.2, ...)`
- Params: `id` (string, required).
- Logic: normalize (trim trailing dot). Exact match via `RulesByID` plus
  every rule whose ID starts with `id + "."`. Preserve document order.
  Return `{count, results: [{id, text}]}`. Unknown id returns
  `mcp.NewToolResultError`.

Register both in `NewServer`, instrumented, names `search_rules` and
`get_rule`.

### Tests

Parser tests (`internal/data/rules_test.go`): feed a small inline fixture
string (5-6 fake rules with sub-rules and continuation lines) through the
parsing function; assert IDs, text joining, and order. Plus one test against
the real embedded file: `len(store.Rules)` is at least a few hundred and a
spot-check that a well-known rule id (pick one after reading the real
document) exists and mentions an expected phrase.

Tool tests (`internal/mcp/rules_test.go`): `search_rules` with a query like
`go again` returns count > 0 and every result's text contains both terms
case-insensitively; `get_rule` on a chapter id returns more than one entry;
`get_rule` on garbage returns IsError; limit is respected and capped at 25.

### Commit

`feat(mcp): add search_rules and get_rule tools over Comprehensive Rules`

---

## Workstream D: `search_cards` filter and pagination gaps

**Goal:** close cheap gaps between what `data.CardFilter` already supports
and what the MCP tool exposes, and add a few missing filters.

### D1: expose what already exists (no store changes)

`data.CardFilter` (internal/data/store.go, around line 224) already has
`Offset`, `TextQuery`, and `LegalIn` fields, and `SearchCards` already
returns `(results, total)`. The MCP handler (`registerSearchCards` in
tools.go) exposes none of them and discards the total.

1. Add tool params:
   - `offset` (number, optional): `Number of results to skip, for pagination`
   - `legal_in` (string, optional): `Only return cards legal in this format
     (blitz, cc, commoner, ll, silver_age, upf)`
2. Wire them into the filter. Validate `legal_in` with `domain.IsKnownFormat`
   when non-empty; invalid value returns `mcp.NewToolResultError`.
3. Stop discarding the total: `cards, total := s.store.SearchCards(filter)`
   and add `"total"` and `"offset"` to the response JSON alongside `count`
   and `results`.

### D2: new filters (store + tool)

Add to `CardFilter` and `matchesFilter` in internal/data/store.go, following
the existing exact-match style of the `Pitch` filter (card numeric stats are
strings in the domain model; keep string equality):

- `Cost string`: matches `card.Cost` exactly.
- `Power string`: matches `card.Power` exactly.
- `Defense string`: matches `card.Defense` exactly.
- `Rarity string`: matches if ANY of `card.Printings` has
  `strings.EqualFold(printing.Rarity, filter.Rarity)`. Rarity codes in the
  data are short strings (inspect `internal/data/english/rarity.json` for the
  valid codes, e.g. C, R, M, L, F; mention the codes in the tool param
  description).

Add matching tool params (`cost`, `power`, `defense`, `rarity`) with
descriptions. No index changes; these ride on the existing candidate scan.

The REST API also uses `CardFilter` but adding fields is purely additive; do
not touch `internal/api` in this workstream.

### Tests

Store-level (`internal/data/store_test.go`, follow existing patterns):

1. Filter `Cost: "0"` returns only cards with Cost == "0" and at least one
   result.
2. Filter `Rarity: "M"` (verify code against rarity.json first) returns only
   cards having a printing with that rarity.
3. Offset pagination: same filter with Limit 5 Offset 0 and Limit 5 Offset 5
   return disjoint cards, and total is identical in both calls.

Tool-level (`internal/mcp/tools_test.go`):

4. `legal_in=cc` returns only cards where
   `GetLegality(FormatCC).Legal` is true (spot-check the returned IDs against
   the store).
5. Invalid `legal_in=modern` returns IsError.
6. Response contains `total` >= `count`.

### Commit

`feat(mcp): expose pagination, legality, and stat filters in search_cards`

---

## Suggested execution order

1. A (smallest, pure exposure of existing data)
2. B (self-contained math)
3. D (small, touches store + tool)
4. C (largest; has the external sourcing phase and a licensing checkpoint)

A, B, D have no dependencies on each other. C1's licensing check is a hard
gate: if the LSS policy does not clearly permit redistributing the rules
text, stop C and report; A, B, D are unaffected.

## Final verification (after all workstreams)

1. Full quality gate (see Ground rules).
2. Manual smoke test: `MCP_MODE=http go run ./cmd/mcp` and exercise each new
   tool with curl or an MCP client; confirm JSON shapes match this document.
3. `jj log` shows one clean conventional commit per workstream (plus the C1
   data commit).
4. Observability: the recent dashboards are built around per-tool metrics
   emitted by `instrumentTool`. New tools are picked up automatically via the
   `mcp.tool.name` label; no dashboard changes required. Confirm the metric
   appears by hitting a new tool once and checking the `/metrics`-exported
   series (or logs) mention the new tool name.
