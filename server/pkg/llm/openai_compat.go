package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openAICompatClient implements LLMClient against any OpenAI-compatible endpoint.
type openAICompatClient struct {
	cfg        Config
	httpClient *http.Client
}

// NewOpenAICompatClient creates a client for DeepSeek / OpenAI / Ollama.
func NewOpenAICompatClient(cfg Config) LLMClient {
	return &openAICompatClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

type apiRequest struct {
	Model          string            `json:"model"`
	Messages       []Message         `json:"messages"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	Temperature    float64           `json:"temperature,omitempty"`
}

type apiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *openAICompatClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}
	if model == "" {
		model = "deepseek-chat"
	}

	apiReq := apiRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
	}
	if req.ResponseFormat == "json_object" {
		apiReq.ResponseFormat = map[string]string{"type": "json_object"}
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	baseURL := strings.TrimRight(c.cfg.BaseURL, "/")
	url := baseURL + "/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("llm: status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: unmarshal response: %w", err)
	}
	if apiResp.Error != nil {
		return CompletionResponse{}, fmt.Errorf("llm: api error: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("llm: no choices in response")
	}

	return CompletionResponse{Content: apiResp.Choices[0].Message.Content}, nil
}
