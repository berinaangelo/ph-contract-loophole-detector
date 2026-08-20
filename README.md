# PH Lease Contract Loophole Detector

Paste or upload a Philippine residential lease contract and, within
seconds (well, minutes on local hardware — see below), see its clauses
ranked by real risk (HIGH/MEDIUM/LOW), each with a plain-language reason,
a real citation, and a suggested action. Educational research tool, not
legal advice — see the disclaimer shown on every result.

Full design rationale, primary flow, and scope decisions live in
[PLAN.md](PLAN.md).

## Screenshots

| Input | Loading | Results |
|---|---|---|
| ![Input screen](docs/screenshots/Screenshot%202026-08-20%20at%203.14.57%20PM.png) | ![Loading screen](docs/screenshots/Screenshot%202026-08-20%20at%203.15.06%20PM.png) | |

## How it works

One request runs three independent findings sources over the contract,
merged into a single scorecard:

- **`severity`** — deterministic, rule-based checks for clauses that are
  illegal or void under Philippine law (HIGH).
- **`checklist`** — deterministic check for expected clause topics the
  contract doesn't appear to address (LOW, only runs once the text looks
  lease-shaped).
- **`retrieve` + `generate`** — hybrid (dense + keyword) search over a
  curated Philippine lease-law knowledge base, floor-gated, explained by a
  local LLM (MEDIUM/LOW).

## Prerequisites

- [Go](https://go.dev/) 1.26+ (see [go.mod](go.mod))
- [Ollama](https://ollama.com/), running locally, with both models pulled:
  ```
  ollama pull nomic-embed-text
  ollama pull qwen2.5:7b
  ```
- [Qdrant](https://qdrant.tech/), running locally (gRPC on `6334`), e.g.:
  ```
  docker run -p 6333:6333 -p 6334:6334 qdrant/qdrant
  ```

## Install

```
git clone <this repo>
cd ph-contract-loophole-detector
go build ./...
```

## Running locally

1. **Ingest the corpus** (once, or whenever `data/corpus/lease_corpus.json`
   changes) — loads the knowledge base into Qdrant + a local Bleve index:
   ```
   go run ./cmd/ingest
   ```
   Add `-fresh` to drop and rebuild both indexes from scratch instead of
   upserting into whatever's already there.

2. **Start the server**:
   ```
   go run ./cmd/server
   ```
   Then open [http://localhost:8080](http://localhost:8080).

Both commands share the same `-bleve`/`-qdrant-host`/`-qdrant-port` flag
defaults, so no flags are needed for a standard local setup. Run
`go run ./cmd/ingest -h` or `go run ./cmd/server -h` to see all of them.

Analysis is a single synchronous request — no streaming or partial
results. On local hardware this can take several minutes for a full lease
(every clause is checked against the LLM before any results are shown),
not seconds; the loading screen says so.

## Project layout

- `internal/extract`, `internal/language`, `internal/clause` — turn raw
  input (pasted text or an uploaded DOCX/PDF) into checkable clauses.
- `internal/severity`, `internal/checklist` — the two deterministic
  findings sources.
- `internal/embed`, `internal/store`, `internal/fuse`, `internal/retrieve`,
  `internal/generate` — the RAG+LLM findings source.
- `internal/server`, `web/` — the HTTP layer and single-page UI.
- `cmd/ingest`, `cmd/server` — the two runnable commands above.
- `data/corpus/` — the hand-curated Philippine lease-law knowledge base.
