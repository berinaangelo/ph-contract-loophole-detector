// Package fuse implements the deterministic half of retrieval: Reciprocal
// Rank Fusion over dense (Qdrant) + keyword (Bleve) ranked results, the
// post-fusion relevance floor, and chunk→issue ranking. Deliberately
// dependency-free — no Qdrant, Bleve, or Ollama imports — so it's
// standalone-testable without live services, same as internal/language,
// internal/severity, and internal/checklist.
package fuse

import (
	"math"
	"sort"
)

// DefaultK is the standard RRF constant (Cormack, Clarke & Buettcher
// 2009's original proposal, and the common default across IR systems that
// implement RRF) — not tuned for this corpus specifically, same "pick a
// reasonable default now" instinct as the 0.5 relevance floor below.
const DefaultK = 60.0

// RelevanceFloor: chunks with a dense cosine similarity below this are
// dropped after fusion. Below this, a "matching" statute/jurisprudence
// excerpt is too weak to ground an LLM explanation in — better to surface
// nothing than a shaky citation.
const RelevanceFloor = 0.5

// RRF fuses any number of ranked ID lists into one score per ID.
// score(id) = Σ 1/(k + rank), summed over every list containing id
// (rank is 1-based position in that list). An ID missing from a list
// simply contributes nothing for that list — this is a union over all
// lists, not an intersection, matching a normal hybrid search.
func RRF(k float64, rankedLists ...[]string) map[string]float64 {
	scores := make(map[string]float64)
	for _, list := range rankedLists {
		for i, id := range list {
			rank := i + 1
			scores[id] += 1.0 / (k + float64(rank))
		}
	}
	return scores
}

// CosineSimilarity computes the same metric a Cosine-distance vector
// collection applies server-side. Used locally only for candidates the
// dense leg's own search didn't already return a score for.
func CosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Candidate is one chunk surviving RRF fusion, carrying what the floor and
// grouping steps need.
type Candidate struct {
	ChunkID    string
	FusedScore float64
	Cosine     float64
	// IssueTags is the set of legal-issue tags (e.g. "security_deposit",
	// "eviction_process") this chunk was ingested under — a statute or
	// jurisprudence excerpt relevant to more than one issue carries
	// multiple tags.
	IssueTags []string
}

// ApplyFloor drops candidates whose Cosine is below floor. Applied
// post-fusion over the union of both legs — not a per-leg gate (gating the
// dense leg alone before fusion would defeat the point of running BM25
// alongside it).
func ApplyFloor(candidates []Candidate, floor float64) []Candidate {
	kept := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Cosine >= floor {
			kept = append(kept, c)
		}
	}
	return kept
}

// RankedIssue is one surviving legal-issue tag, backed by its single best
// (highest fused-score) chunk.
type RankedIssue struct {
	Issue       string
	Score       float64
	BestChunkID string
}

// RankIssues groups surviving candidates by issue tag and ranks issues by
// their best surviving chunk's fused score. The same score that ranks
// issues also picks which chunk represents each one — one signal driving
// both decisions, not two that could disagree. Returns up to topN issues;
// fewer if fewer issues have a surviving chunk ("up to N", never padded).
func RankIssues(candidates []Candidate, topN int) []RankedIssue {
	best := make(map[string]RankedIssue)
	for _, c := range candidates {
		for _, issue := range c.IssueTags {
			current, ok := best[issue]
			if !ok || c.FusedScore > current.Score {
				best[issue] = RankedIssue{
					Issue:       issue,
					Score:       c.FusedScore,
					BestChunkID: c.ChunkID,
				}
			}
		}
	}

	ranked := make([]RankedIssue, 0, len(best))
	for _, ri := range best {
		ranked = append(ranked, ri)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Issue < ranked[j].Issue // stable tiebreak
	})

	if len(ranked) > topN {
		ranked = ranked[:topN]
	}
	return ranked
}
