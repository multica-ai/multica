package knowledge

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

	"github.com/multica-ai/multica/server/pkg/llm"
)

type parserResponse struct {
	ParsedDocument
	Document *ParsedDocument `json:"document,omitempty"`
}

type tokenizerResponse struct {
	Tokens           []string `json:"tokens"`
	TokenizerVersion string   `json:"tokenizer_version"`
}

type parserRenderedImage struct {
	Page       int    `json:"page"`
	MIMEType   string `json:"mime_type"`
	DataBase64 string `json:"data_base64"`
}

type parserRenderResponse struct {
	Images []parserRenderedImage `json:"images"`
}

const (
	maxVisionRenderPages          = 4
	maxVisionRenderBytes          = 16 << 20
	maxVisionRenderResponseBytes  = (maxVisionRenderBytes*4)/3 + 1<<20
	maxVisionRenderRequestTimeout = 60 * time.Second
)

// tokenizeQuery uses the parser's fixed search tokenizer when available. A
// parser outage is returned to the caller so search can expose a warning and
// continue with SearchTokens rather than turning a keyword query into a hard
// failure.
func (s *Service) tokenizeQuery(ctx context.Context, text string) ([]string, error) {
	fallback := SearchTokens(text)
	if strings.TrimSpace(s.parserURL) == "" {
		return fallback, nil
	}
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fallback, internal("failed to encode tokenizer request", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.parserURL+"/v1/tokenize", bytes.NewReader(payload))
	if err != nil {
		return fallback, internal("failed to build tokenizer request", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if s.parserToken != "" {
		request.Header.Set("Authorization", "Bearer "+s.parserToken)
	}
	client := s.parserClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fallback, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fallback, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fallback, fmt.Errorf("tokenizer returned HTTP %d", response.StatusCode)
	}
	var parsed tokenizerResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fallback, err
	}
	if parsed.TokenizerVersion != TokenizerVersion {
		return fallback, fmt.Errorf("tokenizer version %q does not match %q", parsed.TokenizerVersion, TokenizerVersion)
	}
	validated := make([]string, 0, len(parsed.Tokens))
	seen := make(map[string]struct{}, len(parsed.Tokens))
	for _, token := range parsed.Tokens {
		token = strings.TrimSpace(token)
		if token == "" || len([]rune(token)) > 256 {
			return fallback, errors.New("tokenizer returned an invalid token")
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		validated = append(validated, token)
	}
	if len(validated) == 0 && len(fallback) > 0 {
		return fallback, errors.New("tokenizer returned no tokens")
	}
	return validated, nil
}

func (s *Service) parseDocument(ctx context.Context, data []byte, filename, mimeType string) (ParsedDocument, error) {
	if strings.TrimSpace(s.parserURL) == "" {
		return ParseDocument(data, filename, mimeType)
	}
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	filePart, err := form.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return ParsedDocument{}, internal("failed to encode parser request", err)
	}
	if _, err := filePart.Write(data); err != nil {
		return ParsedDocument{}, internal("failed to encode parser request", err)
	}
	metadata, err := json.Marshal(map[string]string{"filename": filename, "mime_type": mimeType})
	if err != nil {
		return ParsedDocument{}, internal("failed to encode parser metadata", err)
	}
	if err := form.WriteField("metadata", string(metadata)); err != nil {
		return ParsedDocument{}, internal("failed to encode parser metadata", err)
	}
	if err := form.Close(); err != nil {
		return ParsedDocument{}, internal("failed to encode parser request", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.parserTO)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.parserURL+"/v1/parse", bytes.NewReader(payload.Bytes()))
	if err != nil {
		return ParsedDocument{}, internal("failed to build parser request", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", form.FormDataContentType())
	if s.parserToken != "" {
		request.Header.Set("Authorization", "Bearer "+s.parserToken)
	}
	client := s.parserClient
	if client == nil {
		client = &http.Client{Timeout: s.parserTO}
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return ParsedDocument{}, retryableKnowledge(http.StatusGatewayTimeout, "parser_timeout", "knowledge parser timed out", err)
		}
		return ParsedDocument{}, retryableKnowledge(http.StatusBadGateway, "parser_unavailable", "knowledge parser is unavailable", err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, (128<<20)+1))
	if readErr != nil {
		return ParsedDocument{}, retryableKnowledge(http.StatusBadGateway, "parser_unavailable", "knowledge parser response could not be read", readErr)
	}
	if len(body) > 128<<20 {
		return ParsedDocument{}, knowledgeError(http.StatusUnprocessableEntity, "parse_output_too_large", "knowledge parser output exceeded the configured limit", nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 500 {
			message = message[:500]
		}
		var parserError struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(body, &parserError)
		code := strings.TrimSpace(parserError.Code)
		if code == "" {
			code = "parse_failed"
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return ParsedDocument{}, retryableKnowledge(http.StatusBadGateway, code, fmt.Sprintf("knowledge parser returned HTTP %d: %s", response.StatusCode, message), nil)
		}
		return ParsedDocument{}, knowledgeError(http.StatusUnprocessableEntity, code, fmt.Sprintf("knowledge parser returned HTTP %d: %s", response.StatusCode, message), nil)
	}
	var parsed parserResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ParsedDocument{}, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "knowledge parser returned invalid JSON", err)
	}
	if parsed.Document != nil {
		parsed.ParsedDocument = *parsed.Document
	}
	if len(parsed.Blocks) == 0 {
		return ParsedDocument{}, knowledgeError(http.StatusUnprocessableEntity, "parse_failed", "knowledge parser returned no readable blocks", nil)
	}
	if parsed.SchemaVersion == "" {
		parsed.SchemaVersion = ParserSchemaVersion
	}
	if parsed.ParserVersion == "" {
		parsed.ParserVersion = ParserVersion
	}
	if parsed.Warnings == nil {
		parsed.Warnings = []string{}
	}
	if parsed.Stats == nil {
		parsed.Stats = map[string]any{}
	}
	parsed.Stats["blocks"] = len(parsed.Blocks)
	sanitized, err := sanitizeParsedDocument(parsed.ParsedDocument)
	if err != nil {
		return ParsedDocument{}, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "knowledge parser returned an unsupported document schema", err)
	}
	return sanitized, nil
}

