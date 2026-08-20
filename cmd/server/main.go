// Command server runs the lease loophole detector's HTTP endpoint
// (internal/server), wiring it to a live Ollama + Qdrant + Bleve corpus.
// Ingestion stays a separate offline step (cmd/ingest) — this assumes the
// corpus already exists in Qdrant + Bleve.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/joho/godotenv"
	"github.com/ollama/ollama/api"

	"ph-contract-loophole-detector/internal/generate"
	"ph-contract-loophole-detector/internal/server"
	"ph-contract-loophole-detector/internal/store"
)

func main() {
	// Best-effort: load a local .env into the process environment (e.g.
	// ANTHROPIC_API_KEY for -llm-provider=claude) if one exists. Nothing
	// downstream reads the key directly — anthropic.NewClient() picks it
	// up from the environment itself — so a missing .env is never fatal
	// here, only a missing credential at the point Claude is actually
	// used is.
	_ = godotenv.Load()

	var (
		addr        = flag.String("addr", ":8080", "HTTP listen address")
		blevePath   = flag.String("bleve", "data/bleve/corpus.bleve", "path to the Bleve index directory")
		qdrantHost  = flag.String("qdrant-host", "localhost", "Qdrant gRPC host")
		qdrantPort  = flag.Int("qdrant-port", 6334, "Qdrant gRPC port")
		llmProvider = flag.String("llm-provider", "ollama", `generation backend: "ollama" (local, default) or "claude" (remote; needs Anthropic credentials, e.g. ANTHROPIC_API_KEY)`)
		llmModel    = flag.String("llm-model", "", "override the generation model string; defaults to the chosen -llm-provider's built-in default")
	)
	flag.Parse()

	ollama, qdrantStore, bleveStore, err := store.Connect(*qdrantHost, *qdrantPort, *blevePath)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	defer bleveStore.Close()

	generator, err := buildGenerator(*llmProvider, *llmModel, ollama)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	router := server.NewRouter(server.Deps{
		Ollama:    ollama,
		Generator: generator,
		Qdrant:    qdrantStore,
		Bleve:     bleveStore,
	})

	log.Printf("server: listening on %s (llm-provider=%s)", *addr, *llmProvider)
	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// buildGenerator resolves -llm-provider/-llm-model into the
// generate.Provider handleAnalyze calls for every request. A Claude
// client is constructed unconditionally when requested — anthropic.NewClient
// resolves credentials itself (env var, auth token, OAuth profile, WIF),
// so there's nothing to check here; a missing credential surfaces as an
// error from the first live call instead.
func buildGenerator(provider, model string, ollama *api.Client) (generate.Provider, error) {
	switch provider {
	case "ollama":
		if model == "" {
			model = generate.DefaultOllamaModel
		}
		return generate.OllamaProvider{Client: ollama, Model: model}, nil
	case "claude":
		if model == "" {
			model = generate.DefaultClaudeModel
		}
		client := anthropic.NewClient()
		return generate.ClaudeProvider{Client: &client, Model: model}, nil
	default:
		return nil, fmt.Errorf(`unknown -llm-provider %q (want "ollama" or "claude")`, provider)
	}
}
