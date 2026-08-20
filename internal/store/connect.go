package store

import (
	"fmt"

	"github.com/ollama/ollama/api"
)

// Connect builds the three clients almost every per-request entrypoint
// needs (cmd/query, cmd/server): an Ollama client from the environment,
// a QdrantStore, and a BleveStore. cmd/ingest deliberately doesn't use
// this — it also needs EnsureCollection and corpus-loading paths,
// different enough that sharing this helper would add awkwardness for
// no real gain.
func Connect(qdrantHost string, qdrantPort int, blevePath string) (*api.Client, *QdrantStore, *BleveStore, error) {
	ollama, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ollama client: %w", err)
	}
	qdrantStore, err := NewQdrantStore(qdrantHost, qdrantPort)
	if err != nil {
		return nil, nil, nil, err
	}
	bleveStore, err := OpenBleveStore(blevePath)
	if err != nil {
		return nil, nil, nil, err
	}
	return ollama, qdrantStore, bleveStore, nil
}
