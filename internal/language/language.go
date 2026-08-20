// Package language implements the pre-retrieval language gate. v1 only
// supports English contract text — mixed English/Filipino ("Taglish") is a
// known real-world case for informal PH leases, but deferred past v1 so the
// severity rules and corpus don't have to handle two languages' phrasing on
// day one. Nothing downstream (severity checks, retrieval, generation) runs
// if this gate fails.
package language

import "strings"

// UnsupportedMessage is shown when the gate rejects input. Exported so the
// HTTP layer and its tests share one string instead of retyping it.
const UnsupportedMessage = "this tool currently supports English-language contract text only"

// commonWords is a small set of high-frequency English function words.
// Legal/contract prose leans on these constantly ("shall", "the", "of",
// "party") regardless of subject matter, so a text genuinely written in
// English will clear the threshold below easily; non-English text won't.
var commonWords = map[string]bool{
	"the": true, "and": true, "of": true, "to": true, "in": true, "a": true,
	"is": true, "for": true, "or": true, "shall": true, "by": true, "on": true,
	"this": true, "that": true, "with": true, "as": true, "be": true, "party": true,
	"agreement": true, "lease": true, "tenant": true, "landlord": true, "rent": true,
	"any": true, "not": true, "will": true, "if": true, "at": true,
}

// minWordsToJudge is the shortest input length treated as too short for the
// word-frequency signal to be meaningful — short snippets pass through
// rather than risk a false rejection.
const minWordsToJudge = 5

// minRecognizedRatio is the fraction of words that must be recognized
// common English words for text to pass. Real English lease prose clears
// this by a wide margin (the/of/shall/tenant/landlord recur constantly);
// most other languages won't hit it at all.
const minRecognizedRatio = 0.12

// IsEnglish reports whether text looks like English prose, using a
// stopword-frequency heuristic rather than a full language-ID model —
// consistent with the project's "pick a reasonable default now" instinct
// for anything not yet load-bearing enough to justify a dependency.
func IsEnglish(text string) bool {
	words := strings.Fields(strings.ToLower(text))
	if len(words) < minWordsToJudge {
		return true
	}

	recognized := 0
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()\"'")
		if commonWords[w] {
			recognized++
		}
	}
	return float64(recognized)/float64(len(words)) >= minRecognizedRatio
}
