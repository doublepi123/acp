package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lcy/anthropic-openai-proxy/internal/translate"
	"github.com/lcy/anthropic-openai-proxy/internal/types"
)

const maxRequestBodyBytes = 64 << 20

// Handler handles HTTP requests, proxying OpenAI Response API to Anthropic.
type Handler struct {
	AnthropicURL string
	AnthropicKey string
	DefaultModel string
	HTTPClient   *http.Client
}

// New creates a new Handler.
func New(anthropicURL, anthropicKey, defaultModel string) *Handler {
	return &Handler{
		AnthropicURL: anthropicURL,
		AnthropicKey: anthropicKey,
		DefaultModel: defaultModel,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// HandleResponses handles POST /v1/responses
func (h *Handler) HandleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	var openaiReq types.OpenAIResponseRequest
	if err := json.Unmarshal(body, &openaiReq); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	openaiReq.Model = h.resolveModel(openaiReq.Model)

	log.Printf("Received request: model=%s stream=%v", openaiReq.Model, openaiReq.Stream)

	anthropicReq, err := translate.ToAnthropicRequest(&openaiReq)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("translation error: %v", err))
		return
	}

	if openaiReq.Stream {
		h.handleStream(r.Context(), w, anthropicReq, openaiReq.Model)
	} else {
		h.handleNonStream(r.Context(), w, anthropicReq, openaiReq.Model)
	}
}

func (h *Handler) resolveModel(model string) string {
	if h.DefaultModel == "" {
		return model
	}
	if model == "" || model == "codex-auto-review" {
		return h.DefaultModel
	}
	return model
}

func (h *Handler) messagesURL() string {
	return strings.TrimRight(h.AnthropicURL, "/") + "/v1/messages"
}

func (h *Handler) handleNonStream(ctx context.Context, w http.ResponseWriter, anthropicReq *types.AnthropicMessageRequest, model string) {
	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.messagesURL(), bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", h.AnthropicKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		log.Printf("Anthropic request failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("anthropic request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read anthropic response")
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Anthropic error: %s", string(respBody))
		writeAnthropicError(w, resp.StatusCode, respBody)
		return
	}

	var anthropicResp types.AnthropicMessageResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse anthropic response")
		return
	}

	openaiResp := translate.ToOpenAIResponse(&anthropicResp, model)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(openaiResp)
}

