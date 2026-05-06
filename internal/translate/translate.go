package translate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doublepi123/acp/internal/types"
)

// ToAnthropicRequest converts an OpenAI Response API request to an Anthropic Messages request.
func ToAnthropicRequest(openaiReq *types.OpenAIResponseRequest) (*types.AnthropicMessageRequest, error) {
	anthropicReq := &types.AnthropicMessageRequest{
		Model:     openaiReq.Model,
		MaxTokens: openaiReq.MaxTokens,
		Stream:    openaiReq.Stream,
	}

	if anthropicReq.MaxTokens <= 0 {
		anthropicReq.MaxTokens = 4096
	}

	if openaiReq.Temperature != nil {
		anthropicReq.Temperature = openaiReq.Temperature
	}
	if openaiReq.TopP != nil {
		anthropicReq.TopP = openaiReq.TopP
	}
	if thinking := convertReasoningConfig(openaiReq.Reasoning, anthropicReq.MaxTokens); thinking != nil {
		anthropicReq.Thinking = thinking
		anthropicReq.Temperature = nil
	}
	if metadata := anthropicMetadata(openaiReq.User, openaiReq.Metadata); metadata != nil {
		anthropicReq.Metadata = metadata
	}

	// Convert tools
	if len(openaiReq.Tools) > 0 {
		tools, customTools, applyPatchTools, err := convertTools(openaiReq.Tools)
		if err != nil {
			return nil, err
		}
		anthropicReq.Tools = tools
		anthropicReq.CustomTools = customTools
		anthropicReq.ApplyPatchTools = applyPatchTools
	}

	// Convert tool_choice
	if openaiReq.ToolChoice != nil {
		anthropicReq.ToolChoice = convertToolChoice(openaiReq.ToolChoice)
		if anthropicReq.Thinking != nil && isForcedToolChoice(anthropicReq.ToolChoice) {
			anthropicReq.Thinking = nil
			anthropicReq.Temperature = openaiReq.Temperature
		}
	}
	if openaiReq.ParallelCalls != nil && !*openaiReq.ParallelCalls {
		anthropicReq.ToolChoice = withDisableParallelToolUse(anthropicReq.ToolChoice, len(anthropicReq.Tools) > 0)
	}

	// Convert input to messages + instructions (system)
	messages, system, err := convertInput(openaiReq.Input, openaiReq.Instructions)
	if err != nil {
		return nil, fmt.Errorf("converting input: %w", err)
	}

	anthropicReq.Messages = messages
	if system != "" {
		anthropicReq.System = system
	}

	return anthropicReq, nil
}

func convertReasoningConfig(reasoning any, maxTokens int) any {
	if reasoning == nil || maxTokens <= 1024 {
		return nil
	}
	if m, ok := reasoning.(map[string]any); ok {
		if effort, _ := m["effort"].(string); effort == "none" {
			return nil
		}
	}
	budget := maxTokens * 3 / 4
	if budget < 1024 {
		budget = 1024
	}
	return map[string]any{
		"type":          "enabled",
		"budget_tokens": budget,
	}
}

func customToolDescription(description string, format any) string {
	if format == nil {
		return description
	}
	b, err := json.Marshal(format)
	if err != nil || len(b) == 0 {
		return description
	}
	formatHint := "Custom tool input format: " + string(b)
	if description == "" {
		return formatHint
	}
	return description + "\n\n" + formatHint
}

func customToolInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "Free-form input for the custom tool.",
			},
		},
		"required":             []string{"input"},
		"additionalProperties": false,
	}
}

func applyPatchToolDescription() string {
	return "Apply exactly one structured file patch operation. Use create_file to create a file, update_file to modify a file, or delete_file to remove a file. For create_file and update_file, provide a V4A diff in operation.diff."
}

func applyPatchToolInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type": map[string]any{
						"type": "string",
						"enum": []string{"create_file", "update_file", "delete_file"},
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Repository-relative path to create, update, or delete.",
					},
					"diff": map[string]any{
						"type":        "string",
						"description": "V4A diff for create_file or update_file operations.",
					},
				},
				"required":             []string{"type", "path"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"operation"},
		"additionalProperties": false,
	}
}

