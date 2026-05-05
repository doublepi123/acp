package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doublepi123/acp/internal/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestHandleResponsesConvertsApplyPatchToolCall(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req types.AnthropicMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode upstream request: %v", err)
		}
		if len(req.Tools) != 1 || req.Tools[0].Name != "apply_patch" {
			t.Fatalf("upstream tools = %#v, want apply_patch tool", req.Tools)
		}
		var body bytes.Buffer
		json.NewEncoder(&body).Encode(types.AnthropicMessageResponse{
			ID:   "msg_1",
			Type: "message",
			Role: "assistant",
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
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body.String())),
		}, nil
	})}

	body := []byte(`{"model":"claude-test","input":"edit","tools":[{"type":"apply_patch"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResponses(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp types.OpenAIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("len(Output) = %d, want 1", len(resp.Output))
	}
	item := resp.Output[0]
	if item.Type != "apply_patch_call" || item.CallID != "call_1" {
		t.Fatalf("output item = %#v, want apply_patch_call", item)
	}
	op, ok := item.Operation.(map[string]any)
	if !ok || op["type"] != "delete_file" || op["path"] != "old.txt" {
		t.Fatalf("operation = %#v, want delete_file old.txt", item.Operation)
	}
}

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

func TestConvertStreamEventMapsApplyPatchCall(t *testing.T) {
	state := newStreamState(nil, map[string]bool{"apply_patch": true})
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
	if addedItem["type"] != "apply_patch_call" || addedItem["name"] != nil {
		t.Fatalf("added item = %#v, want in-progress apply_patch_call without name", addedItem)
	}

	delta := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type:        "input_json_delta",
			PartialJSON: `{"operation":{"type":"delete_file","path":"old.txt"}}`,
		},
	}, "claude-test", state)
	if len(delta) != 0 {
		t.Fatalf("apply_patch input delta emitted %#v, want no function argument delta", delta)
	}

	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want output_item.done", len(events))
	}
	doneItem := decodeEvent(t, events[0])["item"].(map[string]any)
	if doneItem["type"] != "apply_patch_call" || doneItem["call_id"] != "call_1" {
		t.Fatalf("done item = %#v, want completed apply_patch_call", doneItem)
	}
	op := doneItem["operation"].(map[string]any)
	if op["type"] != "delete_file" || op["path"] != "old.txt" {
		t.Fatalf("operation = %#v, want delete_file old.txt", op)
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
	if len(added) != 2 {
		t.Fatalf("len(added) = %d, want output_item.added and web_search_call.in_progress", len(added))
	}
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)
	if addedItem["type"] != "web_search_call" {
		t.Fatalf("added item type = %#v, want web_search_call", addedItem["type"])
	}
	inProgress := decodeEvent(t, added[1])
	if inProgress["type"] != "response.web_search_call.in_progress" || inProgress["item_id"] != "srvtoolu_1" {
		t.Fatalf("in-progress event = %#v, want web_search_call.in_progress for srvtoolu_1", inProgress)
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

func TestNewHandlerDoesNotUseTotalClientTimeout(t *testing.T) {
	h := New("https://api.anthropic.com", "key", "claude-test")
	if h.HTTPClient.Timeout != 0 {
		t.Fatalf("HTTPClient.Timeout = %v, want no total timeout for streaming", h.HTTPClient.Timeout)
	}
}

func TestStreamErrorEventsEmitFailedResponseAndError(t *testing.T) {
	state := newStreamState()
	state.responseID = "resp_1"
	events := streamErrorEvents(state, "claude-test", "stream_error", "upstream interrupted")
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want response.failed and error", len(events))
	}

	failed := decodeEvent(t, events[0])
	if failed["type"] != "response.failed" {
		t.Fatalf("first event type = %#v, want response.failed", failed["type"])
	}
	response := failed["response"].(map[string]any)
	if response["status"] != "failed" {
		t.Fatalf("failed response = %#v, want failed status", response)
	}
	errObj := response["error"].(map[string]any)
	if errObj["code"] != "stream_error" || errObj["message"] != "upstream interrupted" {
		t.Fatalf("response error = %#v, want stream_error message", errObj)
	}

	errEvent := decodeEvent(t, events[1])
	if errEvent["type"] != "error" || errEvent["code"] != "stream_error" {
		t.Fatalf("error event = %#v, want top-level error event", errEvent)
	}
}

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name         string
		defaultModel string
		inputModel   string
		want         string
	}{
		{"empty default returns input", "", "claude-test", "claude-test"},
		{"empty input uses default", "default-model", "", "default-model"},
		{"codex-auto-review uses default", "default-model", "codex-auto-review", "default-model"},
		{"explicit model overrides", "default-model", "custom-model", "custom-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{DefaultModel: tt.defaultModel}
			got := h.resolveModel(tt.inputModel)
			if got != tt.want {
				t.Fatalf("resolveModel(%q) = %q, want %q", tt.inputModel, got, tt.want)
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	h := New("https://api.example.com", "key", "model")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.HandleHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body.status = %q, want ok", body["status"])
	}
}

func TestHandleResponsesMethodNotAllowed(t *testing.T) {
	h := New("https://api.example.com", "key", "model")
	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleResponsesInvalidJSON(t *testing.T) {
	h := New("https://api.example.com", "key", "model")
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleNonStreamUpstreamError(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"server_error","message":"boom"}}`)),
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleNonStreamTransportError(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, http.ErrHandlerTimeout
	})}
	body := []byte(`{"model":"claude-test","input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestHandleStreamUpstreamStatusError(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"rate_limit_error","message":"too fast"}}`)),
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"hello","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

