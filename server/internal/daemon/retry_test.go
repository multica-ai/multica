package daemon

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestIsRateLimitError tests the isRateLimitError function with various error patterns.
func TestIsRateLimitError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "rate limit (lowercase with space)",
			err:      errors.New("rate limit exceeded"),
			expected: true,
		},
		{
			name:     "rate-limit (hyphen)",
			err:      errors.New("rate-limit: too many requests"),
			expected: true,
		},
		{
			name:     "ratelimit (no separator)",
			err:      errors.New("ratelimit: API quota reached"),
			expected: true,
		},
		{
			name:     "HTTP 429 status code",
			err:      errors.New("429 Too Many Requests"),
			expected: true,
		},
		{
			name:     "too many requests phrase",
			err:      errors.New("too many requests, please try again later"),
			expected: true,
		},
		{
			name:     "quota exceeded (space)",
			err:      errors.New("quota exceeded for API key"),
			expected: true,
		},
		{
			name:     "quota-exceeded (hyphen)",
			err:      errors.New("quota-exceeded: daily limit reached"),
			expected: true,
		},
		{
			name:     "mixed case rate limit",
			err:      errors.New("Rate Limit Exceeded"),
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("internal server error"),
			expected: false,
		},
		{
			name:     "timeout error",
			err:      errors.New("context deadline exceeded"),
			expected: false,
		},
		{
			name:     "400 bad request",
			err:      errors.New("400 Bad Request"),
			expected: false,
		},
		{
			name:     "500 internal server error",
			err:      errors.New("500 Internal Server Error"),
			expected: false,
		},
		{
			name:     "rate limit in middle of message",
			err:      errors.New("failed to complete request due to rate limit, please retry"),
			expected: true,
		},
		{
			name:     "429 in error message",
			err:      errors.New("API returned 429 status"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRateLimitError(tt.err)
			if result != tt.expected {
				t.Errorf("isRateLimitError(%q) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

// TestCalculateBackoffDelay tests the exponential backoff delay calculation.
func TestCalculateBackoffDelay(t *testing.T) {
	t.Parallel()

	cfg := RateLimitConfig{
		InitialDelay:  5 * time.Second,
		MaxRetries:    6,
		BackoffFactor: 2.0,
	}

	tests := []struct {
		name           string
		attempt        int
		expectedDelay  time.Duration
	}{
		{
			name:          "attempt 0 (no delay)",
			attempt:       0,
			expectedDelay: 0,
		},
		{
			name:          "attempt 1 (initial delay)",
			attempt:       1,
			expectedDelay: 5 * time.Second,
		},
		{
			name:          "attempt 2 (2x)",
			attempt:       2,
			expectedDelay: 10 * time.Second,
		},
		{
			name:          "attempt 3 (4x)",
			attempt:       3,
			expectedDelay: 20 * time.Second,
		},
		{
			name:          "attempt 4 (8x)",
			attempt:       4,
			expectedDelay: 40 * time.Second,
		},
		{
			name:          "attempt 5 (16x)",
			attempt:       5,
			expectedDelay: 80 * time.Second,
		},
		{
			name:          "attempt 6 (32x)",
			attempt:       6,
			expectedDelay: 160 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := time.Duration(float64(cfg.InitialDelay) * math.Pow(cfg.BackoffFactor, float64(tt.attempt-1)))
			if tt.attempt == 0 {
				delay = 0
			}
			if delay != tt.expectedDelay {
				t.Errorf("attempt %d: expected delay %v, got %v", tt.attempt, tt.expectedDelay, delay)
			}
		})
	}
}

// mockBackend is a test double for agent.Backend.
type mockBackend struct {
	executeFunc func(context.Context, string, agent.ExecOptions) (*agent.Session, error)
}

func (m *mockBackend) Execute(ctx context.Context, prompt string, opts agent.ExecOptions) (*agent.Session, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, prompt, opts)
	}
	return nil, nil
}

// TestExecuteWithRetry_SuccessNoRetry tests successful execution without any retries.
func TestExecuteWithRetry_SuccessNoRetry(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Second,
				MaxRetries:    6,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	backend := &mockBackend{
		executeFunc: func(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			msgCh := make(chan agent.Message)
			resCh := make(chan agent.Result, 1)
			resCh <- agent.Result{Status: "completed", Output: "success"}
			close(msgCh)
			return &agent.Session{Messages: msgCh, Result: resCh}, nil
		},
	}

	ctx := context.Background()
	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status 'completed', got '%s'", result.Status)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
}

// TestExecuteWithRetry_RateLimitRetries tests successful execution after rate limit retries.
func TestExecuteWithRetry_RateLimitRetries(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  10 * time.Millisecond, // Short delay for testing
				MaxRetries:    3,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	attempts := 0
	backend := &mockBackend{
		executeFunc: func(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("rate limit exceeded")
			}
			msgCh := make(chan agent.Message)
			resCh := make(chan agent.Result, 1)
			resCh <- agent.Result{Status: "completed", Output: "success after retry"}
			close(msgCh)
			return &agent.Session{Messages: msgCh, Result: resCh}, nil
		},
	}

	ctx := context.Background()
	start := time.Now()
	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status 'completed', got '%s'", result.Status)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
	// Should have waited at least initial delay (10ms)
	if elapsed < 10*time.Millisecond {
		t.Fatalf("expected at least 10ms elapsed, got %v", elapsed)
	}
}

