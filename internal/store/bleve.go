package store

import (
	"fmt"
	"os"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// bleveDoc is what actually gets indexed — Bleve stores whatever struct
// you hand it and returns it (or its fields) from search hits.
type bleveDoc struct {
	Text      string   `json:"text"`
	Title     string   `json:"title"`
	Source    string   `json:"source"`
	Citation  string   `json:"citation"`
	IssueTags []string `json:"issue_tags"`
}

// BleveStore is the keyword (BM25) half of the hybrid retrieval index.
type BleveStore struct {
	index bleve.Index
}

// OpenBleveStore opens the index at path, creating it (with the mapping
// below) if it doesn't exist yet — same open-or-create idempotency as
// QdrantStore.EnsureCollection.
func OpenBleveStore(path string) (*BleveStore, error) {
	if _, err := os.Stat(path); err == nil {
		idx, err := bleve.Open(path)
		if err != nil {
			return nil, fmt.Errorf("bleve: open %s: %w", path, err)
		}
		return &BleveStore{index: idx}, nil
	}

	idx, err := bleve.New(path, buildIndexMapping())
	if err != nil {
		return nil, fmt.Errorf("bleve: create %s: %w", path, err)
	}
	return &BleveStore{index: idx}, nil
}

// buildIndexMapping keeps "text"/"title" full-text analyzed (what BM25
// search matches against) and "issue_tags"/"source" as unanalyzed
// keywords (exact-match filtering, not tokenized search) — same
// analyzed-vs-exact split the issue tags need at retrieval time.
func buildIndexMapping() mapping.IndexMapping {
	textField := bleve.NewTextFieldMapping()
	keywordField := bleve.NewKeywordFieldMapping()

	doc := bleve.NewDocumentMapping()
	doc.AddFieldMappingsAt("text", textField)
	doc.AddFieldMappingsAt("title", textField)
	doc.AddFieldMappingsAt("citation", keywordField)
	doc.AddFieldMappingsAt("source", keywordField)
	doc.AddFieldMappingsAt("issue_tags", keywordField)

	m := bleve.NewIndexMapping()
	m.DefaultMapping = doc
	return m
}

// Index writes one Chunk. Bleve's document ID is the same logical
// chunk.ID used everywhere else — no translation needed, unlike Qdrant.
func (s *BleveStore) Index(chunk Chunk) error {
	doc := bleveDoc{
		Text:      chunk.Text,
		Title:     chunk.Title,
		Source:    chunk.Source,
		Citation:  chunk.Citation,
		IssueTags: chunk.IssueTags,
	}
	if err := s.index.Index(chunk.ID, doc); err != nil {
		return fmt.Errorf("bleve: index %s: %w", chunk.ID, err)
	}
	return nil
}

// Close releases the index's file handles.
func (s *BleveStore) Close() error {
	return s.index.Close()
}

// Search runs a BM25 keyword search for text, returning up to limit
// chunk IDs ranked by Bleve's default score-descending order — the same
// logical IDs Qdrant uses, no translation needed. Bleve's role in
// retrieval is purely this ranking signal; Qdrant stays the canonical
// source for chunk metadata (see internal/retrieve), so no payload is
// read back here.
func (s *BleveStore) Search(text string, limit int) ([]string, error) {
	q := bleve.NewMatchQuery(text)
	req := bleve.NewSearchRequestOptions(q, limit, 0, false)
	res, err := s.index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("bleve: search: %w", err)
	}

	ids := make([]string, 0, len(res.Hits))
	for _, hit := range res.Hits {
		ids = append(ids, hit.ID)
	}
	return ids, nil
}
