package service

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultTranscriptionModel    = "@cf/openai/whisper-large-v3-turbo"
	DefaultTranscriptionMaxBytes = int64(25 << 20)
)

var (
	ErrTranscriptionProviderNotConfigured = errors.New("transcription provider is not configured")
	ErrTranscriptionProviderFailed        = errors.New("transcription provider failed")
	ErrEmptyTranscript                    = errors.New("transcription returned empty text")
)

// TranscriptionInput carries a completed audio file to a provider.
type TranscriptionInput struct {
	Filename    string
	ContentType string
	Data        []byte
}

// TranscriptionResult is the normalized response returned to clients.
type TranscriptionResult struct {
	Text            string   `json:"text"`
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
}

// TranscriptionProvider hides provider-specific request and response details.
type TranscriptionProvider interface {
	Transcribe(ctx context.Context, input TranscriptionInput) (TranscriptionResult, error)
}

// TranscriptionService selects and calls the configured transcription provider.
type TranscriptionService struct {
	Provider TranscriptionProvider
	MaxBytes int64
}

// NewTranscriptionService creates a service around an already constructed provider.
func NewTranscriptionService(provider TranscriptionProvider, maxBytes int64) *TranscriptionService {
	if maxBytes <= 0 {
		maxBytes = DefaultTranscriptionMaxBytes
	}
	return &TranscriptionService{Provider: provider, MaxBytes: maxBytes}
}

// NewTranscriptionServiceFromEnv builds the configured provider from environment variables.
func NewTranscriptionServiceFromEnv(httpClient *http.Client) *TranscriptionService {
	providerName := strings.ToLower(strings.TrimSpace(os.Getenv("TRANSCRIPTION_PROVIDER")))
	maxBytes := readTranscriptionMaxBytes()

	switch providerName {
	case "cloudflare":
		return NewTranscriptionService(NewCloudflareTranscriptionProvider(CloudflareTranscriptionConfig{
			AccountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
			APIToken:  os.Getenv("CLOUDFLARE_API_TOKEN"),
			Model:     os.Getenv("CLOUDFLARE_TRANSCRIPTION_MODEL"),
			Client:    httpClient,
		}), maxBytes)
	default:
		return NewTranscriptionService(nil, maxBytes)
	}
}

// Transcribe delegates the audio payload to the active provider.
func (s *TranscriptionService) Transcribe(ctx context.Context, input TranscriptionInput) (TranscriptionResult, error) {
	if s == nil || s.Provider == nil {
		return TranscriptionResult{}, ErrTranscriptionProviderNotConfigured
	}
	result, err := s.Provider.Transcribe(ctx, input)
	if err != nil {
		return TranscriptionResult{}, err
	}
	if strings.TrimSpace(result.Text) == "" {
		return TranscriptionResult{}, ErrEmptyTranscript
	}
	return result, nil
}

// readTranscriptionMaxBytes reads the upload cap, falling back to the design default.
func readTranscriptionMaxBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("TRANSCRIPTION_MAX_BYTES"))
	if raw == "" {
		return DefaultTranscriptionMaxBytes
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return DefaultTranscriptionMaxBytes
	}
	return value
}
