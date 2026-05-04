package types

// OpenAIResponseRequest represents an OpenAI Response API request.
type OpenAIResponseRequest struct {
	Model         string         `json:"model"`
	Input         any            `json:"input"`
	Instructions  string         `json:"instructions,omitempty"`
	MaxTokens     int            `json:"max_output_tokens,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
	Tools         []Tool         `json:"tools,omitempty"`
	ToolChoice    any            `json:"tool_choice,omitempty"`
	Reasoning     any            `json:"reasoning,omitempty"`
	ParallelCalls *bool          `json:"parallel_tool_calls,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// InputMessage represents a message in the input field when it's an array.
type InputMessage struct {
	Type             string     `json:"type,omitempty"`
	ID               string     `json:"id,omitempty"`
	Role             string     `json:"role"`
	Content          any        `json:"content"`
	Summary          any        `json:"summary,omitempty"`
	EncryptedContent string     `json:"encrypted_content,omitempty"`
	Status           string     `json:"status,omitempty"`
	Name             string     `json:"name,omitempty"`
	Arguments        string     `json:"arguments,omitempty"`
	CallID           string     `json:"call_id,omitempty"`
	ToolID           string     `json:"tool_call_id,omitempty"`
	Output           any        `json:"output,omitempty"`
	Calls            []ToolCall `json:"tool_calls,omitempty"`
}

// Tool represents a tool definition.
type Tool struct {
	Type              string            `json:"type"`
	Name              string            `json:"name,omitempty"`
	Description       string            `json:"description,omitempty"`
	Parameters        any               `json:"parameters,omitempty"`
	Function          *FunctionTool     `json:"function,omitempty"`
	MaxUses           *int              `json:"max_uses,omitempty"`
	AllowedDomains    []string          `json:"allowed_domains,omitempty"`
	BlockedDomains    []string          `json:"blocked_domains,omitempty"`
	UserLocation      any               `json:"user_location,omitempty"`
	SearchContextSize string            `json:"search_context_size,omitempty"`
	Filters           *WebSearchFilters `json:"filters,omitempty"`
}

// WebSearchFilters represents nested filters for web search tools.
type WebSearchFilters struct {
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

// FunctionTool represents Chat Completions-style nested function tool details.
type FunctionTool struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      *bool  `json:"strict,omitempty"`
}

// ToolCall represents a tool call from the model.
type ToolCall struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	Function  *FunctionCallData `json:"function,omitempty"`
}

// FunctionCallData represents nested function call data.
type FunctionCallData struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// OpenAIResponse represents the OpenAI Response API response.
type OpenAIResponse struct {
	ID        string         `json:"id"`
	Object    string         `json:"object"`
	CreatedAt int64          `json:"created_at"`
	Status    string         `json:"status"`
	Model     string         `json:"model"`
	Output    []OutputItem   `json:"output"`
	Usage     *Usage         `json:"usage,omitempty"`
	Error     *APIError      `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// OutputItem represents an item in the response output array.
type OutputItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Status           string `json:"status,omitempty"`
	Role             string `json:"role,omitempty"`
	Content          any    `json:"content,omitempty"`
	Summary          any    `json:"summary,omitempty"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
	Action           any    `json:"action,omitempty"`
	Name             string `json:"name,omitempty"`
	CallID           string `json:"call_id,omitempty"`
	Arguments        string `json:"arguments,omitempty"`
}

// Usage represents token usage.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// APIError represents an API error.
type APIError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