func TestHandleStreamTransportError(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})}
	body := []byte(`{"model":"claude-test","input":"hello","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestHandleStreamScannerError(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		// Return a body that will cause the scanner to fail due to a large token
		pr, pw := io.Pipe()
		go func() {
			pw.Write([]byte("data: " + string(make([]byte, 1024)) + "\n"))
			pw.Close()
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       pr,
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"hello","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	// Should complete without panic even with parsing error, and still emit [DONE]
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("body does not contain [DONE]: %q", bodyStr)
	}
}

func TestHandleStreamErrorEvent(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		lines := []string{
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n",
			"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"server overloaded\"}}\n",
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Join(lines, ""))),
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"hello","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("body does not contain [DONE]: %q", bodyStr)
	}
	if !strings.Contains(bodyStr, "overloaded_error") {
		t.Fatalf("body does not contain overloaded_error: %q", bodyStr)
	}
}

func TestStreamStringifyToolInput(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"map", map[string]any{"key": "value"}, `{"key":"value"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamStringifyToolInput(tt.input)
			if got != tt.want {
				t.Fatalf("streamStringifyToolInput(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStreamCustomToolInput(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"empty", "", ""},
		{"plain string", "plain", "plain"},
		{"json with input", `{"input":"content"}`, "content"},
		{"json input number", `{"input":42}`, "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamCustomToolInput(tt.args)
			if got != tt.want {
				t.Fatalf("streamCustomToolInput(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestStreamApplyPatchOperation(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"empty", ""},
		{"direct operation", `{"type":"create_file","path":"a.txt"}`},
		{"wrapped operation", `{"operation":{"type":"delete_file","path":"old.txt"}}`},
		{"string value", `"not-json"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamApplyPatchOperation(tt.args)
			if got == nil {
				t.Fatalf("streamApplyPatchOperation(%q) = nil", tt.args)
			}
		})
	}
}

func TestStreamWebSearchAction(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"with query", `{"query":"weather"}`},
		{"empty", ""},
		{"no query", `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := streamWebSearchAction(tt.args)
			if action["type"] != "search" {
				t.Fatalf("action type = %v, want search", action["type"])
			}
		})
	}
}

func TestConvertStreamEventMapsCitations(t *testing.T) {
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
			Type: "text",
		},
	}, "claude-test", state)
	_ = decodeEvent(t, added[0])

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type: "text_delta",
			Text: "check this",
		},
	}, "claude-test", state)

	// Citation without URL should be skipped
	emptyCite := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type: "citations_delta",
			Citation: &types.AnthropicCitation{
				Type:  "char_location",
				Title: "No URL",
			},
		},
	}, "claude-test", state)
	if len(emptyCite) != 0 {
		t.Fatalf("citation without URL emitted events: %v", emptyCite)
	}

	// Citation with URL
	cited := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type: "citations_delta",
			Citation: &types.AnthropicCitation{
				Type:  "char_location",
				URL:   "https://example.com",
				Title: "Example",
			},
		},
	}, "claude-test", state)
	if len(cited) != 1 {
		t.Fatalf("citation with URL emitted %d events, want 1", len(cited))
	}
	citationEvent := decodeEvent(t, cited[0])
	if citationEvent["type"] != "response.output_text.annotation.added" {
		t.Fatalf("citation event type = %v", citationEvent["type"])
	}
}

