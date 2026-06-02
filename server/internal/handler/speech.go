package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	defaultMaxSpeechAudioBytes = 10 * 1024 * 1024
	defaultMaxSpeechTextRunes  = 4000
	defaultSpeechTimeout       = 45 * time.Second
)

var errSpeechNotConfigured = errors.New("speech provider is not configured")

type speechErrorCode string

const (
	speechErrProviderMissing     speechErrorCode = "provider_missing"
	speechErrQuotaExceeded       speechErrorCode = "quota_exceeded"
	speechErrProviderTimeout     speechErrorCode = "provider_timeout"
	speechErrAudioTooLarge       speechErrorCode = "audio_too_large"
	speechErrUnsupportedFormat   speechErrorCode = "unsupported_format"
	speechErrEmptyTranscript     speechErrorCode = "empty_transcript"
	speechErrProviderFailed      speechErrorCode = "provider_failed"
	speechErrInvalidRequest      speechErrorCode = "invalid_request"
	speechErrRateLimited         speechErrorCode = "rate_limited"
	speechErrTextTooLong         speechErrorCode = "text_too_long"
)

type speechError struct {
	Code       speechErrorCode
	Message    string
	StatusCode int
	Err        error
}

func (e *speechError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return string(e.Code) + ": " + e.Err.Error()
	}
	return string(e.Code)
}

func (e *speechError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newSpeechError(code speechErrorCode, status int, msg string, err error) *speechError {
	return &speechError{Code: code, StatusCode: status, Message: msg, Err: err}
}

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
	mode          string
	transcribeURL string
	synthesizeURL string
	apiKey        string
	mock          bool
	client        *http.Client
	timeout       time.Duration
	maxAudioBytes int64
}

func NewHTTPSpeechProxy(cfg SpeechConfig) *HTTPSpeechProxy {
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultSpeechTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	mode := normalizeSpeechMode(cfg)
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultSpeechTimeout
	}
	return &HTTPSpeechProxy{
		mode:          mode,
		transcribeURL: strings.TrimSpace(cfg.TranscribeURL),
		synthesizeURL: strings.TrimSpace(cfg.SynthesizeURL),
		apiKey:        strings.TrimSpace(cfg.APIKey),
		mock:          cfg.Mock || mode == "mock",
		client:        client,
		timeout:       timeout,
		maxAudioBytes: maxSpeechAudioBytes(cfg),
	}
}

func (p *HTTPSpeechProxy) Transcribe(ctx context.Context, input SpeechTranscribeInput) (SpeechTranscribeResult, error) {
	if p == nil {
		return SpeechTranscribeResult{}, newSpeechError(speechErrProviderMissing, http.StatusNotImplemented, "speech provider is not configured", errSpeechNotConfigured)
	}
	if p.mock {
		return SpeechTranscribeResult{Transcript: "Voice message received."}, nil
	}
	if p.mode == "disabled" || p.transcribeURL == "" {
		return SpeechTranscribeResult{}, newSpeechError(speechErrProviderMissing, http.StatusNotImplemented, "speech provider is not configured", errSpeechNotConfigured)
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

	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.transcribeURL, &body)
	if err != nil {
		return SpeechTranscribeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return SpeechTranscribeResult{}, newSpeechError(speechErrProviderTimeout, http.StatusGatewayTimeout, "speech provider timed out", err)
		}
		return SpeechTranscribeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechTranscribeResult{}, upstreamSpeechError("transcribe", resp.StatusCode)
	}
	var out SpeechTranscribeResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&out); err != nil {
		return SpeechTranscribeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
	out.Transcript = strings.TrimSpace(out.Transcript)
	if out.Transcript == "" {
		return SpeechTranscribeResult{}, newSpeechError(speechErrEmptyTranscript, http.StatusUnprocessableEntity, "speech transcript is empty", errors.New("speech transcribe upstream returned empty transcript"))
	}
	return out, nil
}

