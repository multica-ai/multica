package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

// RefreshResult is the parsed response from /v1/oauth/token.
type RefreshResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// PermanentError indicates a 4xx response or any failure AFTER we received a
// 2xx. The caller MUST NOT retry — either the server rejected our request as
// invalid (no amount of retry helps), or the server already rotated our
// refresh_token but we lost the new one to a read/parse error.
type PermanentError struct {
	StatusCode int
	Body       string
	Wrapped    error
}

func (e *PermanentError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("permanent OAuth error: %v", e.Wrapped)
	}
	return fmt.Sprintf("permanent OAuth error: HTTP %d: %s", e.StatusCode, e.Body)
}

func (e *PermanentError) Unwrap() error { return e.Wrapped }

// TransientError indicates retry-exhausted pre-response failures (network,
// DNS, 5xx, 429). The caller keeps serving the cached access_token if it's
// still non-expired; the next refresh tick tries again.
type TransientError struct {
	Attempts int
	LastErr  error
}

func (e *TransientError) Error() string {
	return fmt.Sprintf("transient OAuth error after %d attempts: %v", e.Attempts, e.LastErr)
}

func (e *TransientError) Unwrap() error { return e.LastErr }

// OAuthClient is a single-purpose HTTP client for the production OAuth
// token endpoint. Public fields so tests can override Endpoint/ClientID/
// retry knobs without package-globals.
type OAuthClient struct {
	Endpoint      string
	ClientID      string
	VersionHeader string
	UserAgent     string

	HTTP        *http.Client
	MaxAttempts int           // total tries including the first; default 4
	BackoffBase time.Duration // first-retry jittered in [0, base]; default 500ms
}

// DefaultOAuthClient wires the broker's runtime client to the embedded constants.
func DefaultOAuthClient() *OAuthClient {
	return &OAuthClient{
		Endpoint:      Constants.Endpoint,
		ClientID:      Constants.ClientID,
		VersionHeader: Constants.VersionHeader,
		UserAgent:     "multica-claude-broker/" + Constants.ClaudeVersion,
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		MaxAttempts:   4,
		BackoffBase:   500 * time.Millisecond,
	}
}

// Refresh exchanges a refresh_token for a fresh access_token (+ rotated
// refresh_token). Retries transient pre-response failures with exponential-
// with-full-jitter backoff; never retries past a 2xx; never retries on 4xx.
// Returns either *PermanentError or *TransientError on failure (distinguish
// via errors.As).
func (c *OAuthClient) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	if refreshToken == "" {
		return nil, &PermanentError{Wrapped: errors.New("refresh_token is empty")}
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.ClientID,
	})

	maxAttempts := c.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastTransient error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepWithJitter(ctx, c.BackoffBase, attempt-1); err != nil {
				return nil, &TransientError{Attempts: attempt - 1, LastErr: err}
			}
		}

		result, kind, err := c.doOnce(ctx, body)
		switch kind {
		case outcomeOK:
			return result, nil
		case outcomePermanent:
			var perm *PermanentError
			if errors.As(err, &perm) {
				return nil, perm
			}
			return nil, &PermanentError{Wrapped: err}
		case outcomeTransient:
			lastTransient = err
			continue
		}
	}
	return nil, &TransientError{Attempts: maxAttempts, LastErr: lastTransient}
}

type outcomeKind int

const (
	outcomeOK outcomeKind = iota
	outcomePermanent
	outcomeTransient
)

// doOnce performs exactly one POST. Classification:
//   - Pre-response error (network, DNS, ctx canceled mid-flight): transient
//   - 2xx + read/parse error: permanent (refresh_token rotated server-side, we lost the new one)
//   - 2xx + well-formed body: ok
//   - 4xx: permanent (invalid_grant etc.)
//   - 5xx, 429: transient
func (c *OAuthClient) doOnce(ctx context.Context, body []byte) (*RefreshResult, outcomeKind, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, outcomePermanent, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", c.VersionHeader)
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, outcomeTransient, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	// From here on the server has accepted the request. Any failure means we
	// have a possibly-rotated refresh_token we can never recover. Permanent.
	raw, readErr := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if readErr != nil {
			return nil, outcomePermanent, fmt.Errorf("read 2xx body: %w (refresh_token may have been rotated server-side)", readErr)
		}
		var out RefreshResult
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, outcomePermanent, fmt.Errorf("decode 2xx body: %w (raw: %s)", err, string(raw))
		}
		if out.AccessToken == "" {
			return nil, outcomePermanent, fmt.Errorf("2xx response missing access_token (raw: %s)", string(raw))
		}
		return &out, outcomeOK, nil

	case resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600):
		return nil, outcomeTransient, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))

	default: // 4xx and any unexpected non-2xx, non-5xx
		return nil, outcomePermanent, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
}

// sleepWithJitter sleeps for a random duration in [0, base * 2^attempt],
// capped at 30s. Respects ctx cancellation.
func sleepWithJitter(ctx context.Context, base time.Duration, attempt int) error {
	const capDur = 30 * time.Second
	exp := base << attempt
	if exp <= 0 || exp > capDur {
		exp = capDur
	}
	d := time.Duration(rand.Int64N(int64(exp)))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
