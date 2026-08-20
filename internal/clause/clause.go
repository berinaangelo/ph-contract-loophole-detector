// Package clause segments raw contract text into individually-checkable
// clauses. Real lease agreements are paragraph- or numbered-section-shaped
// ("1. TERM...", "2. RENT..."), so blank-line-separated paragraphs are a
// reasonable proxy for "one clause" without needing a real document
// structure parser — same "pick a reasonable default now, revisit if it
// proves wrong" instinct as the rest of this project. A heading-aware
// parser (numbered/lettered sections, PDF layout) is the natural v2 if
// paragraph splitting misses real-world contract formatting too often.
package clause

import "strings"

// Clause is one segment of contract text, numbered in document order so
// findings can be traced back to where in the contract they came from.
type Clause struct {
	Index int
	Text  string
}

// Split breaks text into clauses on blank lines. Empty/whitespace-only
// segments are dropped rather than kept as empty clauses.
func Split(text string) []Clause {
	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")

	clauses := make([]Clause, 0, len(paragraphs))
	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		clauses = append(clauses, Clause{Index: len(clauses) + 1, Text: trimmed})
	}
	return clauses
}
