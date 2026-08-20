package main

import "testing"

func TestParseCorpus_Valid(t *testing.T) {
	raw := `[
		{"id": "a", "title": "A", "source": "civil_code", "citation": "Art. 1", "issue_tags": ["security_deposit"], "text": "text a"},
		{"id": "b", "title": "B", "source": "clause_pattern", "citation": "Arts. 19, 24", "issue_tags": ["rent_increase"], "text": "text b"}
	]`
	entries, err := parseCorpus([]byte(raw))
	if err != nil {
		t.Fatalf("parseCorpus: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].ID != "a" || entries[1].ID != "b" {
		t.Errorf("entries = %+v, unexpected ids", entries)
	}
}

func TestParseCorpus_MalformedJSON(t *testing.T) {
	if _, err := parseCorpus([]byte("not json")); err == nil {
		t.Fatal("parseCorpus(malformed) = nil error, want an error")
	}
}

func TestParseCorpus_EmptyID(t *testing.T) {
	raw := `[{"id": "", "title": "no id", "text": "some text"}]`
	if _, err := parseCorpus([]byte(raw)); err == nil {
		t.Fatal("parseCorpus(empty id) = nil error, want an error")
	}
}

func TestParseCorpus_EmptyText(t *testing.T) {
	raw := `[{"id": "a", "text": ""}]`
	if _, err := parseCorpus([]byte(raw)); err == nil {
		t.Fatal("parseCorpus(empty text) = nil error, want an error")
	}
}

func TestParseCorpus_DuplicateID(t *testing.T) {
	raw := `[
		{"id": "a", "text": "first"},
		{"id": "a", "text": "second"}
	]`
	if _, err := parseCorpus([]byte(raw)); err == nil {
		t.Fatal("parseCorpus(duplicate id) = nil error, want an error")
	}
}

func TestChunk_MapsAllFields(t *testing.T) {
	e := corpusEntry{
		ID: "x", Title: "T", Source: "civil_code", Citation: "Art. 1",
		IssueTags: []string{"security_deposit"}, Text: "body",
	}
	c := e.chunk()
	if c.ID != e.ID || c.Title != e.Title || c.Source != e.Source ||
		c.Citation != e.Citation || c.Text != e.Text || len(c.IssueTags) != 1 || c.IssueTags[0] != "security_deposit" {
		t.Errorf("chunk() = %+v, missing/mismatched fields from %+v", c, e)
	}
}

func TestEntriesWithNoIssueTags(t *testing.T) {
	entries := []corpusEntry{
		{ID: "has-tags", IssueTags: []string{"security_deposit"}},
		{ID: "no-tags", IssueTags: nil},
		{ID: "empty-tags", IssueTags: []string{}},
	}
	got := entriesWithNoIssueTags(entries)

	want := map[string]bool{"no-tags": true, "empty-tags": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want ids %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q flagged", id)
		}
	}
}

func TestEntriesWithUnknownSource(t *testing.T) {
	entries := []corpusEntry{
		{ID: "ok1", Source: "civil_code"},
		{ID: "ok2", Source: "ra9653"},
		{ID: "ok3", Source: "rules_of_court"},
		{ID: "ok4", Source: "clause_pattern"},
		{ID: "typo", Source: "civl_code"},
		{ID: "empty-source", Source: ""},
	}
	got := entriesWithUnknownSource(entries)

	want := map[string]bool{"typo": true, "empty-source": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want ids %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q flagged as unknown source", id)
		}
	}
}
