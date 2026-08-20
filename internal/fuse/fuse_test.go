package fuse

import (
	"math"
	"testing"
)

func TestRRF_UnionNotIntersection(t *testing.T) {
	dense := []string{"a", "b", "c"}
	keyword := []string{"c", "d"}

	scores := RRF(60, dense, keyword)

	// All four IDs must appear, even "b" and "d" which only one leg
	// returned — a real hybrid-search union, not an intersection.
	for _, id := range []string{"a", "b", "c", "d"} {
		if _, ok := scores[id]; !ok {
			t.Errorf("RRF result missing %q", id)
		}
	}

	// "c" appears in both lists (rank 3 in dense, rank 1 in keyword) so
	// it should score higher than "a" (rank 1 in dense only) — a
	// strong keyword-only match still gets its chance to be fused in.
	wantC := 1.0/(60+3) + 1.0/(60+1)
	if math.Abs(scores["c"]-wantC) > 1e-9 {
		t.Errorf("scores[c] = %v, want %v", scores["c"], wantC)
	}

	wantA := 1.0 / (60 + 1)
	if math.Abs(scores["a"]-wantA) > 1e-9 {
		t.Errorf("scores[a] = %v, want %v", scores["a"], wantA)
	}
}

func TestRRF_EmptyLists(t *testing.T) {
	scores := RRF(60, nil, nil)
	if len(scores) != 0 {
		t.Errorf("got %d scores from empty lists, want 0", len(scores))
	}
}

func TestCosineSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"zero vector", []float32{0, 0}, []float32{1, 1}, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CosineSimilarity(c.a, c.b)
			if math.Abs(got-c.want) > 1e-6 {
				t.Errorf("CosineSimilarity(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestApplyFloor(t *testing.T) {
	candidates := []Candidate{
		{ChunkID: "keep-high", Cosine: 0.9},
		{ChunkID: "keep-exact", Cosine: 0.5},
		{ChunkID: "drop-low", Cosine: 0.49},
		{ChunkID: "drop-zero", Cosine: 0},
	}
	kept := ApplyFloor(candidates, RelevanceFloor)

	var gotIDs []string
	for _, c := range kept {
		gotIDs = append(gotIDs, c.ChunkID)
	}
	want := []string{"keep-high", "keep-exact"}
	if len(gotIDs) != len(want) {
		t.Fatalf("ApplyFloor kept %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Errorf("ApplyFloor kept %v, want %v", gotIDs, want)
		}
	}
}

func TestRankIssues(t *testing.T) {
	candidates := []Candidate{
		{ChunkID: "c1", FusedScore: 0.9, IssueTags: []string{"security_deposit"}},
		{ChunkID: "c2", FusedScore: 0.7, IssueTags: []string{"security_deposit"}}, // weaker chunk, same issue — c1 should win
		{ChunkID: "c3", FusedScore: 0.8, IssueTags: []string{"repairs_habitability"}},
		{ChunkID: "c4", FusedScore: 0.5, IssueTags: []string{"eviction_process"}},
		// An excerpt tagged under multiple issues contributes to each
		// one's ranking independently.
		{ChunkID: "c5", FusedScore: 0.95, IssueTags: []string{"security_deposit", "rent_increase"}},
	}

	ranked := RankIssues(candidates, 5)

	if len(ranked) != 4 {
		t.Fatalf("got %d ranked issues, want 4 (security_deposit, repairs_habitability, eviction_process, rent_increase)", len(ranked))
	}

	// c5 (0.95) backs both security_deposit and rent_increase, tied at the
	// top — the alphabetical tiebreak picks rent_increase first. Either
	// way, security_deposit's best chunk must be c5 (0.95), not c1 (0.9)
	// or c2 (0.7).
	if ranked[0].Issue != "rent_increase" || ranked[0].BestChunkID != "c5" || ranked[0].Score != 0.95 {
		t.Errorf("top issue = %+v, want rent_increase backed by c5 @ 0.95", ranked[0])
	}
	for _, ri := range ranked {
		if ri.Issue == "security_deposit" && (ri.BestChunkID != "c5" || ri.Score != 0.95) {
			t.Errorf("security_deposit = %+v, want backed by c5 @ 0.95", ri)
		}
	}
}

func TestRankIssues_FewerThanTopN(t *testing.T) {
	candidates := []Candidate{
		{ChunkID: "c1", FusedScore: 0.9, IssueTags: []string{"security_deposit"}},
		{ChunkID: "c2", FusedScore: 0.8, IssueTags: []string{"repairs_habitability"}},
	}
	// Only 2 issues have a surviving chunk — must return 2, not pad to 5
	// with anything weaker.
	ranked := RankIssues(candidates, 5)
	if len(ranked) != 2 {
		t.Errorf("got %d ranked issues, want 2 (not padded to 5)", len(ranked))
	}
}

func TestRankIssues_CapsAtTopN(t *testing.T) {
	var candidates []Candidate
	issues := []string{"A", "B", "C", "D", "E", "F", "G"}
	for i, issue := range issues {
		candidates = append(candidates, Candidate{
			ChunkID:    issue,
			FusedScore: float64(len(issues) - i), // A highest, G lowest
			IssueTags:  []string{issue},
		})
	}
	ranked := RankIssues(candidates, 5)
	if len(ranked) != 5 {
		t.Fatalf("got %d ranked issues, want 5", len(ranked))
	}
	if ranked[0].Issue != "A" || ranked[4].Issue != "E" {
		t.Errorf("ranked = %+v, want A..E in order", ranked)
	}
}