func (h *Handler) handleStream(ctx context.Context, w http.ResponseWriter, anthropicReq *types.AnthropicMessageRequest, model string) {
	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.messagesURL(), bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", h.AnthropicKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		log.Printf("Anthropic streaming request failed: %v", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("anthropic request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Anthropic error: %s", string(respBody))
		writeAnthropicError(w, resp.StatusCode, respBody)
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Streaming not supported")
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	state := newStreamState()

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var event types.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Printf("Failed to parse stream event: %v", err)
			continue
		}

		for _, sseEvent := range convertStreamEvent(&event, model, state) {
			if sseEvent == "" {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", sseEvent)
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error: %v", err)
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

type streamState struct {
	responseID string
	createdAt  int64
	seq        int64
	blockTypes map[int]string
	blockNames map[int]string
	blockIDs   map[int]string
	callIDs    map[int]string
	callIndex  map[string]int
	text       map[int]string
	args       map[int]string
	citations  map[int]int
	output     []any
}

func newStreamState() *streamState {
	now := time.Now()
	fallbackID := fmt.Sprintf("resp_%d", now.UnixNano())
	return &streamState{
		responseID: fallbackID,
		createdAt:  now.Unix(),
		blockTypes: make(map[int]string),
		blockNames: make(map[int]string),
		blockIDs:   make(map[int]string),
		callIDs:    make(map[int]string),
		callIndex:  make(map[string]int),
		text:       make(map[int]string),
		args:       make(map[int]string),
		citations:  make(map[int]int),
		output:     []any{},
	}
}

func (s *streamState) itemID(idx int, kind string) string {
	if id := s.blockIDs[idx]; id != "" {
		return id
	}
	id := fmt.Sprintf("%s_%s_%d", s.responseID, kind, idx)
	s.blockIDs[idx] = id
	return id
}

func indexValue(index *int) int {
	if index == nil {
		return 0
	}
	return *index
}

func (s *streamState) event(fields map[string]any) string {
	fields["sequence_number"] = s.seq
	s.seq++
	b, _ := json.Marshal(fields)
	return string(b)
}

func streamWebSearchAction(args string) map[string]any {
	action := map[string]any{"type": "search"}
	var input map[string]any
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return action
	}
	if query, _ := input["query"].(string); query != "" {
		action["query"] = query
		action["queries"] = []string{query}
	}
	return action
}

func convertStreamEvent(event *types.AnthropicStreamEvent, model string, state *streamState) []string {
	switch event.Type {
	case "message_start":
		if event.Message != nil && event.Message.ID != "" {
			state.responseID = event.Message.ID
		}
		resp := map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":         state.responseID,
				"object":     "response",
				"created_at": state.createdAt,
				"status":     "in_progress",
				"model":      model,
				"output":     []any{},
			},
		}
		return []string{state.event(resp)}

	case "content_block_start":
		if event.ContentBlock == nil || event.Index == nil {
			return nil
		}
		idx := *event.Index
		state.blockTypes[idx] = event.ContentBlock.Type
		state.blockNames[idx] = event.ContentBlock.Name

		switch event.ContentBlock.Type {
		case "text":
			itemID := state.itemID(idx, "msg")
			resp := map[string]any{
				"type":         "response.output_item.added",
				"response_id":  state.responseID,
				"output_index": event.Index,
				"item": map[string]any{
					"id":      itemID,
					"type":    "message",
					"role":    "assistant",
					"status":  "in_progress",
					"content": []any{},
				},
			}
			return []string{state.event(resp)}
		case "tool_use":
			itemID := state.itemID(idx, "call")
			state.callIDs[idx] = event.ContentBlock.ID
			state.callIndex[event.ContentBlock.ID] = idx
			resp := map[string]any{
				"type":         "response.output_item.added",
				"response_id":  state.responseID,
				"output_index": event.Index,
				"item": map[string]any{
					"id":        itemID,
					"type":      "function_call",
					"name":      event.ContentBlock.Name,
					"call_id":   event.ContentBlock.ID,
					"arguments": "",
					"status":    "in_progress",
				},
			}
			return []string{state.event(resp)}
		case "server_tool_use":
			itemID := state.itemID(idx, "web_search")
			if event.ContentBlock.ID != "" {
				itemID = event.ContentBlock.ID
				state.blockIDs[idx] = itemID
			}
			state.callIDs[idx] = event.ContentBlock.ID
			state.callIndex[event.ContentBlock.ID] = idx
			resp := map[string]any{
				"type":         "response.output_item.added",
				"response_id":  state.responseID,
				"output_index": event.Index,
				"item": map[string]any{
					"id":     itemID,
					"type":   "web_search_call",
					"status": "in_progress",
					"action": map[string]any{"type": "search"},
				},
			}
			return []string{state.event(resp)}
		case "web_search_tool_result", "web_search_results":
			toolUseID := event.ContentBlock.ToolUseID
			searchIdx, ok := state.callIndex[toolUseID]
			if !ok {
				return nil
			}
			itemID := state.itemID(searchIdx, "web_search")
			item := map[string]any{
				"id":     itemID,
				"type":   "web_search_call",
				"status": "completed",
				"action": streamWebSearchAction(state.args[searchIdx]),
			}
			state.output = append(state.output, item)
			return []string{
				state.event(map[string]any{
					"type":         "response.web_search_call.completed",
					"response_id":  state.responseID,
					"output_index": searchIdx,
					"item_id":      itemID,
				}),
				state.event(map[string]any{
					"type":         "response.output_item.done",
					"response_id":  state.responseID,
					"output_index": searchIdx,
					"item":         item,
				}),
			}
		}

	case "content_block_delta":
		if event.Delta == nil {
			return nil
		}
		idx := indexValue(event.Index)
		switch event.Delta.Type {
		case "text_delta":
			state.text[idx] += event.Delta.Text
			resp := map[string]any{
				"type":          "response.output_text.delta",
				"response_id":   state.responseID,
				"output_index":  event.Index,
				"item_id":       state.itemID(idx, "msg"),
				"content_index": 0,
				"delta":         event.Delta.Text,
			}
			return []string{state.event(resp)}
		case "input_json_delta":
			state.args[idx] += event.Delta.PartialJSON
			if state.blockTypes[idx] == "server_tool_use" {
				return nil
			}
			resp := map[string]any{
				"type":         "response.function_call_arguments.delta",
				"response_id":  state.responseID,
				"output_index": event.Index,
				"item_id":      state.itemID(idx, "call"),
				"delta":        event.Delta.PartialJSON,
			}
			return []string{state.event(resp)}
		case "citations_delta":
			if event.Delta.Citation == nil {
				return nil
			}
			citation := event.Delta.Citation
			url := citation.URL
			if url == "" {
				url = citation.Source
			}
			if url == "" {
				return nil
			}
			annotationIndex := state.citations[idx]
			state.citations[idx]++
			resp := map[string]any{
				"type":             "response.output_text.annotation.added",
				"response_id":      state.responseID,
				"output_index":     event.Index,
				"item_id":          state.itemID(idx, "msg"),
				"content_index":    0,
				"annotation_index": annotationIndex,
				"annotation": map[string]any{
					"type":        "url_citation",
					"start_index": 0,
					"end_index":   utf8.RuneCountInString(state.text[idx]),
					"url":         url,
					"title":       citation.Title,
				},
			}
			return []string{state.event(resp)}
		}

	case "content_block_stop":
		if event.Index == nil {
			return nil
		}
		idx := *event.Index
		itemID := state.itemID(idx, state.blockTypes[idx])
		item := map[string]any{
			"id":     itemID,
			"status": "completed",
		}
		var events []string
		switch state.blockTypes[idx] {
		case "text":
			item["type"] = "message"
			item["role"] = "assistant"
			item["content"] = []map[string]any{
				{
					"type": "output_text",
					"text": state.text[idx],
				},
			}
			state.output = append(state.output, item)
		case "tool_use":
			item["type"] = "function_call"
			item["name"] = state.blockNames[idx]
			item["call_id"] = state.callIDs[idx]
			item["arguments"] = state.args[idx]
			state.output = append(state.output, item)
			done := map[string]any{
				"type":         "response.function_call_arguments.done",
				"response_id":  state.responseID,
				"output_index": event.Index,
				"item_id":      itemID,
				"call_id":      state.callIDs[idx],
				"name":         state.blockNames[idx],
				"arguments":    state.args[idx],
			}
			events = append(events, state.event(done))
		case "server_tool_use":
			searching := map[string]any{
				"type":         "response.web_search_call.searching",
				"response_id":  state.responseID,
				"output_index": event.Index,
				"item_id":      itemID,
			}
			return []string{state.event(searching)}
		case "web_search_tool_result", "web_search_results":
			return nil
		}
		resp := map[string]any{
			"type":         "response.output_item.done",
			"response_id":  state.responseID,
			"output_index": event.Index,
			"item":         item,
		}
		events = append(events, state.event(resp))
		return events

	case "message_delta":
		resp := map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     state.responseID,
				"object": "response",
				"status": "completed",
				"model":  model,
				"output": state.output,
			},
		}
		return []string{state.event(resp)}

	case "message_stop":
		return nil

	case "error":
		resp := map[string]any{
			"type":  "error",
			"error": event.Error,
		}
		return []string{state.event(resp)}
	}

	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(types.OpenAIResponse{
		Status: "failed",
		Error: &types.APIError{
			Type:    "api_error",
			Code:    fmt.Sprintf("%d", status),
			Message: message,
		},
	})
}

func writeAnthropicError(w http.ResponseWriter, status int, body []byte) {
	var errResp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		writeError(w, status, errResp.Error.Message)
		return
	}
	writeError(w, status, string(body))
}

// HandleHealth handles health check.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
