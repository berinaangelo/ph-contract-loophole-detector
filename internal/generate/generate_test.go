package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"ph-contract-loophole-detector/internal/store"
)

func TestCandidateID_KeysByClauseAndIssue(t *testing.T) {
	a := Candidate{ClauseIndex: 3, Issue: "termination_notice"}
	b := Candidate{ClauseIndex: 7, Issue: "termination_notice"}

	idA, idB := candidateID(a), candidateID(b)
	if idA == idB {
		t.Fatalf("candidateID collided for two different clauses with the same issue: %q == %q", idA, idB)
	}
	if idA != "c3_termination_notice" || idB != "c7_termination_notice" {
		t.Errorf("candidateID = %q / %q, want c3_termination_notice / c7_termination_notice", idA, idB)
	}
}

func TestChunkCandidates_SplitsAtCap(t *testing.T) {
	candidates := make([]Candidate, 25)
	groups := chunkCandidates(candidates, 10)

	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	if len(groups[0]) != 10 || len(groups[1]) != 10 || len(groups[2]) != 5 {
		t.Errorf("group sizes = %d/%d/%d, want 10/10/5", len(groups[0]), len(groups[1]), len(groups[2]))
	}
}

func TestChunkCandidates_UnderCapIsOneGroup(t *testing.T) {
	candidates := make([]Candidate, 4)
	groups := chunkCandidates(candidates, 10)
	if len(groups) != 1 || len(groups[0]) != 4 {
		t.Fatalf("got %d groups, want 1 group of 4", len(groups))
	}
}

func TestAssemble_SeverityByChunkSource(t *testing.T) {
	group := []Candidate{
		{ClauseIndex: 1, Issue: "rent_increase", Chunk: store.Chunk{Source: "clause_pattern"}},
		{ClauseIndex: 2, Issue: "security_deposit", Chunk: store.Chunk{Source: "civil_code"}},
		{ClauseIndex: 3, Issue: "eviction_process", Chunk: store.Chunk{Source: "ra9653"}},
		{ClauseIndex: 4, Issue: "dispute_resolution_venue", Chunk: store.Chunk{Source: "rules_of_court"}},
	}

	got := assemble(group, nil)

	want := []string{"MEDIUM", "LOW", "LOW", "LOW"}
	for i, w := range want {
		if got[i].Severity != w {
			t.Errorf("got[%d].Severity = %q, want %q (source %q)", i, got[i].Severity, w, group[i].Chunk.Source)
		}
	}
}

func TestAssemble_ZipsByCompositeID(t *testing.T) {
	group := []Candidate{
		{ClauseIndex: 1, Issue: "security_deposit", ClauseText: "clause one", Chunk: store.Chunk{Citation: "cite1"}},
		{ClauseIndex: 2, Issue: "eviction_process", ClauseText: "clause two", Chunk: store.Chunk{Citation: "cite2"}},
	}
	lines := map[string]llmLine{
		"c1_security_deposit": {Explanation: "exp1", Action: "act1"},
		"c2_eviction_process": {Explanation: "exp2", Action: "act2"},
	}

	got := assemble(group, lines)

	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	if got[0].Explanation != "exp1" || got[0].Action != "act1" || got[0].Citation != "cite1" || got[0].ClauseText != "clause one" {
		t.Errorf("got[0] = %+v, unexpected", got[0])
	}
	if got[1].Explanation != "exp2" || got[1].Action != "act2" {
		t.Errorf("got[1] = %+v, unexpected", got[1])
	}
}

func TestAssemble_FallsBackWhenCandidateMissing(t *testing.T) {
	group := []Candidate{
		{ClauseIndex: 1, Issue: "security_deposit"},
		{ClauseIndex: 2, Issue: "eviction_process"},
	}
	// Model only returned a line for the first candidate.
	lines := map[string]llmLine{"c1_security_deposit": {Explanation: "exp1", Action: "act1"}}

	got := assemble(group, lines)

	if got[1].Explanation != fallbackExplanation || got[1].Action != fallbackAction {
		t.Errorf("got[1] = %+v, want fallback explanation/action for missing candidate", got[1])
	}
	// The present candidate is unaffected.
	if got[0].Explanation != "exp1" || got[0].Action != "act1" {
		t.Errorf("got[0] = %+v, unaffected candidate altered", got[0])
	}
}

func TestAssemble_EmptyFieldsAlsoFallBackIndependently(t *testing.T) {
	group := []Candidate{{ClauseIndex: 1, Issue: "utilities"}}
	// Explanation present, action empty — each field falls back on its own.
	lines := map[string]llmLine{"c1_utilities": {Explanation: "real explanation", Action: ""}}

	got := assemble(group, lines)

	if got[0].Explanation != "real explanation" {
		t.Errorf("Explanation = %q, want the real (non-empty) explanation preserved", got[0].Explanation)
	}
	if got[0].Action != fallbackAction {
		t.Errorf("Action = %q, want fallback for empty action", got[0].Action)
	}
}

func TestBuildSchema_EnumMatchesIDsExactly(t *testing.T) {
	ids := []string{"c1_security_deposit", "c2_eviction_process"}
	raw := buildSchema(ids)

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("buildSchema produced invalid JSON: %v", err)
	}

	findings := schema["properties"].(map[string]any)["findings"].(map[string]any)
	if int(findings["minItems"].(float64)) != 2 || int(findings["maxItems"].(float64)) != 2 {
		t.Errorf("minItems/maxItems = %v/%v, want 2/2", findings["minItems"], findings["maxItems"])
	}

	items := findings["items"].(map[string]any)
	idProp := items["properties"].(map[string]any)["id"].(map[string]any)
	enum := idProp["enum"].([]any)
	if len(enum) != 2 || enum[0] != ids[0] || enum[1] != ids[1] {
		t.Errorf("enum = %v, want exactly %v", enum, ids)
	}
}

func TestBuildPrompt_IncludesClauseTextAndCitation(t *testing.T) {
	group := []Candidate{
		{ClauseIndex: 1, Issue: "security_deposit", ClauseText: "Tenant forfeits the deposit for any reason.",
			Chunk: store.Chunk{Text: "deposit excerpt", Citation: "RA 9653, Sec. 5", Source: "ra9653"}},
	}
	prompt := buildPrompt(group)

	for _, want := range []string{"c1_security_deposit", "Tenant forfeits the deposit for any reason.", "deposit excerpt", "RA 9653, Sec. 5"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q; got: %s", want, prompt)
		}
	}
}

func TestSeverityFor(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"clause_pattern", "MEDIUM"},
		{"civil_code", "LOW"},
		{"ra9653", "LOW"},
		{"rules_of_court", "LOW"},
	}
	for _, c := range cases {
		if got := severityFor(c.source); got != c.want {
			t.Errorf("severityFor(%q) = %q, want %q", c.source, got, c.want)
		}
	}
}