func (p *HTTPSpeechProxy) Synthesize(ctx context.Context, input SpeechSynthesizeInput) (SpeechSynthesizeResult, error) {
	if p == nil {
		return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderMissing, http.StatusNotImplemented, "speech provider is not configured", errSpeechNotConfigured)
	}
	if p.mock {
		return SpeechSynthesizeResult{
			AudioBase64: base64.StdEncoding.EncodeToString([]byte("mock audio")),
			ContentType: "audio/mpeg",
		}, nil
	}
	if p.mode == "disabled" || p.synthesizeURL == "" {
		return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderMissing, http.StatusNotImplemented, "speech provider is not configured", errSpeechNotConfigured)
	}

	body, err := json.Marshal(input)
	if err != nil {
		return SpeechSynthesizeResult{}, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "invalid request body", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.synthesizeURL, bytes.NewReader(body))
	if err != nil {
		return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderTimeout, http.StatusGatewayTimeout, "speech provider timed out", err)
		}
		return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechSynthesizeResult{}, upstreamSpeechError("synthesize", resp.StatusCode)
	}
	var out SpeechSynthesizeResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, p.maxAudioBytes)).Decode(&out); err != nil {
		return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
	out.ContentType = strings.TrimSpace(out.ContentType)
	if out.ContentType == "" {
		out.ContentType = "audio/mpeg"
	}
	if strings.TrimSpace(out.AudioBase64) == "" {
		return SpeechSynthesizeResult{}, newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", errors.New("speech synthesize upstream returned empty audio"))
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
		writeSpeechError(w, newSpeechError(speechErrProviderMissing, http.StatusNotImplemented, "speech provider is not configured", errSpeechNotConfigured))
		return
	}
	if !h.allowSpeechRequest(r, workspaceID, userID) {
		writeSpeechError(w, newSpeechError(speechErrRateLimited, http.StatusTooManyRequests, "speech rate limit exceeded", nil))
		return
	}

	limit := maxSpeechAudioBytes(h.cfg.Speech)
	r.Body = http.MaxBytesReader(w, r.Body, limit+1024)
	if err := r.ParseMultipartForm(limit); err != nil {
		writeSpeechError(w, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "invalid audio upload", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeSpeechError(w, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "audio file is required", err))
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !isAllowedSpeechContentType(contentType) {
		writeSpeechError(w, newSpeechError(speechErrUnsupportedFormat, http.StatusUnsupportedMediaType, "unsupported audio content type", nil))
		return
	}
	audio, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeSpeechError(w, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "failed to read audio", err))
		return
	}
	if len(audio) == 0 {
		writeSpeechError(w, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "audio file is empty", nil))
		return
	}
	if int64(len(audio)) > limit {
		writeSpeechError(w, newSpeechError(speechErrAudioTooLarge, http.StatusRequestEntityTooLarge, "audio file is too large", nil))
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
		writeSpeechError(w, err)
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
		writeSpeechError(w, newSpeechError(speechErrProviderMissing, http.StatusNotImplemented, "speech provider is not configured", errSpeechNotConfigured))
		return
	}
	if !h.allowSpeechRequest(r, workspaceID, userID) {
		writeSpeechError(w, newSpeechError(speechErrRateLimited, http.StatusTooManyRequests, "speech rate limit exceeded", nil))
		return
	}

	var req synthesizeSpeechRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeSpeechError(w, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "invalid request body", err))
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeSpeechError(w, newSpeechError(speechErrInvalidRequest, http.StatusBadRequest, "text is required", nil))
		return
	}
	if len([]rune(req.Text)) > maxSpeechTextRunes(h.cfg.Speech) {
		writeSpeechError(w, newSpeechError(speechErrTextTooLong, http.StatusRequestEntityTooLarge, "text is too long", nil))
		return
	}

	result, err := h.Speech.Synthesize(r.Context(), SpeechSynthesizeInput{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Text:        req.Text,
		Voice:       strings.TrimSpace(req.Voice),
	})
	if err != nil {
		writeSpeechError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeSpeechError(w http.ResponseWriter, err error) {
	var se *speechError
	if errors.As(err, &se) {
		status := se.StatusCode
		if status == 0 {
			status = http.StatusBadGateway
		}
		msg := se.Message
		if msg == "" {
			msg = "speech provider request failed"
		}
		writeJSON(w, status, map[string]string{
			"error": msg,
			"code":  string(se.Code),
		})
		return
	}
	slog.Warn("speech provider request failed", "error", redact.Text(err.Error()))
	writeJSON(w, http.StatusBadGateway, map[string]string{
		"error": "speech provider request failed",
		"code":  string(speechErrProviderFailed),
	})
}

func upstreamSpeechError(operation string, status int) error {
	err := fmt.Errorf("speech %s upstream returned %d", operation, status)
	switch status {
	case http.StatusTooManyRequests, http.StatusPaymentRequired:
		return newSpeechError(speechErrQuotaExceeded, http.StatusTooManyRequests, "speech quota exceeded", err)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return newSpeechError(speechErrProviderTimeout, http.StatusGatewayTimeout, "speech provider timed out", err)
	default:
		return newSpeechError(speechErrProviderFailed, http.StatusBadGateway, "speech provider request failed", err)
	}
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

func normalizeSpeechMode(cfg SpeechConfig) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mock && mode == "" {
		return "mock"
	}
	switch mode {
	case "", "disabled":
		if cfg.TranscribeURL != "" || cfg.SynthesizeURL != "" {
			return "external"
		}
		return "disabled"
	case "mock", "external":
		return mode
	default:
		slog.Warn("invalid speech provider mode, disabling speech", "mode", mode)
		return "disabled"
	}
}

func maxSpeechAudioBytes(cfg SpeechConfig) int64 {
	if cfg.MaxAudioBytes > 0 {
		return cfg.MaxAudioBytes
	}
	return defaultMaxSpeechAudioBytes
}

func maxSpeechTextRunes(cfg SpeechConfig) int {
	if cfg.MaxTextRunes > 0 {
		return cfg.MaxTextRunes
	}
	return defaultMaxSpeechTextRunes
}

func defaultSpeechRateLimit(cfg SpeechConfig) WebhookRateLimit {
	if cfg.RateLimit.Limit > 0 && cfg.RateLimit.Window > 0 {
		return cfg.RateLimit
	}
	return WebhookRateLimit{Limit: 20, Window: time.Minute}
}

func DefaultSpeechRateLimitForConfig(cfg SpeechConfig) WebhookRateLimit {
	return defaultSpeechRateLimit(cfg)
}

func (h *Handler) allowSpeechRequest(r *http.Request, workspaceID, userID string) bool {
	if h.SpeechRateLimiter == nil {
		return true
	}
	key := workspaceID + ":" + userID
	return h.SpeechRateLimiter.Allow(r.Context(), key)
}
