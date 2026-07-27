# Comprehensive Rules data

`comprehensive-rules.txt` is the official Flesh and Blood Comprehensive
Rules, published by Legend Story Studios Limited, embedded verbatim for the
`search_rules` and `get_rule` MCP tools (Workstream C, phase C2).

## Source

- URL: https://rules.fabtcg.com/txt/latest/en-fab-cr.txt
- Format: plain text export, served directly by Legend Story Studios
  alongside the PDF at https://rules.fabtcg.com/pdf/en-fab-cr.pdf. The text
  export was used as-is because it already has no page numbers, running
  headers, or footers to strip; a PDF re-extraction was not needed.
- Document version: 2.14.0
- Document date: 2026-06-10 (both printed on the PDF cover page; the text
  export carries neither, so the PDF is downloaded solely to read them)
- Retrieval date: 2026-07-27

## Conversion

```
curl -sL -A "Mozilla/5.0" -f https://rules.fabtcg.com/txt/latest/en-fab-cr.txt -o en-fab-cr.txt
perl -pe 's/[ \t]+$//' en-fab-cr.txt > comprehensive-rules.txt
```

Only trailing whitespace was stripped from each line. No other changes were
made: the text is otherwise byte-identical to what Legend Story Studios
serves. See `scripts/sync-rules.sh` for the full, automated version of this
process, including the PDF cover-page check used to confirm the version and
date above.

## Numbering convention observed

The rules follow the Chapter.Section.Rule scheme the document itself
describes in its preface ("The rules are presented in the form
Chapter.Section.Rule"):

- Chapter headings: a bare integer plus a title, e.g. `9 Additional Rules`.
  Pattern: `^[0-9]+ [A-Za-z]`.
- Section headings: `chapter.section` plus a title, e.g. `1.0 General`. No
  trailing dot. Pattern: `^[0-9]+\.[0-9]+ [A-Za-z]`.
- Rule entries: `chapter.section.rule`, optionally followed by a single
  lowercase letter for a lettered sub-rule, e.g. `1.0.1` and `1.0.1a`. No
  trailing dot (the PDF renders these with a trailing dot and indentation;
  the text export does not). Pattern: `^[0-9]+\.[0-9]+\.[0-9]+[a-z]?\s`.
  Continuation lines (including `Example:` lines) belong to the preceding
  rule entry and do not start with this pattern.

1182 lines in the file match the rule-entry pattern above.

### Known irregularity: Glossary and Acknowledgments placement

The Glossary and Acknowledgments sections are present but appear inline
between chapters 1/2 and 3, not at the end of the document as the PDF's
table of contents implies (Glossary p.103, Acknowledgments p.119, both after
chapter 9). Concretely: chapter 1 is immediately followed by a `1 Glossary`
heading, then chapter 2, then a `2 Acknowledgments` heading, then chapters
3-9 in order. This is how Legend Story Studios' own text export orders them;
it was left as-is to keep the file unmodified.

The Glossary entries also do not follow the Chapter.Section.Rule pattern:
they are numbered `1.-1.N` (e.g. `1.-1.1 (1H)`), a literal `-1` used as a
section placeholder, distinct from the numeric `chapter.section` used
everywhere else. They will not match the rule-entry regex above and need
separate handling if the Glossary is ever indexed.

## Licensing basis

This project is a non-commercial, open source fan project (not a commercial
entity), does not directly monetize this data or the tools built on it, and
redistributes the text unmodified. This is covered by the Legend Story
Studios "Terms of Use for Game and Studio Assets and IP" (their fan content
policy) at:

https://fabtcg.com/resources/terms-use-licensed-assets/

Under "FLESH AND BLOOD THIRD PARTY APPLICATIONS", the policy grants
permission to build:

- "Rules Enforcement Applications": "You may create Third Party Apps that
  provide rules enforcement functions ("Rules Apps") as long as the use of
  assets to create said Rules Apps is compliant with the rest of this
  document."
- "APIs": "You may create APIs that transfer content related to the Game as
  long as said API is not directly monetized."

"Assets" is defined to include "any written content and lore created by the
Studio," which covers the Comprehensive Rules text. Conditions that apply
and must be preserved by anything built on this data:

- Not a commercial entity ("You may not create Third Party Apps if you are a
  commercial entity").
- No direct monetization (indirect monetization such as donations/ad-sense
  is permitted).
- A disclaimer of non-affiliation with Legend Story Studios, per the policy's
  required Third Party App disclaimer.
- A copyright attribution "© Legend Story Studios" where possible.

The Comprehensive Rules document's own front matter (preface, acknowledgments
page) carries only a standard copyright notice ("© 2019 Legend Story
Studios. All Rights Reserved.") and does not itself grant a redistribution
license, so the Third Party Applications policy above is the operative
permission, not the document's own front matter.

## Refresh

Run `./scripts/sync-rules.sh` to re-download, re-check the version, and
re-clean the file. Update the version, date, and retrieval date in this
README when it changes.
