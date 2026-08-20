package retrieve

import (
	"math"
	"testing"

	"ph-contract-loophole-detector/internal/fuse"
	"ph-contract-loophole-detector/internal/store"
)

func TestBuildCandidates_DenseOnlyHit(t *testing.T) {
	denseHits := []store.ScoredChunk{
		{Chunk: store.Chunk{ID: "c1", IssueTags: []string{"security_deposit"}}, Cosine: 0.9},
	}
	candidates := buildCandidates(denseHits, nil, nil, nil)

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	c := candidates[0]
	if c.ChunkID != "c1" || c.Cosine != 0.9 || len(c.IssueTags) != 1 || c.IssueTags[0] != "security_deposit" {
		t.Errorf("candidate = %+v, unexpected", c)
	}
}

func TestBuildCandidates_KeywordOnlyResidual_ComputesCosineLocally(t *testing.T) {
	// c1 is keyword-only — never returned by dense search — so its cosine
	// must come from the residual fetch + local CosineSimilarity, not a
	// zero value.
	keywordIDs := []string{"c1"}
	residual := map[string]store.ChunkVector{
		"c1": {
			Chunk:  store.Chunk{ID: "c1", IssueTags: []string{"eviction_process"}},
			Vector: []float32{1, 0, 0},
		},
	}
	queryVector := []float32{1, 0, 0} // identical vector -> cosine 1.0

	candidates := buildCandidates(nil, keywordIDs, residual, queryVector)

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	c := candidates[0]
	if math.Abs(c.Cosine-1.0) > 1e-9 {
		t.Errorf("Cosine = %v, want ~1.0 (computed via CosineSimilarity)", c.Cosine)
	}
	if len(c.IssueTags) != 1 || c.IssueTags[0] != "eviction_process" {
		t.Errorf("IssueTags = %v, want [eviction_process]", c.IssueTags)
	}
}

func TestBuildCandidates_UnionOfBothLegs(t *testing.T) {
	denseHits := []store.ScoredChunk{
		{Chunk: store.Chunk{ID: "dense-only"}, Cosine: 0.8},
		{Chunk: store.Chunk{ID: "both"}, Cosine: 0.7},
	}
	keywordIDs := []string{"both", "keyword-only"}
	residual := map[string]store.ChunkVector{
		"keyword-only": {Chunk: store.Chunk{ID: "keyword-only"}, Vector: []float32{1, 0}},
	}

	candidates := buildCandidates(denseHits, keywordIDs, residual, []float32{1, 0})

	ids := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		ids[c.ChunkID] = true
	}
	for _, want := range []string{"dense-only", "both", "keyword-only"} {
		if !ids[want] {
			t.Errorf("candidates missing %q; got %v", want, ids)
		}
	}
}

func TestMissingFrom_ExcludesDenseHits(t *testing.T) {
	denseHits := []store.ScoredChunk{{Chunk: store.Chunk{ID: "a"}}}
	got := missingFrom([]string{"a", "b", "c"}, denseHits)

	want := []string{"b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}

func TestResolveResults_ZipsIssueWithChunk(t *testing.T) {
	ranked := []fuse.RankedIssue{
		{Issue: "security_deposit", Score: 0.95, BestChunkID: "c5"},
		{Issue: "repairs_habitability", Score: 0.8, BestChunkID: "c3"},
	}
	chunkByID := map[string]store.Chunk{
		"c5": {ID: "c5", Text: "deposit excerpt", Citation: "Civil Code, Art. 1"},
		"c3": {ID: "c3", Text: "repairs excerpt", Citation: "Civil Code, Art. 2"},
	}

	got := resolveResults(ranked, chunkByID)

	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// Order follows ranked (already rank-ordered), not the map.
	if got[0].Issue != "security_deposit" || got[0].Score != 0.95 || got[0].Chunk.Text != "deposit excerpt" {
		t.Errorf("got[0] = %+v, unexpected", got[0])
	}
	if got[1].Issue != "repairs_habitability" || got[1].Chunk.Citation != "Civil Code, Art. 2" {
		t.Errorf("got[1] = %+v, unexpected", got[1])
	}
}

func TestChunkIndex_MergesBothLegs(t *testing.T) {
	denseHits := []store.ScoredChunk{{Chunk: store.Chunk{ID: "a", Text: "from dense"}}}
	residual := map[string]store.ChunkVector{
		"b": {Chunk: store.Chunk{ID: "b", Text: "from residual"}},
	}

	idx := chunkIndex(denseHits, residual)

	if idx["a"].Text != "from dense" || idx["b"].Text != "from residual" {
		t.Errorf("chunkIndex = %+v, unexpected", idx)
	}
}
