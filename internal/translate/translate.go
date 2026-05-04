package translate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lcy/anthropic-openai-proxy/internal/types"
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

	// Convert tools
	if len(openaiReq.Tools) > 0 {
		anthropicReq.Tools = make([]types.AnthropicTool, 0, len(openaiReq.Tools))
		for _, t := range openaiReq.Tools {
			switch t.Type {
			case "function", "":
				name := t.Name
				description := t.Description
				parameters := t.Parameters
				if t.Function != nil {
					if name == "" {
						name = t.Function.Name
					}
					if description == "" {
						description = t.Function.Description
					}
					if parameters == nil {
						parameters = t.Function.Parameters
					}
				}
				if name == "" {
					return nil, fmt.Errorf("function tool missing name")
				}
				anthropicReq.Tools = append(anthropicReq.Tools, types.AnthropicTool{
					Name:        name,
					Description: description,
					InputSchema: parameters,
				})
			case "web_search", "web_search_preview", "web_search_preview_2025_03_11":
				anthropicReq.Tools = append(anthropicReq.Tools, types.AnthropicTool{
					Type:           "web_search_20250305",
					Name:           "web_search",
					MaxUses:        t.MaxUses,
					AllowedDomains: t.AllowedDomains,
					BlockedDomains: t.BlockedDomains,
					UserLocation:   t.UserLocation,
				})
			}
		}
	}

	// Convert tool_choice
	if openaiReq.ToolChoice != nil {
		anthropicReq.ToolChoice = convertToolChoice(openaiReq.ToolChoice)
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

func convertToolChoice(tc any) any {
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]string{"type": "auto"}
		case "none":
			return map[string]string{"type": "none"}
		case "required":
			return map[string]string{"type": "any"}
		default:
			return map[string]any{"type": "tool", "name": v}
		}
	case map[string]any:
		if t, ok := v["type"]; ok {
			switch t {
			case "function":
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
					messages = append(messages, *anthropicMsg)
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
	case "reasoning":
		return nil
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

	return &types.AnthropicMessage{
		Role:    role,
		Content: content,
	}
}

func convertFunctionCallMessage(msg types.InputMessage) *types.AnthropicMessage {
	callID := msg.CallID
	if callID == "" {
		callID = msg.ID
	}

	input := parseJSONOrString(msg.Arguments)
	if msg.Arguments == "" && msg.Content != nil {
		input = parseJSONOrString(contentToString(msg.Content))
	}
	if msg.Arguments == "" && msg.Content == nil {
		input = map[string]any{}
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

func convertFunctionCallOutputMessage(msg types.InputMessage) *types.AnthropicMessage {
	callID := msg.CallID
	if callID == "" {
		callID = msg.ToolID
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
	if text, ok := m["text"].(string); ok {
		return text
	}
	return fmt.Sprint(m["text"])
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
		return s
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
		annotations = append(annotations, map[string]any{
			"type":        "url_citation",
			"start_index": startIndex,
			"end_index":   startIndex + utf8.RuneCountInString(block.Text),
			"url":         url,
			"title":       c.Title,
		})
	}
	return annotations
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

// ToOpenAIResponse converts an Anthropic response to an OpenAI Response API response.
func ToOpenAIResponse(anthropicResp *types.AnthropicMessageResponse, model string) *types.OpenAIResponse {
	output := make([]types.OutputItem, 0)

	textContent := ""
	webSearchCalls := make([]types.OutputItem, 0)
	functionCalls := make([]types.OutputItem, 0)
	var annotations []map[string]any

	for i, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			startIndex := utf8.RuneCountInString(textContent)
			textContent += block.Text
			annotations = append(annotations, blockCitations(block, startIndex)...)
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			functionCalls = append(functionCalls, types.OutputItem{
				ID:        fmt.Sprintf("%s_%d", block.ID, i),
				Type:      "function_call",
				Status:    "completed",
				Name:      block.Name,
				Arguments: string(args),
				CallID:    block.ID,
			})
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

	output = append(output, webSearchCalls...)

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

	return &types.OpenAIResponse{
		ID:        anthropicResp.ID,
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Status:    "completed",
		Model:     model,
		Output:    output,
		Usage: &types.Usage{
			InputTokens:  anthropicResp.Usage.InputTokens,
			OutputTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:  anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}
}
