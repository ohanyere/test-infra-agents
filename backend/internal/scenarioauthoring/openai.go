package scenarioauthoring

import (
	"context"
	"encoding/json"
	"errors"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"strings"
	"time"
)

type OpenAIConfig struct {
	APIKey, Model, BaseURL string
	Timeout                time.Duration
}
type OpenAIGenerator struct {
	client  openai.Client
	model   string
	timeout time.Duration
}

func NewOpenAIGenerator(config OpenAIConfig) (*OpenAIGenerator, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("SCENARIO_LLM_MODEL is required")
	}
	options := []option.RequestOption{option.WithAPIKey(config.APIKey)}
	if config.BaseURL != "" {
		options = append(options, option.WithBaseURL(config.BaseURL))
	}
	if config.Timeout <= 0 {
		config.Timeout = 45 * time.Second
	}
	return &OpenAIGenerator{client: openai.NewClient(options...), model: config.Model, timeout: config.Timeout}, nil
}
func (g *OpenAIGenerator) FromDescription(ctx context.Context, input DescriptionInput) (ScenarioDraft, error) {
	if strings.TrimSpace(input.Description) == "" || len(input.Description) > 20000 {
		return ScenarioDraft{}, errors.New("description must be between 1 and 20000 characters")
	}
	input.Description = NewSanitizer().redactText(input.Description, newRedactionContext())
	return g.generate(ctx, map[string]any{"authoring_mode": "description", "description": input.Description, "capabilities": CurrentCapabilities()})
}
func (g *OpenAIGenerator) FromIncident(ctx context.Context, input IncidentInput) (ScenarioDraft, error) {
	input = NewSanitizer().sanitizeProviderIncident(input)
	return g.generate(ctx, map[string]any{"authoring_mode": "incident_import", "description": input.Description, "timeline": input.Timeline, "observed_facts": input.Facts, "sanitized_evidence": input.SanitizedEvidence, "capabilities": CurrentCapabilities()})
}
func (g *OpenAIGenerator) generate(ctx context.Context, input any) (ScenarioDraft, error) {
	payload, _ := json.Marshal(input)
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	response, err := g.client.Responses.New(callCtx, responses.ResponseNewParams{Model: g.model, Instructions: openai.String(generatorInstructions), Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(string(payload))}, Text: responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{Name: "scenario_draft", Description: openai.String("An Evalplane scenario proposal for human review"), Schema: draftSchema(), Strict: openai.Bool(true)}}}})
	if err != nil {
		return ScenarioDraft{}, errors.New("OpenAI scenario generation failed")
	}
	var draft ScenarioDraft
	if json.Unmarshal([]byte(response.OutputText()), &draft) != nil {
		return draft, errors.New("OpenAI returned an invalid structured scenario draft")
	}
	draft.SourceMetadata, _ = json.Marshal(map[string]any{"generator_provider": "openai", "generator_model": g.model})
	return draft, nil
}

const generatorInstructions = `Create an Evalplane scenario DRAFT, never a final decision. Never invent incident facts or measurable thresholds. Separate facts from inference; put uncertainty in assumptions and missing information in warnings. Keep agent task and visible context free of ground truth, criteria, thresholds, injected cause, execution details, and expected solution. Preserve unsupported incident conditions and list unsupported capabilities; never translate them into traffic_spike. Historical remediation is source context, not a required action. Prefer outcome-based criteria. Do not provide chain-of-thought.`

func draftSchema() map[string]any {
	stringArray := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	object := map[string]any{"type": "object"}
	criterion := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"key": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "metric_key": map[string]any{"type": "string"}, "operator": map[string]any{"type": "string"}, "target_value": map[string]any{}, "unit": map[string]any{"type": []string{"string", "null"}}, "requires_confirmation": map[string]any{"type": "boolean"}}, "required": []string{"key", "name", "metric_key", "operator", "target_value", "unit", "requires_confirmation"}}
	properties := map[string]any{"name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "category": map[string]any{"type": "string"}, "difficulty": map[string]any{"type": "string"}, "duration_seconds": map[string]any{"type": "integer"}, "agent_spec": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"task": map[string]any{"type": "string"}, "visible_context": object, "available_observations": stringArray, "allowed_actions": stringArray, "allowed_resources": map[string]any{"type": "array", "items": object}}, "required": []string{"task", "visible_context", "available_observations", "allowed_actions", "allowed_resources"}}, "environment": object, "conditions": map[string]any{"type": "array", "items": object}, "expected_behaviors": stringArray, "forbidden_behaviors": stringArray, "criteria": map[string]any{"type": "array", "items": criterion}, "ground_truth": object, "execution_spec": object, "assumptions": stringArray, "warnings": stringArray, "runnable_now": map[string]any{"type": "boolean"}, "unsupported_capabilities": stringArray, "source_metadata": object}
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
