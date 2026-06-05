package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const cloudflareAIRunBaseURL = "https://api.cloudflare.com/client/v4/accounts"

// CloudflareTranscriptionConfig contains the credentials and model for Workers AI.
type CloudflareTranscriptionConfig struct {
	AccountID string
	APIToken  string
	Model     string
	Client    *http.Client
}

// CloudflareTranscriptionProvider calls Cloudflare Workers AI Whisper.
type CloudflareTranscriptionProvider struct {
	accountID string
	apiToken  string
	model     string
	client    *http.Client
}

// NewCloudflareTranscriptionProvider creates a Cloudflare Workers AI provider.
func NewCloudflareTranscriptionProvider(cfg CloudflareTranscriptionConfig) *CloudflareTranscriptionProvider {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultTranscriptionModel
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &CloudflareTranscriptionProvider{
		accountID: strings.TrimSpace(cfg.AccountID),
		apiToken:  strings.TrimSpace(cfg.APIToken),
		model:     model,
		client:    client,
	}
}

// Transcribe sends a completed audio file to Cloudflare and normalizes the response.
func (p *CloudflareTranscriptionProvider) Transcribe(ctx context.Context, input TranscriptionInput) (TranscriptionResult, error) {
	if p == nil || p.accountID == "" || p.apiToken == "" || p.model == "" {
		return TranscriptionResult{}, ErrTranscriptionProviderNotConfigured
	}

	body, err := json.Marshal(map[string]string{
		"audio": base64.StdEncoding.EncodeToString(input.Data),
	})
	if err != nil {
		return TranscriptionResult{}, fmt.Errorf("%w: encode request: %v", ErrTranscriptionProviderFailed, err)
	}

	endpoint := fmt.Sprintf("%s/%s/ai/run/%s", cloudflareAIRunBaseURL, p.accountID, p.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TranscriptionResult{}, fmt.Errorf("%w: build request: %v", ErrTranscriptionProviderFailed, err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return TranscriptionResult{}, fmt.Errorf("%w: request failed: %v", ErrTranscriptionProviderFailed, err)
	}
	defer resp.Body.Close()

	var payload struct {
		Success bool `json:"success"`
		Result  struct {
			Text string `json:"text"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return TranscriptionResult{}, fmt.Errorf("%w: decode response: %v", ErrTranscriptionProviderFailed, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !payload.Success {
		return TranscriptionResult{}, fmt.Errorf("%w: %s", ErrTranscriptionProviderFailed, cloudflareErrorMessage(payload.Errors))
	}
	text := strings.TrimSpace(payload.Result.Text)
	if text == "" {
		return TranscriptionResult{}, ErrEmptyTranscript
	}

	return TranscriptionResult{
		Text:     text,
		Provider: "cloudflare",
		Model:    p.model,
	}, nil
}

// cloudflareErrorMessage returns a safe upstream error summary without credentials.
func cloudflareErrorMessage(errorsPayload []struct {
	Message string `json:"message"`
}) string {
	for _, item := range errorsPayload {
		if strings.TrimSpace(item.Message) != "" {
			return item.Message
		}
	}
	return errors.New("upstream error").Error()
}
