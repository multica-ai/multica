package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_IdentityHeaders_PostJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Client-Platform"); got != "daemon" {
			t.Errorf("expected X-Client-Platform daemon, got %q", got)
		}
		if got := r.Header.Get("X-Client-Version"); got != "9.9.9" {
			t.Errorf("expected X-Client-Version 9.9.9, got %q", got)
		}
		if got := r.Header.Get("X-Client-OS"); got != normalizeGOOS(runtime.GOOS) {
			t.Errorf("expected X-Client-OS %q, got %q", normalizeGOOS(runtime.GOOS), got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("expected Authorization Bearer tok, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ok": "1"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")
	c.SetVersion("9.9.9")

	if err := c.postJSON(context.Background(), "/api/daemon/test", map[string]any{}, nil); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
}

func TestClient_IdentityHeaders_GetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Client-Platform"); got != "daemon" {
			t.Errorf("expected X-Client-Platform daemon, got %q", got)
		}
		if got := r.Header.Get("X-Client-Version"); got != "1.2.3" {
			t.Errorf("expected X-Client-Version 1.2.3, got %q", got)
		}
		if got := r.Header.Get("X-Client-OS"); got == "" {
			t.Errorf("expected X-Client-OS to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")
	c.SetVersion("1.2.3")

	var out map[string]any
	if err := c.getJSON(context.Background(), "/api/daemon/test", &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
}

func TestClient_VersionOmittedWhenUnset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Client-Platform"); got != "daemon" {
			t.Errorf("expected X-Client-Platform daemon, got %q", got)
		}
		// SetVersion not called → header must be omitted (not "").
		if vals := r.Header.Values("X-Client-Version"); len(vals) != 0 {
			t.Errorf("expected X-Client-Version absent, got %v", vals)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.postJSON(context.Background(), "/api/daemon/test", nil, nil); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
}

// noSleepRetry replaces retrySleep with an immediate no-op so tests don't
// actually wait the 4s/8s/16s/... backoffs. Returns a restore func.
func noSleepRetry(t *testing.T) func() {
	t.Helper()
	prev := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	return func() { retrySleep = prev }
}

func TestIsTransientError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil is not transient", nil, false},
		{"5xx is transient", &requestError{StatusCode: http.StatusBadGateway}, true},
		{"503 is transient", &requestError{StatusCode: http.StatusServiceUnavailable}, true},
		{"408 is transient", &requestError{StatusCode: http.StatusRequestTimeout}, true},
		{"429 is transient", &requestError{StatusCode: http.StatusTooManyRequests}, true},
		{"400 is permanent", &requestError{StatusCode: http.StatusBadRequest}, false},
		{"401 is permanent", &requestError{StatusCode: http.StatusUnauthorized}, false},
		{"404 is permanent", &requestError{StatusCode: http.StatusNotFound}, false},
		{"transport-level error is transient", errors.New("connection reset by peer"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientError(tc.err); got != tc.want {
				t.Fatalf("isTransientError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestPostJSONWithRetry_TransientThenSuccess(t *testing.T) {
	defer noSleepRetry(t)()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	schedule := []time.Duration{time.Nanosecond, time.Nanosecond, time.Nanosecond}
	if err := c.postJSONWithRetry(context.Background(), "/x", map[string]any{}, nil, schedule); err != nil {
		t.Fatalf("postJSONWithRetry: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 attempts (2 transient + 1 success), got %d", got)
	}
}

func TestPostJSONWithRetry_TransientExhausts(t *testing.T) {
	defer noSleepRetry(t)()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	schedule := []time.Duration{time.Nanosecond, time.Nanosecond}
	err := c.postJSONWithRetry(context.Background(), "/x", map[string]any{}, nil, schedule)
	if err == nil {
		t.Fatal("expected error after schedule exhausted, got nil")
	}
	if !isTransientError(err) {
		t.Fatalf("expected transient error, got %v", err)
	}
	if got := calls.Load(); got != int32(len(schedule)+1) {
		t.Fatalf("expected %d attempts (initial + %d retries), got %d", len(schedule)+1, len(schedule), got)
	}
}

func TestPostJSONWithRetry_PermanentBailsImmediately(t *testing.T) {
	defer noSleepRetry(t)()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	schedule := []time.Duration{time.Nanosecond, time.Nanosecond, time.Nanosecond}
	err := c.postJSONWithRetry(context.Background(), "/x", map[string]any{}, nil, schedule)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt on permanent error, got %d", got)
	}
}

func TestPostJSONWithRetry_CtxCancelStopsRetries(t *testing.T) {
	// Use the real sleeper here so we can observe a cancel preempting it.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel quickly so the first sleep is aborted long before its 1s.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	c := NewClient(srv.URL)
	schedule := []time.Duration{time.Second, time.Second, time.Second}
	start := time.Now()
	err := c.postJSONWithRetry(ctx, "/x", map[string]any{}, nil, schedule)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error after ctx cancel, got nil")
	}
	if elapsed > 750*time.Millisecond {
		t.Fatalf("expected ctx cancel to short-circuit retry, took %s", elapsed)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt before cancel, got %d", got)
	}
}

func TestDefaultTerminalRetrySchedule_MatchesAgreedPlan(t *testing.T) {
	// MUL-2780 settled on a 5-step exponential backoff (4s, 8s, 16s, 32s, 64s).
	// Pin it so a future "tidy this up" refactor can't silently flatten or
	// shorten the recovery window without explicit discussion.
	want := []time.Duration{4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 64 * time.Second}
	if len(defaultTerminalRetrySchedule) != len(want) {
		t.Fatalf("schedule length: got %d, want %d", len(defaultTerminalRetrySchedule), len(want))
	}
	for i, d := range want {
		if defaultTerminalRetrySchedule[i] != d {
			t.Errorf("schedule[%d]: got %s, want %s", i, defaultTerminalRetrySchedule[i], d)
		}
	}
}

func TestNormalizeGOOS(t *testing.T) {
	cases := map[string]string{
		"darwin":  "macos",
		"windows": "windows",
		"linux":   "linux",
		"freebsd": "freebsd",
	}
	for in, want := range cases {
		if got := normalizeGOOS(in); got != want {
			t.Errorf("normalizeGOOS(%q) = %q, want %q", in, got, want)
		}
	}
}
