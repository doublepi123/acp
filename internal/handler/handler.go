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

	"github.com/doublepi123/acp/internal/translate"
	"github.com/doublepi123/acp/internal/types"
)

const maxRequestBodyBytes = 64 << 20

const anthropicVersion = "2023-06-01"

// Handler handles HTTP requests, proxying OpenAI Response API to Anthropic.
type Handler struct {
	AnthropicURL string
	AnthropicKey string
	DefaultModel string
	ProxyToken   string
	HTTPClient   *http.Client
}

// New creates a new Handler.
func New(anthropicURL, anthropicKey, defaultModel string) *Handler {
	return &Handler{
		AnthropicURL: anthropicURL,
		AnthropicKey: anthropicKey,
		DefaultModel: defaultModel,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:  10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}
}

// WithProxyToken sets an optional bearer token that clients must provide.
func (h *Handler) WithProxyToken(token string) *Handler {
	h.ProxyToken = token
	return h
}

// HandleResponses handles POST /v1/responses
func (h *Handler) HandleResponses(w http.ResponseWriter, r *http.Request) {
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized: invalid or missing bearer token")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

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

func setAnthropicHeaders(req *http.Request, key string, anthropicReq *types.AnthropicMessageRequest) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicVersion)

	var betas []string
	if anthropicReq.Thinking != nil {
		betas = append(betas, "extended-thinking-2025-04-11")
	}
	for _, t := range anthropicReq.Tools {
		if t.Type == translate.AnthropicWebSearchToolType {
			betas = append(betas, "web-search-2025-03-05")
			break
		}
	}
	if len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}
}

func (h *Handler) handleNonStream(ctx context.Context, w http.ResponseWriter, anthropicReq *types.AnthropicMessageRequest, model string) {
	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.messagesURL(), bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}

	setAnthropicHeaders(req, h.AnthropicKey, anthropicReq)

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

	openaiResp := translate.ToOpenAIResponse(&anthropicResp, model, anthropicReq.CustomTools, anthropicReq.ApplyPatchTools)

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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.messagesURL(), bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}

	setAnthropicHeaders(req, h.AnthropicKey, anthropicReq)
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported on this server")
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	state := newStreamState(anthropicReq.CustomTools, anthropicReq.ApplyPatchTools)

	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			data, ok = strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
		}
		var event types.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Printf("Failed to parse stream event: %v", err)
			continue
		}

		// Stop processing on error event from Anthropic
		if event.Type == "error" {
			code := "anthropic_error"
			message := "Anthropic stream error"
			if event.Error != nil {
				if event.Error.Type != "" {
					code = event.Error.Type
				}
				if event.Error.Message != "" {
					message = event.Error.Message
				}
			}
			if !writeStreamEvents(w, flusher, streamErrorEvents(state, model, code, message)) {
				return
			}
			break
		}

		if !writeStreamEvents(w, flusher, convertStreamEvent(&event, model, state)) {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error: %v", err)
		writeStreamEvents(w, flusher, streamErrorEvents(state, model, "stream_error", err.Error()))
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

type streamState struct {
	responseID      string
	createdAt       int64
	seq             int64
	blockTypes      map[int]string
	blockNames      map[int]string
	blockIDs        map[int]string
	callIDs         map[int]string
	callIndex       map[string]int
	text            map[int]string
	thinking        map[int]string
	signatures      map[int]string
	args            map[int]string
	citations       map[int]int
	citationTextLen map[int]int
	output          []any
	outputIndices   map[int]int
	nextOutputIdx   int
	stopReason      string
	inputTokens     int
	outputTokens    int
	customTools     map[string]bool
	applyPatchTools map[string]bool
}

func newStreamState(customTools, applyPatchTools map[string]bool) *streamState {
	now := time.Now()
	fallbackID := fmt.Sprintf("resp_%d", now.UnixNano())
	if customTools == nil {
		customTools = map[string]bool{}
	}
	if applyPatchTools == nil {
		applyPatchTools = map[string]bool{}
	}
	return &streamState{
		responseID:      fallbackID,
		createdAt:       now.Unix(),
		blockTypes:      make(map[int]string),
		blockNames:      make(map[int]string),
		blockIDs:        make(map[int]string),
		callIDs:         make(map[int]string),
		callIndex:       make(map[string]int),
		text:            make(map[int]string),
		thinking:        make(map[int]string),
		signatures:      make(map[int]string),
		args:            make(map[int]string),
		citations:       make(map[int]int),
		citationTextLen: make(map[int]int),
		output:          []any{},
		outputIndices:   make(map[int]int),
		customTools:     customTools,
		applyPatchTools: applyPatchTools,
	}
}

func (s *streamState) isCustomTool(name string) bool {
	return s.customTools[name]
}

func (s *streamState) isApplyPatchTool(name string) bool {
	return s.applyPatchTools[name]
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

func writeStreamEvents(w http.ResponseWriter, flusher http.Flusher, events []string) bool {
	for _, sseEvent := range events {
		if sseEvent == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", sseEvent); err != nil {
			log.Printf("Failed to write stream event: %v", err)
			return false
		}
		flusher.Flush()
	}
	return true
}

func streamErrorEvents(state *streamState, model, code, message string) []string {
	failed := map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id":         state.responseID,
			"object":     "response",
			"created_at": state.createdAt,
			"status":     "failed",
			"model":      model,
			"output":     state.output,
			"error": map[string]any{
				"code":    code,
				"message": message,
			},
			"incomplete_details": nil,
		},
	}
	errEvent := map[string]any{
		"type":    "error",
		"code":    code,
		"message": message,
		"param":   nil,
	}
	return []string{state.event(failed), state.event(errEvent)}
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

