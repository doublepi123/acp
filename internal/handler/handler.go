package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lcy/anthropic-openai-proxy/internal/translate"
	"github.com/lcy/anthropic-openai-proxy/internal/types"
)

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

	body, err := io.ReadAll(r.Body)
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
		h.handleStream(w, anthropicReq, openaiReq.Model)
	} else {
		h.handleNonStream(w, anthropicReq, openaiReq.Model)
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

func (h *Handler) handleNonStream(w http.ResponseWriter, anthropicReq *types.AnthropicMessageRequest, model string) {
	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request")
		return
	}

	req, err := http.NewRequest(http.MethodPost, h.AnthropicURL+"/v1/messages", bytes.NewReader(reqBody))
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
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

func (h *Handler) handleStream(w http.ResponseWriter, anthropicReq *types.AnthropicMessageRequest, model string) {
	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request")
		return
	}

	req, err := http.NewRequest(http.MethodPost, h.AnthropicURL+"/v1/messages", bytes.NewReader(reqBody))
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Streaming not supported")
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	msgID := fmt.Sprintf("resp_%d", time.Now().UnixNano())

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

		sseEvent := convertStreamEvent(&event, model, msgID)
		if sseEvent == "" {
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", sseEvent)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error: %v", err)
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func convertStreamEvent(event *types.AnthropicStreamEvent, model, msgID string) string {
	switch event.Type {
	case "message_start":
		resp := map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":         event.Message.ID,
				"object":     "response",
				"created_at": time.Now().Unix(),
				"status":     "in_progress",
				"model":      model,
				"output":     []any{},
			},
		}
		b, _ := json.Marshal(resp)
		return string(b)

	case "content_block_start":
		if event.ContentBlock == nil {
			return ""
		}
		switch event.ContentBlock.Type {
		case "text":
			resp := map[string]any{
				"type":         "response.output_item.added",
				"output_index": event.Index,
				"item": map[string]any{
					"id":      fmt.Sprintf("%s_msg_%d", msgID, *event.Index),
					"type":    "message",
					"role":    "assistant",
					"status":  "in_progress",
					"content": []any{},
				},
			}
			b, _ := json.Marshal(resp)
			return string(b)
		case "tool_use":
			resp := map[string]any{
				"type":         "response.output_item.added",
				"output_index": event.Index,
				"item": map[string]any{
					"id":      fmt.Sprintf("%s_call_%d", msgID, *event.Index),
					"type":    "function_call",
					"name":    event.ContentBlock.Name,
					"call_id": event.ContentBlock.ID,
					"status":  "in_progress",
				},
			}
			b, _ := json.Marshal(resp)
			return string(b)
		}

	case "content_block_delta":
		if event.Delta == nil {
			return ""
		}
		switch event.Delta.Type {
		case "text_delta":
			resp := map[string]any{
				"type":          "response.output_text.delta",
				"output_index":  event.Index,
				"content_index": 0,
				"delta":         event.Delta.Text,
			}
			b, _ := json.Marshal(resp)
			return string(b)
		case "input_json_delta":
			resp := map[string]any{
				"type":         "response.function_call_arguments.delta",
				"output_index": event.Index,
				"delta":        event.Delta.PartialJSON,
			}
			b, _ := json.Marshal(resp)
			return string(b)
		}

	case "content_block_stop":
		resp := map[string]any{
			"type":         "response.output_item.done",
			"output_index": event.Index,
			"item": map[string]any{
				"status": "completed",
			},
		}
		b, _ := json.Marshal(resp)
		return string(b)

	case "message_delta":
		resp := map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     msgID,
				"object": "response",
				"status": "completed",
			},
		}
		b, _ := json.Marshal(resp)
		return string(b)

	case "message_stop":
		return ""

	case "error":
		resp := map[string]any{
			"type":  "error",
			"error": event.Error,
		}
		b, _ := json.Marshal(resp)
		return string(b)
	}

	return ""
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

// HandleHealth handles health check.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
