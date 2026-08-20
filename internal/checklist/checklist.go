// Package checklist implements the deterministic LOW-severity "missing
// clause" gap check — the fourth finding bucket from the brainstorm
// (best-practice gaps, not risky per se, just weaker than standard). Unlike
// severity's per-clause HIGH check, this runs once over the whole contract:
// a topic is either addressed somewhere in the document or it isn't. No
// RAG/LLM call needed — same "deterministic where the answer doesn't
// require judgment" instinct as internal/severity.
package checklist

import "strings"

// Finding is one expected clause topic the contract doesn't appear to
// address.
type Finding struct {
	Severity    string // always "LOW" for this package
	Topic       string
	Explanation string
	Action      string
}

const lowSeverity = "LOW"

// topic is one expected-clause-topic check: present if any of its keywords
// appears anywhere in the (lowercased) contract text.
type topic struct {
	Name        string
	Keywords    []string
	Explanation string
	Action      string
}

// topics are the expected-clause-topics starter set for PH residential
// lease agreements. Not exhaustive — a reasonable default v1 checklist,
// same instinct as severity's starter rule set.
var topics = []topic{
	{
		Name:        "repairs and maintenance responsibility",
		Keywords:    []string{"repair", "maintenance", "maintain the"},
		Explanation: "The contract doesn't appear to specify who is responsible for repairs and maintenance.",
		Action:      "Consider adding a clause assigning repair/maintenance responsibility between landlord and tenant.",
	},
	{
		Name:        "grounds and notice period for termination",
		Keywords:    []string{"terminat", "notice period", "days' notice", "days notice"},
		Explanation: "The contract doesn't appear to state the grounds or notice period for early termination by either party.",
		Action:      "Consider adding a clause defining valid grounds for termination and a minimum notice period.",
	},
	{
		Name:        "renewal terms",
		Keywords:    []string{"renew", "extension of this lease", "extend the lease"},
		Explanation: "The contract doesn't appear to describe how (or whether) the lease can be renewed.",
		Action:      "Consider adding a renewal clause specifying the process, notice period, and any rent adjustment.",
	},
	{
		Name:        "dispute resolution / venue",
		Keywords:    []string{"dispute", "arbitration", "mediation", "venue", "jurisdiction"},
		Explanation: "The contract doesn't appear to specify how disputes between the parties will be resolved.",
		Action:      "Consider adding a dispute-resolution clause (e.g. barangay conciliation as legally required for parties in the same city/municipality, then venue).",
	},
	{
		Name:        "utilities responsibility",
		Keywords:    []string{"utilit", "electric", "water bill", "internet"},
		Explanation: "The contract doesn't appear to state who pays for utilities (electricity, water, etc.).",
		Action:      "Consider adding a clause assigning responsibility for utility bills.",
	},
	{
		Name:        "subletting / assignment policy",
		Keywords:    []string{"sublet", "sublease", "assign this lease", "assignment of this"},
		Explanation: "The contract doesn't appear to state whether subletting or assigning the lease is allowed.",
		Action:      "Consider adding a clause explicitly allowing or prohibiting subletting/assignment.",
	},
	{
		Name:        "house rules / property use restrictions",
		Keywords:    []string{"house rules", "pets", "use of the premises", "residential use only"},
		Explanation: "The contract doesn't appear to reference house rules or restrictions on use of the premises.",
		Action:      "Consider adding or attaching house rules covering pets, noise, guests, and permitted use.",
	},
}

// leaseMarkers are words a genuine lease/rental contract is expected to
// use somewhere. Check's own topics are all keyword-*absence* checks,
// which can't tell "this lease is missing a clause" apart from "this text
// isn't a lease at all" — both look identical to an absence check. So
// callers must gate on LooksLikeLease before running Check at all,
// instead of trying to infer non-lease input from Check's output.
var leaseMarkers = []string{"landlord", "lessor", "tenant", "lessee", "lease", "rent"}

// minLeaseMarkers is deliberately low — real lease text clears it by a
// wide margin (any contract mentions "tenant"/"landlord" and "rent"/
// "lease" repeatedly), so this only screens out clearly off-topic input,
// not borderline-but-real leases.
const minLeaseMarkers = 2

// LooksLikeLease reports whether contractText contains enough lease-marker
// vocabulary to be worth running Check (and the rest of the analysis
// pipeline) on at all.
func LooksLikeLease(contractText string) bool {
	lower := strings.ToLower(contractText)
	found := 0
	for _, marker := range leaseMarkers {
		if strings.Contains(lower, marker) {
			found++
			if found >= minLeaseMarkers {
				return true
			}
		}
	}
	return false
}

// Check reports which expected-clause topics don't appear anywhere in
// contractText. Callers should only call this once LooksLikeLease(text)
// is true — see LooksLikeLease's doc comment for why.
func Check(contractText string) []Finding {
	lower := strings.ToLower(contractText)

	var findings []Finding
	for _, t := range topics {
		if anyContains(lower, t.Keywords) {
			continue
		}
		findings = append(findings, Finding{
			Severity:    lowSeverity,
			Topic:       t.Name,
			Explanation: t.Explanation,
			Action:      t.Action,
		})
	}
	return findings
}

func anyContains(lower string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
