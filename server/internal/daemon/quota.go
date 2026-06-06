package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// handleQuotaCheck probes the provider's rate-limit API and reports the
// result back to the server. Runs in a goroutine; all errors are reported
// rather than returned.
func (d *Daemon) handleQuotaCheck(ctx context.Context, rt Runtime, requestID string) {
	d.logger.Info("quota check requested", "runtime_id", rt.ID, "request_id", requestID, "provider", rt.Provider)

	result := d.probeProviderQuota(rt.Provider)
	result["provider"] = rt.Provider

	status := "completed"
	if errMsg, _ := result["error"].(string); errMsg != "" && result["rate_requests"] == nil && result["rate_tokens"] == nil && result["credits_limit"] == nil {
		status = "failed"
	}

	payload := map[string]any{
		"status": status,
		"result": result,
	}
	if status == "failed" {
		payload["error"] = result["error"]
	}

	d.reportQuotaCheckResult(ctx, rt, requestID, payload)
}

func (d *Daemon) probeProviderQuota(provider string) map[string]any {
	switch provider {
	case "claude":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return map[string]any{"error": "ANTHROPIC_API_KEY not set"}
		}
		return probeRateLimitHeaders(
			"https://api.anthropic.com/v1/models",
			map[string]string{
				"x-api-key":         key,
				"anthropic-version": "2023-06-01",
			},
		)

	case "codex":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return map[string]any{"error": "OPENAI_API_KEY not set"}
		}
		return probeRateLimitHeaders(
			"https://api.openai.com/v1/models",
			map[string]string{"Authorization": "Bearer " + key},
		)

	case "droid":
		key := os.Getenv("FACTORY_API_KEY")
		if key == "" {
			// Try underlying provider keys as a fallback.
			if ak := os.Getenv("ANTHROPIC_API_KEY"); ak != "" {
				res := probeRateLimitHeaders(
					"https://api.anthropic.com/v1/models",
					map[string]string{"x-api-key": ak, "anthropic-version": "2023-06-01"},
				)
				res["provider_note"] = "droid/anthropic"
				return res
			}
			if ok := os.Getenv("OPENAI_API_KEY"); ok != "" {
				res := probeRateLimitHeaders(
					"https://api.openai.com/v1/models",
					map[string]string{"Authorization": "Bearer " + ok},
				)
				res["provider_note"] = "droid/openai"
				return res
			}
			return map[string]any{"error": "FACTORY_API_KEY not set"}
		}
		return probeFactoryCreditsLimit(key)

	default: // cursor, antigravity, and others
		return map[string]any{"error": "not_supported"}
	}
}

// probeRateLimitHeaders makes a lightweight GET request and extracts the
// standard x-ratelimit-* headers used by both Anthropic and OpenAI.
func probeRateLimitHeaders(url string, headers map[string]string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("failed to build request: %v", err)}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("request failed: %v", err)}
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	// Accept 200 and 429 — both carry rate-limit headers.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusTooManyRequests {
		return map[string]any{"error": fmt.Sprintf("unexpected status %d", resp.StatusCode)}
	}

	result := map[string]any{}
	if w := parseQuotaWindow(resp.Header,
		"x-ratelimit-limit-requests",
		"x-ratelimit-remaining-requests",
		"x-ratelimit-reset-requests",
	); w != nil {
		result["rate_requests"] = w
	}
	if w := parseQuotaWindow(resp.Header,
		"x-ratelimit-limit-tokens",
		"x-ratelimit-remaining-tokens",
		"x-ratelimit-reset-tokens",
	); w != nil {
		result["rate_tokens"] = w
	}

	if result["rate_requests"] == nil && result["rate_tokens"] == nil {
		result["error"] = "provider returned no rate-limit headers"
	}
	return result
}

// parseQuotaWindow reads three related rate-limit headers and returns a
// structured window, or nil if the limit header is absent.
func parseQuotaWindow(h http.Header, limitKey, remainingKey, resetKey string) map[string]any {
	limitStr := h.Get(limitKey)
	if limitStr == "" {
		return nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(limitStr))
	if err != nil {
		return nil
	}
	remaining, _ := strconv.Atoi(strings.TrimSpace(h.Get(remainingKey)))

	w := map[string]any{
		"limit":     limit,
		"remaining": remaining,
	}
	if resetStr := strings.TrimSpace(h.Get(resetKey)); resetStr != "" {
		if t, err := time.Parse(time.RFC3339, resetStr); err == nil {
			w["resets_at"] = t
		} else {
			// Some providers use a relative duration like "1m30s" or plain seconds.
			if d, err := time.ParseDuration(resetStr); err == nil {
				w["resets_at"] = time.Now().Add(d)
			}
		}
	}
	return w
}

// probeFactoryCreditsLimit calls the Factory.ai public API to fetch the
// organisation's per-user credit cap.
func probeFactoryCreditsLimit(apiKey string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.factory.ai/api/v0/organization/usage/limits/global", nil)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("failed to build request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("request failed: %v", err)}
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return map[string]any{"error": "FACTORY_API_KEY is invalid or expired"}
	}
	if resp.StatusCode != http.StatusOK {
		return map[string]any{"error": fmt.Sprintf("unexpected status %d", resp.StatusCode)}
	}

	var body struct {
		Limit *int `json:"limit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return map[string]any{"error": fmt.Sprintf("failed to decode response: %v", err)}
	}

	return map[string]any{"credits_limit": body.Limit}
}

// reportQuotaCheckResult delivers the quota probe result with retry.
func (d *Daemon) reportQuotaCheckResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "quota_check", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportQuotaCheckResult(ctx, rt.ID, requestID, payload)
	})
}
