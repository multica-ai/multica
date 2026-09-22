package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ProtocolOpenAI = "openai_compatible"
	ProtocolCohere = "cohere_compatible"
)

// CompatibleConfig describes one workspace-owned provider connection. It is
// intentionally independent from the deployment LLM client: knowledge-base
// providers are selected per workspace and their credentials never enter the
// process-wide MULTICA_LLM configuration.
type CompatibleConfig struct {
	BaseURL    string
	APIKey     string
	Protocol   string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// CompatibleClient is the narrow server-side transport for user-configured
// OpenAI-compatible and Cohere-compatible providers. Callers receive typed
// results and never need to import an SDK or construct an authenticated HTTP
// request themselves.
type CompatibleClient struct {
	baseURL    string
	apiKey     string
	protocol   string
	httpClient *http.Client
	timeout    time.Duration
}

// HTTPError preserves the upstream status without retaining a full response
// body. The short message is useful for diagnostics, while the caller owns
// the public error mapping so provider details are not exposed to end users.
type HTTPError struct {
	StatusCode int
	Message    string
	// RetryAfter is the parsed Retry-After value supplied by the upstream.
	// RetryAfterSet distinguishes a valid zero-second response from a missing
	// header, which matters to durable job scheduling.
	RetryAfter    time.Duration
	RetryAfterSet bool
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "llm: upstream HTTP error"
	}
	return fmt.Sprintf("llm: upstream returned HTTP %d: %s", e.StatusCode, e.Message)
}

func NewCompatibleClient(cfg CompatibleConfig) *CompatibleClient {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" {
		protocol = ProtocolOpenAI
	}
	// Provider credentials are user-owned secrets. Do not let a compatible
	// provider redirect an authenticated request to another origin, even when
	// the custom HTTP client would otherwise follow redirects automatically.
	clientCopy := *client
	previousRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameURLOrigin(via[0].URL, req.URL) {
			return http.ErrUseLastResponse
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	return &CompatibleClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:     strings.TrimSpace(cfg.APIKey),
		protocol:   protocol,
		httpClient: &clientCopy,
		timeout:    timeout,
	}
}

func sameURLOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	leftPort := left.Port()
	if leftPort == "" {
		if strings.EqualFold(left.Scheme, "http") {
			leftPort = "80"
		} else if strings.EqualFold(left.Scheme, "https") {
			leftPort = "443"
		}
	}
	rightPort := right.Port()
	if rightPort == "" {
		if strings.EqualFold(right.Scheme, "http") {
			rightPort = "80"
		} else if strings.EqualFold(right.Scheme, "https") {
			rightPort = "443"
		}
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) && leftPort == rightPort
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if when.Before(now) {
		return 0, true
	}
	return when.Sub(now), true
}

func (c *CompatibleClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *CompatibleClient) endpoint(path string) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	return c.baseURL + "/" + strings.TrimLeft(path, "/"), nil
}

func (c *CompatibleClient) doJSON(ctx context.Context, path string, requestBody any, responseBody any) error {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return err
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("llm: encode compatible request: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llm: build compatible request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		if c.protocol == ProtocolCohere {
			req.Header.Set("X-API-Key", c.apiKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("llm: compatible upstream timeout: %w", context.DeadlineExceeded)
		}
		return fmt.Errorf("llm: compatible upstream request: %w", err)
	}
	defer resp.Body.Close()
	responseBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return fmt.Errorf("llm: read compatible response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBytes))
		if len(message) > 1000 {
			message = message[:1000]
		}
		retryAfter, retryAfterSet := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return &HTTPError{StatusCode: resp.StatusCode, Message: message, RetryAfter: retryAfter, RetryAfterSet: retryAfterSet}
	}
	if responseBody == nil || len(bytes.TrimSpace(responseBytes)) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBytes, responseBody); err != nil {
		return fmt.Errorf("llm: decode compatible response: %w", err)
	}
	return nil
}

type compatibleChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// VisionImage is a caller-owned image that is embedded as a data URL in an
// OpenAI-compatible multimodal chat request. It deliberately carries bytes,
// not a public object URL: knowledge source images stay inside the server to
// provider boundary and are never exposed as browser-readable storage URLs.
type VisionImage struct {
	MIMEType string
	Data     []byte
}

const (
	maxVisionImages     = 4
	maxVisionImageBytes = 8 << 20
)

func encodeVisionImages(images []VisionImage) ([]map[string]any, error) {
	if len(images) == 0 {
		return nil, errors.New("llm: vision request requires at least one image")
	}
	if len(images) > maxVisionImages {
		return nil, fmt.Errorf("llm: vision request contains more than %d images", maxVisionImages)
	}
	parts := make([]map[string]any, 0, len(images))
	for _, image := range images {
		mimeType := strings.ToLower(strings.TrimSpace(strings.SplitN(image.MIMEType, ";", 2)[0]))
		switch mimeType {
		case "image/png", "image/jpeg", "image/webp":
		default:
			return nil, fmt.Errorf("llm: unsupported vision image MIME type %q", image.MIMEType)
		}
		if len(image.Data) == 0 || len(image.Data) > maxVisionImageBytes {
			return nil, fmt.Errorf("llm: vision image must be between 1 byte and %d bytes", maxVisionImageBytes)
		}
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(image.Data),
			},
		})
	}
	return parts, nil
}

func (c *CompatibleClient) generateJSONWithMessages(ctx context.Context, model string, messages []map[string]any, maxTokens int) (string, error) {
	payload := map[string]any{
		"model":           strings.TrimSpace(model),
		"messages":        messages,
		"response_format": map[string]string{"type": "json_object"},
	}
	if maxTokens > 0 {
		payload["max_completion_tokens"] = maxTokens
	}
	var response compatibleChatResponse
	if err := c.doJSON(ctx, "chat/completions", payload, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 {
		return "", errors.New("llm: compatible upstream returned no choices")
	}
	content := response.Choices[0].Message.Content
	if len(content) == 0 || string(content) == "null" {
		return "", errors.New("llm: compatible upstream returned empty content")
	}
	var text string
	if err := json.Unmarshal(content, &text); err != nil {
		return "", fmt.Errorf("llm: compatible content is not text: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("llm: compatible upstream returned empty content")
	}
	return text, nil
}

// GenerateJSON calls /chat/completions and returns the assistant content. The
// caller validates the returned schema; json_object mode is only an upstream
// syntax hint, not a substitute for validation.
func (c *CompatibleClient) GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	if c.protocol != ProtocolOpenAI {
		return "", fmt.Errorf("llm: JSON generation requires %s protocol", ProtocolOpenAI)
	}
	messages := make([]map[string]any, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	messages = append(messages, map[string]any{"role": "user", "content": userPrompt})
	return c.generateJSONWithMessages(ctx, model, messages, maxTokens)
}

// GenerateVisionJSON sends a bounded page-image set plus a text instruction to
// an OpenAI-compatible multimodal model. The response contract is identical to
// GenerateJSON so callers still perform their own strict schema and evidence
// validation before persisting anything.
func (c *CompatibleClient) GenerateVisionJSON(ctx context.Context, model, systemPrompt, userPrompt string, images []VisionImage, maxTokens int) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	if c.protocol != ProtocolOpenAI {
		return "", fmt.Errorf("llm: vision requires %s protocol", ProtocolOpenAI)
	}
	imageParts, err := encodeVisionImages(images)
	if err != nil {
		return "", err
	}
	content := make([]map[string]any, 0, len(imageParts)+1)
	content = append(content, map[string]any{"type": "text", "text": userPrompt})
	content = append(content, imageParts...)
	messages := make([]map[string]any, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	messages = append(messages, map[string]any{"role": "user", "content": content})
	return c.generateJSONWithMessages(ctx, model, messages, maxTokens)
}

type compatibleEmbeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

type CompatibleModel struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type compatibleModelsResponse struct {
	Data []CompatibleModel `json:"data"`
}

// Models calls the optional OpenAI-compatible /models endpoint. Failure to
// enumerate models should not prevent a user from entering a model id by
// hand; the settings handler treats this as a best-effort discovery method.
func (c *CompatibleClient) Models(ctx context.Context) ([]CompatibleModel, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	endpoint, err := c.endpoint("models")
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("llm: build compatible models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		if c.protocol == ProtocolCohere {
			req.Header.Set("X-API-Key", c.apiKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: compatible models request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("llm: read compatible models response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 1000 {
			message = message[:1000]
		}
		retryAfter, retryAfterSet := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return nil, &HTTPError{StatusCode: resp.StatusCode, Message: message, RetryAfter: retryAfter, RetryAfterSet: retryAfterSet}
	}
	var parsed compatibleModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("llm: decode compatible models response: %w", err)
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].ID < parsed.Data[j].ID })
	return parsed.Data, nil
}