func isForcedToolChoice(tc any) bool {
	switch v := tc.(type) {
	case map[string]string:
		return v["type"] == "any" || v["type"] == "tool"
	case map[string]any:
		t, _ := v["type"].(string)
		return t == "any" || t == "tool"
	}
	return false
}

func convertToolChoice(tc any) any {
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]any{"type": "auto"}
		case "none":
			return map[string]any{"type": "none"}
		case "required":
			return map[string]any{"type": "any"}
		default:
			return map[string]any{"type": "tool", "name": v}
		}
	case map[string]string:
		switch v["type"] {
		case "auto":
			return map[string]any{"type": "auto"}
		case "none":
			return map[string]any{"type": "none"}
		case "required", "any":
			return map[string]any{"type": "any"}
		case "tool", "function", "custom":
			if name := v["name"]; name != "" {
				return map[string]any{"type": "tool", "name": name}
			}
			if v["type"] == "tool" {
				return map[string]any{"type": "any"}
			}
		case "apply_patch":
			return map[string]any{"type": "tool", "name": "apply_patch"}
		}
	case map[string]any:
		if t, ok := v["type"]; ok {
			switch t {
			case "auto":
				return map[string]any{"type": "auto"}
			case "none":
				return map[string]any{"type": "none"}
			case "required", "any":
				return map[string]any{"type": "any"}
			case "tool":
				if name, ok := v["name"].(string); ok && name != "" {
					return map[string]any{"type": "tool", "name": name}
				}
				return map[string]any{"type": "any"}
			case "function", "custom", "apply_patch":
				if t == "apply_patch" {
					return map[string]any{"type": "tool", "name": "apply_patch"}
				}
				if name, ok := v["name"].(string); ok {
					return map[string]any{"type": "tool", "name": name}
				}
				fn, ok := v["function"].(map[string]any)
				if !ok {
					return nil
				}
				if name, ok := fn["name"].(string); ok {
					return map[string]any{"type": "tool", "name": name}
				}
			}
		}
	}
	return nil
}

func withDisableParallelToolUse(tc any, hasTools bool) any {
	if !hasTools {
		return tc
	}

	choice, ok := tc.(map[string]any)
	if !ok || choice == nil {
		return map[string]any{
			"type":                      "auto",
			"disable_parallel_tool_use": true,
		}
	}

	if choice["type"] == "none" {
		return choice
	}

	out := make(map[string]any, len(choice)+1)
	for k, v := range choice {
		out[k] = v
	}
	out["disable_parallel_tool_use"] = true
	return out
}

func convertInput(input any, instructions string) ([]types.AnthropicMessage, string, error) {
	var messages []types.AnthropicMessage
	system := instructions

	switch v := input.(type) {
	case string:
		messages = []types.AnthropicMessage{
			{Role: "user", Content: v},
		}
	case []any:
		raw, _ := json.Marshal(v)
		var inputMsgs []types.InputMessage
		if err := json.Unmarshal(raw, &inputMsgs); err != nil {
			return nil, "", fmt.Errorf("unmarshalling input messages: %w", err)
		}

		for _, msg := range inputMsgs {
			if msg.Role == "system" || msg.Role == "developer" {
				text := contentToString(msg.Content)
				if system == "" {
					system = text
				} else {
					system += "\n\n" + text
				}
			} else {
				anthropicMsg := convertInputMessage(msg)
				if anthropicMsg != nil {
					messages = appendOrMergeMessage(messages, *anthropicMsg)
				}
			}
		}
	default:
		raw, _ := json.Marshal(v)
		messages = []types.AnthropicMessage{
			{Role: "user", Content: string(raw)},
		}
	}

	return messages, system, nil
}

