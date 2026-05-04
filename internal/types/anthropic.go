package types

// AnthropicMessageRequest represents an Anthropic Messages API request.
type AnthropicMessageRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        any                `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	Thinking      any                `json:"thinking,omitempty"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
}

// AnthropicMessage represents a message in Anthropic format.
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// AnthropicContentBlock represents a content block.
type AnthropicContentBlock struct {
	Type             string                     `json:"type"`
	Text             string                     `json:"text,omitempty"`
	Thinking         string                     `json:"thinking,omitempty"`
	Signature        string                     `json:"signature,omitempty"`
	Data             string                     `json:"data,omitempty"`
	Citations        []AnthropicCitation        `json:"citations,omitempty"`
	Source           *AnthropicSource           `json:"source,omitempty"`
	ID               string                     `json:"id,omitempty"`
	Name             string                     `json:"name,omitempty"`
	Input            any                        `json:"input,omitempty"`
	Content          any                        `json:"content,omitempty"`
	ToolUseID        string                     `json:"tool_use_id,omitempty"`
	WebSearchResults *AnthropicWebSearchResults `json:"web_search_results,omitempty"`
}

// AnthropicCitation represents a citation attached to a text block.
type AnthropicCitation struct {
	Type           string `json:"type"`
	URL            string `json:"url,omitempty"`
	Title          string `json:"title,omitempty"`
	CitedText      string `json:"cited_text,omitempty"`
	EncryptedIndex string `json:"encrypted_index,omitempty"`
	Source         string `json:"source,omitempty"`
}

// AnthropicWebSearchResults holds web search results from Anthropic.
type AnthropicWebSearchResults struct {
	SearchResults []AnthropicWebSearchResult `json:"search_results"`
}

// AnthropicSource represents an image source.
type AnthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// AnthropicTool represents a tool definition for Anthropic.
type AnthropicTool struct {
	Type           string   `json:"type,omitempty"`
	Name           string   `json:"name,omitempty"`
	Description    string   `json:"description,omitempty"`
	InputSchema    any      `json:"input_schema,omitempty"`
	MaxUses        *int     `json:"max_uses,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
	UserLocation   any      `json:"user_location,omitempty"`
}

// AnthropicMessageResponse represents an Anthropic Messages API response.
type AnthropicMessageResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicUsage represents token usage from Anthropic.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicStreamEvent represents a streaming SSE event from Anthropic.
type AnthropicStreamEvent struct {
	Type         string                    `json:"type"`
	Index        *int                      `json:"index,omitempty"`
	Delta        *AnthropicDelta           `json:"delta,omitempty"`
	ContentBlock *AnthropicContentBlock    `json:"content_block,omitempty"`
	Message      *AnthropicMessageResponse `json:"message,omitempty"`
	Usage        *AnthropicUsage           `json:"usage,omitempty"`
	Error        *AnthropicError           `json:"error,omitempty"`
}

// AnthropicDelta represents a delta update in streaming.
type AnthropicDelta struct {
	Type        string             `json:"type,omitempty"`
	Text        string             `json:"text,omitempty"`
	Thinking    string             `json:"thinking,omitempty"`
	Signature   string             `json:"signature,omitempty"`
	PartialJSON string             `json:"partial_json,omitempty"`
	Citation    *AnthropicCitation `json:"citation,omitempty"`
}

// AnthropicError represents an Anthropic API error.
type AnthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AnthropicWebSearchResult represents a single web search result from Anthropic.
type AnthropicWebSearchResult struct {
	URL              string `json:"url"`
	Title            string `json:"title"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
	PageAge          string `json:"page_age,omitempty"`
}