// renderVisionPages asks the isolated parser to rasterize a small, explicit
// page set. The parser response is validated before it reaches the model
// client; in particular, pages cannot be duplicated, omitted, or replaced by
// an arbitrarily large data URI.
func (s *Service) renderVisionPages(ctx context.Context, data []byte, filename, mimeType string, pages []int) (map[int]llm.VisionImage, error) {
	if strings.TrimSpace(s.parserURL) == "" {
		return nil, knowledgeError(http.StatusUnprocessableEntity, "vision_render_unavailable", "vision page rendering is not configured", nil)
	}
	if len(data) == 0 || int64(len(data)) > s.maxUpload {
		return nil, badRequest("file_too_large", "source exceeds the knowledge upload limit")
	}
	if len(pages) == 0 || len(pages) > maxVisionRenderPages {
		return nil, knowledgeError(http.StatusUnprocessableEntity, "vision_render_failed", "vision rendering requested an invalid page set", nil)
	}
	requested := make(map[int]struct{}, len(pages))
	normalizedPages := make([]int, 0, len(pages))
	for _, page := range pages {
		if page < 1 {
			return nil, knowledgeError(http.StatusUnprocessableEntity, "vision_render_failed", "vision rendering requested an invalid page number", nil)
		}
		if _, exists := requested[page]; exists {
			continue
		}
		requested[page] = struct{}{}
		normalizedPages = append(normalizedPages, page)
	}

	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	filePart, err := form.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, internal("failed to encode vision render request", err)
	}
	if _, err := filePart.Write(data); err != nil {
		return nil, internal("failed to encode vision render request", err)
	}
	metadata, err := json.Marshal(map[string]any{"filename": filename, "mime_type": mimeType, "pages": normalizedPages})
	if err != nil {
		return nil, internal("failed to encode vision render metadata", err)
	}
	if err := form.WriteField("metadata", string(metadata)); err != nil {
		return nil, internal("failed to encode vision render metadata", err)
	}
	if err := form.Close(); err != nil {
		return nil, internal("failed to encode vision render request", err)
	}
	renderTimeout := maxVisionRenderRequestTimeout
	if s.parserTO > 0 && s.parserTO < renderTimeout {
		renderTimeout = s.parserTO
	}
	requestCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.parserURL+"/v1/render", bytes.NewReader(payload.Bytes()))
	if err != nil {
		return nil, internal("failed to build vision render request", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", form.FormDataContentType())
	if s.parserToken != "" {
		request.Header.Set("Authorization", "Bearer "+s.parserToken)
	}
	client := s.parserClient
	if client == nil {
		client = &http.Client{Timeout: renderTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return nil, retryableKnowledge(http.StatusGatewayTimeout, "parser_timeout", "vision page renderer timed out", err)
		}
		return nil, retryableKnowledge(http.StatusBadGateway, "parser_unavailable", "vision page renderer is unavailable", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxVisionRenderResponseBytes)+1))
	if err != nil {
		return nil, retryableKnowledge(http.StatusBadGateway, "parser_unavailable", "vision page renderer response could not be read", err)
	}
	if len(body) > maxVisionRenderResponseBytes {
		return nil, knowledgeError(http.StatusUnprocessableEntity, "vision_render_failed", "vision page renderer response is too large", nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var parserError struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &parserError)
		code := strings.TrimSpace(parserError.Code)
		if code == "vision_render_unavailable" {
			return nil, knowledgeError(http.StatusUnprocessableEntity, code, "vision page rendering is unavailable", nil)
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return nil, retryableKnowledge(http.StatusBadGateway, "parser_unavailable", "vision page renderer is unavailable", nil)
		}
		if code == "" {
			code = "vision_render_failed"
		}
		return nil, knowledgeError(http.StatusUnprocessableEntity, code, "vision page renderer rejected the source", nil)
	}
	var parsed parserRenderResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer returned invalid JSON", err)
	}
	images := make(map[int]llm.VisionImage, len(parsed.Images))
	totalBytes := 0
	for _, image := range parsed.Images {
		if _, ok := requested[image.Page]; !ok || image.Page < 1 {
			return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer returned an unexpected page", nil)
		}
		if _, duplicate := images[image.Page]; duplicate {
			return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer returned a duplicate page", nil)
		}
		mimeType := strings.ToLower(strings.TrimSpace(strings.SplitN(image.MIMEType, ";", 2)[0]))
		if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" {
			return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer returned an unsupported image type", nil)
		}
		data, decodeErr := base64.StdEncoding.DecodeString(image.DataBase64)
		if decodeErr != nil || len(data) == 0 || len(data) > 8<<20 {
			return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer returned invalid image data", decodeErr)
		}
		totalBytes += len(data)
		if totalBytes > maxVisionRenderBytes {
			return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer returned too much image data", nil)
		}
		images[image.Page] = llm.VisionImage{MIMEType: mimeType, Data: data}
	}
	if len(images) != len(requested) {
		return nil, knowledgeError(http.StatusBadGateway, "parser_invalid_response", "vision page renderer omitted a requested page", nil)
	}
	return images, nil
}