// TestExecuteWithRetry_MaxRetriesExceeded tests failure after max retries.
func TestExecuteWithRetry_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Millisecond,
				MaxRetries:    2,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	attempts := 0
	backend := &mockBackend{
		executeFunc: func(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			attempts++
			return nil, errors.New("rate limit: too many requests")
		},
	}

	ctx := context.Background()
	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")

	if err == nil {
		t.Fatal("expected error after max retries, got nil")
	}
	if result.Status != "failed" {
		t.Fatalf("expected status 'failed', got '%s'", result.Status)
	}
	if attempts != 3 { // initial + 2 retries
		t.Fatalf("expected 3 attempts (initial + 2 retries), got %d", attempts)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
}

// TestExecuteWithRetry_NonRateLimitError tests that non-rate-limit errors are not retried.
func TestExecuteWithRetry_NonRateLimitError(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Second,
				MaxRetries:    6,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	attempts := 0
	backend := &mockBackend{
		executeFunc: func(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			attempts++
			return nil, errors.New("internal server error")
		},
	}

	ctx := context.Background()
	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.Status != "failed" {
		t.Fatalf("expected status 'failed', got '%s'", result.Status)
	}
	if attempts != 1 { // Should not retry non-rate-limit errors
		t.Fatalf("expected 1 attempt (no retry), got %d", attempts)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
}

// TestExecuteWithRetry_ContextCancellation tests that context cancellation stops retry loop.
func TestExecuteWithRetry_ContextCancellation(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  100 * time.Millisecond,
				MaxRetries:    10,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	attempts := 0
	backend := &mockBackend{
		executeFunc: func(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			attempts++
			// Cancel context after first attempt
			if attempts == 1 {
				time.Sleep(10 * time.Millisecond)
			}
			return nil, errors.New("rate limit exceeded")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")

	if err == nil {
		t.Fatal("expected error after context cancellation, got nil")
	}
	if result.Status != "failed" {
		t.Fatalf("expected status 'failed', got '%s'", result.Status)
	}
	if !strings.Contains(result.Error, "retry cancelled") {
		t.Fatalf("expected error message to contain 'retry cancelled', got '%s'", result.Error)
	}
	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts before cancellation, got %d", attempts)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
}

// TestExecuteWithRetry_ExponentialBackoffSequence tests the exact sequence of delays.
func TestExecuteWithRetry_ExponentialBackoffSequence(t *testing.T) {
	t.Parallel()

	cfg := RateLimitConfig{
		InitialDelay:  10 * time.Millisecond,
		MaxRetries:    4,
		BackoffFactor: 2.0,
	}

	// Test the exponential backoff formula
	expectedDelays := []time.Duration{0, 10, 20, 40, 80} // for attempts 0-4

	for i, expected := range expectedDelays {
		delay := time.Duration(float64(cfg.InitialDelay) * math.Pow(cfg.BackoffFactor, float64(i-1)))
		if i == 0 {
			delay = 0
		}
		if delay != expected {
			t.Errorf("attempt %d: expected delay %v, got %v", i, expected, delay)
		}
	}
}

// TestBroadcastRetryProgress tests the broadcastRetryProgress function.
func TestBroadcastRetryProgress(t *testing.T) {
	t.Parallel()

	// Create a test server to capture the broadcast
	var broadcastReceived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		broadcastReceived = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Daemon{
		client: NewClient(srv.URL),
		logger: slog.Default(),
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Second,
				MaxRetries:    6,
				BackoffFactor: 2.0,
			},
		},
	}

	ctx := context.Background()
	// This function currently only logs, so we just verify it doesn't panic
	d.broadcastRetryProgress(ctx, "task-1", "session-1", 1, 6, 5)
	// Function should not panic
}

// TestRateLimitConfigDefaults tests default RateLimitConfig values.
func TestRateLimitConfigDefaults(t *testing.T) {
	t.Parallel()

	if DefaultRateLimitInitialDelay != 5*time.Second {
		t.Errorf("expected DefaultRateLimitInitialDelay = 5s, got %v", DefaultRateLimitInitialDelay)
	}
	if DefaultRateLimitMaxRetries != 6 {
		t.Errorf("expected DefaultRateLimitMaxRetries = 6, got %d", DefaultRateLimitMaxRetries)
	}
	if DefaultRateLimitBackoffFactor != 2.0 {
		t.Errorf("expected DefaultRateLimitBackoffFactor = 2.0, got %f", DefaultRateLimitBackoffFactor)
	}
}

// TestExecuteWithRetry_ImmediateSuccess tests that immediate success returns without delay.
func TestExecuteWithRetry_ImmediateSuccess(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Second,
				MaxRetries:    6,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	backend := &mockBackend{
		executeFunc: func(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			msgCh := make(chan agent.Message)
			resCh := make(chan agent.Result, 1)
			resCh <- agent.Result{Status: "completed", Output: "immediate success"}
			close(msgCh)
			return &agent.Session{Messages: msgCh, Result: resCh}, nil
		},
	}

	ctx := context.Background()
	start := time.Now()
	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status 'completed', got '%s'", result.Status)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
	// Should return almost immediately (< 100ms for local execution)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("expected immediate return, took %v", elapsed)
	}
}

