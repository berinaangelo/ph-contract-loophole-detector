// Package store is the corpus write/read side for both retrieval
// backends (PLAN.md's Qdrant dense + Bleve keyword hybrid). Ingestion
// (cmd/ingest) writes here; retrieval (internal/retrieve) reads from
// here — sharing this package keeps both sides agreeing on one Chunk
// shape and one ID scheme instead of duplicating either.
package store

import (
	"github.com/google/uuid"
)

// Chunk is one retrievable unit of the lease knowledge base — a Civil
// Code/RA 9653/Rules of Court excerpt or a clause-pattern entry from
// data/corpus/lease_corpus.json (see that file's README for the schema).
type Chunk struct {
	// ID is the logical, human-readable identifier, e.g.
	// "civil_code_1654". Stable across re-ingestion runs so re-running
	// ingestion overwrites the same record instead of duplicating it.
	ID string

	// Text is what gets embedded and keyword-indexed.
	Text string

	// Title is the entry's short plain-language label.
	Title string

	// Source is "civil_code", "ra9653", "rules_of_court", or
	// "clause_pattern".
	Source string

	// Citation is the human-readable source string shown to the user,
	// e.g. "Civil Code, Art. 1654" — pulled straight from retrieval
	// metadata per PLAN.md's generation contract (never LLM-generated).
	Citation string

	// IssueTags is the set of severity/checklist issue tags this chunk
	// backs (data/corpus/lease_corpus.json's issue_tags). An excerpt
	// relevant to more than one issue carries multiple tags.
	IssueTags []string
}

// qdrantNamespace anchors the deterministic UUID derivation below. Any
// fixed UUID works here — it only needs to stay constant across runs so
// the same logical ID always maps to the same Qdrant point ID.
var qdrantNamespace = uuid.MustParse("6f6d6564-6963-616c-7261-670000000000")

// QdrantID derives a stable UUID from a Chunk's logical ID. Qdrant point
// IDs must be a uint64 or UUID, not an arbitrary string, so this is the
// bridge between the logical ID used everywhere else and what Qdrant
// accepts — deterministic, so re-ingesting the same logical ID upserts
// the same point rather than creating a duplicate.
func QdrantID(logicalID string) string {
	return uuid.NewSHA1(qdrantNamespace, []byte(logicalID)).String()
}
