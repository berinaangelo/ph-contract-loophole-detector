// Package redflag implements the deterministic emergency short-circuit
// (PLAN.md "Primary flow" step 3). It runs after the language gate and,
// if triggered, skips retrieval and the LLM entirely — safety framing
// requires this to be rule-based, not model-judgment-based (NOTES.md §5).
package redflag

import "strings"

// Headline and Message are the fixed copy for the "seek emergency care"
// short-circuit, copied verbatim from the red-flag result screen in
// knowledge-base/mockups/query-flow.html so the UI and backend never
// drift on wording. Exported so the future HTTP layer (build order step
// 7) has a single source of truth instead of duplicating the copy.
const (
	Headline = "⚠ SEEK EMERGENCY CARE NOW"
	Message  = "Your symptoms may indicate a medical emergency. Call your local emergency number or go to the nearest emergency department immediately."
)

// Rule matches when every group has at least one phrase present in the
// (lowercased) input text — AND across groups, OR within a group. A
// single-group rule triggers on any one of its phrases alone.
type Rule struct {
	Name   string
	Groups [][]string
}

// Result identifies whether a rule triggered, and which one, so callers
// (and tests) can verify why the short-circuit fired, not just that it
// did.
type Result struct {
	Triggered bool
	RuleName  string
}

// Rules is the starter set of well-known clinical emergency categories.
// Phrases are deliberately multi-word wherever a single common word would
// false-positive via substring matching (e.g. "arm weakness" rather than
// "arm", since the latter would match inside "alarm" or "farm").
var Rules = []Rule{
	{
		Name: "possible heart attack",
		Groups: [][]string{
			{"chest pain", "chest pressure", "chest tightness"},
			{"shortness of breath", "trouble breathing", "difficulty breathing", "can't breathe", "cant breathe"},
		},
	},
	{
		Name: "possible stroke (FAST signs)",
		Groups: [][]string{
			{
				"facial droop", "face drooping", "arm weakness", "slurred speech",
				"sudden confusion", "sudden trouble speaking", "sudden trouble seeing",
				"sudden numbness on one side",
			},
		},
	},
	{
		Name: "anaphylaxis / severe allergic reaction",
		Groups: [][]string{
			{
				"swelling of face", "swelling of throat", "swelling in my throat",
				"throat closing", "throat is closing", "tongue swelling", "throat swelling",
			},
			{"trouble breathing", "wheezing", "can't breathe", "cant breathe"},
		},
	},
	{
		Name: "severe / uncontrolled bleeding",
		Groups: [][]string{
			{"won't stop bleeding", "wont stop bleeding", "uncontrolled bleeding", "bleeding heavily", "severe bleeding"},
		},
	},
	{
		Name: "loss of consciousness",
		Groups: [][]string{
			{"passed out", "lost consciousness", "unresponsive", "can't wake up", "cant wake up"},
		},
	},
	{
		Name: "severe difficulty breathing at rest",
		Groups: [][]string{
			{"can't breathe", "cant breathe", "gasping for air", "turning blue", "lips turning blue"},
		},
	},
	{
		Name: "thunderclap headache",
		Groups: [][]string{
			{"worst headache of my life", "sudden severe headache", "thunderclap headache"},
		},
	},
}

// Check reports whether text matches any red-flag rule. It returns the
// first matching rule; callers only need to know a match occurred to
// short-circuit, but the rule name is surfaced for testability and future
// logging/eval use.
func Check(text string) Result {
	lower := strings.ToLower(text)
	for _, rule := range Rules {
		if rule.matches(lower) {
			return Result{Triggered: true, RuleName: rule.Name}
		}
	}
	return Result{}
}

// matches reports whether every group in the rule has at least one phrase
// present in lower (which must already be lowercased).
func (r Rule) matches(lower string) bool {
	for _, group := range r.Groups {
		if !anyContains(lower, group) {
			return false
		}
	}
	return true
}

func anyContains(lower string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
