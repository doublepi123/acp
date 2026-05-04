package handler

import (
	"encoding/json"
	"testing"

	"github.com/doublepi123/acp/internal/types"
)

func TestConvertStreamEventUsesStableResponseAndItemIDs(t *testing.T) {
	state := newStreamState()
	idx := 0

	created := convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}
	createdEvent := decodeEvent(t, created[0])
	response := createdEvent["response"].(map[string]any)
	if response["id"] != "msg_1" {
		t.Fatalf("response id = %#v, want msg_1", response["id"])
	}

	added := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "text",
		},
	}, "claude-test", state)
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type: "text_delta",
			Text: "hello",
		},
	}, "claude-test", state)

	done := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	if len(done) != 2 {
		t.Fatalf("len(done) = %d, want output_text.done and output_item.done", len(done))
	}
	textDone := decodeEvent(t, done[0])
	if textDone["type"] != "response.output_text.done" {
		t.Fatalf("first done event type = %#v, want response.output_text.done", textDone["type"])
	}
	doneItem := decodeEvent(t, done[1])["item"].(map[string]any)
	if addedItem["id"] != doneItem["id"] {
		t.Fatalf("added id = %#v, done id = %#v, want stable id", addedItem["id"], doneItem["id"])
	}
	content := doneItem["content"].([]any)[0].(map[string]any)
	if content["text"] != "hello" {
		t.Fatalf("done text = %#v, want accumulated text", content["text"])
	}

	completed := convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_delta",
	}, "claude-test", state)
	completedResponse := decodeEvent(t, completed[0])["response"].(map[string]any)
	if completedResponse["id"] != "msg_1" {
		t.Fatalf("completed response id = %#v, want msg_1", completedResponse["id"])
	}
}

func TestConvertStreamEventAccumulatesFunctionArguments(t *testing.T) {
	state := newStreamState()
	idx := 1

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	added := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "tool_use",
			ID:   "call_1",
			Name: "lookup",
		},
	}, "claude-test", state)
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)

	for _, part := range []string{`{"q":`, `"weather"}`} {
		convertStreamEvent(&types.AnthropicStreamEvent{
			Type:  "content_block_delta",
			Index: &idx,
			Delta: &types.AnthropicDelta{
				Type:        "input_json_delta",
				PartialJSON: part,
			},
		}, "claude-test", state)
	}

	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want arguments.done and output_item.done", len(events))
	}

	argsDone := decodeEvent(t, events[0])
	if argsDone["type"] != "response.function_call_arguments.done" || argsDone["arguments"] != `{"q":"weather"}` {
		t.Fatalf("arguments done = %#v, want accumulated arguments", argsDone)
	}

	doneItem := decodeEvent(t, events[1])["item"].(map[string]any)
	if addedItem["id"] != doneItem["id"] {
		t.Fatalf("added id = %#v, done id = %#v, want stable id", addedItem["id"], doneItem["id"])
	}
	if doneItem["call_id"] != "call_1" || doneItem["arguments"] != `{"q":"weather"}` {
		t.Fatalf("done item = %#v, want call_id and accumulated arguments", doneItem)
	}
}

func TestConvertStreamEventMapsCustomToolCall(t *testing.T) {
	state := newStreamState(map[string]bool{"apply_patch": true})
	idx := 1

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	added := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "tool_use",
			ID:   "call_1",
			Name: "apply_patch",
		},
	}, "claude-test", state)
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)
	if addedItem["type"] != "custom_tool_call" || addedItem["input"] != "" {
		t.Fatalf("added item = %#v, want in-progress custom_tool_call", addedItem)
	}

	delta := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type:        "input_json_delta",
			PartialJSON: `{"input":"patch text"}`,
		},
	}, "claude-test", state)
	if len(delta) != 0 {
		t.Fatalf("custom tool input delta emitted %#v, want no JSON argument delta", delta)
	}

	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want input.done and output_item.done", len(events))
	}
	inputDone := decodeEvent(t, events[0])
	if inputDone["type"] != "response.custom_tool_call_input.done" || inputDone["input"] != "patch text" {
		t.Fatalf("input done = %#v, want finalized custom input", inputDone)
	}
	doneItem := decodeEvent(t, events[1])["item"].(map[string]any)
	if doneItem["type"] != "custom_tool_call" || doneItem["input"] != "patch text" {
		t.Fatalf("done item = %#v, want completed custom_tool_call", doneItem)
	}
}

