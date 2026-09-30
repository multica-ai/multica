package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	responseEngineFinalizePath       = "/internal/v1/response/finalize"
	responseEngineMaxResponseBytes   = 64 << 10
	responseEngineResponseReadBuffer = responseEngineMaxResponseBytes + 1
)

type HTTPTaskResponseFinalizerConfig struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type HTTPTaskResponseFinalizer struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewHTTPTaskResponseFinalizer(cfg HTTPTaskResponseFinalizerConfig) (*HTTPTaskResponseFinalizer, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, errors.New("response engine base URL is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, errors.New("response engine service token is required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("response engine timeout must be positive")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse response engine base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("response engine base URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("response engine base URL must include a host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + responseEngineFinalizePath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return &HTTPTaskResponseFinalizer{
		endpoint: parsed.String(),
		token:    token,
		client:   &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (c *HTTPTaskResponseFinalizer) FinalizeTaskCompletion(
	ctx context.Context,
	input TaskResponseFinalizationInput,
) (TaskResponseFinalizationResult, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return TaskResponseFinalizationResult{}, fmt.Errorf("encode response finalization request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return TaskResponseFinalizationResult{}, fmt.Errorf("build response finalization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return TaskResponseFinalizationResult{}, fmt.Errorf("response finalization request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return TaskResponseFinalizationResult{}, fmt.Errorf(
			"response finalization returned HTTP %d",
			resp.StatusCode,
		)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseEngineResponseReadBuffer))
	if err != nil {
		return TaskResponseFinalizationResult{}, fmt.Errorf("read response finalization response: %w", err)
	}
	if len(raw) > responseEngineMaxResponseBytes {
		return TaskResponseFinalizationResult{}, errors.New("response finalization response exceeded size limit")
	}

	var result TaskResponseFinalizationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return TaskResponseFinalizationResult{}, fmt.Errorf("decode response finalization response: %w", err)
	}
	return result, nil
}