func convertInputMessage(msg types.InputMessage) *types.AnthropicMessage {
	switch msg.Type {
	case "function_call":
		return convertFunctionCallMessage(msg)
	case "function_call_output":
		return convertFunctionCallOutputMessage(msg)
	case "custom_tool_call":
		return convertCustomToolCallMessage(msg)
	case "custom_tool_call_output":
		return convertFunctionCallOutputMessage(msg)
	case "apply_patch_call":
		return convertApplyPatchCallMessage(msg)
	case "apply_patch_call_output":
		return convertFunctionCallOutputMessage(msg)
	case "reasoning":
		return convertReasoningMessage(msg)
	case "web_search_call":
		return nil
	}

	role := msg.Role
	switch role {
	case "assistant":
	case "user":
	case "tool":
	default:
		role = "user"
	}

	content := convertContent(msg.Content, msg.Calls)

	// Handle tool results
	if role == "tool" || msg.CallID != "" {
		callID := msg.CallID
		if callID == "" {
			callID = msg.ToolID
		}
		if callID == "" {
			return nil
		}
		return &types.AnthropicMessage{
			Role: "user",
			Content: []types.AnthropicContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: callID,
					Content:   content,
				},
			},
		}
	}

	if blocks, ok := content.([]types.AnthropicContentBlock); ok && len(blocks) == 0 {
		return nil
	}

	return &types.AnthropicMessage{
		Role:    role,
		Content: content,
	}
}

func appendOrMergeMessage(messages []types.AnthropicMessage, msg types.AnthropicMessage) []types.AnthropicMessage {
	if len(messages) == 0 {
		return append(messages, msg)
	}
	last := &messages[len(messages)-1]

	if last.Role == "assistant" && msg.Role == "assistant" {
		last.Content = appendContentBlocks(last.Content, msg.Content)
		return messages
	}

	if last.Role == "user" && msg.Role == "user" && hasToolResult(last.Content) {
		last.Content = appendContentBlocks(last.Content, msg.Content)
		return messages
	}

	return append(messages, msg)
}

func hasToolResult(content any) bool {
	blocks, ok := content.([]types.AnthropicContentBlock)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

func appendContentBlocks(left any, right any) []types.AnthropicContentBlock {
	blocks := contentBlocksFromAny(left)
	blocks = append(blocks, contentBlocksFromAny(right)...)
	return blocks
}

func convertReasoningMessage(msg types.InputMessage) *types.AnthropicMessage {
	content := reasoningText(msg.Content)
	if content == "" {
		content = reasoningText(msg.Summary)
	}

	if content == "" {
		return nil
	}

	block := types.AnthropicContentBlock{
		Type:      "thinking",
		Thinking:  content,
		Signature: msg.EncryptedContent,
	}

	return &types.AnthropicMessage{
		Role:    "assistant",
		Content: []types.AnthropicContentBlock{block},
	}
}

func reasoningText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if t != "reasoning_text" && t != "summary_text" {
				continue
			}
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func convertFunctionCallMessage(msg types.InputMessage) *types.AnthropicMessage {
	callID := msg.CallID
	if callID == "" {
		callID = msg.ID
	}
	if callID == "" {
		return nil
	}

	input := parseJSONOrString(msg.Arguments)
	if msg.Arguments == "" && msg.Content != nil {
		input = parseJSONOrString(contentToString(msg.Content))
	}
	if msg.Arguments == "" && msg.Content == nil {
		input = map[string]any{}
	}
	if _, ok := input.(map[string]any); !ok {
		input = map[string]any{"value": input}
	}

	return &types.AnthropicMessage{
		Role: "assistant",
		Content: []types.AnthropicContentBlock{
			{
				Type:  "tool_use",
				ID:    callID,
				Name:  msg.Name,
				Input: input,
			},
		},
	}
}

func convertCustomToolCallMessage(msg types.InputMessage) *types.AnthropicMessage {
	callID := msg.CallID
	if callID == "" {
		callID = msg.ID
	}
	if callID == "" {
		return nil
	}

	input := msg.Input
	if input == "" && msg.Arguments != "" {
		input = msg.Arguments
	}
	if input == "" && msg.Content != nil {
		input = contentToString(msg.Content)
	}

	return &types.AnthropicMessage{
		Role: "assistant",
		Content: []types.AnthropicContentBlock{
			{
				Type:  "tool_use",
				ID:    callID,
				Name:  msg.Name,
				Input: map[string]any{"input": input},
			},
		},
	}
}

func convertApplyPatchCallMessage(msg types.InputMessage) *types.AnthropicMessage {
	callID := msg.CallID
	if callID == "" {
		callID = msg.ID
	}
	if callID == "" {
		return nil
	}

	input := map[string]any{"operation": applyPatchOperation(msg.Operation)}
	if msg.Operation == nil {
		if msg.Input != "" {
			input["operation"] = parseJSONOrString(msg.Input)
		} else if msg.Arguments != "" {
			input["operation"] = parseJSONOrString(msg.Arguments)
		}
	}

	return &types.AnthropicMessage{
		Role: "assistant",
		Content: []types.AnthropicContentBlock{
			{
				Type:  "tool_use",
				ID:    callID,
				Name:  "apply_patch",
				Input: input,
			},
		},
	}
}

func convertFunctionCallOutputMessage(msg types.InputMessage) *types.AnthropicMessage {
	callID := msg.CallID
	if callID == "" {
		callID = msg.ToolID
	}
	if callID == "" {
		return nil
	}
	content := msg.Output
	if content == nil {
		content = msg.Content
	}

	return &types.AnthropicMessage{
		Role: "user",
		Content: []types.AnthropicContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: callID,
				Content:   convertToolResultContent(content),
			},
		},
	}
}

