package translate

import (
	"testing"

	"github.com/lcy/anthropic-openai-proxy/internal/types"
)

func TestToAnthropicRequestConvertsWebSearchToolWithName(t *testing.T) {
	maxUses := 3
	req := &types.OpenAIResponseRequest{
		Model:     "claude-sonnet-4-20250514",
		Input:     "search",
		MaxTokens: 1024,
		Tools: []types.Tool{
			{
				Type:           "web_search",
				MaxUses:        &maxUses,
				AllowedDomains: []string{"example.com"},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(got.Tools))
	}

	tool := got.Tools[0]
	if tool.Type != "web_search_20250305" {
		t.Fatalf("tool.Type = %q, want web_search_20250305", tool.Type)
	}
	if tool.Name != "web_search" {
		t.Fatalf("tool.Name = %q, want web_search", tool.Name)
	}
	if tool.MaxUses == nil || *tool.MaxUses != maxUses {
		t.Fatalf("tool.MaxUses = %v, want %d", tool.MaxUses, maxUses)
	}
	if len(tool.AllowedDomains) != 1 || tool.AllowedDomains[0] != "example.com" {
		t.Fatalf("tool.AllowedDomains = %#v, want [example.com]", tool.AllowedDomains)
	}
}

func TestToAnthropicRequestConvertsNestedFunctionTool(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:     "claude-sonnet-4-20250514",
		Input:     "call a tool",
		MaxTokens: 1024,
		Tools: []types.Tool{
			{
				Type: "function",
				Function: &types.FunctionTool{
					Name:        "lookup",
					Description: "Look up a value.",
					Parameters:  map[string]any{"type": "object"},
				},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(got.Tools))
	}

	tool := got.Tools[0]
	if tool.Name != "lookup" {
		t.Fatalf("tool.Name = %q, want lookup", tool.Name)
	}
	if tool.Description != "Look up a value." {
		t.Fatalf("tool.Description = %q, want nested description", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Fatal("tool.InputSchema is nil, want nested parameters")
	}
}

func TestToAnthropicRequestRejectsFunctionToolWithoutName(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:     "claude-sonnet-4-20250514",
		Input:     "call a tool",
		MaxTokens: 1024,
		Tools:     []types.Tool{{Type: "function"}},
	}

	if _, err := ToAnthropicRequest(req); err == nil {
		t.Fatal("ToAnthropicRequest returned nil error, want missing name error")
	}
}
