package ghuser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiVersion        = "2022-11-28"
	apiResponseLimit  = 8 << 20
	maxMessageLength  = 500
	defaultRetryAfter = time.Minute
)

// APIError is a failed GitHub API call. Message is GitHub's own explanation
// and is safe to show the user; it never contains a token.
type APIError struct {
	// Status is the HTTP status. GraphQL errors arrive with 200.
	Status int
	// GraphQLType is the first GraphQL error's type (FORBIDDEN, NOT_FOUND,
	// UNPROCESSABLE, ...), empty for REST.
	GraphQLType string
	Message     string
	RateLimited bool
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	if e.GraphQLType != "" {
		return fmt.Sprintf("github graphql %s: %s", e.GraphQLType, e.Message)
	}
	return fmt.Sprintf("github status %d: %s", e.Status, e.Message)
}

// REST sends one request to the REST API as the token's user. body is
// JSON-encoded when non-nil; out is decoded from a 2xx response when non-nil.
func (s *Service) REST(ctx context.Context, accessToken, method, path string, body, out any) error {
	data, err := s.do(ctx, accessToken, method, path, body, "application/vnd.github+json")
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("ghuser: malformed github response")
	}
	return nil
}

// GraphQL runs one query or mutation as the token's user. Any entry in the
// response's `errors` array fails the call.
func (s *Service) GraphQL(ctx context.Context, accessToken, query string, variables map[string]any, out any) error {
	// mergeStateStatus is still served under the merge-info preview.
	data, err := s.do(ctx, accessToken, http.MethodPost, "/graphql",
		map[string]any{"query": query, "variables": variables},
		"application/vnd.github.merge-info-preview+json")
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return errors.New("ghuser: malformed github graphql response")
	}
	if len(envelope.Errors) > 0 {
		first := envelope.Errors[0]
		return &APIError{
			Status:      http.StatusOK,
			GraphQLType: first.Type,
			Message:     truncateMessage(first.Message),
			RateLimited: first.Type == "RATE_LIMITED",
			RetryAfter:  defaultRetryAfter,
		}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return errors.New("ghuser: malformed github graphql data")
	}
	return nil
}

func (s *Service) do(ctx context.Context, accessToken, method, path string, body any, accept string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.apiBase+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Transport errors embed only the URL, which carries no credential.
		return nil, fmt.Errorf("ghuser: github request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, apiResponseLimit+1))
	if err != nil {
		return nil, errors.New("ghuser: read github response failed")
	}
	if len(data) > apiResponseLimit {
		return nil, errors.New("ghuser: github response exceeds size limit")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, nil
	}
	apiErr := &APIError{Status: resp.StatusCode, Message: restErrorMessage(data, resp.StatusCode)}
	if isRateLimited(resp, apiErr.Message) {
		apiErr.RateLimited = true
		apiErr.RetryAfter = retryAfter(resp, s.now())
	}
	return nil, apiErr
}

// restErrorMessage joins GitHub's top-level message with the first detailed
// error, which is where reasons such as a failed status check live.
func restErrorMessage(data []byte, status int) string {
	var parsed struct {
		Message string            `json:"message"`
		Errors  []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(data, &parsed) != nil || parsed.Message == "" {
		return http.StatusText(status)
	}
	msg := parsed.Message
	if len(parsed.Errors) > 0 {
		var detail string
		var obj struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(parsed.Errors[0], &detail) != nil {
			if json.Unmarshal(parsed.Errors[0], &obj) == nil {
				detail = obj.Message
			}
		}
		if detail != "" && !strings.Contains(msg, detail) {
			msg += ": " + detail
		}
	}
	return truncateMessage(msg)
}

func truncateMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > maxMessageLength {
		return msg[:maxMessageLength]
	}
	return msg
}

func isRateLimited(resp *http.Response, message string) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
		return true
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "rate limit") || strings.Contains(lower, "abuse detection")
}

func retryAfter(resp *http.Response, now time.Time) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			if d := time.Unix(unix, 0).Sub(now); d > 0 {
				return d
			}
		}
	}
	return defaultRetryAfter
}
