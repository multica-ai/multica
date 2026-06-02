package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxSpeechAudioBytes = 10 * 1024 * 1024
	maxSpeechTextRunes  = 4000
)

var errSpeechNotConfigured = errors.New("speech provider is not configured")

type SpeechProxy interface {
	Transcribe(ctx context.Context, input SpeechTranscribeInput) (SpeechTranscribeResult, error)
	Synthesize(ctx context.Context, input SpeechSynthesizeInput) (SpeechSynthesizeResult, error)
}

type SpeechTranscribeInput struct {
	WorkspaceID string
	UserID      string
	Filename    string
	ContentType string
	Audio       []byte
}

type SpeechTranscribeResult struct {
	Transcript string `json:"transcript"`
}

type SpeechSynthesizeInput struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	Text        string `json:"text"`
	Voice       string `json:"voice,omitempty"`
}

type SpeechSynthesizeResult struct {
	AudioBase64 string `json:"audio_base64"`
	ContentType string `json:"content_type"`
}

type HTTPSpeechProxy struct {
	transcribeURL string
	synthesizeURL string
	apiKey        string
	mock          bool
	client        *http.Client
}

func NewHTTPSpeechProxy(cfg SpeechConfig) *HTTPSpeechProxy {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	return &HTTPSpeechProxy{
		transcribeURL: strings.TrimSpace(cfg.TranscribeURL),
		synthesizeURL: strings.TrimSpace(cfg.SynthesizeURL),
		apiKey:        strings.TrimSpace(cfg.APIKey),
		mock:          cfg.Mock,
		client:        client,
	}
}

func (p *HTTPSpeechProxy) Transcribe(ctx context.Context, input SpeechTranscribeInput) (SpeechTranscribeResult, error) {
	if p == nil {
		return SpeechTranscribeResult{}, errSpeechNotConfigured
	}
	if p.mock {
		return SpeechTranscribeResult{Transcript: "Voice message received."}, nil
	}
	if p.transcribeURL == "" {
		return SpeechTranscribeResult{}, errSpeechNotConfigured
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", input.Filename)
	if err != nil {
		return SpeechTranscribeResult{}, err
	}
	if _, err := part.Write(input.Audio); err != nil {
		return SpeechTranscribeResult{}, err
	}
	_ = writer.WriteField("workspace_id", input.WorkspaceID)
	_ = writer.WriteField("user_id", input.UserID)
	if err := writer.Close(); err != nil {
		return SpeechTranscribeResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transcribeURL, &body)
	if err != nil {
		return SpeechTranscribeResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return SpeechTranscribeResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechTranscribeResult{}, fmt.Errorf("speech transcribe upstream returned %d", resp.StatusCode)
	}
	var out SpeechTranscribeResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&out); err != nil {
		return SpeechTranscribeResult{}, err
	}
	out.Transcript = strings.TrimSpace(out.Transcript)
	if out.Transcript == "" {
		return SpeechTranscribeResult{}, errors.New("speech transcribe upstream returned empty transcript")
	}
	return out, nil
}

func (p *HTTPSpeechProxy) Synthesize(ctx context.Context, input SpeechSynthesizeInput) (SpeechSynthesizeResult, error) {
	if p == nil {
		return SpeechSynthesizeResult{}, errSpeechNotConfigured
	}
	if p.mock {
		return SpeechSynthesizeResult{
			AudioBase64: base64.StdEncoding.EncodeToString([]byte("mock audio")),
			ContentType: "audio/mpeg",
		}, nil
	}
	if p.synthesizeURL == "" {
		return SpeechSynthesizeResult{}, errSpeechNotConfigured
	}

	body, err := json.Marshal(input)
	if err != nil {
		return SpeechSynthesizeResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.synthesizeURL, bytes.NewReader(body))
	if err != nil {
		return SpeechSynthesizeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return SpeechSynthesizeResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechSynthesizeResult{}, fmt.Errorf("speech synthesize upstream returned %d", resp.StatusCode)
	}
	var out SpeechSynthesizeResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSpeechAudioBytes)).Decode(&out); err != nil {
		return SpeechSynthesizeResult{}, err
	}
	out.ContentType = strings.TrimSpace(out.ContentType)
	if out.ContentType == "" {
		out.ContentType = "audio/mpeg"
	}
	if strings.TrimSpace(out.AudioBase64) == "" {
		return SpeechSynthesizeResult{}, errors.New("speech synthesize upstream returned empty audio")
	}
	return out, nil
}

func (h *Handler) TranscribeSpeech(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace is required")
		return
	}
	if h.Speech == nil {
		writeError(w, http.StatusNotImplemented, "speech provider is not configured")
		return
	}

	if err := r.ParseMultipartForm(maxSpeechAudioBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid audio upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "audio file is required")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !isAllowedSpeechContentType(contentType) {
		writeError(w, http.StatusBadRequest, "unsupported audio content type")
		return
	}
	audio, err := io.ReadAll(io.LimitReader(file, maxSpeechAudioBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read audio")
		return
	}
	if len(audio) == 0 {
		writeError(w, http.StatusBadRequest, "audio file is empty")
		return
	}
	if len(audio) > maxSpeechAudioBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "audio file is too large")
		return
	}

	result, err := h.Speech.Transcribe(r.Context(), SpeechTranscribeInput{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Filename:    sanitizeSpeechFilename(header.Filename),
		ContentType: contentType,
		Audio:       audio,
	})
	if err != nil {
		writeSpeechProxyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type synthesizeSpeechRequest struct {
	Text  string `json:"text"`
	Voice string `json:"voice"`
}

func (h *Handler) SynthesizeSpeech(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace is required")
		return
	}
	if h.Speech == nil {
		writeError(w, http.StatusNotImplemented, "speech provider is not configured")
		return
	}

	var req synthesizeSpeechRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if len([]rune(req.Text)) > maxSpeechTextRunes {
		writeError(w, http.StatusRequestEntityTooLarge, "text is too long")
		return
	}

	result, err := h.Speech.Synthesize(r.Context(), SpeechSynthesizeInput{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Text:        req.Text,
		Voice:       strings.TrimSpace(req.Voice),
	})
	if err != nil {
		writeSpeechProxyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeSpeechProxyError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSpeechNotConfigured) {
		writeError(w, http.StatusNotImplemented, "speech provider is not configured")
		return
	}
	writeError(w, http.StatusBadGateway, "speech provider request failed")
}

func isAllowedSpeechContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "audio/aac", "audio/mp4", "audio/m4a", "audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/webm", "audio/ogg":
		return true
	default:
		return false
	}
}

func sanitizeSpeechFilename(filename string) string {
	filename = strings.TrimSpace(filepath.Base(filename))
	if filename == "." || filename == "/" || filename == "" {
		return "voice-message.m4a"
	}
	return filename
}