func convertContent(content any, calls []types.ToolCall) any {
	if calls != nil {
		blocks := make([]types.AnthropicContentBlock, 0, len(calls)+1)
		if content != nil {
			blocks = append(blocks, contentBlocksFromAny(content)...)
		}
		for _, c := range calls {
			name := c.Name
			arguments := c.Arguments
			if c.Function != nil {
				if name == "" {
					name = c.Function.Name
				}
				if arguments == "" {
					arguments = c.Function.Arguments
				}
			}
			if c.ID == "" {
				continue
			}
			blocks = append(blocks, types.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    c.ID,
				Name:  name,
				Input: parseJSONOrString(arguments),
			})
		}
		return blocks
	}

	switch v := content.(type) {
	case string:
		return v
	case []any:
		blocks := make([]types.AnthropicContentBlock, 0, len(v))
		for _, item := range v {
			block := convertContentBlock(item)
			if block != nil {
				blocks = append(blocks, *block)
			}
		}
		return blocks
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func contentBlocksFromAny(content any) []types.AnthropicContentBlock {
	if blocks, ok := content.([]types.AnthropicContentBlock); ok {
		return blocks
	}
	converted := convertContent(content, nil)
	switch v := converted.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []types.AnthropicContentBlock{{Type: "text", Text: v}}
	case []types.AnthropicContentBlock:
		return v
	default:
		return []types.AnthropicContentBlock{{Type: "text", Text: fmt.Sprint(v)}}
	}
}

func convertContentBlock(item any) *types.AnthropicContentBlock {
	m, ok := item.(map[string]any)
	if !ok {
		return &types.AnthropicContentBlock{Type: "text", Text: fmt.Sprint(item)}
	}

	t, _ := m["type"].(string)
	switch t {
	case "text", "input_text", "output_text":
		return &types.AnthropicContentBlock{Type: "text", Text: textValue(m)}
	case "thinking":
		return &types.AnthropicContentBlock{
			Type:      "thinking",
			Thinking:  textValueKey(m, "thinking"),
			Signature: textValueKey(m, "signature"),
		}
	case "redacted_thinking":
		return &types.AnthropicContentBlock{Type: "redacted_thinking", Data: textValueKey(m, "data")}
	case "image_url":
		return imageBlock(imageURLValue(m["image_url"]))
	case "input_image":
		return imageBlock(imageURLValue(m["image_url"]))
	case "image":
		mediaType, _ := m["media_type"].(string)
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		return &types.AnthropicContentBlock{
			Type: "image",
			Source: &types.AnthropicSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      fmt.Sprint(m["data"]),
			},
		}
	}
	return &types.AnthropicContentBlock{Type: "text", Text: fmt.Sprint(item)}
}

func textValue(m map[string]any) string {
	return textValueKey(m, "text")
}

