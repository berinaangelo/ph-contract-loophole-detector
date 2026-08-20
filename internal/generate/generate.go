// Package generate is the RAG+LLM tier's second half (PLAN.md Primary
// flow step 4): one or a few structured-output Ollama calls that turn
// internal/retrieve's per-clause candidates into MEDIUM/LOW findings, each
// with a short plain-language explanation and a suggested action. The
// LLM's only job is those two lines — the excerpt, citation, and clause
// text always come straight from retrieval metadata (never generated), so
// they can't be hallucinated, same contract internal/severity and
// internal/checklist already hold for their own findings.
//
// Batches per contract, not per clause (PLAN.md Design decisions): a
// clause is quick, but a whole lease's surviving candidates can plausibly
// exceed one structured-output call's safe size on a 7B local model, so
// Answer chunks them into capped, sequential batches instead of one call
// per clause or one unbounded call per contract.
package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ollama/ollama/api"

	"ph-contract-loophole-detector/internal/store"
)

// Model is the generation model PLAN.md's defaults settled on.
const Model = "qwen2.5:7b"

// BatchCap is the most candidates sent in one structured-output call —
// PLAN.md Design decisions' starting cap, verified live against Model at
// this size during implementation. A typical lease's surviving candidates
// still only take ~2-3 sequential calls at this cap, nowhere near one call
// per clause.
const BatchCap = 10

const (
	mediumSeverity = "MEDIUM"
	lowSeverity    = "LOW"

	// clausePatternSource is the one store.Chunk.Source value that maps to
	// MEDIUM (data/corpus/README.md: "clause_pattern entries are for the
	// MEDIUM tier — clauses that are legal but one-sided"). Every other
	// source (civil_code, ra9653, rules_of_court) maps to LOW.
	clausePatternSource = "clause_pattern"
)

// fallbackExplanation and fallbackAction are used when the model's
// response is missing or empty for a candidate (small local models aren't
// perfectly reliable) — keeps every finding well-formed rather than
// partial or requiring a retry loop, same instinct as the old app's single
// fallbackReason.
const (
	fallbackExplanation = "This clause matches a known issue area, but a plain-language explanation could not be generated — see the citation and excerpt above."
	fallbackAction      = "Review this clause against the citation above, or ask a lawyer to weigh in."
)

// systemPrompt carries PLAN.md's safety framing explicitly: educational
// tool, never "sign or don't," and — for clause_pattern-backed candidates
// specifically — never state or imply the clause is illegal (that's
// internal/severity's deterministic HIGH tier's job, not this one's).
const systemPrompt = `You are assisting a tool that reviews Philippine residential lease clauses for research/educational purposes, always paired with advice to consult a lawyer — never a diagnosis of "sign" or "don't sign." For each candidate given, write a short (1-2 sentence) plain-language explanation of why the excerpt is relevant to the clause, and a short concrete suggested action the user could take (e.g. what to ask the landlord to change, or to seek legal review). Reason only from the clause text and excerpt provided — never invent facts not present in them. If a candidate is marked "legal but one-sided," phrase the explanation that way — legal to write, but worth negotiating — never state or imply the clause is illegal.`

// Candidate is one clause+issue pairing surviving internal/retrieve's
// relevance floor, ready to be explained. internal/server builds these by
// zipping internal/clause.Split's output with each clause's
// internal/retrieve.Query results — generate itself never calls retrieve
// or touches live store/Qdrant/Bleve clients.
type Candidate struct {
	ClauseIndex int
	ClauseText  string
	Issue       string
	Chunk       store.Chunk
}

// Finding is one fully-assembled RAG+LLM finding — PLAN.md's shared
// Finding shape, generate's own version of it (internal/server unifies
// this with severity.Finding and checklist.Finding, per PLAN.md Design
// decisions; the three source packages deliberately don't share a struct
// directly).
type Finding struct {
	Severity    string // "MEDIUM" or "LOW", from Candidate.Chunk.Source
	Issue       string
	Explanation string
	Action      string
	Citation    string
	ClauseText  string
}

// Answer is generate's entry point, called once per contract (not once
// per clause or per candidate). If candidates is empty, it returns (nil,
// nil) without calling the LLM at all — same instinct as not calling a
// model with nothing grounded to hand it.
func Answer(ctx context.Context, ollama *api.Client, candidates []Candidate) ([]Finding, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	var findings []Finding
	for _, group := range chunkCandidates(candidates, BatchCap) {
		batchFindings, err := batch(ctx, ollama, group)
		if err != nil {
			return nil, fmt.Errorf("generate: %w", err)
		}
		findings = append(findings, batchFindings...)
	}
	return findings, nil
}

// chunkCandidates splits candidates into groups of at most cap items,
// preserving order — the batching PLAN.md's Design decisions calls for,
// so one contract's worth of candidates becomes a small number of
// sequential calls instead of one unbounded call or one call per clause.
func chunkCandidates(candidates []Candidate, cap int) [][]Candidate {
	var groups [][]Candidate
	for len(candidates) > 0 {
		n := cap
		if n > len(candidates) {
			n = len(candidates)
		}
		groups = append(groups, candidates[:n])
		candidates = candidates[n:]
	}
	return groups
}

