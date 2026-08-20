package store

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// QdrantCollection is the single collection the whole lease knowledge
// base lives in (data/corpus/lease_corpus.json) — one corpus, one
// retrieval pass.
const QdrantCollection = "lease_corpus"

// QdrantStore writes Chunks (with their embeddings) into Qdrant.
type QdrantStore struct {
	client *qdrant.Client
}

// NewQdrantStore connects to a local Qdrant instance's gRPC port (6334 by
// default — the REST port 6333 is a different protocol).
func NewQdrantStore(host string, port int) (*QdrantStore, error) {
	client, err := qdrant.NewClient(&qdrant.Config{Host: host, Port: port})
	if err != nil {
		return nil, fmt.Errorf("qdrant: connect: %w", err)
	}
	return &QdrantStore{client: client}, nil
}

// EnsureCollection creates QdrantCollection if it doesn't already exist —
// safe to call on every ingestion run.
func (s *QdrantStore) EnsureCollection(ctx context.Context, vectorSize uint64) error {
	exists, err := s.client.CollectionExists(ctx, QdrantCollection)
	if err != nil {
		return fmt.Errorf("qdrant: check collection: %w", err)
	}
	if exists {
		return nil
	}
	err = s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: QdrantCollection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     vectorSize,
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("qdrant: create collection: %w", err)
	}
	return nil
}

// DropCollection deletes QdrantCollection if it exists — a no-op
// otherwise, same exists-check idiom as EnsureCollection. Paired with it
// for cmd/ingest's -fresh flag: a genuine clean-slate re-ingest instead
// of upserting into whatever's already there.
func (s *QdrantStore) DropCollection(ctx context.Context) error {
	exists, err := s.client.CollectionExists(ctx, QdrantCollection)
	if err != nil {
		return fmt.Errorf("qdrant: check collection: %w", err)
	}
	if !exists {
		return nil
	}
	if err := s.client.DeleteCollection(ctx, QdrantCollection); err != nil {
		return fmt.Errorf("qdrant: delete collection: %w", err)
	}
	return nil
}

// Upsert writes one Chunk's embedding + payload. The point ID is derived
// deterministically from chunk.ID (see QdrantID), so re-ingesting the
// same logical chunk overwrites it instead of duplicating it.
func (s *QdrantStore) Upsert(ctx context.Context, chunk Chunk, vector []float32) error {
	point := &qdrant.PointStruct{
		Id:      qdrant.NewIDUUID(QdrantID(chunk.ID)),
		Vectors: qdrant.NewVectorsDense(vector),
		Payload: qdrant.NewValueMap(map[string]any{
			"chunk_id":   chunk.ID,
			"text":       chunk.Text,
			"title":      chunk.Title,
			"source":     chunk.Source,
			"citation":   chunk.Citation,
			"issue_tags": toAnySlice(chunk.IssueTags),
		}),
	}
	wait := true
	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: QdrantCollection,
		Wait:           &wait,
		Points:         []*qdrant.PointStruct{point},
	})
	if err != nil {
		return fmt.Errorf("qdrant: upsert %s: %w", chunk.ID, err)
	}
	return nil
}

func toAnySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// ScoredChunk is one Search hit: the chunk's full metadata plus its
// dense cosine similarity to the query (QdrantCollection is a
// Distance_Cosine collection, so ScoredPoint.Score already *is* cosine
// similarity — no separate calculation needed).
type ScoredChunk struct {
	Chunk
	Cosine float64
}

// Search runs a dense nearest-neighbor query for vector, returning up to
// limit hits with full payload attached. Deliberately does not set
// QueryPoints.ScoreThreshold — PLAN.md's relevance floor applies after
// fusion, over the union of both retrieval legs, not as a per-leg gate
// here (see internal/retrieve).
func (s *QdrantStore) Search(ctx context.Context, vector []float32, limit int) ([]ScoredChunk, error) {
	points, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: QdrantCollection,
		Query:          qdrant.NewQueryDense(vector),
		Limit:          ptr(uint64(limit)),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
	}

	out := make([]ScoredChunk, 0, len(points))
	for _, p := range points {
		out = append(out, ScoredChunk{
			Chunk:  chunkFromPayload(p.GetPayload()),
			Cosine: float64(p.GetScore()),
		})
	}
	return out, nil
}

// ChunkVector is a chunk's full metadata plus its stored embedding.
type ChunkVector struct {
	Chunk
	Vector []float32
}

// GetChunksWithVectors batch-fetches chunks by their logical IDs, with
// both payload and the stored embedding. Used only for the residual set
// of fused candidates the dense leg's own Search didn't already cover
// (see internal/retrieve) — one batched point lookup, not another ANN
// search.
func (s *QdrantStore) GetChunksWithVectors(ctx context.Context, logicalIDs []string) (map[string]ChunkVector, error) {
	if len(logicalIDs) == 0 {
		return map[string]ChunkVector{}, nil
	}

	ids := make([]*qdrant.PointId, len(logicalIDs))
	for i, id := range logicalIDs {
		ids[i] = qdrant.NewIDUUID(QdrantID(id))
	}

	points, err := s.client.Get(ctx, &qdrant.GetPoints{
		CollectionName: QdrantCollection,
		Ids:            ids,
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		WithVectors:    qdrant.NewWithVectorsEnable(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: get: %w", err)
	}

	out := make(map[string]ChunkVector, len(points))
	for _, p := range points {
		chunk := chunkFromPayload(p.GetPayload())
		out[chunk.ID] = ChunkVector{
			Chunk:  chunk,
			Vector: p.GetVectors().GetVector().GetDense().GetData(),
		}
	}
	return out, nil
}

// chunkFromPayload decodes a Qdrant payload map back into a Chunk. The
// inverse of Upsert's qdrant.NewValueMap call.
func chunkFromPayload(payload map[string]*qdrant.Value) Chunk {
	return Chunk{
		ID:        payload["chunk_id"].GetStringValue(),
		Text:      payload["text"].GetStringValue(),
		Title:     payload["title"].GetStringValue(),
		Source:    payload["source"].GetStringValue(),
		Citation:  payload["citation"].GetStringValue(),
		IssueTags: stringList(payload["issue_tags"]),
	}
}

func stringList(v *qdrant.Value) []string {
	if v == nil {
		return nil
	}
	values := v.GetListValue().GetValues()
	out := make([]string, 0, len(values))
	for _, item := range values {
		out = append(out, item.GetStringValue())
	}
	return out
}

func ptr[T any](v T) *T { return &v }