func TestConvertStreamEventContentBlockStartUnknownType(t *testing.T) {
	state := newStreamState()
	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	idx := 0
	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "unknown_block_type",
		},
	}, "claude-test", state)
	// Unknown type should not emit events but should allocate an output index
	if events != nil {
		t.Fatalf("unknown block type emitted events: %v", events)
	}
}

func TestConvertStreamEventMessageStop(t *testing.T) {
	state := newStreamState()
	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_stop",
	}, "claude-test", state)
	if events != nil {
		t.Fatalf("message_stop emitted events: %v", events)
	}
}

func TestConvertStreamEventThinkingRedacted(t *testing.T) {
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
			Type: "redacted_thinking",
			Data: "opaque_1",
		},
	}, "claude-test", state)
	addedItem := decodeEvent(t, added[0])["item"].(map[string]any)
	if addedItem["type"] != "reasoning" {
		t.Fatalf("item type = %v, want reasoning", addedItem["type"])
	}

	// Signature delta
	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &types.AnthropicDelta{
			Type:      "signature_delta",
			Signature: "sig_redacted",
		},
	}, "claude-test", state)

	done := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	doneItem := decodeEvent(t, done[0])["item"].(map[string]any)
	if doneItem["encrypted_content"] != "sig_redacted" {
		t.Fatalf("encrypted_content = %v, want sig_redacted", doneItem["encrypted_content"])
	}
}

func TestHandleResponsesTranslateError(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	body := []byte(`{"model":"claude-test","input":"test","tools":[{"type":"bogus"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleNonStreamAnthropicErrorParsing(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("plain text error")),
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestContentBlockStopForServerToolUseSearching(t *testing.T) {
	state := newStreamState()
	idx := 1

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "server_tool_use",
			Name: "web_search",
		},
	}, "claude-test", state)

	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx,
	}, "claude-test", state)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1 searching event", len(events))
	}
	searchEvent := decodeEvent(t, events[0])
	if searchEvent["type"] != "response.web_search_call.searching" {
		t.Fatalf("event type = %v, want response.web_search_call.searching", searchEvent["type"])
	}
}

func TestContentBlockStartForWebSearchResult(t *testing.T) {
	state := newStreamState()

	convertStreamEvent(&types.AnthropicStreamEvent{
		Type: "message_start",
		Message: &types.AnthropicMessageResponse{
			ID: "msg_1",
		},
	}, "claude-test", state)

	// Set up a server_tool_use block first so callIndex is populated
	idx := 1
	convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &types.AnthropicContentBlock{
			Type: "server_tool_use",
			ID:   "srv_1",
			Name: "web_search",
		},
	}, "claude-test", state)

	// Now add the web_search_tool_result that references it
	resultIdx := 2
	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &resultIdx,
		ContentBlock: &types.AnthropicContentBlock{
			Type:      "web_search_tool_result",
			ToolUseID: "srv_1",
		},
	}, "claude-test", state)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 (completed + output_item.done)", len(events))
	}
}

func TestWebSearchResultWithoutPriorCall(t *testing.T) {
	state := newStreamState()
	resultIdx := 0
	events := convertStreamEvent(&types.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &resultIdx,
		ContentBlock: &types.AnthropicContentBlock{
			Type:      "web_search_tool_result",
			ToolUseID: "unknown_id",
		},
	}, "claude-test", state)
	if events != nil {
		t.Fatalf("web_search_tool_result without known call emitted events: %v", events)
	}
}

func TestHandleNonStreamFailedResponseParse(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("not json")),
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestHandleStreamNilDelta(t *testing.T) {
	h := New("https://anthropic.example", "key", "claude-test")
	h.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		lines := []string{
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n",
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n",
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n",
			"data: {\"type\":\"content_block_delta\",\"index\":0}\n",
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n",
			"data: {\"type\":\"message_delta\"}\n",
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Join(lines, ""))),
		}, nil
	})}
	body := []byte(`{"model":"claude-test","input":"test","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	if !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream did not complete: %q", rec.Body.String())
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
