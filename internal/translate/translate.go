package translate

import (
	"encoding/json"
	"fmt"
	"time"

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
			if t.Type == "function" || t.Type == "" {
				anthropicReq.Tools = append(anthropicReq.Tools, types.AnthropicTool{
					Name:        t.Name,
					Description: t.Description,
					InputSchema: t.Parameters,
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
				if name, ok := v["function"].(map[string]any)["name"].(string); ok {
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
	role := msg.Role
	switch role {
	case "assistant":
	case "user":
	default:
		role = "user"
	}

	content := convertContent(msg.Content, msg.Calls)

	// Handle tool results
	if role == "tool" || msg.CallID != "" {
		return &types.AnthropicMessage{
			Role: "user",
			Content: []types.AnthropicContentBlock{
				{
					Type:      "tool_result",
					ID:        msg.CallID,
					Input:     content,
				},
			},
		}
	}

	return &types.AnthropicMessage{
		Role:    role,
		Content: content,
	}
}

func convertContent(content any, calls []types.ToolCall) any {
	if calls != nil {
		blocks := make([]types.AnthropicContentBlock, 0, len(calls))
		for _, c := range calls {
			var input any
			if err := json.Unmarshal([]byte(c.Arguments), &input); err != nil {
				input = json.RawMessage(c.Arguments)
			}
			blocks = append(blocks, types.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    c.ID,
				Name:  c.Name,
				Input: input,
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

func convertContentBlock(item any) *types.AnthropicContentBlock {
	m, ok := item.(map[string]any)
	if !ok {
		return &types.AnthropicContentBlock{Type: "text", Text: fmt.Sprint(item)}
	}

	t, _ := m["type"].(string)
	switch t {
	case "text":
		return &types.AnthropicContentBlock{Type: "text", Text: fmt.Sprint(m["text"])}
	case "image_url":
		if img, ok := m["image_url"].(map[string]any); ok {
			url, _ := img["url"].(string)
			return &types.AnthropicContentBlock{
				Type: "image",
				Source: &types.AnthropicSource{
					Type:      "url",
					MediaType: "image/jpeg",
					Data:      url,
				},
			}
		}
	case "image":
		return &types.AnthropicContentBlock{
			Type: "image",
			Source: &types.AnthropicSource{
				Type:      "base64",
				MediaType: "image/jpeg",
				Data:      fmt.Sprint(m["data"]),
			},
		}
	}
	return &types.AnthropicContentBlock{Type: "text", Text: fmt.Sprint(item)}
}

func contentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// ToOpenAIResponse converts an Anthropic response to an OpenAI Response API response.
func ToOpenAIResponse(anthropicResp *types.AnthropicMessageResponse, model string) *types.OpenAIResponse {
	output := make([]types.OutputItem, 0)

	textContent := ""
	toolCalls := make([]types.OutputItem, 0)

	for i, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			textContent += block.Text
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, types.OutputItem{
				ID:        fmt.Sprintf("%s_%d", block.ID, i),
				Type:      "function_call",
				Status:    "completed",
				Name:      block.Name,
				Arguments: string(args),
				CallID:    block.ID,
			})
		}
	}

	if textContent != "" {
		output = append(output, types.OutputItem{
			ID:      fmt.Sprintf("msg_%s", anthropicResp.ID),
			Type:    "message",
			Status:  "completed",
			Role:    "assistant",
			Content: []map[string]string{{"type": "output_text", "text": textContent}},
		})
	}

	output = append(output, toolCalls...)

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
