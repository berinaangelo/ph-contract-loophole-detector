// Package embed wraps the single embedding call the project standardizes
// on (Ollama, nomic-embed-text — PLAN.md "Defaults picked"). Ingestion
// (build order step 4) and live query embedding (step 5) both need
// exactly this call, so it lives here once instead of being duplicated.
package embed

import (
	"context"
	"fmt"

	"github.com/ollama/ollama/api"
)

// Model is the embedding model every corpus chunk and every query is
// embedded with. Changing it invalidates any already-ingested vectors —
// same model in, same model out, always.
const Model = "nomic-embed-text"

// Dimensions is nomic-embed-text's output vector size, confirmed against
// a live local call during the ingestion planning pass. Used to size the
// Qdrant collection.
const Dimensions = 768

// Embed returns the embedding vector for text using Model.
func Embed(ctx context.Context, client *api.Client, text string) ([]float32, error) {
	resp, err := client.Embed(ctx, &api.EmbedRequest{
		Model: Model,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("embed: no embedding returned for input")
	}
	return resp.Embeddings[0], nil
}