func streamCustomToolInput(args string) string {
	if args == "" {
		return ""
	}
	var input any
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return args
	}
	switch v := input.(type) {
	case map[string]any:
		if raw, ok := v["input"]; ok {
			return streamStringifyToolInput(raw)
		}
	case string:
		return v
	}
	return streamStringifyToolInput(input)
}

func streamStringifyToolInput(input any) string {
	switch v := input.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func streamApplyPatchOperation(args string) any {
	if args == "" {
		return map[string]any{}
	}
	var input any
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return map[string]any{"value": args}
	}
	switch v := input.(type) {
	case map[string]any:
		if op, ok := v["operation"]; ok {
			return op
		}
		if _, hasType := v["type"]; hasType {
			if _, hasPath := v["path"]; hasPath {
				return v
			}
		}
	}
	return input
}

func convertStreamEvent(event *types.AnthropicStreamEvent, model string, state *streamState) []string {
	switch event.Type {
	case "message_start":
		if event.Message != nil && event.Message.ID != "" {
			state.responseID = event.Message.ID
		}
		if event.Message != nil {
			state.inputTokens = event.Message.Usage.InputTokens
			state.outputTokens = event.Message.Usage.OutputTokens
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
		case "thinking", "redacted_thinking":
			state.outputIndices[idx] = state.nextOutputIdx
			state.nextOutputIdx++
			itemID := state.itemID(idx, "reasoning")
			if event.ContentBlock.Thinking != "" {
				state.thinking[idx] = event.ContentBlock.Thinking
			}
			if event.ContentBlock.Signature != "" {
				state.signatures[idx] = event.ContentBlock.Signature
			}
			if event.ContentBlock.Data != "" {
				state.signatures[idx] = event.ContentBlock.Data
			}
			resp := map[string]any{
				"type":         "response.output_item.added",
				"response_id":  state.responseID,
				"output_index": state.outputIndices[idx],
				"item": map[string]any{
					"id":      itemID,
					"type":    "reasoning",
					"status":  "in_progress",
					"summary": []any{},
				},
			}
			return []string{state.event(resp)}
		case "text":
			state.outputIndices[idx] = state.nextOutputIdx
			state.nextOutputIdx++
			itemID := state.itemID(idx, "msg")
			resp := map[string]any{
				"type":         "response.output_item.added",
				"response_id":  state.responseID,
				"output_index": state.outputIndices[idx],
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
			state.outputIndices[idx] = state.nextOutputIdx
			state.nextOutputIdx++
			itemID := state.itemID(idx, "call")
			state.callIDs[idx] = event.ContentBlock.ID
			state.callIndex[event.ContentBlock.ID] = idx
			item := map[string]any{
				"id":      itemID,
				"type":    "function_call",
				"name":    event.ContentBlock.Name,
				"call_id": event.ContentBlock.ID,
				"status":  "in_progress",
			}
			if state.isApplyPatchTool(event.ContentBlock.Name) {
				item["type"] = "apply_patch_call"
				delete(item, "name")
			} else if state.isCustomTool(event.ContentBlock.Name) {
				item["type"] = "custom_tool_call"
				item["input"] = ""
			} else {
				item["arguments"] = ""
			}
			resp := map[string]any{
				"type":         "response.output_item.added",
				"response_id":  state.responseID,
				"output_index": state.outputIndices[idx],
				"item":         item,
			}
			return []string{state.event(resp)}
		case "server_tool_use":
			state.outputIndices[idx] = state.nextOutputIdx
			state.nextOutputIdx++
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
				"output_index": state.outputIndices[idx],
				"item": map[string]any{
					"id":     itemID,
					"type":   "web_search_call",
					"status": "in_progress",
					"action": map[string]any{"type": "search"},
				},
			}
			inProgress := map[string]any{
				"type":         "response.web_search_call.in_progress",
				"response_id":  state.responseID,
				"output_index": state.outputIndices[idx],
				"item_id":      itemID,
			}
			return []string{state.event(resp), state.event(inProgress)}
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
			outIdx := state.outputIndices[searchIdx]
			return []string{
				state.event(map[string]any{
					"type":         "response.web_search_call.completed",
					"response_id":  state.responseID,
					"output_index": outIdx,
					"item_id":      itemID,
				}),
				state.event(map[string]any{
					"type":         "response.output_item.done",
					"response_id":  state.responseID,
					"output_index": outIdx,
					"item":         item,
				}),
			}
		default:
			// Assign an output index for unknown content block types to avoid
			// index misalignment with subsequent blocks.
			state.outputIndices[idx] = state.nextOutputIdx
			state.nextOutputIdx++
		}

	case "content_block_delta":
		if event.Delta == nil {
			return nil
		}
		idx := indexValue(event.Index)
		switch event.Delta.Type {
		case "thinking_delta":
			state.thinking[idx] += event.Delta.Thinking
			return nil
		case "signature_delta":
			state.signatures[idx] += event.Delta.Signature
			return nil
		case "text_delta":
			state.text[idx] += event.Delta.Text
			resp := map[string]any{
				"type":          "response.output_text.delta",
				"response_id":   state.responseID,
				"output_index":  state.outputIndices[idx],
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
			if state.isApplyPatchTool(state.blockNames[idx]) {
				return nil
			}
			if state.isCustomTool(state.blockNames[idx]) {
				return nil
			}
			resp := map[string]any{
				"type":         "response.function_call_arguments.delta",
				"response_id":  state.responseID,
				"output_index": state.outputIndices[idx],
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
			startIdx := 0
			if prevLen, ok := state.citationTextLen[idx]; ok {
				startIdx = prevLen
			}
			currentLen := utf8.RuneCountInString(state.text[idx])
			state.citationTextLen[idx] = currentLen
			state.citations[idx]++
			resp := map[string]any{
				"type":             "response.output_text.annotation.added",
				"response_id":      state.responseID,
				"output_index":     state.outputIndices[idx],
				"item_id":          state.itemID(idx, "msg"),
				"content_index":    0,
				"annotation_index": annotationIndex,
				"annotation": map[string]any{
					"type":        "url_citation",
					"start_index": startIdx,
					"end_index":   currentLen,
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
		case "thinking":
			item["type"] = "reasoning"
			item["content"] = []map[string]any{
				{
					"type": "reasoning_text",
					"text": state.thinking[idx],
				},
			}
			item["summary"] = []map[string]any{
				{
					"type": "summary_text",
					"text": state.thinking[idx],
				},
			}
			if state.signatures[idx] != "" {
				item["encrypted_content"] = state.signatures[idx]
			}
			state.output = append(state.output, item)
		case "redacted_thinking":
			item["type"] = "reasoning"
			if state.signatures[idx] != "" {
				item["encrypted_content"] = state.signatures[idx]
			}
			state.output = append(state.output, item)
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
			textDone := map[string]any{
				"type":          "response.output_text.done",
				"response_id":   state.responseID,
				"output_index":  state.outputIndices[idx],
				"item_id":       itemID,
				"content_index": 0,
				"text":          state.text[idx],
			}
			events = append(events, state.event(textDone))
		case "tool_use":
			if state.isApplyPatchTool(state.blockNames[idx]) {
				item["type"] = "apply_patch_call"
				item["call_id"] = state.callIDs[idx]
				item["operation"] = streamApplyPatchOperation(state.args[idx])
				state.output = append(state.output, item)
			} else if state.isCustomTool(state.blockNames[idx]) {
				input := streamCustomToolInput(state.args[idx])
				item["type"] = "custom_tool_call"
				item["name"] = state.blockNames[idx]
				item["call_id"] = state.callIDs[idx]
				item["input"] = input
				state.output = append(state.output, item)
				done := map[string]any{
					"type":         "response.custom_tool_call_input.done",
					"response_id":  state.responseID,
					"output_index": state.outputIndices[idx],
					"item_id":      itemID,
					"input":        input,
				}
				events = append(events, state.event(done))
			} else {
				item["type"] = "function_call"
				item["name"] = state.blockNames[idx]
				item["call_id"] = state.callIDs[idx]
				item["arguments"] = state.args[idx]
				state.output = append(state.output, item)
				done := map[string]any{
					"type":         "response.function_call_arguments.done",
					"response_id":  state.responseID,
					"output_index": state.outputIndices[idx],
					"item_id":      itemID,
					"call_id":      state.callIDs[idx],
					"name":         state.blockNames[idx],
					"arguments":    state.args[idx],
				}
				events = append(events, state.event(done))
			}
		case "server_tool_use":
			searching := map[string]any{
				"type":         "response.web_search_call.searching",
				"response_id":  state.responseID,
				"output_index": state.outputIndices[idx],
				"item_id":      itemID,
			}
			return []string{state.event(searching)}
		case "web_search_tool_result", "web_search_results":
			return nil
		}
		resp := map[string]any{
			"type":         "response.output_item.done",
			"response_id":  state.responseID,
			"output_index": state.outputIndices[idx],
			"item":         item,
		}
		events = append(events, state.event(resp))
		return events

	case "message_delta":
		if event.Delta != nil {
			state.stopReason = event.Delta.StopReason
		}
		if event.Usage != nil {
			state.outputTokens = event.Usage.OutputTokens
		}
		status := "completed"
		var incompleteDetails map[string]any
		switch state.stopReason {
		case "max_tokens":
			status = "incomplete"
			incompleteDetails = map[string]any{"reason": "max_output_tokens"}
		}
		resp := map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     state.responseID,
				"object": "response",
				"status": status,
				"model":  model,
				"output": state.output,
				"usage": map[string]any{
					"input_tokens":  state.inputTokens,
					"output_tokens": state.outputTokens,
					"total_tokens":  state.inputTokens + state.outputTokens,
				},
				"incomplete_details": incompleteDetails,
			},
		}
		return []string{state.event(resp)}

	case "message_stop":
		return nil
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
	if !h.checkAuth(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized: invalid or missing bearer token")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) checkAuth(r *http.Request) bool {
	if h.ProxyToken == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(auth, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return token == h.ProxyToken
}
