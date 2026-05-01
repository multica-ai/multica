package llm

import "context"

// LLMClient is a minimal interface for one-shot structured completions.
type LLMClient interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is a request for a chat completion.
type CompletionRequest struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	ResponseFormat string    `json:"response_format,omitempty"` // "json_object" or ""
	Temperature    float64   `json:"temperature,omitempty"`
}

// CompletionResponse holds the response from the LLM.
type CompletionResponse struct {
	Content string
}

// Config configures an LLM client instance.
type Config struct {
	BaseURL string // e.g. "https://api.deepseek.com"
	APIKey  string
	Model   string // default model to use when request.Model is empty
}
