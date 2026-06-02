package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeSpeechProxy struct {
	transcribe func(context.Context, SpeechTranscribeInput) (SpeechTranscribeResult, error)
	synthesize func(context.Context, SpeechSynthesizeInput) (SpeechSynthesizeResult, error)
}

type denyingSpeechLimiter struct{}

func (denyingSpeechLimiter) Allow(context.Context, string) bool { return false }

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
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415, body = %s", w.Code, w.Body.String())
	}
	assertSpeechErrorCode(t, w.Body.Bytes(), string(speechErrUnsupportedFormat))
}

func TestTranscribeSpeech_RateLimited(t *testing.T) {
	handler := &Handler{
		Speech: fakeSpeechProxy{
			transcribe: func(context.Context, SpeechTranscribeInput) (SpeechTranscribeResult, error) {
				t.Fatal("transcribe should not be called")
				return SpeechTranscribeResult{}, nil
			},
		},
		SpeechRateLimiter: denyingSpeechLimiter{},
	}

	req := httptest.NewRequest("POST", "/api/speech/transcribe", strings.NewReader(""))
	req.Header.Set("X-User-ID", testUserID)
	req = withSpeechTestWorkspace(req)

	w := httptest.NewRecorder()
	handler.TranscribeSpeech(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429, body = %s", w.Code, w.Body.String())
	}
	assertSpeechErrorCode(t, w.Body.Bytes(), string(speechErrRateLimited))
}

func TestTranscribeSpeech_AudioTooLarge(t *testing.T) {
	handler := &Handler{
		Speech: fakeSpeechProxy{
			transcribe: func(context.Context, SpeechTranscribeInput) (SpeechTranscribeResult, error) {
				t.Fatal("transcribe should not be called")
				return SpeechTranscribeResult{}, nil
			},
		},
		cfg: Config{Speech: SpeechConfig{MaxAudioBytes: 4}},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := createSpeechFormFile(writer, "voice.m4a", "audio/m4a")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("12345")); err != nil {
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
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", w.Code, w.Body.String())
	}
	assertSpeechErrorCode(t, w.Body.Bytes(), string(speechErrAudioTooLarge))
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
	assertSpeechErrorCode(t, w.Body.Bytes(), string(speechErrProviderMissing))
}

func TestSynthesizeSpeech_TextTooLong(t *testing.T) {
	handler := &Handler{
		Speech: fakeSpeechProxy{
			synthesize: func(context.Context, SpeechSynthesizeInput) (SpeechSynthesizeResult, error) {
				t.Fatal("synthesize should not be called")
				return SpeechSynthesizeResult{}, nil
			},
		},
		cfg: Config{Speech: SpeechConfig{MaxTextRunes: 3}},
	}

	req := httptest.NewRequest("POST", "/api/speech/synthesize", strings.NewReader(`{"text":"four"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req = withSpeechTestWorkspace(req)

	w := httptest.NewRecorder()
	handler.SynthesizeSpeech(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", w.Code, w.Body.String())
	}
	assertSpeechErrorCode(t, w.Body.Bytes(), string(speechErrTextTooLong))
}

func TestHTTPSpeechProxy_DisabledProviderReturnsRecoverableError(t *testing.T) {
	proxy := NewHTTPSpeechProxy(SpeechConfig{Mode: "disabled"})

	_, err := proxy.Transcribe(context.Background(), SpeechTranscribeInput{
		Filename:    "voice.m4a",
		ContentType: "audio/m4a",
		Audio:       []byte("audio"),
	})
	var se *speechError
	if err == nil || !strings.Contains(err.Error(), string(speechErrProviderMissing)) {
		t.Fatalf("expected provider missing error, got %v", err)
	}
	if !errorAs(err, &se) || se.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501 speech error, got %#v", err)
	}
}

func TestHTTPSpeechProxy_TranscribeMapsQuotaAndRedactsUpstreamBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota exhausted for sk-proj-secret"}`))
	}))
	defer server.Close()

	proxy := NewHTTPSpeechProxy(SpeechConfig{
		Mode:          "external",
		TranscribeURL: server.URL,
		APIKey:        "sk-proj-secret",
	})

	_, err := proxy.Transcribe(context.Background(), SpeechTranscribeInput{
		Filename:    "voice.m4a",
		ContentType: "audio/m4a",
		Audio:       []byte("audio"),
	})
	var se *speechError
	if !errorAs(err, &se) {
		t.Fatalf("expected speech error, got %v", err)
	}
	if se.Code != speechErrQuotaExceeded || se.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("speech error = %#v", se)
	}
	if strings.Contains(err.Error(), "sk-proj-secret") || strings.Contains(err.Error(), "quota exhausted") {
		t.Fatalf("upstream body leaked through error: %v", err)
	}
}

func TestHTTPSpeechProxy_TranscribeEmptyTranscript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"transcript":"   "}`))
	}))
	defer server.Close()

	proxy := NewHTTPSpeechProxy(SpeechConfig{Mode: "external", TranscribeURL: server.URL})
	_, err := proxy.Transcribe(context.Background(), SpeechTranscribeInput{
		Filename:    "voice.m4a",
		ContentType: "audio/m4a",
		Audio:       []byte("audio"),
	})
	var se *speechError
	if !errorAs(err, &se) {
		t.Fatalf("expected speech error, got %v", err)
	}
	if se.Code != speechErrEmptyTranscript || se.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("speech error = %#v", se)
	}
}

func TestHTTPSpeechProxy_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	proxy := NewHTTPSpeechProxy(SpeechConfig{
		Mode:          "external",
		TranscribeURL: server.URL,
		Timeout:       time.Millisecond,
	})
	_, err := proxy.Transcribe(context.Background(), SpeechTranscribeInput{
		Filename:    "voice.m4a",
		ContentType: "audio/m4a",
		Audio:       []byte("audio"),
	})
	var se *speechError
	if !errorAs(err, &se) {
		t.Fatalf("expected speech error, got %v", err)
	}
	if se.Code != speechErrProviderTimeout || se.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("speech error = %#v", se)
	}
}

func createSpeechFormFile(writer *multipart.Writer, filename, contentType string) (io.Writer, error) {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", contentType)
	return writer.CreatePart(header)
}

func assertSpeechErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var got struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, string(body))
	}
	if got.Code != want {
		t.Fatalf("code = %q, want %q; body=%s", got.Code, want, string(body))
	}
}

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}
