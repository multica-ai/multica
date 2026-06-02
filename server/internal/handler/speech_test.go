package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeSpeechProxy struct {
	transcribe func(context.Context, SpeechTranscribeInput) (SpeechTranscribeResult, error)
	synthesize func(context.Context, SpeechSynthesizeInput) (SpeechSynthesizeResult, error)
}

func (f fakeSpeechProxy) Transcribe(ctx context.Context, input SpeechTranscribeInput) (SpeechTranscribeResult, error) {
	return f.transcribe(ctx, input)
}

func (f fakeSpeechProxy) Synthesize(ctx context.Context, input SpeechSynthesizeInput) (SpeechSynthesizeResult, error) {
	return f.synthesize(ctx, input)
}

func withSpeechTestWorkspace(req *http.Request) *http.Request {
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
}

func TestTranscribeSpeech_Success(t *testing.T) {
	handler := &Handler{
		Speech: fakeSpeechProxy{
			transcribe: func(_ context.Context, input SpeechTranscribeInput) (SpeechTranscribeResult, error) {
				if input.WorkspaceID != testWorkspaceID {
					t.Fatalf("workspace id = %q, want %q", input.WorkspaceID, testWorkspaceID)
				}
				if input.UserID != testUserID {
					t.Fatalf("user id = %q, want %q", input.UserID, testUserID)
				}
				if input.ContentType != "audio/m4a" {
					t.Fatalf("content type = %q, want audio/m4a", input.ContentType)
				}
				if string(input.Audio) != "audio-bytes" {
					t.Fatalf("audio = %q, want audio-bytes", string(input.Audio))
				}
				return SpeechTranscribeResult{Transcript: "ship it"}, nil
			},
		},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := createSpeechFormFile(writer, "voice.m4a", "audio/m4a")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("audio-bytes")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/speech/transcribe", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req = withSpeechTestWorkspace(req)

	w := httptest.NewRecorder()
	handler.TranscribeSpeech(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp SpeechTranscribeResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Transcript != "ship it" {
		t.Fatalf("transcript = %q, want ship it", resp.Transcript)
	}
}

func TestTranscribeSpeech_RejectsUnsupportedAudioType(t *testing.T) {
	handler := &Handler{
		Speech: fakeSpeechProxy{
			transcribe: func(context.Context, SpeechTranscribeInput) (SpeechTranscribeResult, error) {
				t.Fatal("transcribe should not be called")
				return SpeechTranscribeResult{}, nil
			},
		},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := createSpeechFormFile(writer, "voice.txt", "text/plain")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("not-audio")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/speech/transcribe", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req = withSpeechTestWorkspace(req)

	w := httptest.NewRecorder()
	handler.TranscribeSpeech(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

func TestSynthesizeSpeech_Success(t *testing.T) {
	handler := &Handler{
		Speech: fakeSpeechProxy{
			synthesize: func(_ context.Context, input SpeechSynthesizeInput) (SpeechSynthesizeResult, error) {
				if input.WorkspaceID != testWorkspaceID {
					t.Fatalf("workspace id = %q, want %q", input.WorkspaceID, testWorkspaceID)
				}
				if input.UserID != testUserID {
					t.Fatalf("user id = %q, want %q", input.UserID, testUserID)
				}
				if input.Text != "hello agent" {
					t.Fatalf("text = %q, want hello agent", input.Text)
				}
				return SpeechSynthesizeResult{AudioBase64: "YXVkaW8=", ContentType: "audio/mpeg"}, nil
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/speech/synthesize", strings.NewReader(`{"text":" hello agent "}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req = withSpeechTestWorkspace(req)

	w := httptest.NewRecorder()
	handler.SynthesizeSpeech(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp SpeechSynthesizeResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AudioBase64 != "YXVkaW8=" || resp.ContentType != "audio/mpeg" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestSynthesizeSpeech_MissingProviderReturns501(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/speech/synthesize", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req = withSpeechTestWorkspace(req)

	w := httptest.NewRecorder()
	(&Handler{}).SynthesizeSpeech(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501, body = %s", w.Code, w.Body.String())
	}
}

func createSpeechFormFile(writer *multipart.Writer, filename, contentType string) (io.Writer, error) {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", contentType)
	return writer.CreatePart(header)
}
