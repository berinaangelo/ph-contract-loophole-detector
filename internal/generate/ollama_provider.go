package generate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ollama/ollama/api"
)

// DefaultOllamaModel is the local generation model PLAN.md's defaults
// settled on. cmd/server lets -llm-model override this per run.
const DefaultOllamaModel = "qwen2.5:7b"

var _ Provider = OllamaProvider{}

// OllamaProvider is the local Provider — the only backend this project
// used before the remote-Claude switch. Model is required; callers
// default it to DefaultOllamaModel rather than relying on a zero value.
type OllamaProvider struct {
	Client *api.Client
	Model  string
}

// Batch requests structured JSON output constrained by schema via
// Ollama's Format field, same call shape this project has always used.
func (p OllamaProvider) Batch(ctx context.Context, systemPrompt, prompt string, schema json.RawMessage) (string, error) {
	stream := false
	req := &api.GenerateRequest{
		Model:  p.Model,
		System: systemPrompt,
		Prompt: prompt,
		Format: schema,
		Stream: &stream,
	}

	var responseText string
	err := p.Client.Generate(ctx, req, func(resp api.GenerateResponse) error {
		responseText = resp.Response
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	return responseText, nil
}
