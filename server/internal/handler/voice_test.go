package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/llm"
)

type voiceUpstream struct {
	srv      *httptest.Server
	requests atomic.Int64
	status   int
	fields   map[string]string
	filename string
	audio    string
}

func newVoiceUpstream(t *testing.T) *voiceUpstream {
	t.Helper()
	u := &voiceUpstream{status: http.StatusOK, fields: map[string]string{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.requests.Add(1)
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for key, values := range r.MultipartForm.Value {
				u.fields[key] = values[0]
			}
			if file, header, err := r.FormFile("file"); err == nil {
				raw, _ := io.ReadAll(file)
				u.audio, u.filename = string(raw), header.Filename
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(u.status)
		if u.status == http.StatusOK {
			_, _ = w.Write([]byte(`{"text":"切换工作区以后角标不更新"}`))
		} else {
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func voiceHandler(u *voiceUpstream, model string) *Handler {
	retries, _ := llm.Retries(0)
	return &Handler{
		LLM: llm.New(llm.Config{APIKey: "k", BaseURL: u.srv.URL, MaxRetries: retries}),
		cfg: Config{LLMTranscribeModel: model},
	}
}

func voiceRequest(t *testing.T, contentType string, audio []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for key, value := range fields {
		_ = mw.WriteField(key, value)
	}
	if audio != nil {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="../../etc/passwd"`)
		header.Set("Content-Type", contentType)
		part, err := mw.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(audio)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcriptions", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	return req
}

func TestTranscribeVoiceReturnsText(t *testing.T) {
	u := newVoiceUpstream(t)
	h := voiceHandler(u, "gpt-4o-mini-transcribe")
	w := httptest.NewRecorder()
	h.TranscribeVoice(w, voiceRequest(t, "audio/webm;codecs=opus", []byte("opus-bytes"), map[string]string{
		"language": "ZH",
		"prompt":   "Multica, Lambda",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res voiceTranscriptionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Text != "切换工作区以后角标不更新" {
		t.Errorf("text = %q", res.Text)
	}
	if u.filename != "voice.webm" {
		t.Errorf("upstream filename = %q, want the server-chosen voice.webm", u.filename)
	}
	if u.audio != "opus-bytes" {
		t.Errorf("upstream audio = %q", u.audio)
	}
	if u.fields["model"] != "gpt-4o-mini-transcribe" || u.fields["language"] != "zh" || u.fields["prompt"] != "Multica, Lambda" {
		t.Errorf("upstream fields = %v", u.fields)
	}
}

func TestTranscribeVoiceDisabledSendsNothing(t *testing.T) {
	cases := map[string]*Handler{
		"no transcription model": nil,
		"no llm layer":           {LLM: llm.New(llm.Config{}), cfg: Config{LLMTranscribeModel: "whisper-1"}},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			u := newVoiceUpstream(t)
			if h == nil {
				h = voiceHandler(u, "")
			}
			w := httptest.NewRecorder()
			h.TranscribeVoice(w, voiceRequest(t, "audio/webm", []byte("a"), nil))
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "voice_input_not_configured") {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if n := u.requests.Load(); n != 0 {
				t.Fatalf("disabled voice input made %d upstream requests", n)
			}
			if h.voiceTranscriptionAvailable() {
				t.Fatal("voiceTranscriptionAvailable should be false")
			}
		})
	}
}

func TestTranscribeVoiceRejectsBadInput(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		audio       []byte
		fields      map[string]string
		userID      string
		want        int
	}{
		{"unauthenticated", "audio/webm", []byte("a"), nil, "", http.StatusUnauthorized},
		{"missing file", "", nil, nil, testUserID, http.StatusBadRequest},
		{"empty file", "audio/webm", []byte{}, nil, testUserID, http.StatusBadRequest},
		{"not audio", "text/html", []byte("<script>"), nil, testUserID, http.StatusUnsupportedMediaType},
		{"bad language", "audio/webm", []byte("a"), map[string]string{"language": "zh-CN; rm"}, testUserID, http.StatusBadRequest},
		{"too large", "audio/webm", bytes.Repeat([]byte("a"), maxVoiceClipSize+1), nil, testUserID, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newVoiceUpstream(t)
			h := voiceHandler(u, "whisper-1")
			req := voiceRequest(t, tc.contentType, tc.audio, tc.fields)
			req.Header.Set("X-User-ID", tc.userID)
			w := httptest.NewRecorder()
			h.TranscribeVoice(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if n := u.requests.Load(); n != 0 {
				t.Fatalf("rejected request reached the upstream %d times", n)
			}
		})
	}
}

func TestTranscribeVoiceUpstreamFailure(t *testing.T) {
	u := newVoiceUpstream(t)
	u.status = http.StatusInternalServerError
	h := voiceHandler(u, "whisper-1")
	w := httptest.NewRecorder()
	h.TranscribeVoice(w, voiceRequest(t, "audio/mp4", []byte("a"), nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("upstream error leaked to the client: %s", w.Body.String())
	}
}

func TestGetConfigVoiceInputAvailable(t *testing.T) {
	u := newVoiceUpstream(t)
	for model, want := range map[string]bool{"": false, "whisper-1": true} {
		h := voiceHandler(u, model)
		w := httptest.NewRecorder()
		h.GetConfig(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		var cfg AppConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.VoiceInputAvailable != want {
			t.Errorf("model %q: voice_input_available = %v, want %v", model, cfg.VoiceInputAvailable, want)
		}
	}
}
