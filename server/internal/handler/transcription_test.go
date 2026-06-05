package handler

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

type mockTranscriptionProvider struct {
	result service.TranscriptionResult
	err    error
	input  service.TranscriptionInput
}

func (p *mockTranscriptionProvider) Transcribe(ctx context.Context, input service.TranscriptionInput) (service.TranscriptionResult, error) {
	p.input = input
	if p.err != nil {
		return service.TranscriptionResult{}, p.err
	}
	return p.result, nil
}

func newMultipartAudioRequest(t *testing.T, filename string, data []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/transcriptions?workspace_id="+testWorkspaceID, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func withTranscriptionService(t *testing.T, svc *service.TranscriptionService) {
	t.Helper()
	previous := testHandler.TranscriptionService
	testHandler.TranscriptionService = svc
	t.Cleanup(func() {
		testHandler.TranscriptionService = previous
	})
}

func TestTranscribeAudio_Success(t *testing.T) {
	provider := &mockTranscriptionProvider{
		result: service.TranscriptionResult{
			Text:     "voice transcript",
			Provider: "cloudflare",
			Model:    service.DefaultTranscriptionModel,
		},
	}
	withTranscriptionService(t, service.NewTranscriptionService(provider, 1024))

	req := newMultipartAudioRequest(t, "voice.wav", []byte("RIFF....WAVEfmt "))
	w := httptest.NewRecorder()
	testHandler.TranscribeAudio(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if provider.input.Filename != "voice.wav" {
		t.Fatalf("expected filename to be passed to provider, got %q", provider.input.Filename)
	}
	if provider.input.ContentType != "audio/wav" {
		t.Fatalf("expected audio/wav, got %q", provider.input.ContentType)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"text":"voice transcript"`)) {
		t.Fatalf("expected transcript response, got %s", w.Body.String())
	}
}

func TestTranscribeAudio_ProviderNotConfigured(t *testing.T) {
	withTranscriptionService(t, service.NewTranscriptionService(nil, 1024))

	req := newMultipartAudioRequest(t, "voice.wav", []byte("RIFF....WAVEfmt "))
	w := httptest.NewRecorder()
	testHandler.TranscribeAudio(w, req)

	if w.Code != http.StatusFailedDependency {
		t.Fatalf("expected 424, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTranscribeAudio_ProviderFailure(t *testing.T) {
	withTranscriptionService(t, service.NewTranscriptionService(&mockTranscriptionProvider{
		err: service.ErrTranscriptionProviderFailed,
	}, 1024))

	req := newMultipartAudioRequest(t, "voice.wav", []byte("RIFF....WAVEfmt "))
	w := httptest.NewRecorder()
	testHandler.TranscribeAudio(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTranscribeAudio_MissingFile(t *testing.T) {
	withTranscriptionService(t, service.NewTranscriptionService(&mockTranscriptionProvider{}, 1024))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/transcriptions?workspace_id="+testWorkspaceID, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.TranscribeAudio(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTranscribeAudio_EmptyTranscript(t *testing.T) {
	withTranscriptionService(t, service.NewTranscriptionService(&mockTranscriptionProvider{
		err: service.ErrEmptyTranscript,
	}, 1024))

	req := newMultipartAudioRequest(t, "voice.wav", []byte("RIFF....WAVEfmt "))
	w := httptest.NewRecorder()
	testHandler.TranscribeAudio(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTranscribeAudio_UnknownErrorMapsToBadGateway(t *testing.T) {
	withTranscriptionService(t, service.NewTranscriptionService(&mockTranscriptionProvider{
		err: errors.New("upstream exploded"),
	}, 1024))

	req := newMultipartAudioRequest(t, "voice.wav", []byte("RIFF....WAVEfmt "))
	w := httptest.NewRecorder()
	testHandler.TranscribeAudio(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}
