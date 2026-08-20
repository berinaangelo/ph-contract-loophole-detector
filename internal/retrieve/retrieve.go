// Package retrieve is the hybrid-search half of the RAG+LLM finding tier
// (PLAN.md Primary flow step 4): given one clause's text, embed it, run
// dense (Qdrant) + keyword (Bleve) search, fuse the two ranked lists with
// internal/fuse.RRF, drop anything under internal/fuse.RelevanceFloor, and
// rank the surviving issue tags. This runs once per clause, not once per
// document — internal/server calls it in a loop, one call per clause, and
// attaches the clause index/text itself when mapping a Result into the
// shared Finding shape (PLAN.md Design decisions).
package retrieve

import (
	"context"
	"fmt"

	"github.com/ollama/ollama/api"

	"ph-contract-loophole-detector/internal/embed"
	"ph-contract-loophole-detector/internal/fuse"
	"ph-contract-loophole-detector/internal/store"
)

// DenseLimit and KeywordLimit are how many hits each retrieval leg
// returns before fusion — a reasonable default now, same "pick a
// reasonable default, revisit if it proves wrong" instinct as
// fuse.RelevanceFloor/fuse.DefaultK.
const (
	DenseLimit   = 10
	KeywordLimit = 10
)

// TopIssuesPerClause caps how many ranked issue tags Query returns for a
// single clause. A single clause realistically triggers only a handful of
// distinct legal issues, so this is a small cap, not a padding target —
// fuse.RankIssues already returns fewer if fewer issues survive.
const TopIssuesPerClause = 3

// Result is one surviving issue tag for a clause, backed by its best
// matching corpus chunk. Deliberately carries no clause index/text — see
// the package doc comment.
type Result struct {
	Issue string
	Score float64
	Chunk store.Chunk
}

// Query embeds clauseText, runs both retrieval legs, fuses and floors the
// results, and returns up to TopIssuesPerClause ranked Results. A nil,
// nil return (no error) means nothing survived the relevance floor — a
// legitimate signal per PLAN.md step 5's "no match" logic, not a failure.
func Query(ctx context.Context, ollama *api.Client, qdrant *store.QdrantStore, bleve *store.BleveStore, clauseText string) ([]Result, error) {
	vector, err := embed.Embed(ctx, ollama, clauseText)
	if err != nil {
		return nil, fmt.Errorf("retrieve: embed clause: %w", err)
	}

	denseHits, err := qdrant.Search(ctx, vector, DenseLimit)
	if err != nil {
		return nil, fmt.Errorf("retrieve: dense search: %w", err)
	}

	keywordIDs, err := bleve.Search(clauseText, KeywordLimit)
	if err != nil {
		return nil, fmt.Errorf("retrieve: keyword search: %w", err)
	}

	residualIDs := missingFrom(keywordIDs, denseHits)
	residual, err := qdrant.GetChunksWithVectors(ctx, residualIDs)
	if err != nil {
		return nil, fmt.Errorf("retrieve: fetch keyword-only chunks: %w", err)
	}

	candidates := buildCandidates(denseHits, keywordIDs, residual, vector)
	kept := fuse.ApplyFloor(candidates, fuse.RelevanceFloor)
	ranked := fuse.RankIssues(kept, TopIssuesPerClause)
	if len(ranked) == 0 {
		return nil, nil
	}

	return resolveResults(ranked, chunkIndex(denseHits, residual)), nil
}

// missingFrom returns the keywordIDs not already present among denseHits
// — the residual set fuse's dense leg didn't already score, and the only
// IDs GetChunksWithVectors needs to fetch (PLAN.md's "not another ANN
// search" instinct, see store.QdrantStore.GetChunksWithVectors).
func missingFrom(keywordIDs []string, denseHits []store.ScoredChunk) []string {
	have := make(map[string]bool, len(denseHits))
	for _, h := range denseHits {
		have[h.Chunk.ID] = true
	}

	var missing []string
	for _, id := range keywordIDs {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// buildCandidates fuses the dense and keyword ranked ID lists with
// fuse.RRF, then attaches each surviving ID's cosine (straight from the
// dense hit, or computed locally via fuse.CosineSimilarity for a
// keyword-only residual chunk) and issue tags. Pure — no live calls —
// so it's unit-testable against plain fixtures.
func buildCandidates(denseHits []store.ScoredChunk, keywordIDs []string, residual map[string]store.ChunkVector, queryVector []float32) []fuse.Candidate {
	denseIDs := make([]string, len(denseHits))
	cosineByID := make(map[string]float64, len(denseHits))
	tagsByID := make(map[string][]string, len(denseHits)+len(residual))
	for i, h := range denseHits {
		denseIDs[i] = h.Chunk.ID
		cosineByID[h.Chunk.ID] = h.Cosine
		tagsByID[h.Chunk.ID] = h.Chunk.IssueTags
	}
	for id, cv := range residual {
		cosineByID[id] = fuse.CosineSimilarity(queryVector, cv.Vector)
		tagsByID[id] = cv.Chunk.IssueTags
	}

	scores := fuse.RRF(fuse.DefaultK, denseIDs, keywordIDs)

	candidates := make([]fuse.Candidate, 0, len(scores))
	for id, fusedScore := range scores {
		candidates = append(candidates, fuse.Candidate{
			ChunkID:    id,
			FusedScore: fusedScore,
			Cosine:     cosineByID[id],
			IssueTags:  tagsByID[id],
		})
	}
	return candidates
}

// chunkIndex builds a chunk-ID → full Chunk lookup from both retrieval
// legs' results, for resolveResults to zip ranked issues against.
func chunkIndex(denseHits []store.ScoredChunk, residual map[string]store.ChunkVector) map[string]store.Chunk {
	byID := make(map[string]store.Chunk, len(denseHits)+len(residual))
	for _, h := range denseHits {
		byID[h.Chunk.ID] = h.Chunk
	}
	for id, cv := range residual {
		byID[id] = cv.Chunk
	}
	return byID
}

// resolveResults zips each ranked issue with its backing chunk's full
// metadata. Pure — unit-testable the same way generate.assemble is.
func resolveResults(ranked []fuse.RankedIssue, chunkByID map[string]store.Chunk) []Result {
	out := make([]Result, len(ranked))
	for i, ri := range ranked {
		out[i] = Result{
			Issue: ri.Issue,
			Score: ri.Score,
			Chunk: chunkByID[ri.BestChunkID],
		}
	}
	return out
}