func textValueKey(m map[string]any, key string) string {
	if text, ok := m["text"].(string); ok {
		if key == "text" {
			return text
		}
	}
	if text, ok := m[key].(string); ok {
		return text
	}
	if key == "text" {
		return fmt.Sprint(m["text"])
	}
	return ""
}

func imageURLValue(v any) string {
	switch img := v.(type) {
	case string:
		return img
	case map[string]any:
		url, _ := img["url"].(string)
		return url
	default:
		return ""
	}
}

func imageBlock(rawURL string) *types.AnthropicContentBlock {
	if rawURL == "" {
		return nil
	}
	if source := dataURLSource(rawURL); source != nil {
		return &types.AnthropicContentBlock{Type: "image", Source: source}
	}
	return &types.AnthropicContentBlock{
		Type: "image",
		Source: &types.AnthropicSource{
			Type: "url",
			URL:  rawURL,
		},
	}
}

func dataURLSource(rawURL string) *types.AnthropicSource {
	if !strings.HasPrefix(rawURL, "data:") {
		return nil
	}
	header, data, ok := strings.Cut(strings.TrimPrefix(rawURL, "data:"), ",")
	if !ok {
		return nil
	}
	mediaType, _, _ := strings.Cut(header, ";")
	if mediaType == "" {
		mediaType = "image/jpeg"
	}
	return &types.AnthropicSource{
		Type:      "base64",
		MediaType: mediaType,
		Data:      data,
	}
}

func convertToolResultContent(content any) any {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		return convertContent(v, nil)
	default:
		return contentToString(v)
	}
}

func parseJSONOrString(s string) any {
	if s == "" {
		return map[string]any{}
	}
	var input any
	if err := json.Unmarshal([]byte(s), &input); err != nil {
		return map[string]any{"value": s}
	}
	if _, ok := input.(map[string]any); !ok {
		return map[string]any{"value": input}
	}
	return input
}

func contentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			block := convertContentBlock(item)
			if block == nil {
				continue
			}
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func blockCitations(block types.AnthropicContentBlock, startIndex int) []map[string]any {
	var annotations []map[string]any
	for _, c := range block.Citations {
		url := c.URL
		if url == "" {
			url = c.Source
		}
		if url == "" {
			continue
		}
		cStart, cEnd := citationRange(block.Text, c.CitedText, startIndex)
		annotations = append(annotations, map[string]any{
			"type":        "url_citation",
			"start_index": cStart,
			"end_index":   cEnd,
			"url":         url,
			"title":       c.Title,
		})
	}
	return annotations
}

func citationRange(text, citedText string, startIndex int) (int, int) {
	if citedText == "" {
		return startIndex, startIndex + utf8.RuneCountInString(text)
	}
	pos := strings.Index(text, citedText)
	if pos < 0 {
		return startIndex, startIndex
	}
	prefixLen := utf8.RuneCountInString(text[:pos])
	citedLen := utf8.RuneCountInString(citedText)
	return startIndex + prefixLen, startIndex + prefixLen + citedLen
}

func webSearchAction(input any) any {
	query := ""
	if m, ok := input.(map[string]any); ok {
		query, _ = m["query"].(string)
	}
	action := map[string]any{"type": "search"}
	if query != "" {
		action["query"] = query
		action["queries"] = []string{query}
	}
	return action
}

func customToolInput(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case map[string]any:
		if raw, ok := v["input"]; ok {
			return stringifyToolInput(raw)
		}
	case map[string]string:
		if raw, ok := v["input"]; ok {
			return raw
		}
	}
	return stringifyToolInput(input)
}

