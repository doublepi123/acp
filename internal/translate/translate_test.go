package translate

import (
	"encoding/json"
	"strings"
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

func TestToAnthropicRequestConvertsResponsesInputBlocksAndFunctionOutput(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "describe this"},
					map[string]any{"type": "input_image", "image_url": "https://example.com/a.png"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64,abc123"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"id":        "fc_1",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": `{"q":"weather"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  "sunny",
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(got.Messages))
	}

	userBlocks, ok := got.Messages[0].Content.([]types.AnthropicContentBlock)
	if !ok {
		t.Fatalf("user content type = %T, want []AnthropicContentBlock", got.Messages[0].Content)
	}
	if userBlocks[0].Type != "text" || userBlocks[0].Text != "describe this" {
		t.Fatalf("first block = %#v, want input_text converted to text", userBlocks[0])
	}
	if userBlocks[1].Source == nil || userBlocks[1].Source.Type != "url" || userBlocks[1].Source.URL != "https://example.com/a.png" {
		t.Fatalf("url image block = %#v, want Anthropic url source", userBlocks[1])
	}
	if userBlocks[2].Source == nil || userBlocks[2].Source.Type != "base64" || userBlocks[2].Source.MediaType != "image/png" || userBlocks[2].Source.Data != "abc123" {
		t.Fatalf("data image block = %#v, want parsed base64 source", userBlocks[2])
	}

	callBlocks := got.Messages[1].Content.([]types.AnthropicContentBlock)
	if callBlocks[0].Type != "tool_use" || callBlocks[0].ID != "call_1" || callBlocks[0].Name != "lookup" {
		t.Fatalf("function_call block = %#v, want tool_use with call id/name", callBlocks[0])
	}
	input, ok := callBlocks[0].Input.(map[string]any)
	if !ok || input["q"] != "weather" {
		t.Fatalf("tool input = %#v, want parsed arguments", callBlocks[0].Input)
	}

	resultBlocks := got.Messages[2].Content.([]types.AnthropicContentBlock)
	if resultBlocks[0].Type != "tool_result" || resultBlocks[0].ToolUseID != "call_1" || resultBlocks[0].Content != "sunny" {
		t.Fatalf("function_call_output block = %#v, want tool_result", resultBlocks[0])
	}
}

func TestToAnthropicRequestPreservesReasoningWithFunctionCall(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "look this up"},
				},
			},
			map[string]any{
				"type":              "reasoning",
				"id":                "rs_1",
				"encrypted_content": "sig_1",
				"content": []any{
					map[string]any{"type": "reasoning_text", "text": "I should use a tool."},
				},
			},
			map[string]any{
				"type":      "function_call",
				"id":        "fc_1",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": `{"q":"weather"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  "sunny",
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want user, assistant, tool result", len(got.Messages))
	}
	assistantBlocks := got.Messages[1].Content.([]types.AnthropicContentBlock)
	if len(assistantBlocks) != 2 {
		t.Fatalf("len(assistantBlocks) = %d, want thinking plus tool_use", len(assistantBlocks))
	}
	if assistantBlocks[0].Type != "thinking" || assistantBlocks[0].Thinking != "I should use a tool." || assistantBlocks[0].Signature != "sig_1" {
		t.Fatalf("thinking block = %#v, want preserved thinking/signature", assistantBlocks[0])
	}
	if assistantBlocks[1].Type != "tool_use" || assistantBlocks[1].ID != "call_1" {
		t.Fatalf("tool block = %#v, want tool_use after thinking", assistantBlocks[1])
	}
}

func TestToAnthropicRequestDropsOpaqueOnlyReasoning(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "look this up"},
				},
			},
			map[string]any{
				"type":              "reasoning",
				"id":                "rs_1",
				"encrypted_content": "opaque_1",
			},
			map[string]any{
				"type":      "function_call",
				"id":        "fc_1",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": `{"q":"weather"}`,
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want user plus assistant tool call", len(got.Messages))
	}
	assistantBlocks := got.Messages[1].Content.([]types.AnthropicContentBlock)
	if len(assistantBlocks) != 1 || assistantBlocks[0].Type != "tool_use" {
		t.Fatalf("assistantBlocks = %#v, want only tool_use", assistantBlocks)
	}
	raw, err := json.Marshal(got.Messages)
	if err != nil {
		t.Fatalf("json.Marshal messages: %v", err)
	}
	if strings.Contains(string(raw), "redacted_thinking") {
		t.Fatalf("messages JSON contains redacted_thinking: %s", string(raw))
	}
}

func TestToAnthropicRequestDropsRedactedThinkingContentBlock(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "redacted_thinking", "data": "opaque_1"},
					map[string]any{"type": "output_text", "text": "visible"},
				},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	blocks := got.Messages[0].Content.([]types.AnthropicContentBlock)
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "visible" {
		t.Fatalf("blocks = %#v, want only visible text", blocks)
	}
}

func TestToAnthropicRequestDropsRedactedOnlyContentMessage(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "continue"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "redacted_thinking", "data": "opaque_1"},
				},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Fatalf("messages = %#v, want only user message", got.Messages)
	}
}

