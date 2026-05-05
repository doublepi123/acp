package translate

import (
	"testing"

	"github.com/doublepi123/acp/internal/types"
)

func TestToAnthropicRequestNormalizesFunctionToolSchema(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "call a tool",
		Tools: []types.Tool{
			{
				Type: "function",
				Name: "empty_schema",
			},
			{
				Type: "function",
				Name: "coerced_schema",
				Parameters: map[string]any{
					"type":                 "string",
					"properties":           map[string]any{"value": map[string]any{"type": "string"}},
					"required":             []string{"value"},
					"additionalProperties": false,
					"enum":                 []string{"ignored"},
				},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2", len(got.Tools))
	}
	defaultSchema := got.Tools[0].InputSchema.(map[string]any)
	if got.Tools[0].Type != "custom" || defaultSchema["type"] != "object" || defaultSchema["properties"] == nil {
		t.Fatalf("default tool = %#v schema=%#v, want LiteLLM-style custom object schema", got.Tools[0], defaultSchema)
	}
	coercedSchema := got.Tools[1].InputSchema.(map[string]any)
	if got.Tools[1].Type != "custom" || coercedSchema["type"] != "object" {
		t.Fatalf("coerced tool = %#v schema=%#v, want custom object schema", got.Tools[1], coercedSchema)
	}
	if _, ok := coercedSchema["enum"]; ok {
		t.Fatalf("coerced schema = %#v, want unsupported enum filtered", coercedSchema)
	}
	if coercedSchema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %#v, want false preserved", coercedSchema["additionalProperties"])
	}
}

func TestToAnthropicRequestMapsLiteLLMStyleUserMetadata(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "hello",
		User:  "codex-local-user",
		Metadata: map[string]any{
			"user_id": "metadata-user",
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if got.Metadata["user_id"] != "codex-local-user" {
		t.Fatalf("Metadata = %#v, want request user_id", got.Metadata)
	}

	req.User = "person@example.com"
	got, err = ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if got.Metadata["user_id"] != "metadata-user" {
		t.Fatalf("Metadata = %#v, want valid metadata fallback", got.Metadata)
	}
}

func TestToAnthropicRequestMapsWebSearchContextSize(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "search",
		Tools: []types.Tool{
			{
				Type:              "web_search_preview",
				SearchContextSize: "high",
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].MaxUses == nil || *got.Tools[0].MaxUses != 10 {
		t.Fatalf("Tools = %#v, want web search max_uses from high context", got.Tools)
	}
}

func TestConvertToolChoiceLiteLLMVariants(t *testing.T) {
	tests := []struct {
		name string
		tc   any
		want map[string]any
	}{
		{"auto map", map[string]any{"type": "auto"}, map[string]any{"type": "auto"}},
		{"required map", map[string]any{"type": "required"}, map[string]any{"type": "any"}},
		{"tool map without name", map[string]any{"type": "tool"}, map[string]any{"type": "any"}},
		{"tool map with name", map[string]any{"type": "tool", "name": "lookup"}, map[string]any{"type": "tool", "name": "lookup"}},
		{"map string required", map[string]string{"type": "required"}, map[string]any{"type": "any"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToolChoice(tt.tc)
			m, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("convertToolChoice(%v) = %T, want map", tt.tc, got)
			}
			if m["type"] != tt.want["type"] || m["name"] != tt.want["name"] {
				t.Fatalf("convertToolChoice(%v) = %v, want %v", tt.tc, m, tt.want)
			}
		})
	}
}

func TestNormalizeAnthropicInputSchemaVariants(t *testing.T) {
	tests := []struct {
		name   string
		schema any
	}{
		{"nil", nil},
		{"string map", map[string]string{"type": "object"}},
		{"struct", struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
		}{
			Type:       "object",
			Properties: map[string]any{"q": map[string]any{"type": "string"}},
		}},
		{"non map", []string{"bad"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAnthropicInputSchema(tt.schema)
			if got["type"] != "object" {
				t.Fatalf("schema = %#v, want object type", got)
			}
			if _, ok := got["properties"]; !ok {
				t.Fatalf("schema = %#v, want properties", got)
			}
		})
	}
}

func TestConvertToolCustomNestedAndUnknown(t *testing.T) {
	tool, kind, err := convertTool(types.Tool{
		Type: "custom",
		Function: &types.FunctionTool{
			Name:        "run_freeform",
			Description: "Run freeform input.",
		},
	})
	if err != nil {
		t.Fatalf("convertTool custom returned error: %v", err)
	}
	if kind != toolKindCustom || tool.Name != "run_freeform" || tool.Type != "custom" {
		t.Fatalf("tool=%#v kind=%v, want custom run_freeform", tool, kind)
	}

	if _, _, err := convertTool(types.Tool{Type: "bogus"}); err == nil {
		t.Fatal("convertTool unknown returned nil error")
	}
}

func TestWebSearchToolFilterOverrides(t *testing.T) {
	tool := webSearchTool(types.Tool{
		Type:           "web_search",
		AllowedDomains: []string{"ignored.example"},
		BlockedDomains: []string{"ignored-block.example"},
		Filters: &types.WebSearchFilters{
			AllowedDomains: []string{"allowed.example"},
			BlockedDomains: []string{"blocked.example"},
		},
	})

	if len(tool.AllowedDomains) != 1 || tool.AllowedDomains[0] != "allowed.example" {
		t.Fatalf("AllowedDomains = %#v, want filter override", tool.AllowedDomains)
	}
	if len(tool.BlockedDomains) != 1 || tool.BlockedDomains[0] != "blocked.example" {
		t.Fatalf("BlockedDomains = %#v, want filter override", tool.BlockedDomains)
	}
}

func TestAnthropicMetadataValidation(t *testing.T) {
	if got := anthropicMetadata("", nil); got != nil {
		t.Fatalf("anthropicMetadata empty = %#v, want nil", got)
	}
	if got := anthropicMetadata("+1 (555) 123-4567", map[string]any{"user_id": "fallback"}); got["user_id"] != "fallback" {
		t.Fatalf("anthropicMetadata phone fallback = %#v, want fallback", got)
	}
	if validAnthropicUserID("person@example.com") {
		t.Fatal("validAnthropicUserID(email) = true, want false")
	}
	if validAnthropicUserID("+15551234567") {
		t.Fatal("validAnthropicUserID(phone) = true, want false")
	}
}
