// Package severity implements the deterministic HIGH-severity short-circuit
// — clause patterns that are illegal or void under Philippine law regardless
// of context. This runs before retrieval or generation ever touch a clause:
// HIGH is the tier a user is most likely to act on immediately (or not
// sign at all), so it has to be rule-based, not model-judgment-based, the
// same reasoning the medical red-flag check used for "seek emergency care
// now." MEDIUM/LOW findings (context-dependent unfairness, best-practice
// gaps) are the RAG+LLM tier — see internal/retrieve and internal/generate.
//
// Every rule cites the specific statute/rule it's based on. This is a
// starter set covering common, well-documented PH residential lease
// loopholes — not exhaustive, and citations should be verified against the
// current text of the law before this is relied on for anything beyond
// research/education (see the disclaimer surfaced with every result).
package severity

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Finding is one HIGH-severity issue located in a clause.
type Finding struct {
	Severity    string // always "HIGH" for this package
	Issue       string
	Statute     string
	Explanation string
	Action      string
	ClauseText  string
}

const highSeverity = "HIGH"

// phraseRule matches when every group has at least one phrase present in
// the (lowercased) clause text — AND across groups, OR within a group.
type phraseRule struct {
	Issue       string
	Groups      [][]string
	Statute     string
	Explanation string
	Action      string
}

// phraseRules are the starter set of well-documented illegal/void lease
// clause patterns that keyword matching alone can catch. Phrases are
// deliberately multi-word wherever a shorter form would false-positive
// (e.g. "cut off electricity" rather than "cut", "cut off" alone).
var phraseRules = []phraseRule{
	{
		Issue: "self-help eviction / lockout clause",
		Groups: [][]string{
			{
				"change the locks", "padlock the premises", "padlock the unit",
				"cut off electricity", "cut off the electricity", "cut off water",
				"cut off the water", "disconnect utilities", "remove the tenant's belongings",
				"remove tenant's belongings", "evict the tenant without", "without a court order",
				"without going to court", "without judicial process", "without filing a case",
			},
		},
		Statute: "Civil Code Art. 536; Rule 70, Rules of Court",
		Explanation: "A landlord cannot lawfully retake possession by self-help — locking out a " +
			"tenant, cutting utilities, or removing belongings without a court order. Eviction " +
			"requires a judicial ejectment (unlawful detainer) action even if the tenant is in default.",
		Action: "This clause is likely unenforceable as written. Do not sign as-is; request it be " +
			"removed or revised to reference the judicial eviction process.",
	},
	{
		Issue: "blanket deposit forfeiture",
		Groups: [][]string{
			{
				"deposit shall be forfeited", "deposit is forfeited", "deposit shall not be refunded",
				"deposit is non-refundable", "forfeit the entire deposit", "forfeit the deposit for any reason",
				"deposit will not be returned",
			},
		},
		Statute: "Civil Code general principles on unjust enrichment; RA 9653 intent (deposit answers " +
			"for unpaid rent/utilities/damages, refundable net of actual deductions)",
		Explanation: "A security deposit exists to cover actual unpaid rent, utilities, or damage — a " +
			"clause forfeiting it outright \"for any reason\" (rather than net of an itemized " +
			"accounting) goes beyond that purpose and is vulnerable to challenge.",
		Action: "Ask for the clause to specify forfeiture only covers documented unpaid amounts or " +
			"damage, with any remaining balance refunded.",
	},
	{
		Issue: "waiver of habitability / repair obligations",
		Groups: [][]string{
			{
				"waives the right to demand repairs", "tenant waives the right to repairs",
				"landlord is not obliged to make any repairs", "not obligated to make any repairs",
				"as-is where-is basis", "tenant assumes all risk of habitability",
				"no warranty of habitability",
			},
		},
		Statute: "Civil Code Art. 1654",
		Explanation: "The Civil Code imposes on the lessor an implied obligation to make necessary " +
			"repairs so the property stays fit for its intended use — a blanket waiver of all repair " +
			"obligations, especially for structural or habitability issues, is not fully enforceable.",
		Action: "Ask that the clause distinguish tenant-caused damage (tenant's responsibility) from " +
			"normal wear/structural issues (landlord's responsibility).",
	},
	{
		Issue: "waiver of legal remedies / improper venue",
		Groups: [][]string{
			{
				"waives all rights under the rent control act", "tenant waives all legal remedies",
				"waives the right to seek legal remedy", "tenant waives any right to sue",
				"exclusive venue shall be", "venue shall lie exclusively in",
			},
		},
		Statute: "Rule 70, Rules of Court (ejectment venue is the location of the property); Civil " +
			"Code Art. 6 (rights contrary to law/public policy cannot be waived in advance)",
		Explanation: "A blanket advance waiver of statutory remedies, or a venue clause that routes " +
			"disputes away from where the property is located, is generally unenforceable for actions " +
			"like ejectment.",
		Action: "Flag this clause for legal review before signing — advance waivers of this kind are " +
			"rarely enforceable as written.",
	},
}

