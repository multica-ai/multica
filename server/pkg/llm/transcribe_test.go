package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscribeSendsMultipartAudio(t *testing.T) {
	var (
		path   string
		fields = map[string]string{}
		audio  string
		name   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		for key, values := range r.MultipartForm.Value {
			fields[key] = values[0]
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
		} else {
			raw, _ := io.ReadAll(file)
			audio, name = string(raw), header.Filename
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"  hello world \n"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0)})
	text, err := c.Transcribe(context.Background(), TranscribeRequest{
		Model:       "gpt-4o-mini-transcribe",
		Audio:       strings.NewReader("RIFF-audio"),
		Filename:    "voice.webm",
		ContentType: "audio/webm",
		Language:    "zh",
		Prompt:      "Multica, Lambda",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "hello world" {
		t.Errorf("text = %q, want trimmed %q", text, "hello world")
	}
	if path != "/audio/transcriptions" {
		t.Errorf("path = %q", path)
	}
	if audio != "RIFF-audio" || name != "voice.webm" {
		t.Errorf("file = %q (%q)", audio, name)
	}
	want := map[string]string{
		"model":           "gpt-4o-mini-transcribe",
		"language":        "zh",
		"prompt":          "Multica, Lambda",
		"response_format": "json",
	}
	for key, value := range want {
		if fields[key] != value {
			t.Errorf("field %s = %q, want %q", key, fields[key], value)
		}
	}
}

func TestTranscribeOmitsEmptyHints(t *testing.T) {
	var fields map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		fields = r.MultipartForm.Value
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: retries(0)})
	if _, err := c.Transcribe(context.Background(), TranscribeRequest{
		Model: "whisper-1", Audio: strings.NewReader("a"), Filename: "voice.webm", ContentType: "audio/webm",
	}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	for _, key := range []string{"language", "prompt"} {
		if _, ok := fields[key]; ok {
			t.Errorf("empty %s should be omitted, got %q", key, fields[key])
		}
	}
}

func TestTranscribeRequiresModel(t *testing.T) {
	transport := &countingHTTPClient{}
	c := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", HTTPClient: transport})
	if _, err := c.Transcribe(context.Background(), TranscribeRequest{Audio: strings.NewReader("a")}); err == nil {
		t.Fatal("expected an error without a model")
	}
	if n := transport.requests.Load(); n != 0 {
		t.Fatalf("made %d upstream requests without a model", n)
	}
}
