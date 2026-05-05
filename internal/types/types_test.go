package types

import (
	"encoding/json"
	"testing"
)

func TestOpenAIResponseRequestMarshal(t *testing.T) {
	temp := 0.7
	req := OpenAIResponseRequest{
		Model:       "claude-test",
		Input:       "hello",
		Instructions: "be helpful",
		MaxTokens:   4096,
		Temperature: &temp,
		Stream:      true,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var back OpenAIResponseRequest
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if back.Model != "claude-test" {
		t.Fatalf("Model = %q, want claude-test", back.Model)
	}
	if back.Instructions != "be helpful" {
		t.Fatalf("Instructions = %q", back.Instructions)
	}
}

func TestAnthropicMessageRequestMarshal(t *testing.T) {
	temp := 0.5
	req := AnthropicMessageRequest{
		Model:     "claude-test",
		MaxTokens: 4096,
		Temperature: &temp,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "hello"},
		},
		CustomTools:     map[string]bool{"patch": true},
		ApplyPatchTools: map[string]bool{"apply_patch": true},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var back AnthropicMessageRequest
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if back.CustomTools != nil {
		t.Fatalf("CustomTools should not be serialized: %v", back.CustomTools)
	}
}

func TestToolMarshal(t *testing.T) {
	maxUses := 3
	tool := Tool{
		Type:           "web_search",
		MaxUses:        &maxUses,
		AllowedDomains: []string{"example.com"},
	}
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var back Tool
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if back.Type != "web_search" {
		t.Fatalf("Type = %q", back.Type)
	}
	if *back.MaxUses != 3 {
		t.Fatalf("MaxUses = %v", *back.MaxUses)
	}
}

func TestWebSearchFiltersMarshal(t *testing.T) {
	filters := WebSearchFilters{
		AllowedDomains: []string{"a.com"},
		BlockedDomains: []string{"b.com"},
	}
	data, err := json.Marshal(filters)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var back WebSearchFilters
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(back.AllowedDomains) != 1 || back.AllowedDomains[0] != "a.com" {
		t.Fatalf("AllowedDomains = %v", back.AllowedDomains)
	}
}

func TestOpenAIResponseMarshal(t *testing.T) {
	resp := OpenAIResponse{
		ID:     "resp_1",
		Object: "response",
		Status: "completed",
		Model:  "claude-test",
		Error: &APIError{
			Type:    "api_error",
			Code:    "500",
			Message: "boom",
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid JSON")
	}
}

func TestAnthropicStreamEventMarshal(t *testing.T) {
	idx := 0
	event := AnthropicStreamEvent{
		Type:  "message_start",
		Index: &idx,
		Message: &AnthropicMessageResponse{
			ID: "msg_1",
		},
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var back AnthropicStreamEvent
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if back.Type != "message_start" {
		t.Fatalf("Type = %q", back.Type)
	}
}

func TestOutputItemMarshal(t *testing.T) {
	item := OutputItem{
		ID:     "item_1",
		Type:   "function_call",
		Status: "completed",
		Name:   "lookup",
		CallID: "call_1",
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid JSON: %s", data)
	}
}

func TestAnthropicContentBlockMarshal(t *testing.T) {
	block := AnthropicContentBlock{
		Type: "text",
		Text: "hello",
		Citations: []AnthropicCitation{
			{Type: "char_location", URL: "https://example.com", Title: "Example"},
		},
	}
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid JSON: %s", data)
	}
}