func TestConvertStreamEventAccumulatesThinkingAsReasoning(t *testing.T) {
	state := newStreamState()
	idx := 0

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	added := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "thinking",
		},
	}, "claude-test", state)
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)
	if addedItem["type"] != "reasoning" {
		t.Fatalf("added item = %#v, want reasoning", addedItem)
	}

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type:     "thinking_delta",
			Thinking: "I should use a tool.",
		},
	}, "claude-test", state)
	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type:      "signature_delta",
			Signature: "sig_1",
		},
	}, "claude-test", state)

	done := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	doneItem := decodeEvent(t, done[0])["item"].(map[string]any)
	if doneItem["type"] != "reasoning" || doneItem["encrypted_content"] != "sig_1" {
		t.Fatalf("done item = %#v, want completed reasoning with signature", doneItem)
	}
	content := doneItem["content"].([]any)[0].(map[string]any)
	if content["text"] != "I should use a tool." {
		t.Fatalf("reasoning content = %#v, want thinking text", content)
	}

	completed := convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_delta",
	}, "claude-test", state)
	output := decodeEvent(t, completed[0])["response"].(map[string]any)["output"].([]any)
	if output[0].(map[string]any)["type"] != "reasoning" {
		t.Fatalf("completed output = %#v, want reasoning item", output)
	}
}

func TestConvertStreamEventMapsServerWebSearchWithoutFunctionCall(t *testing.T) {
	state := newStreamState()
	idx := 1
	resultIdx := 2

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	added := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "server_tool_use",
			ID:   "srvtoolu_1",
			Name: "web_search",
		},
	}, "claude-test", state)
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)
	if addedItem["type"] != "web_search_call" {
		t.Fatalf("added item type = %#v, want web_search_call", addedItem["type"])
	}

	delta := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type:        "input_json_delta",
			PartialJSON: `{"query":"weather"}`,
		},
	}, "claude-test", state)
	if len(delta) != 0 {
		t.Fatalf("server tool input delta emitted %#v, want no function argument delta", delta)
	}

	searching := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	searchingEvent := decodeEvent(t, searching[0])
	if searchingEvent["type"] != "response.web_search_call.searching" {
		t.Fatalf("server tool stop event = %#v, want web_search_call.searching", searchingEvent)
	}

	done := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &resultIdx,
		ContentBlock: &types.AnthropicContentBlock{
			Type:      "web_search_tool_result",
			ToolUseID: "srvtoolu_1",
		},
	}, "claude-test", state)
	if len(done) != 2 {
		t.Fatalf("len(done) = %d, want completed and output_item.done", len(done))
	}
	completed := decodeEvent(t, done[0])
	if completed["type"] != "response.web_search_call.completed" {
		t.Fatalf("result start event = %#v, want web_search_call.completed", completed)
	}
	doneItem := decodeEvent(t, done[1])["item"].(map[string]any)
	if doneItem["type"] != "web_search_call" || doneItem["status"] != "completed" {
		t.Fatalf("done item = %#v, want completed web_search_call", doneItem)
	}
	action := doneItem["action"].(map[string]any)
	if action["query"] != "weather" {
		t.Fatalf("done action = %#v, want accumulated query", action)
	}
}

func TestStreamOutputIndicesNoGapsAfterSkippedBlock(t *testing.T) {
	state := newStreamState()

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	idx0 := 0
	idx1 := 1
	idx2 := 2

	added0 := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx0,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "text",
		},
	}, "claude-test", state)
	outIdx0 := decodeEvent(t, added0[0])["output_index"].(float64)
	if outIdx0 != 0 {
		t.Fatalf("first output_index = %v, want 0", outIdx0)
	}

	result := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx1,
		ContentBlock: &types.AnthropicContentBlock{
			Type:      "web_search_tool_result",
			ToolUseID: "srvtoolu_1",
		},
	}, "claude-test", state)
	if len(result) != 0 {
		t.Fatalf("web_search_tool_result emitted %#v, want nil (skipped block)", result)
	}

	added2 := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx2,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "text",
		},
	}, "claude-test", state)
	outIdx2 := decodeEvent(t, added2[0])["output_index"].(float64)
	if outIdx2 != 1 {
		t.Fatalf("third output_index = %v, want 1 (no gap after skipped block)", outIdx2)
	}
}

func TestMessagesURLTrimsTrailingSlash(t *testing.T) {
	h := &Handler{AnthropicURL: "https://api.anthropic.com/"}
	if got := h.messagesURL(); got != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("messagesURL() = %q, want trimmed URL", got)
	}
}

func decodeEvent(t *testing.T, raw string) map[string]any {
	t.Helper()

	var event map[string]any
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", raw, err)
	}
	return event
}