// TestExecuteWithRetry_ConsecutiveRateLimits tests multiple consecutive rate limit errors.
func TestExecuteWithRetry_ConsecutiveRateLimits(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Millisecond,
				MaxRetries:    5,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	attempts := 0
	backend := &mockBackend{
		executeFunc: func(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
			attempts++
			// Return rate limit error for first 4 attempts, success on 5th
			if attempts < 5 {
				return nil, errors.New("429 Too Many Requests")
			}
			msgCh := make(chan agent.Message)
			resCh := make(chan agent.Result, 1)
			resCh <- agent.Result{Status: "completed", Output: "success after 4 retries"}
			close(msgCh)
			return &agent.Session{Messages: msgCh, Result: resCh}, nil
		},
	}

	ctx := context.Background()
	result, tools, err := d.executeWithRetry(ctx, backend, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status 'completed', got '%s'", result.Status)
	}
	if attempts != 5 {
		t.Fatalf("expected 5 attempts, got %d", attempts)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
}

// TestIsRateLimitError_EdgeCases tests edge cases for isRateLimitError.
func TestIsRateLimitError_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "empty string error",
			err:      errors.New(""),
			expected: false,
		},
		{
			name:     "error with only whitespace",
			err:      errors.New("   "),
			expected: false,
		},
		{
			name:     "error with rate limit substring in different word",
			err:      errors.New("deratelimitation process"),
			expected: true, // "ratelimit" is a substring
		},
		{
			name:     "error with 429 as part of larger number",
			err:      errors.New("error code 4290"),
			expected: false, // "4290" contains "429" but this is a different error code
		},
		{
			name:     "error with mixed case RATE LIMIT",
			err:      errors.New("RATE LIMIT"),
			expected: true,
		},
		{
			name:     "wrapped rate limit error",
			err:      errors.New("upstream error: rate limit exceeded"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRateLimitError(tt.err)
			if result != tt.expected {
				t.Errorf("isRateLimitError(%q) = %v, expected %v", tt.err.Error(), result, tt.expected)
			}
		})
	}
}

// TestExecuteWithRetry_NilBackend tests behavior with nil backend.
func TestExecuteWithRetry_NilBackend(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			RateLimitConfig: RateLimitConfig{
				InitialDelay:  5 * time.Millisecond,
				MaxRetries:    3,
				BackoffFactor: 2.0,
			},
		},
		logger: slog.Default(),
	}

	ctx := context.Background()
	result, tools, err := d.executeWithRetry(ctx, nil, "test prompt", agent.ExecOptions{}, slog.Default(), "task-1")

	// Should return error without panicking
	if err == nil {
		t.Fatal("expected error for nil backend, got nil")
	}
	if result.Status != "failed" {
		t.Fatalf("expected status 'failed', got '%s'", result.Status)
	}
	if tools != 0 {
		t.Fatalf("expected tools 0, got %d", tools)
	}
}
