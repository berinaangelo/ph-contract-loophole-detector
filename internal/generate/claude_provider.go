package generate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// DefaultClaudeModel is the remote generation model used when
// -llm-provider=claude and -llm-model isn't set. Chosen over Opus 5 for
// this project as the lighter/cheaper default; still overridable per run.
const DefaultClaudeModel = "claude-sonnet-5"

// claudeMaxTokens bounds one batch's structured-output response. BatchCap
// candidates each produce a short 1-2 sentence explanation and action, so
// this is generous headroom, not a measured ceiling like Ollama's local
// timeout was.
const claudeMaxTokens = 4096

// findingsToolName is the single tool Batch forces Claude to call — never
// exposed to a user, just the structured-output vehicle: forcing this one
// tool call is how buildSchema's JSON Schema gets enforced on a Claude
// response, the same role Ollama's Format field plays for OllamaProvider.
const findingsToolName = "submit_findings"

var _ Provider = ClaudeProvider{}

// ClaudeProvider is the remote Provider, opted into via cmd/server's
// -llm-provider=claude flag. Model is required; callers default it to
// DefaultClaudeModel rather than relying on a zero value.
type ClaudeProvider struct {
	Client *anthropic.Client
	Model  string
}

// claudeToolSchema is the subset of buildSchema's JSON Schema shape
// anthropic.ToolInputSchemaParam needs — buildSchema always emits an
// object schema, so Properties/Required are the only fields to lift out.
type claudeToolSchema struct {
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

// Batch forces a single call to findingsToolName, whose input_schema is
// schema, so Claude structurally cannot answer with anything but the
// requested findings shape — the same guarantee Ollama's Format field
// gives OllamaProvider. The tool call's input is round-tripped back
// through encoding/json rather than read as text, per the documented
// Claude tool-call JSON-escaping pitfall.
func (p ClaudeProvider) Batch(ctx context.Context, systemPrompt, prompt string, schema json.RawMessage) (string, error) {
	var s claudeToolSchema
	if err := json.Unmarshal(schema, &s); err != nil {
		return "", fmt.Errorf("claude provider: decode schema: %w", err)
	}

	tool := anthropic.ToolParam{
		Name:        findingsToolName,
		Description: anthropic.String("Submit the explanation and action for each candidate id listed in the prompt."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: s.Properties,
			Required:   s.Required,
		},
	}

	resp, err := p.Client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:      anthropic.Model(p.Model),
		MaxTokens:  claudeMaxTokens,
		System:     []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:   []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(findingsToolName),
	})
	if err != nil {
		return "", fmt.Errorf("claude messages.new: %w", err)
	}

	for _, block := range resp.Content {
		if variant, ok := block.AsAny().(anthropic.ToolUseBlock); ok && variant.Name == findingsToolName {
			return variant.JSON.Input.Raw(), nil
		}
	}
	return "", fmt.Errorf("claude provider: no %s tool call in response (stop_reason %s)", findingsToolName, resp.StopReason)
}
