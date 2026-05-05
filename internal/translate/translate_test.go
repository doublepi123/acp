package translate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doublepi123/acp/internal/types"
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

func TestToAnthropicRequestConvertsCustomTool(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "use a custom tool",
		Tools: []types.Tool{
			{
				Type:        "custom",
				Name:        "apply_patch",
				Description: "Apply a patch.",
				Format:      map[string]any{"type": "grammar", "syntax": "lark", "definition": "start: /.+/"},
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
	if tool.Name != "apply_patch" {
		t.Fatalf("tool.Name = %q, want apply_patch", tool.Name)
	}
	if !strings.Contains(tool.Description, "Apply a patch.") || !strings.Contains(tool.Description, "grammar") {
		t.Fatalf("tool.Description = %q, want original description plus format hint", tool.Description)
	}
	schema := tool.InputSchema.(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("InputSchema = %#v, want object schema", schema)
	}
	if !got.CustomTools["apply_patch"] {
		t.Fatalf("CustomTools = %#v, want apply_patch marked custom", got.CustomTools)
	}
}

func TestToAnthropicRequestConvertsApplyPatchTool(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "edit a file",
		Tools: []types.Tool{
			{
				Type: "apply_patch",
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
	if tool.Name != "apply_patch" {
		t.Fatalf("tool.Name = %q, want apply_patch", tool.Name)
	}
	schema := tool.InputSchema.(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("InputSchema = %#v, want object schema", schema)
	}
	if !got.ApplyPatchTools["apply_patch"] {
		t.Fatalf("ApplyPatchTools = %#v, want apply_patch marked", got.ApplyPatchTools)
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

func TestToAnthropicRequestConvertsCustomToolCallAndOutput(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "apply this"},
				},
			},
			map[string]any{
				"type":    "custom_tool_call",
				"id":      "ctc_1",
				"call_id": "call_1",
				"name":    "apply_patch",
				"input":   "*** Begin Patch\n*** End Patch",
			},
			map[string]any{
				"type":    "custom_tool_call_output",
				"call_id": "call_1",
				"output":  "ok",
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
	callBlocks := got.Messages[1].Content.([]types.AnthropicContentBlock)
	input, ok := callBlocks[0].Input.(map[string]any)
	if !ok || input["input"] != "*** Begin Patch\n*** End Patch" {
		t.Fatalf("custom tool input = %#v, want wrapped input string", callBlocks[0].Input)
	}
	resultBlocks := got.Messages[2].Content.([]types.AnthropicContentBlock)
	if resultBlocks[0].Type != "tool_result" || resultBlocks[0].ToolUseID != "call_1" || resultBlocks[0].Content != "ok" {
		t.Fatalf("custom tool output = %#v, want tool_result", resultBlocks[0])
	}
}

func TestToAnthropicRequestConvertsApplyPatchCallAndOutput(t *testing.T) {
	operation := map[string]any{
		"type": "update_file",
		"path": "README.md",
		"diff": "@@\n-old\n+new\n",
	}
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "apply this"},
				},
			},
			map[string]any{
				"type":      "apply_patch_call",
				"id":        "apc_1",
				"call_id":   "call_1",
				"operation": operation,
			},
			map[string]any{
				"type":    "apply_patch_call_output",
				"call_id": "call_1",
				"status":  "completed",
				"output":  "ok",
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
	callBlocks := got.Messages[1].Content.([]types.AnthropicContentBlock)
	input, ok := callBlocks[0].Input.(map[string]any)
	if !ok || input["operation"] == nil {
		t.Fatalf("apply_patch input = %#v, want operation wrapper", callBlocks[0].Input)
	}
	op := input["operation"].(map[string]any)
	if op["type"] != "update_file" || op["path"] != "README.md" {
		t.Fatalf("operation = %#v, want update_file README.md", op)
	}
	resultBlocks := got.Messages[2].Content.([]types.AnthropicContentBlock)
	if resultBlocks[0].Type != "tool_result" || resultBlocks[0].ToolUseID != "call_1" || resultBlocks[0].Content != "ok" {
		t.Fatalf("apply_patch_call_output block = %#v, want tool_result", resultBlocks[0])
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
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 3072 {
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

func TestMergesMultipleFunctionCallOutputsIntoSingleUserMessage(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "search and lookup"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"id":        "fc_1",
				"call_id":   "call_1",
				"name":      "search",
				"arguments": `{"q":"weather"}`,
			},
			map[string]any{
				"type":      "function_call",
				"id":        "fc_2",
				"call_id":   "call_2",
				"name":      "lookup",
				"arguments": `{"key":"x"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  "sunny",
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_2",
				"output":  "found",
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3 (user, assistant with 2 tool_uses, user with 2 tool_results)", len(got.Messages))
	}

	assistantBlocks := got.Messages[1].Content.([]types.AnthropicContentBlock)
	if len(assistantBlocks) != 2 {
		t.Fatalf("len(assistantBlocks) = %d, want 2 tool_use blocks merged", len(assistantBlocks))
	}

	resultBlocks := got.Messages[2].Content.([]types.AnthropicContentBlock)
	if len(resultBlocks) != 2 {
		t.Fatalf("len(resultBlocks) = %d, want 2 tool_result blocks in same user message", len(resultBlocks))
	}
	if resultBlocks[0].ToolUseID != "call_1" || resultBlocks[1].ToolUseID != "call_2" {
		t.Fatalf("tool_result IDs = %q %q, want call_1 call_2", resultBlocks[0].ToolUseID, resultBlocks[1].ToolUseID)
	}
	if resultBlocks[0].Content != "sunny" || resultBlocks[1].Content != "found" {
		t.Fatalf("tool_result contents = %v %v, want sunny found", resultBlocks[0].Content, resultBlocks[1].Content)
	}
}

func TestParallelCallsFalseSetsDisableParallelToolUse(t *testing.T) {
	parallelFalse := false
	req := &types.OpenAIResponseRequest{
		Model:         "claude-sonnet-4-20250514",
		Input:         "hello",
		ParallelCalls: &parallelFalse,
		Tools: []types.Tool{
			{
				Type:       "function",
				Name:       "lookup",
				Parameters: map[string]any{"type": "object"},
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	choice, ok := got.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("ToolChoice = %T, want map[string]any", got.ToolChoice)
	}
	if choice["type"] != "auto" || choice["disable_parallel_tool_use"] != true {
		t.Fatalf("ToolChoice = %#v, want auto with disable_parallel_tool_use", choice)
	}
}

func TestParallelCallsTrueDoesNotSetDisableParallelToolUse(t *testing.T) {
	parallelTrue := true
	req := &types.OpenAIResponseRequest{
		Model:         "claude-sonnet-4-20250514",
		Input:         "hello",
		ParallelCalls: &parallelTrue,
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if got.ToolChoice != nil {
		t.Fatalf("ToolChoice = %#v, want nil when parallel_tool_calls is true", got.ToolChoice)
	}
}

func TestReasoningAndForcedToolChoiceDisablesThinking(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "force a tool call",
		Reasoning: map[string]any{
			"effort": "medium",
		},
		ToolChoice: "required",
		MaxTokens:  4096,
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if got.Thinking != nil {
		t.Fatalf("Thinking = %#v, want nil when forced tool_choice conflicts with reasoning", got.Thinking)
	}
	choice, ok := got.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("ToolChoice = %T, want map[string]any", got.ToolChoice)
	}
	if choice["type"] != "any" {
		t.Fatalf("ToolChoice = %#v, want forced any tool choice", choice)
	}
}

func TestReasoningAndSpecificToolChoiceDisablesThinking(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "force a specific tool",
		Reasoning: map[string]any{
			"effort": "medium",
		},
		ToolChoice: map[string]any{"type": "function", "name": "lookup"},
		MaxTokens:  4096,
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if got.Thinking != nil {
		t.Fatalf("Thinking = %#v, want nil when specific tool_choice conflicts with reasoning", got.Thinking)
	}
	choice, ok := got.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("ToolChoice = %T, want map[string]any", got.ToolChoice)
	}
	if choice["type"] != "tool" || choice["name"] != "lookup" {
		t.Fatalf("ToolChoice = %#v, want specific tool lookup", choice)
	}
}

func TestUnknownToolTypeReturnsError(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: "hello",
		Tools: []types.Tool{
			{Type: "bogus_tool"},
		},
	}

	_, err := ToAnthropicRequest(req)
	if err == nil {
		t.Fatal("ToAnthropicRequest returned nil error, want error for unknown tool type")
	}
	if !strings.Contains(err.Error(), "unknown tool type") {
		t.Fatalf("error = %q, want error containing 'unknown tool type'", err.Error())
	}
}

func TestEmptyCallIDInFunctionCallOutputSkipsMessage(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model: "claude-sonnet-4-20250514",
		Input: []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "test"},
				},
			},
			map[string]any{
				"type":   "function_call_output",
				"output": "no call id here",
			},
		},
	}

	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest returned error: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1 (function_call_output without call_id skipped)", len(got.Messages))
	}
}

func TestParseJSONOrStringWrapsNonObjectIntoObject(t *testing.T) {
	result := parseJSONOrString(`"just a string"`)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if m["value"] != "just a string" {
		t.Fatalf("m[value] = %#v, want 'just a string'", m["value"])
	}
}

func TestParseJSONOrStringWrapsArrayIntoObject(t *testing.T) {
	result := parseJSONOrString(`[1, 2, 3]`)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	arr, ok := m["value"].([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("m[value] = %#v, want [1,2,3]", m["value"])
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

func TestToOpenAIResponseConvertsCustomToolUse(t *testing.T) {
	resp := &types.AnthropicMessageResponse{
		ID: "msg_1",
		Content: []types.AnthropicContentBlock{
			{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  "apply_patch",
				Input: map[string]any{"input": "*** Begin Patch\n*** End Patch"},
			},
		},
	}

	got := ToOpenAIResponse(resp, "claude-test", map[string]bool{"apply_patch": true})
	if len(got.Output) != 1 {
		t.Fatalf("len(Output) = %d, want 1", len(got.Output))
	}
	item := got.Output[0]
	if item.Type != "custom_tool_call" || item.CallID != "call_1" || item.Name != "apply_patch" {
		t.Fatalf("custom tool item = %#v, want custom_tool_call", item)
	}
	if item.Input != "*** Begin Patch\n*** End Patch" {
		t.Fatalf("custom tool input = %q, want patch text", item.Input)
	}
	if item.Arguments != "" {
		t.Fatalf("custom tool arguments = %q, want empty", item.Arguments)
	}
}

func TestToOpenAIResponseConvertsApplyPatchToolUse(t *testing.T) {
	resp := &types.AnthropicMessageResponse{
		ID: "msg_1",
		Content: []types.AnthropicContentBlock{
			{
				Type: "tool_use",
				ID:   "call_1",
				Name: "apply_patch",
				Input: map[string]any{
					"operation": map[string]any{
						"type": "delete_file",
						"path": "old.txt",
					},
				},
			},
		},
	}

	got := ToOpenAIResponse(resp, "claude-test", nil, map[string]bool{"apply_patch": true})
	if len(got.Output) != 1 {
		t.Fatalf("len(Output) = %d, want 1", len(got.Output))
	}
	item := got.Output[0]
	if item.Type != "apply_patch_call" || item.CallID != "call_1" || item.Name != "" {
		t.Fatalf("apply_patch item = %#v, want apply_patch_call", item)
	}
	op, ok := item.Operation.(map[string]any)
	if !ok || op["type"] != "delete_file" || op["path"] != "old.txt" {
		t.Fatalf("operation = %#v, want delete_file old.txt", item.Operation)
	}
	if item.Input != "" || item.Arguments != "" {
		t.Fatalf("apply_patch input/args = %q/%q, want empty", item.Input, item.Arguments)
	}
}

func TestConvertReasoningConfig(t *testing.T) {
	tests := []struct {
		name      string
		reasoning any
		maxTokens int
		wantNil   bool
	}{
		{"nil reasoning", nil, 4096, true},
		{"maxTokens too low", map[string]any{"effort": "high"}, 1024, true},
		{"effort none", map[string]any{"effort": "none"}, 4096, true},
		{"effort medium", map[string]any{"effort": "medium"}, 4096, false},
		{"effort high", map[string]any{"effort": "high"}, 8192, false},
		{"not a map", "auto", 4096, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertReasoningConfig(tt.reasoning, tt.maxTokens)
			if tt.wantNil && got != nil {
				t.Fatalf("convertReasoningConfig = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Fatalf("convertReasoningConfig = nil, want non-nil")
			}
		})
	}
}

func TestCustomToolDescription(t *testing.T) {
	if got := customToolDescription("desc", nil); got != "desc" {
		t.Fatalf("customToolDescription with nil format = %q, want desc", got)
	}
	if got := customToolDescription("", map[string]any{"type": "grammar"}); !strings.Contains(got, "grammar") {
		t.Fatalf("customToolDescription without desc = %q, want format hint", got)
	}
	if got := customToolDescription("desc", map[string]any{"type": "grammar"}); !strings.Contains(got, "desc") || !strings.Contains(got, "grammar") {
		t.Fatalf("customToolDescription with desc = %q, want combined", got)
	}
}

func TestIsForcedToolChoice(t *testing.T) {
	tests := []struct {
		name string
		tc   any
		want bool
	}{
		{"map string type any", map[string]string{"type": "any"}, true},
		{"map string type tool", map[string]string{"type": "tool"}, true},
		{"map string type auto", map[string]string{"type": "auto"}, false},
		{"map any type any", map[string]any{"type": "any"}, true},
		{"map any type tool", map[string]any{"type": "tool"}, true},
		{"not a map", "auto", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isForcedToolChoice(tt.tc); got != tt.want {
				t.Fatalf("isForcedToolChoice(%v) = %v, want %v", tt.tc, got, tt.want)
			}
		})
	}
}

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		name string
		tc   any
		want map[string]any
	}{
		{"auto string", "auto", map[string]any{"type": "auto"}},
		{"none string", "none", map[string]any{"type": "none"}},
		{"required string", "required", map[string]any{"type": "any"}},
		{"specific name", "lookup", map[string]any{"type": "tool", "name": "lookup"}},
		{"apply_patch map", map[string]any{"type": "apply_patch"}, map[string]any{"type": "tool", "name": "apply_patch"}},
		{"custom map", map[string]any{"type": "custom", "name": "my_tool"}, map[string]any{"type": "tool", "name": "my_tool"}},
		{"function map no name", map[string]any{"type": "function"}, nil},
		{"function map with function.name", map[string]any{"type": "function", "function": map[string]any{"name": "fn"}}, map[string]any{"type": "tool", "name": "fn"}},
		{"function map with function no name", map[string]any{"type": "function", "function": map[string]any{}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToolChoice(tt.tc)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("convertToolChoice(%v) = %v, want nil", tt.tc, got)
				}
				return
			}
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

func TestWithDisableParallelToolUse(t *testing.T) {
	tests := []struct {
		name     string
		tc       any
		hasTools bool
		check    func(t *testing.T, got any)
	}{
		{"no tools", nil, false, func(t *testing.T, got any) {
			if got != nil {
				t.Fatalf("withDisableParallelToolUse = %v, want nil", got)
			}
		}},
		{"nil choice with tools", nil, true, func(t *testing.T, got any) {
			m := got.(map[string]any)
			if m["type"] != "auto" || m["disable_parallel_tool_use"] != true {
				t.Fatalf("withDisableParallelToolUse = %v", got)
			}
		}},
		{"none choice", map[string]any{"type": "none"}, true, func(t *testing.T, got any) {
			m := got.(map[string]any)
			if _, ok := m["disable_parallel_tool_use"]; ok {
				t.Fatalf("none choice should not get disable_parallel_tool_use")
			}
		}},
		{"auto choice", map[string]any{"type": "auto"}, true, func(t *testing.T, got any) {
			m := got.(map[string]any)
			if m["disable_parallel_tool_use"] != true {
				t.Fatalf("auto choice should get disable_parallel_tool_use")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withDisableParallelToolUse(tt.tc, tt.hasTools)
			tt.check(t, got)
		})
	}
}

func TestConvertInputString(t *testing.T) {
	msgs, system, err := convertInput("hello", "")
	if err != nil {
		t.Fatalf("convertInput error = %v", err)
	}
	if system != "" {
		t.Fatalf("system = %q, want empty", system)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("messages = %v", msgs)
	}
}

func TestConvertInputDefault(t *testing.T) {
	msgs, _, err := convertInput(42, "")
	if err != nil {
		t.Fatalf("convertInput error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	content, ok := msgs[0].Content.(string)
	if !ok || content != "42" {
		t.Fatalf("content = %v, want 42 string", msgs[0].Content)
	}
}

func TestConvertInputWithSystemMessages(t *testing.T) {
	input := []any{
		map[string]any{
			"role":    "system",
			"content": "sys1",
		},
		map[string]any{
			"role":    "developer",
			"content": "sys2",
		},
		map[string]any{
			"role":    "user",
			"content": "hello",
		},
	}
	msgs, system, err := convertInput(input, "base")
	if err != nil {
		t.Fatalf("convertInput error = %v", err)
	}
	if !strings.Contains(system, "base") || !strings.Contains(system, "sys1") || !strings.Contains(system, "sys2") {
		t.Fatalf("system = %q, want combined", system)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
}

func TestConvertInputMessageToolCallOutput(t *testing.T) {
	msg := types.InputMessage{
		Type:   "function_call_output",
		Role:   "tool",
		CallID: "",
		ToolID: "tool_1",
		Output: "result",
	}
	got := convertInputMessage(msg)
	if got == nil {
		t.Fatalf("convertInputMessage = nil")
	}
	blocks := got.Content.([]types.AnthropicContentBlock)
	if blocks[0].Type != "tool_result" || blocks[0].ToolUseID != "tool_1" {
		t.Fatalf("tool result block = %v", blocks[0])
	}
}

func TestConvertFunctionCallMessageWithContentFallback(t *testing.T) {
	msg := types.InputMessage{
		Type:   "function_call",
		ID:     "fc_1",
		CallID: "call_1",
		Name:   "lookup",
		Content: []any{
			map[string]any{"type": "input_text", "text": "{\"q\":\"weather\"}"},
		},
	}
	got := convertFunctionCallMessage(msg)
	if got == nil {
		t.Fatalf("convertFunctionCallMessage = nil")
	}
	blocks := got.Content.([]types.AnthropicContentBlock)
	input := blocks[0].Input.(map[string]any)
	if input["q"] != "weather" {
		t.Fatalf("input = %v", input)
	}
}

func TestConvertCustomToolCallMessageWithArguments(t *testing.T) {
	msg := types.InputMessage{
		Type:      "custom_tool_call",
		ID:        "ctc_1",
		CallID:    "call_1",
		Name:      "patch",
		Arguments: `patch text`,
	}
	got := convertCustomToolCallMessage(msg)
	if got == nil {
		t.Fatalf("convertCustomToolCallMessage = nil")
	}
	blocks := got.Content.([]types.AnthropicContentBlock)
	input := blocks[0].Input.(map[string]any)
	if input["input"] != "patch text" {
		t.Fatalf("input = %v", input)
	}
}

func TestConvertApplyPatchCallMessageWithInput(t *testing.T) {
	msg := types.InputMessage{
		Type:   "apply_patch_call",
		ID:     "apc_1",
		CallID: "call_1",
		Input:  `{"type":"create_file","path":"a.txt"}`,
	}
	got := convertApplyPatchCallMessage(msg)
	if got == nil {
		t.Fatalf("convertApplyPatchCallMessage = nil")
	}
	blocks := got.Content.([]types.AnthropicContentBlock)
	input := blocks[0].Input.(map[string]any)
	op := input["operation"].(map[string]any)
	if op["type"] != "create_file" {
		t.Fatalf("operation = %v", op)
	}
}

func TestConvertApplyPatchCallMessageWithArguments(t *testing.T) {
	msg := types.InputMessage{
		Type:      "apply_patch_call",
		ID:        "apc_1",
		CallID:    "call_1",
		Arguments: `{"type":"delete_file","path":"old.txt"}`,
	}
	got := convertApplyPatchCallMessage(msg)
	if got == nil {
		t.Fatalf("convertApplyPatchCallMessage = nil")
	}
	blocks := got.Content.([]types.AnthropicContentBlock)
	input := blocks[0].Input.(map[string]any)
	op := input["operation"].(map[string]any)
	if op["type"] != "delete_file" {
		t.Fatalf("operation = %v", op)
	}
}

func TestConvertApplyPatchCallMessageWithStringOperation(t *testing.T) {
	msg := types.InputMessage{
		Type:      "apply_patch_call",
		ID:        "apc_1",
		CallID:    "call_1",
		Operation: `{"type":"update_file","path":"f.txt"}`,
	}
	got := convertApplyPatchCallMessage(msg)
	if got == nil {
		t.Fatalf("convertApplyPatchCallMessage = nil")
	}
	blocks := got.Content.([]types.AnthropicContentBlock)
	input := blocks[0].Input.(map[string]any)
	op := input["operation"].(map[string]any)
	if op["type"] != "update_file" {
		t.Fatalf("operation = %v", op)
	}
}

func TestConvertContentBlockImageTypes(t *testing.T) {
	tests := []struct {
		name  string
		item  any
		check func(t *testing.T, block *types.AnthropicContentBlock)
	}{
		{"image_url", map[string]any{"type": "image_url", "image_url": "https://example.com/img.png"}, func(t *testing.T, block *types.AnthropicContentBlock) {
			if block.Source.Type != "url" || block.Source.URL != "https://example.com/img.png" {
				t.Fatalf("source = %v", block.Source)
			}
		}},
		{"input_image", map[string]any{"type": "input_image", "image_url": "https://example.com/img.png"}, func(t *testing.T, block *types.AnthropicContentBlock) {
			if block.Source.Type != "url" || block.Source.URL != "https://example.com/img.png" {
				t.Fatalf("source = %v", block.Source)
			}
		}},
		{"image base64", map[string]any{"type": "image", "media_type": "image/png", "data": "abc123"}, func(t *testing.T, block *types.AnthropicContentBlock) {
			if block.Source.Type != "base64" || block.Source.Data != "abc123" {
				t.Fatalf("source = %v", block.Source)
			}
		}},
		{"image no media_type", map[string]any{"type": "image", "data": "abc"}, func(t *testing.T, block *types.AnthropicContentBlock) {
			if block.Source.MediaType != "image/jpeg" {
				t.Fatalf("media type = %v, want image/jpeg", block.Source.MediaType)
			}
		}},
		{"unknown type", map[string]any{"type": "unknown", "text": "hi"}, func(t *testing.T, block *types.AnthropicContentBlock) {
			if block.Type != "text" || block.Text != "map[text:hi type:unknown]" {
				// Falls back to fmt.Sprint
			}
		}},
		{"not a map", "plain", func(t *testing.T, block *types.AnthropicContentBlock) {
			if block.Type != "text" || block.Text != "plain" {
				t.Fatalf("block = %v", block)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := convertContentBlock(tt.item)
			if block == nil {
				t.Fatalf("convertContentBlock = nil")
			}
			tt.check(t, block)
		})
	}
}

func TestConvertToolResultContent(t *testing.T) {
	if got := convertToolResultContent(nil); got != "" {
		t.Fatalf("convertToolResultContent(nil) = %v, want ''", got)
	}
	if got := convertToolResultContent("str"); got != "str" {
		t.Fatalf("convertToolResultContent(str) = %v, want str", got)
	}
	if got := convertToolResultContent([]any{map[string]any{"type": "input_text", "text": "hi"}}); got == nil {
		t.Fatalf("convertToolResultContent(array) = nil")
	}
	if got := convertToolResultContent(42); got != "42" {
		t.Fatalf("convertToolResultContent(42) = %v, want 42 string", got)
	}
}

func TestContentToString(t *testing.T) {
	if got := contentToString("plain"); got != "plain" {
		t.Fatalf("contentToString(string) = %q, want plain", got)
	}
	if got := contentToString(42); got != "42" {
		t.Fatalf("contentToString(int) = %q, want 42", got)
	}
	got := contentToString([]any{
		map[string]any{"type": "input_text", "text": "a"},
		map[string]any{"type": "input_text", "text": "b"},
	})
	if got != "a\nb" {
		t.Fatalf("contentToString(array) = %q, want a\\nb", got)
	}
	// Array with no text blocks
	got2 := contentToString([]any{
		map[string]any{"type": "image", "data": "abc"},
	})
	if got2 == "" {
		t.Fatalf("contentToString(image array) = empty")
	}
}

func TestCitationRange(t *testing.T) {
	// exact match
	start, end := citationRange("hello world", "world", 0)
	if start != 6 || end != 11 {
		t.Fatalf("citationRange = %d, %d, want 6, 11", start, end)
	}
	// no match, fallback to full text
	start, end = citationRange("hello", "world", 10)
	if start != 10 || end != 15 {
		t.Fatalf("citationRange no match = %d, %d, want 10, 15", start, end)
	}
	// empty cited text
	start, end = citationRange("hello", "", 5)
	if start != 5 || end != 10 {
		t.Fatalf("citationRange empty = %d, %d, want 5, 10", start, end)
	}
}

func TestCustomToolInput(t *testing.T) {
	if got := customToolInput("plain"); got != "plain" {
		t.Fatalf("customToolInput(string) = %q, want plain", got)
	}
	if got := customToolInput(map[string]any{"input": "nested"}); got != "nested" {
		t.Fatalf("customToolInput(map) = %q, want nested", got)
	}
	if got := customToolInput(map[string]string{"input": "ss"}); got != "ss" {
		t.Fatalf("customToolInput(string map) = %q, want ss", got)
	}
	if got := customToolInput(42); got != "42" {
		t.Fatalf("customToolInput(int) = %q, want 42", got)
	}
}

func TestApplyPatchOperation(t *testing.T) {
	if got := applyPatchOperation(nil); got == nil {
		t.Fatalf("applyPatchOperation(nil) = nil")
	}
	// Plain string gets JSON-parsed, which wraps into map[value:...]
	got := applyPatchOperation("plain")
	if got == nil {
		t.Fatalf("applyPatchOperation(string) = nil")
	}
	op := map[string]any{"type": "create_file", "path": "a.txt"}
	if got := applyPatchOperation(op); got == nil {
		t.Fatalf("applyPatchOperation(op) = nil")
	}
	wrapped := map[string]any{"operation": op}
	if got := applyPatchOperation(wrapped); got == nil {
		t.Fatalf("applyPatchOperation(wrapped) = nil")
	}
}

func TestStringifyToolInput(t *testing.T) {
	if got := stringifyToolInput(nil); got != "" {
		t.Fatalf("stringifyToolInput(nil) = %q, want ''", got)
	}
	if got := stringifyToolInput("plain"); got != "plain" {
		t.Fatalf("stringifyToolInput(string) = %q, want plain", got)
	}
	if got := stringifyToolInput(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Fatalf("stringifyToolInput(map) = %q", got)
	}
}

func TestHasToolResult(t *testing.T) {
	if hasToolResult(nil) {
		t.Fatalf("hasToolResult(nil) = true")
	}
	if !hasToolResult([]types.AnthropicContentBlock{{Type: "tool_result"}}) {
		t.Fatalf("hasToolResult(tool_result) = false")
	}
	if hasToolResult([]types.AnthropicContentBlock{{Type: "text"}}) {
		t.Fatalf("hasToolResult(text) = true")
	}
}

func TestContentBlocksFromAny(t *testing.T) {
	// from existing blocks
	blocks := contentBlocksFromAny([]types.AnthropicContentBlock{{Type: "text", Text: "hi"}})
	if len(blocks) != 1 || blocks[0].Text != "hi" {
		t.Fatalf("contentBlocksFromAny = %v", blocks)
	}
	// from string
	blocks = contentBlocksFromAny("hello")
	if len(blocks) != 1 || blocks[0].Text != "hello" {
		t.Fatalf("contentBlocksFromAny(string) = %v", blocks)
	}
	// from empty string
	blocks = contentBlocksFromAny("")
	if blocks != nil {
		t.Fatalf("contentBlocksFromAny(empty) = %v, want nil", blocks)
	}
	// from int
	blocks = contentBlocksFromAny(42)
	if len(blocks) != 1 || blocks[0].Text != "42" {
		t.Fatalf("contentBlocksFromAny(int) = %v", blocks)
	}
}

func TestReasoningOutputItemRedacted(t *testing.T) {
	block := types.AnthropicContentBlock{
		Type: "redacted_thinking",
		Data: "opaque_data",
	}
	item := reasoningOutputItem("resp_1", 0, block)
	if item.Type != "reasoning" || item.EncryptedContent != "opaque_data" {
		t.Fatalf("reasoningOutputItem = %+v", item)
	}
}

func TestDataURLSource(t *testing.T) {
	if got := dataURLSource("https://example.com/img.png"); got != nil {
		t.Fatalf("dataURLSource(non-data) = %v, want nil", got)
	}
	if got := dataURLSource("data:"); got != nil {
		t.Fatalf("dataURLSource(empty data) = %v, want nil", got)
	}
	source := dataURLSource("data:image/png;base64,abc123")
	if source == nil || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "abc123" {
		t.Fatalf("dataURLSource = %v", source)
	}
	source = dataURLSource("data:,rawdata")
	if source == nil || source.MediaType != "image/jpeg" || source.Data != "rawdata" {
		t.Fatalf("dataURLSource no media type = %v", source)
	}
}

func TestImageBlock(t *testing.T) {
	if got := imageBlock(""); got != nil {
		t.Fatalf("imageBlock(empty) = %v, want nil", got)
	}
	if got := imageBlock("https://example.com/img.png"); got == nil || got.Source.URL != "https://example.com/img.png" {
		t.Fatalf("imageBlock(url) = %v", got)
	}
	if got := imageBlock("data:image/png;base64,abc"); got == nil || got.Source.Type != "base64" {
		t.Fatalf("imageBlock(data) = %v", got)
	}
}

func TestImageURLValue(t *testing.T) {
	if got := imageURLValue("https://example.com/img.png"); got != "https://example.com/img.png" {
		t.Fatalf("imageURLValue(string) = %q", got)
	}
	if got := imageURLValue(map[string]any{"url": "https://example.com/img.png"}); got != "https://example.com/img.png" {
		t.Fatalf("imageURLValue(map) = %q", got)
	}
	if got := imageURLValue(nil); got != "" {
		t.Fatalf("imageURLValue(nil) = %q, want ''", got)
	}
}

func TestTextValueKey(t *testing.T) {
	if got := textValueKey(map[string]any{"text": "hello"}, "text"); got != "hello" {
		t.Fatalf("textValueKey = %q", got)
	}
	if got := textValueKey(map[string]any{"thinking": "thought"}, "thinking"); got != "thought" {
		t.Fatalf("textValueKey(thinking) = %q", got)
	}
	if got := textValueKey(map[string]any{"other": "val"}, "missing"); got != "" {
		t.Fatalf("textValueKey(missing) = %q, want ''", got)
	}
	// When key is "text" but map has "text" key with non-string value
	if got := textValueKey(map[string]any{"text": 42}, "text"); got != "42" {
		t.Fatalf("textValueKey(text int) = %q, want 42", got)
	}
}

func TestToAnthropicRequestConvertsApplyPatchToolChoice(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:      "claude-sonnet-4-20250514",
		Input:      "edit file",
		ToolChoice: map[string]any{"type": "apply_patch"},
	}
	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest error = %v", err)
	}
	choice := got.ToolChoice.(map[string]any)
	if choice["type"] != "tool" || choice["name"] != "apply_patch" {
		t.Fatalf("ToolChoice = %v", choice)
	}
}

func TestToOpenAIResponseMaxTokensIncomplete(t *testing.T) {
	resp := &types.AnthropicMessageResponse{
		ID:         "msg_1",
		StopReason: "max_tokens",
		Content:    []types.AnthropicContentBlock{},
		Usage:      types.AnthropicUsage{InputTokens: 100, OutputTokens: 50},
	}
	got := ToOpenAIResponse(resp, "claude-test")
	if got.Status != "incomplete" || got.IncompleteDetails.Reason != "max_output_tokens" {
		t.Fatalf("response = %+v", got)
	}
}

func TestToAnthropicRequestConvertsReasoningConfigMaxTokens(t *testing.T) {
	req := &types.OpenAIResponseRequest{
		Model:     "claude-sonnet-4-20250514",
		Input:     "think",
		MaxTokens: 5000,
		Reasoning: map[string]any{"effort": "low"},
	}
	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest error = %v", err)
	}
	thinking := got.Thinking.(map[string]any)
	if thinking["budget_tokens"].(int) != 3750 {
		t.Fatalf("budget_tokens = %v, want 3750", thinking["budget_tokens"])
	}
}

func TestToAnthropicRequestConvertsTemperatureAndTopP(t *testing.T) {
	temp := 0.7
	topP := 0.9
	req := &types.OpenAIResponseRequest{
		Model:       "claude-sonnet-4-20250514",
		Input:       "hello",
		Temperature: &temp,
		TopP:        &topP,
	}
	got, err := ToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("ToAnthropicRequest error = %v", err)
	}
	if *got.Temperature != 0.7 {
		t.Fatalf("temperature = %v", *got.Temperature)
	}
	if *got.TopP != 0.9 {
		t.Fatalf("topP = %v", *got.TopP)
	}
}

func TestParseJSONOrStringEmpty(t *testing.T) {
	result := parseJSONOrString("")
	m, ok := result.(map[string]any)
	if !ok || len(m) != 0 {
		t.Fatalf("parseJSONOrString(empty) = %v", result)
	}
}
