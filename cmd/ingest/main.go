// Command ingest loads data/corpus/lease_corpus.json into the live
// Qdrant + Bleve corpus (internal/store): embed each entry once via
// internal/embed, then write it to both retrieval legs. Idempotent by
// default — QdrantID/Bleve's doc-ID are both derived from a corpus
// entry's stable `id`, so re-running overwrites rather than duplicates —
// with a -fresh flag for a genuine clean-slate re-ingest.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ollama/ollama/api"

	"ph-contract-loophole-detector/internal/embed"
	"ph-contract-loophole-detector/internal/store"
)

// corpusEntry mirrors data/corpus/lease_corpus.json's schema
// (data/corpus/README.md's field table).
type corpusEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Source    string   `json:"source"`
	Citation  string   `json:"citation"`
	IssueTags []string `json:"issue_tags"`
	Text      string   `json:"text"`
}

func (e corpusEntry) chunk() store.Chunk {
	return store.Chunk{
		ID:        e.ID,
		Text:      e.Text,
		Title:     e.Title,
		Source:    e.Source,
		Citation:  e.Citation,
		IssueTags: e.IssueTags,
	}
}

// knownSources are data/corpus/README.md's sourcing-rules list — anything
// else is almost certainly a typo, since generate.severityFor
// (internal/generate/generate.go) silently treats any non-"clause_pattern"
// source as LOW rather than erroring.
var knownSources = map[string]bool{
	"civil_code":     true,
	"ra9653":         true,
	"rules_of_court": true,
	"clause_pattern": true,
}

func main() {
	var (
		corpusPath = flag.String("corpus", "data/corpus/lease_corpus.json", "path to the corpus JSON file")
		blevePath  = flag.String("bleve", "data/bleve/corpus.bleve", "path to the Bleve index directory")
		qdrantHost = flag.String("qdrant-host", "localhost", "Qdrant gRPC host")
		qdrantPort = flag.Int("qdrant-port", 6334, "Qdrant gRPC port")
		fresh      = flag.Bool("fresh", false, "delete and recreate the Qdrant collection + Bleve index before ingesting, instead of upserting into whatever's already there")
	)
	flag.Parse()

	entries, err := loadCorpus(*corpusPath)
	if err != nil {
		log.Fatalf("ingest: %v", err)
	}
	for _, id := range entriesWithNoIssueTags(entries) {
		log.Printf("ingest: warning: entry %q has no issue_tags — it can never surface via fuse.RankIssues", id)
	}
	for _, id := range entriesWithUnknownSource(entries) {
		log.Printf("ingest: warning: entry %q has an unrecognized source — it will silently get LOW severity from generate", id)
	}

	if err := os.MkdirAll(filepath.Dir(*blevePath), 0o755); err != nil {
		log.Fatalf("ingest: create bleve parent dir: %v", err)
	}

	ctx := context.Background()

	ollama, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatalf("ingest: ollama client: %v", err)
	}
	qdrantStore, err := store.NewQdrantStore(*qdrantHost, *qdrantPort)
	if err != nil {
		log.Fatalf("ingest: %v", err)
	}

	if *fresh {
		log.Printf("ingest: -fresh: dropping %q and clearing %s", store.QdrantCollection, *blevePath)
		if err := qdrantStore.DropCollection(ctx); err != nil {
			log.Fatalf("ingest: %v", err)
		}
		if err := os.RemoveAll(*blevePath); err != nil {
			log.Fatalf("ingest: clear bleve dir: %v", err)
		}
	}

	if err := qdrantStore.EnsureCollection(ctx, embed.Dimensions); err != nil {
		log.Fatalf("ingest: %v", err)
	}
	bleveStore, err := store.OpenBleveStore(*blevePath)
	if err != nil {
		log.Fatalf("ingest: %v", err)
	}
	defer bleveStore.Close()

	for i, e := range entries {
		chunk := e.chunk()

		vector, err := embed.Embed(ctx, ollama, chunk.Text)
		if err != nil {
			log.Fatalf("ingest: embed %s: %v", chunk.ID, err)
		}
		if err := qdrantStore.Upsert(ctx, chunk, vector); err != nil {
			log.Fatalf("ingest: qdrant upsert %s: %v", chunk.ID, err)
		}
		if err := bleveStore.Index(chunk); err != nil {
			log.Fatalf("ingest: bleve index %s: %v", chunk.ID, err)
		}
		log.Printf("ingest: [%d/%d] %s", i+1, len(entries), chunk.ID)
	}

	log.Printf("ingest: done — %d entries into %q + %s", len(entries), store.QdrantCollection, *blevePath)
}

func loadCorpus(path string) ([]corpusEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseCorpus(raw)
}

// parseCorpus decodes raw corpus JSON and fails fast on the two fields
// ingestion structurally can't proceed without: a non-empty id (the
// shared Qdrant/Bleve document key) and non-empty text (what gets
// embedded) — plus duplicate ids, which would otherwise silently
// overwrite each other with no warning. Pure — unit-testable without
// touching the filesystem or live services.
func parseCorpus(raw []byte) ([]corpusEntry, error) {
	var entries []corpusEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse corpus JSON: %w", err)
	}

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.ID == "" {
			return nil, fmt.Errorf("corpus entry with empty id (title %q)", e.Title)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("duplicate corpus entry id %q", e.ID)
		}
		seen[e.ID] = true
		if e.Text == "" {
			return nil, fmt.Errorf("corpus entry %q has empty text", e.ID)
		}
	}
	return entries, nil
}

// entriesWithNoIssueTags returns the ids of entries with an empty
// issue_tags list — not fatal (an entry might be intentionally
// mid-edit), but worth flagging loudly rather than discovering later as
// a finding that silently never surfaces.
func entriesWithNoIssueTags(entries []corpusEntry) []string {
	var ids []string
	for _, e := range entries {
		if len(e.IssueTags) == 0 {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// entriesWithUnknownSource returns the ids of entries whose source isn't
// one of data/corpus/README.md's 4 known values.
func entriesWithUnknownSource(entries []corpusEntry) []string {
	var ids []string
	for _, e := range entries {
		if !knownSources[e.Source] {
			ids = append(ids, e.ID)
		}
	}
	return ids
}
