// Command server runs the lease loophole detector's HTTP endpoint
// (internal/server), wiring it to a live Ollama + Qdrant + Bleve corpus.
// Ingestion stays a separate offline step (cmd/ingest) — this assumes the
// corpus already exists in Qdrant + Bleve.
package main

import (
	"flag"
	"log"
	"net/http"

	"ph-contract-loophole-detector/internal/server"
	"ph-contract-loophole-detector/internal/store"
)

func main() {
	var (
		addr       = flag.String("addr", ":8080", "HTTP listen address")
		blevePath  = flag.String("bleve", "data/bleve/corpus.bleve", "path to the Bleve index directory")
		qdrantHost = flag.String("qdrant-host", "localhost", "Qdrant gRPC host")
		qdrantPort = flag.Int("qdrant-port", 6334, "Qdrant gRPC port")
	)
	flag.Parse()

	ollama, qdrantStore, bleveStore, err := store.Connect(*qdrantHost, *qdrantPort, *blevePath)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	defer bleveStore.Close()

	router := server.NewRouter(server.Deps{
		Ollama: ollama,
		Qdrant: qdrantStore,
		Bleve:  bleveStore,
	})

	log.Printf("server: listening on %s", *addr)
	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