// candidateID builds the composite id a candidate is keyed by in the
// batch schema — (clause index, issue tag), not issue tag alone, since
// two different clauses in one contract can independently surface the
// same issue tag (PLAN.md Design decisions).
func candidateID(c Candidate) string {
	return fmt.Sprintf("c%d_%s", c.ClauseIndex, c.Issue)
}

// llmLine is one candidate's model-generated explanation + action.
type llmLine struct {
	Explanation string
	Action      string
}

// findingsResponse is the shape requested via the structured-output
// schema built by buildSchema.
type findingsResponse struct {
	Findings []struct {
		ID          string `json:"id"`
		Explanation string `json:"explanation"`
		Action      string `json:"action"`
	} `json:"findings"`
}

// batch makes one structured-output Generate request for group (at most
// BatchCap candidates) and returns their assembled Findings — the live
// half of Answer.
func batch(ctx context.Context, ollama *api.Client, group []Candidate) ([]Finding, error) {
	ids := make([]string, len(group))
	for i, c := range group {
		ids[i] = candidateID(c)
	}

	stream := false
	req := &api.GenerateRequest{
		Model:  Model,
		System: systemPrompt,
		Prompt: buildPrompt(group),
		Format: buildSchema(ids),
		Stream: &stream,
	}

	var responseText string
	err := ollama.Generate(ctx, req, func(resp api.GenerateResponse) error {
		responseText = resp.Response
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ollama generate: %w", err)
	}

	var parsed findingsResponse
	if err := json.Unmarshal([]byte(responseText), &parsed); err != nil {
		return nil, fmt.Errorf("parse structured response: %w", err)
	}

	lines := make(map[string]llmLine, len(parsed.Findings))
	for _, item := range parsed.Findings {
		if _, exists := lines[item.ID]; !exists {
			lines[item.ID] = llmLine{Explanation: item.Explanation, Action: item.Action}
		}
	}
	return assemble(group, lines), nil
}

// buildPrompt renders the numbered candidate list the model reasons over:
// each candidate's composite id, clause text (quoted verbatim, so the
// model explains *this* clause, not a generic one), issue tag, and
// grounding excerpt + citation.
func buildPrompt(group []Candidate) string {
	var b strings.Builder
	b.WriteString("Candidate clause/issue pairs:\n\n")
	for _, c := range group {
		fmt.Fprintf(&b, "id: %s\nissue: %s\nclause: %q\nexcerpt: %q\ncitation: %s\nlegal but one-sided: %t\n\n",
			candidateID(c), c.Issue, c.ClauseText, c.Chunk.Text, c.Chunk.Citation, c.Chunk.Source == clausePatternSource)
	}
	fmt.Fprintf(&b, "Respond with an explanation and action for each of the %d ids listed above.", len(group))
	return b.String()
}

// buildSchema builds the JSON Schema sent as GenerateRequest.Format.
// Constraining "id" to an enum of exactly this batch's composite ids means
// the model structurally cannot answer a candidate it wasn't asked about
// — same trick the old app's condition-enum schema used.
func buildSchema(ids []string) json.RawMessage {
	n := len(ids)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"findings": map[string]any{
				"type":     "array",
				"minItems": n,
				"maxItems": n,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string", "enum": ids},
						"explanation": map[string]any{"type": "string"},
						"action":      map[string]any{"type": "string"},
					},
					"required": []string{"id", "explanation", "action"},
				},
			},
		},
		"required": []string{"findings"},
	}
	// Safe to ignore the error: schema is built entirely from Go maps of
	// strings/ints/slices, always marshals cleanly.
	raw, _ := json.Marshal(schema)
	return raw
}

// assemble zips each candidate with its line by composite id — pure,
// unit-tested without live Ollama. A candidate the model's response is
// missing (or left an empty field for) gets the fallback text instead of
// a partial/broken finding. Severity is assigned here too, purely from
// candidate.Chunk.Source.
func assemble(group []Candidate, lines map[string]llmLine) []Finding {
	out := make([]Finding, len(group))
	for i, c := range group {
		line, ok := lines[candidateID(c)]
		explanation := line.Explanation
		if !ok || explanation == "" {
			explanation = fallbackExplanation
		}
		action := line.Action
		if !ok || action == "" {
			action = fallbackAction
		}

		out[i] = Finding{
			Severity:    severityFor(c.Chunk.Source),
			Issue:       c.Issue,
			Explanation: explanation,
			Action:      action,
			Citation:    c.Chunk.Citation,
			ClauseText:  c.ClauseText,
		}
	}
	return out
}

// severityFor maps a chunk's source to the RAG tier's severity —
// confirmed rule: clause_pattern -> MEDIUM, everything else
// (civil_code/ra9653/rules_of_court) -> LOW.
func severityFor(source string) string {
	if source == clausePatternSource {
		return mediumSeverity
	}
	return lowSeverity
}
