package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTranscriptionService_DisabledWithoutProvider(t *testing.T) {
	svc := NewTranscriptionService(nil, 0)
	_, err := svc.Transcribe(context.Background(), TranscriptionInput{Data: []byte("audio")})
	if err != ErrTranscriptionProviderNotConfigured {
		t.Fatalf("expected provider-not-configured, got %v", err)
	}
}

func TestCloudflareTranscriptionProvider_TranscribesAudio(t *testing.T) {
	audio := []byte("fake wav bytes")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			if req.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("missing bearer token")
			}
			if !strings.Contains(req.URL.Path, "/ai/run/@cf/openai/whisper-large-v3-turbo") {
				t.Fatalf("unexpected path %s", req.URL.Path)
			}

			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body["audio"] != base64.StdEncoding.EncodeToString(audio) {
				t.Fatalf("audio was not base64 encoded")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"success": true,
					"result": { "text": "  hello from audio  " }
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}

	provider := NewCloudflareTranscriptionProvider(CloudflareTranscriptionConfig{
		AccountID: "account",
		APIToken:  "token",
		Client:    client,
	})
	result, err := provider.Transcribe(context.Background(), TranscriptionInput{
		Filename:    "voice.wav",
		ContentType: "audio/wav",
		Data:        audio,
	})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if result.Text != "hello from audio" {
		t.Fatalf("expected trimmed transcript, got %q", result.Text)
	}
	if result.Provider != "cloudflare" {
		t.Fatalf("expected cloudflare provider, got %q", result.Provider)
	}
	if result.Model != DefaultTranscriptionModel {
		t.Fatalf("expected default model, got %q", result.Model)
	}
}

func TestCloudflareTranscriptionProvider_RequiresConfig(t *testing.T) {
	provider := NewCloudflareTranscriptionProvider(CloudflareTranscriptionConfig{})
	_, err := provider.Transcribe(context.Background(), TranscriptionInput{Data: []byte("audio")})
	if err != ErrTranscriptionProviderNotConfigured {
		t.Fatalf("expected provider-not-configured, got %v", err)
	}
}