// Embeddings calls /embeddings and validates response ordering. A provider
// that omits an index or returns duplicates is incompatible with a batch and
// must not be allowed to associate one text with another text's vector.
func (c *CompatibleClient) Embeddings(ctx context.Context, model string, inputs []string) ([][]float64, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if c.protocol != ProtocolOpenAI {
		return nil, fmt.Errorf("llm: embeddings require %s protocol", ProtocolOpenAI)
	}
	if len(inputs) == 0 {
		return [][]float64{}, nil
	}
	payload := map[string]any{"model": strings.TrimSpace(model), "input": inputs}
	var response compatibleEmbeddingResponse
	if err := c.doJSON(ctx, "embeddings", payload, &response); err != nil {
		return nil, err
	}
	if len(response.Data) != len(inputs) {
		return nil, fmt.Errorf("llm: embedding response count %d, expected %d", len(response.Data), len(inputs))
	}
	sort.Slice(response.Data, func(i, j int) bool { return response.Data[i].Index < response.Data[j].Index })
	out := make([][]float64, len(inputs))
	for i, item := range response.Data {
		if item.Index != i || len(item.Embedding) == 0 {
			return nil, fmt.Errorf("llm: invalid embedding response index %d at position %d", item.Index, i)
		}
		for _, value := range item.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, errors.New("llm: embedding response contains a non-finite value")
			}
		}
		var normSquared float64
		for _, value := range item.Embedding {
			normSquared += value * value
		}
		if normSquared == 0 {
			return nil, errors.New("llm: embedding response contains a zero vector")
		}
		out[i] = item.Embedding
	}
	return out, nil
}

type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type compatibleRerankResponse struct {
	Results []RerankResult `json:"results"`
}

// Rerank calls the Cohere-compatible /rerank endpoint. It does not compare
// scores with keyword/vector scores; callers use the returned indexes only to
// order an already validated candidate set.
func (c *CompatibleClient) Rerank(ctx context.Context, model, query string, documents []string, topN int) ([]RerankResult, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if c.protocol != ProtocolCohere {
		return nil, fmt.Errorf("llm: rerank requires %s protocol", ProtocolCohere)
	}
	payload := map[string]any{
		"model":     strings.TrimSpace(model),
		"query":     query,
		"documents": documents,
	}
	if topN > 0 {
		payload["top_n"] = topN
	}
	var response compatibleRerankResponse
	if err := c.doJSON(ctx, "rerank", payload, &response); err != nil {
		return nil, err
	}
	seen := make(map[int]struct{}, len(response.Results))
	for _, result := range response.Results {
		if result.Index < 0 || result.Index >= len(documents) || math.IsNaN(result.RelevanceScore) || math.IsInf(result.RelevanceScore, 0) {
			return nil, fmt.Errorf("llm: invalid rerank result index or score")
		}
		if _, ok := seen[result.Index]; ok {
			return nil, fmt.Errorf("llm: duplicate rerank result index %d", result.Index)
		}
		seen[result.Index] = struct{}{}
	}
	return response.Results, nil
}
