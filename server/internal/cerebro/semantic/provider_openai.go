package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider is an OpenAI-compatible `/v1/embeddings` client. The same
// shape works against the OpenAI API, Azure OpenAI, the Firtal AI Gateway,
// or any local server (Ollama, llama.cpp, etc.) that mimics the endpoint —
// the wire format is the de-facto standard for embeddings.
//
// The HTTP client is intentionally small: one endpoint, one model, fixed
// dimension. Anything more nuanced belongs in a different provider.
type OpenAIProvider struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
	// dimensions is the value sent in the request body. OpenAI's
	// text-embedding-3-* models honour this and return the truncated MRL
	// projection; classical providers ignore the field and return their
	// native dim, which we then truncate ourselves.
	dimensions int
}

// NewOpenAIProvider constructs an OpenAI-compatible embedding client.
// endpoint should be the full URL incl. /v1/embeddings — same convention as
// the firtal-gateway runtime config so operators do not have to learn a new
// shape.
func NewOpenAIProvider(endpoint, apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		endpoint:   endpoint,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		dimensions: EmbeddingDim,
	}
}

func (p *OpenAIProvider) Model() string { return "openai:" + p.model + ":v1" }

type openAIEmbedRequest struct {
	Input          string `json:"input"`
	Model          string `json:"model"`
	Dimensions     int    `json:"dimensions,omitempty"`
	EncodingFormat string `json:"encoding_format,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		// An embedding request with empty input is wasted budget and many
		// providers reject it outright. Return a neutral zero-vector with
		// a single seeded bucket — the same shape as the deterministic
		// provider's empty-string case.
		vec := make([]float32, EmbeddingDim)
		vec[0] = 1
		return vec, nil
	}
	body, err := json.Marshal(openAIEmbedRequest{
		Input:          text,
		Model:          p.model,
		Dimensions:     p.dimensions,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embed request: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var decoded openAIEmbedResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return nil, errors.New("embed provider error: " + decoded.Error.Message)
	}
	if len(decoded.Data) == 0 || len(decoded.Data[0].Embedding) == 0 {
		return nil, errors.New("embed response: empty data")
	}
	vec := decoded.Data[0].Embedding
	if len(vec) > EmbeddingDim {
		vec = vec[:EmbeddingDim]
	} else if len(vec) < EmbeddingDim {
		padded := make([]float32, EmbeddingDim)
		copy(padded, vec)
		vec = padded
	}
	return normalise(vec), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