func TestToAnthropicRequestConvertsReasoningConfig(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:     "claude-sonnet-4-20250514",
		Input:     "think",
		MaxTokens: 4096,
		Reasoning: map[string]any{
			"effort": "medium",
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	thinking := got.Thinking.(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 1024 {
		t.Fatalf("Thinking = %#v, want enabled budget", thinking)
	}
}

func TestToAnthropicRequestHandlesMalformedToolChoice(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:      "claude-sonnet-4-20250514",
		Input:      "hello",
		ToolChoice: map[string]any{"type": "function"},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if got.ToolChoice != nil {
		t.Fatalf("ToolChoice = %#v, want nil for malformed input", got.ToolChoice)
	}
}

func TestToAnthropicRequestConvertsResponsesFunctionToolChoice(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:      "claude-sonnet-4-20250514",
		Input:      "hello",
		ToolChoice: map[string]any{"type": "function", "name": "lookup"},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	want := map[string]any{"type": "tool", "name": "lookup"}
	gotChoice, ok := got.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("ToolChoice = %T, want map[string]any", got.ToolChoice)
	}
	if gotChoice["type"] != want["type"] || gotChoice["name"] != want["name"] {
		t.Fatalf("ToolChoice = %#v, want %#v", gotChoice, want)
	}
}

func TestToAnthropicRequestConvertsNestedChatCompletionToolCalls(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"role":    "assistant",
				"content": "checking",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "lookup",
							"arguments": `{"q":"x"}`,
						},
					},
				},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	blocks := got.Messages[0].Content.([]types.AnthropicContentBlock)
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want text plus tool_use", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "checking" {
		t.Fatalf("text block = %#v, want assistant text preserved", blocks[0])
	}
	if blocks[1].Type != "tool_use" || blocks[1].Name != "lookup" {
		t.Fatalf("tool block = %#v, want nested function call converted", blocks[1])
	}
	args, err := json.Marshal(blocks[1].Input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	if string(args) != `{"q":"x"}` {
		t.Fatalf("tool input = %s, want parsed arguments", string(args))
	}
}

func TestToOpenAIResponseConvertsWebSearchCallAndCitations(t *testing.T) {
	resp := &types.AnthropicMessageResponse{
		ID: "msg_1",
		Content: []types.AnthropicContentBlock{
			{
				Type:  "server_tool_use",
				ID:    "srvtoolu_1",
				Name:  "web_search",
				Input: map[string]any{"query": "weather"},
			},
			{
				Type:      "web_search_tool_result",
				ToolUseID: "srvtoolu_1",
				Content: []any{
					map[string]any{"type": "web_search_result", "url": "https://example.com", "title": "Example"},
				},
			},
			{
				Type: "text",
				Text: "It is sunny.",
				Citations: []types.AnthropicCitation{
					{Type: "web_search_result_location", URL: "https://example.com", Title: "Example"},
				},
			},
		},
	}

	got := ToOpenAIResponse(resp, "claude-test")
	if len(got.Output) != 2 {
		t.Fatalf("len(Output) = %d, want web_search_call plus message", len(got.Output))
	}
	if got.Output[0].Type != "web_search_call" || got.Output[0].ID != "srvtoolu_1" || got.Output[0].Status != "completed" {
		t.Fatalf("web search item = %#v, want completed web_search_call", got.Output[0])
	}
	action := got.Output[0].Action.(map[string]any)
	if action["type"] != "search" || action["query"] != "weather" {
		t.Fatalf("web search action = %#v, want search query", action)
	}

	content := got.Output[1].Content.([]map[string]any)
	annotations := content[0]["annotations"].([]map[string]any)
	if len(annotations) != 1 || annotations[0]["url"] != "https://example.com" || annotations[0]["title"] != "Example" {
		t.Fatalf("annotations = %#v, want url citation", annotations)
	}
}

func TestToOpenAIResponseConvertsThinkingBlocksToReasoning(t *testing.T) {
	resp := &types.AnthropicMessageResponse{
		ID: "msg_1",
		Content: []types.AnthropicContentBlock{
			{
				Type:      "thinking",
				Thinking:  "I should use a tool.",
				Signature: "sig_1",
			},
			{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  "lookup",
				Input: map[string]any{"q": "weather"},
			},
		},
	}

	got := ToOpenAIResponse(resp, "claude-test")
	if len(got.Output) != 2 {
		t.Fatalf("len(Output) = %d, want reasoning plus function_call", len(got.Output))
	}
	reasoning := got.Output[0]
	if reasoning.Type != "reasoning" || reasoning.EncryptedContent != "sig_1" {
		t.Fatalf("reasoning item = %#v, want encrypted reasoning", reasoning)
	}
	content := reasoning.Content.([]map[string]any)
	if content[0]["text"] != "I should use a tool." {
		t.Fatalf("reasoning content = %#v, want thinking text", content)
	}
	if got.Output[1].Type != "function_call" || got.Output[1].CallID != "call_1" {
		t.Fatalf("function item = %#v, want function_call after reasoning", got.Output[1])
	}
}
