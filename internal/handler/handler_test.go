package handler

import (
	"encoding/json"
	"testing"

	"github.com/lcy/anthropic-openai-proxy/internal/types"
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
	doneItem := decodeEvent(t, done[0])["item"].(map[string]any)
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