func applyPatchOperation(input any) any {
	switch v := input.(type) {
	case nil:
		return map[string]any{}
	case string:
		return parseJSONOrString(v)
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

func stringifyToolInput(input any) string {
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

// ToOpenAIResponse converts an Anthropic response to an OpenAI Response API response.
func ToOpenAIResponse(anthropicResp *types.AnthropicMessageResponse, model string, customTools, applyPatchTools map[string]bool) *types.OpenAIResponse {
	output := make([]types.OutputItem, 0)
	if customTools == nil {
		customTools = map[string]bool{}
	}
	if applyPatchTools == nil {
		applyPatchTools = map[string]bool{}
	}

	textContent := ""
	webSearchCalls := make([]types.OutputItem, 0)
	reasoningItems := make([]types.OutputItem, 0)
	functionCalls := make([]types.OutputItem, 0)
	var annotations []map[string]any

	for i, block := range anthropicResp.Content {
		switch block.Type {
		case "thinking", "redacted_thinking":
			reasoningItems = append(reasoningItems, reasoningOutputItem(anthropicResp.ID, i, block))
		case "text":
			startIndex := utf8.RuneCountInString(textContent)
			textContent += block.Text
			annotations = append(annotations, blockCitations(block, startIndex)...)
		case "tool_use":
			if applyPatchTools[block.Name] {
				functionCalls = append(functionCalls, types.OutputItem{
					ID:        fmt.Sprintf("%s_call_%d", anthropicResp.ID, i),
					Type:      "apply_patch_call",
					Status:    "completed",
					CallID:    block.ID,
					Operation: applyPatchOperation(block.Input),
				})
			} else if customTools[block.Name] {
				functionCalls = append(functionCalls, types.OutputItem{
					ID:     fmt.Sprintf("%s_call_%d", anthropicResp.ID, i),
					Type:   "custom_tool_call",
					Status: "completed",
					Name:   block.Name,
					Input:  customToolInput(block.Input),
					CallID: block.ID,
				})
			} else {
				args, _ := json.Marshal(block.Input)
				functionCalls = append(functionCalls, types.OutputItem{
					ID:        fmt.Sprintf("%s_call_%d", anthropicResp.ID, i),
					Type:      "function_call",
					Status:    "completed",
					Name:      block.Name,
					Arguments: string(args),
					CallID:    block.ID,
				})
			}
		case "server_tool_use":
			if block.Name == "web_search" {
				webSearchCalls = append(webSearchCalls, types.OutputItem{
					ID:     block.ID,
					Type:   "web_search_call",
					Status: "completed",
					Action: webSearchAction(block.Input),
				})
			}
		case "web_search_tool_result", "web_search_results":
			// Search result blocks are internal to Anthropic. User-visible citations
			// are attached to subsequent text blocks and converted there.
		}
	}

	output = append(output, reasoningItems...)

	if textContent != "" {
		contentItem := map[string]any{
			"type": "output_text",
			"text": textContent,
		}
		if len(annotations) > 0 {
			contentItem["annotations"] = annotations
		}
		output = append(output, types.OutputItem{
			ID:      fmt.Sprintf("msg_%s", anthropicResp.ID),
			Type:    "message",
			Status:  "completed",
			Role:    "assistant",
			Content: []map[string]any{contentItem},
		})
	}

	output = append(output, functionCalls...)
	output = append(output, webSearchCalls...)

	status := "completed"
	var incompleteDetails *types.IncompleteDetails
	switch anthropicResp.StopReason {
	case "max_tokens":
		status = "incomplete"
		incompleteDetails = &types.IncompleteDetails{Reason: "max_output_tokens"}
	}

	return &types.OpenAIResponse{
		ID:        anthropicResp.ID,
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Status:    status,
		Model:     model,
		Output:    output,
		Usage: &types.Usage{
			InputTokens:  anthropicResp.Usage.InputTokens,
			OutputTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:  anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
		IncompleteDetails: incompleteDetails,
	}
}

func reasoningOutputItem(responseID string, index int, block types.AnthropicContentBlock) types.OutputItem {
	item := types.OutputItem{
		ID:     fmt.Sprintf("rs_%s_%d", responseID, index),
		Type:   "reasoning",
		Status: "completed",
	}
	switch block.Type {
	case "thinking":
		item.Content = []map[string]any{
			{
				"type": "reasoning_text",
				"text": block.Thinking,
			},
		}
		item.Summary = []map[string]any{
			{
				"type": "summary_text",
				"text": block.Thinking,
			},
		}
		item.EncryptedContent = block.Signature
	case "redacted_thinking":
		item.EncryptedContent = block.Data
	}
	return item
}
