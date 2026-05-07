package translate

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/doublepi123/acp/internal/types"
)

// AnthropicWebSearchToolType is the Anthropic web search tool type version.
const AnthropicWebSearchToolType = "web_search_20250305"

var (
	anthropicUserEmailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	anthropicUserPhonePattern = regexp.MustCompile(`^\+?[\d\s()\-]{7,}$`)
)

type toolKind int

const (
	toolKindFunction toolKind = iota
	toolKindCustom
	toolKindApplyPatch
)

func convertTools(openaiTools []types.Tool) ([]types.AnthropicTool, map[string]bool, map[string]bool, error) {
	anthropicTools := make([]types.AnthropicTool, 0, len(openaiTools))
	customTools := make(map[string]bool)
	applyPatchTools := make(map[string]bool)

	for _, openaiTool := range openaiTools {
		anthropicTool, kind, err := convertTool(openaiTool)
		if err != nil {
			return nil, nil, nil, err
		}
		anthropicTools = append(anthropicTools, anthropicTool)

		switch kind {
		case toolKindCustom:
			customTools[anthropicTool.Name] = true
		case toolKindApplyPatch:
			applyPatchTools[anthropicTool.Name] = true
		}
	}

	if len(customTools) == 0 {
		customTools = nil
	}
	if len(applyPatchTools) == 0 {
		applyPatchTools = nil
	}
	return anthropicTools, customTools, applyPatchTools, nil
}

func convertTool(t types.Tool) (types.AnthropicTool, toolKind, error) {
	switch t.Type {
	case "function", "":
		name, description, parameters := functionToolFields(t)
		if name == "" {
			return types.AnthropicTool{}, toolKindFunction, fmt.Errorf("function tool missing name")
		}
		return types.AnthropicTool{
			Name:        name,
			Description: description,
			InputSchema: normalizeAnthropicInputSchema(parameters),
		}, toolKindFunction, nil
	case "custom":
		name := t.Name
		description := t.Description
		if t.Function != nil {
			if name == "" {
				name = t.Function.Name
			}
			if description == "" {
				description = t.Function.Description
			}
		}
		if name == "" {
			return types.AnthropicTool{}, toolKindCustom, fmt.Errorf("custom tool missing name")
		}
		return types.AnthropicTool{
			Name:        name,
			Description: customToolDescription(description, t.Format),
			InputSchema: customToolInputSchema(),
		}, toolKindCustom, nil
	case "apply_patch":
		return types.AnthropicTool{
			Name:        "apply_patch",
			Description: applyPatchToolDescription(),
			InputSchema: applyPatchToolInputSchema(),
		}, toolKindApplyPatch, nil
	case "web_search", "web_search_preview", "web_search_preview_2025_03_11":
		return webSearchTool(t), toolKindFunction, nil
	default:
		return types.AnthropicTool{}, toolKindFunction, fmt.Errorf("unknown tool type: %q", t.Type)
	}
}

func functionToolFields(t types.Tool) (string, string, any) {
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
	return name, description, parameters
}

func webSearchTool(t types.Tool) types.AnthropicTool {
	allowedDomains := t.AllowedDomains
	blockedDomains := t.BlockedDomains
	if t.Filters != nil {
		if len(t.Filters.AllowedDomains) > 0 {
			allowedDomains = t.Filters.AllowedDomains
		}
		if len(t.Filters.BlockedDomains) > 0 {
			blockedDomains = t.Filters.BlockedDomains
		}
	}
	maxUses := t.MaxUses
	if maxUses == nil {
		maxUses = webSearchMaxUses(t.SearchContextSize)
	}
	return types.AnthropicTool{
		Type:           AnthropicWebSearchToolType,
		Name:           "web_search",
		MaxUses:        maxUses,
		AllowedDomains: allowedDomains,
		BlockedDomains: blockedDomains,
		UserLocation:   t.UserLocation,
	}
}

func webSearchMaxUses(searchContextSize string) *int {
	maxUsesByContext := map[string]int{
		"low":    1,
		"medium": 5,
		"high":   10,
	}
	maxUses, ok := maxUsesByContext[searchContextSize]
	if !ok {
		return nil
	}
	return &maxUses
}

func normalizeAnthropicInputSchema(schema any) map[string]any {
	raw, ok := mapStringAny(schema)
	if !ok || len(raw) == 0 {
		return defaultAnthropicInputSchema()
	}

	if rawType, _ := raw["type"].(string); rawType != "object" {
		raw["type"] = "object"
		if _, ok := raw["properties"]; !ok {
			raw["properties"] = map[string]any{}
		}
	}

	allowed := map[string]bool{
		"type":                 true,
		"properties":           true,
		"additionalProperties": true,
		"required":             true,
		"$defs":                true,
		"strict":               true,
	}
	filtered := make(map[string]any, len(raw))
	for key, value := range raw {
		if allowed[key] {
			filtered[key] = value
		}
	}
	if _, ok := filtered["type"]; !ok {
		filtered["type"] = "object"
	}
	if _, ok := filtered["properties"]; !ok {
		filtered["properties"] = map[string]any{}
	}
	return filtered
}

func defaultAnthropicInputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func mapStringAny(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case nil:
		return nil, false
	case map[string]any:
		out := make(map[string]any, len(m))
		for key, value := range m {
			out[key] = value
		}
		return out, true
	case map[string]string:
		out := make(map[string]any, len(m))
		for key, value := range m {
			out[key] = value
		}
		return out, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}

func anthropicMetadata(user string, metadata map[string]any) map[string]any {
	if validAnthropicUserID(user) {
		return map[string]any{"user_id": user}
	}
	if metadata == nil {
		return nil
	}
	if userID, _ := metadata["user_id"].(string); validAnthropicUserID(userID) {
		return map[string]any{"user_id": userID}
	}
	return nil
}

func validAnthropicUserID(userID string) bool {
	if userID == "" {
		return false
	}
	if anthropicUserEmailPattern.MatchString(userID) {
		return false
	}
	return !anthropicUserPhonePattern.MatchString(userID)
}