// Check runs every phrase rule and numeric check against one clause's text,
// returning all HIGH findings it triggers (a single clause can trigger more
// than one — e.g. an illegal deposit amount stated alongside a forfeiture
// clause).
func Check(clauseText string) []Finding {
	lower := strings.ToLower(clauseText)

	var findings []Finding
	for _, rule := range phraseRules {
		if rule.matches(lower) {
			findings = append(findings, Finding{
				Severity:    highSeverity,
				Issue:       rule.Issue,
				Statute:     rule.Statute,
				Explanation: rule.Explanation,
				Action:      rule.Action,
				ClauseText:  clauseText,
			})
		}
	}
	findings = append(findings, checkDepositCap(clauseText)...)
	findings = append(findings, checkRentIncreaseCap(clauseText)...)
	findings = append(findings, checkPenaltyInterest(clauseText)...)
	return findings
}

func (r phraseRule) matches(lower string) bool {
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

// monthsAdvanceRE / monthsDepositRE extract "<N> month(s) advance/deposit"
// style phrasing (also matches the shorthand "mo"/"mos"), in either
// "N months advance" or "advance of N months" order. The optional `\)?`
// after the digits absorbs the common "six (6) months" spelled-out-plus-
// parenthetical style PH contracts often use.
var (
	monthsAdvanceRE = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*\)?\s*(?:month|mo)s?\.?\s*(?:'?s)?\s*advance|advance\s*(?:rent\s*)?(?:of\s*)?(\d+(?:\.\d+)?)\s*\)?\s*(?:month|mo)s?`)
	monthsDepositRE = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*\)?\s*(?:month|mo)s?\.?\s*(?:'?s)?\s*(?:security\s*)?deposit|deposit\s*(?:of\s*)?(\d+(?:\.\d+)?)\s*\)?\s*(?:month|mo)s?`)
)

// RA 9653 (Rent Control Act) caps, for units it covers: advance rent up to
// 1 month, security deposit up to 2 months. Coverage itself depends on the
// unit's monthly rent and location, which this package can't determine from
// clause text alone — the finding is phrased as "if this unit is covered."
const (
	maxAdvanceMonths = 1.0
	maxDepositMonths = 2.0
)

func checkDepositCap(clauseText string) []Finding {
	var findings []Finding
	if n, ok := firstNumber(monthsAdvanceRE, clauseText); ok && n > maxAdvanceMonths {
		findings = append(findings, Finding{
			Severity: highSeverity,
			Issue:    "advance rent exceeds Rent Control Act cap",
			Statute:  "RA 9653 (Rent Control Act), Sec. 5",
			Explanation: fmt.Sprintf("This clause requires %s months advance rent. For units covered "+
				"by the Rent Control Act, advance rent is capped at %g month.", formatMonths(n), maxAdvanceMonths),
			Action:     "Confirm whether this unit is covered by the Rent Control Act's rent ceiling; if so, this term exceeds the legal cap and should be renegotiated.",
			ClauseText: clauseText,
		})
	}
	if n, ok := firstNumber(monthsDepositRE, clauseText); ok && n > maxDepositMonths {
		findings = append(findings, Finding{
			Severity: highSeverity,
			Issue:    "security deposit exceeds Rent Control Act cap",
			Statute:  "RA 9653 (Rent Control Act), Sec. 5",
			Explanation: fmt.Sprintf("This clause requires a %s-month security deposit. For units "+
				"covered by the Rent Control Act, the deposit is capped at %g months.", formatMonths(n), maxDepositMonths),
			Action:     "Confirm whether this unit is covered by the Rent Control Act's rent ceiling; if so, this term exceeds the legal cap and should be renegotiated.",
			ClauseText: clauseText,
		})
	}
	return findings
}

// rentIncreaseRE extracts "<N>%" figures near the word "increase" — the
// Rent Control Act caps annual increases (for covered units) at 7%.
var rentIncreaseRE = regexp.MustCompile(`(?i)increas\w*[^.]{0,40}?(\d+(?:\.\d+)?)\s*%|(\d+(?:\.\d+)?)\s*%[^.]{0,40}?increas\w*`)

const maxAnnualIncreasePercent = 7.0

func checkRentIncreaseCap(clauseText string) []Finding {
	n, ok := firstNumber(rentIncreaseRE, clauseText)
	if !ok || n <= maxAnnualIncreasePercent {
		return nil
	}
	return []Finding{{
		Severity: highSeverity,
		Issue:    "rent increase exceeds Rent Control Act cap",
		Statute:  "RA 9653 (Rent Control Act), Sec. 4",
		Explanation: fmt.Sprintf("This clause allows a %g%% rent increase. For units covered by the "+
			"Rent Control Act, annual increases are capped at %g%%.", n, maxAnnualIncreasePercent),
		Action:     "Confirm whether this unit is covered by the Rent Control Act's rent ceiling; if so, this term exceeds the legal cap and should be renegotiated.",
		ClauseText: clauseText,
	}}
}

// penaltyInterestRE extracts "<N>% per day" / "<N>% daily" / "<N>% per
// month" / "<N>% monthly" style penalty-interest phrasing.
var penaltyInterestRE = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*%\s*(per\s*day|daily|per\s*month|monthly)`)

// Thresholds are rough, not statutory bright lines — Philippine courts
// routinely reduce penalty/interest clauses as unconscionable (Civil Code
// Art. 1229) once they get well past normal commercial rates, and case law
// commonly treats rates around/above these as a strong signal, not a hard
// cutoff.
const (
	maxDailyPenaltyPercent   = 1.0
	maxMonthlyPenaltyPercent = 5.0
)

func checkPenaltyInterest(clauseText string) []Finding {
	m := penaltyInterestRE.FindStringSubmatch(clauseText)
	if m == nil {
		return nil
	}
	rate, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	period := strings.ToLower(m[2])
	isDaily := strings.Contains(period, "day")

	threshold := maxMonthlyPenaltyPercent
	unit := "month"
	if isDaily {
		threshold = maxDailyPenaltyPercent
		unit = "day"
	}
	if rate <= threshold {
		return nil
	}

	return []Finding{{
		Severity: highSeverity,
		Issue:    "potentially unconscionable penalty/interest rate",
		Statute:  "Civil Code Art. 1229 (courts may equitably reduce iniquitous or unconscionable penalties)",
		Explanation: fmt.Sprintf("This clause charges %g%% per %s on late/unpaid rent. Philippine courts "+
			"have repeatedly reduced penalty or interest rates well above normal commercial rates as "+
			"unconscionable.", rate, unit),
		Action:     "This rate is high enough to be worth challenging or renegotiating before signing.",
		ClauseText: clauseText,
	}}
}

// firstNumber runs re against text and returns the first non-empty
// capture group parsed as a float, since these patterns alternate between
// two orderings ("N months advance" vs "advance of N months") using
// separate capture groups.
func firstNumber(re *regexp.Regexp, text string) (float64, bool) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	for _, group := range m[1:] {
		if group == "" {
			continue
		}
		if n, err := strconv.ParseFloat(group, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func formatMonths(n float64) string {
	return strconv.FormatFloat(n, 'g', -1, 64)
}
