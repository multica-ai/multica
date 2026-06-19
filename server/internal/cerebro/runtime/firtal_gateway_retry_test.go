package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestIsTransientTransportErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"io.EOF", io.EOF, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"broken pipe", syscall.EPIPE, true},
		{"wrapped EOF", &url.Error{Op: "Post", URL: "http://x", Err: io.EOF}, true},
		{"context canceled — not transient", context.Canceled, false},
		{"deadline exceeded — not transient", context.DeadlineExceeded, false},
		{"unrelated error", errors.New("boom"), false},
		{"text-only EOF", errors.New(`Post "http://x": EOF`), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientTransportErr(tc.err); got != tc.want {
				t.Fatalf("isTransientTransportErr(%v)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransientGatewayError(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"gateway EOF", `call gateway: Post "http://x/api/ai/proxy/v1/chat/completions": EOF`, true},
		{"anthropic gateway reset", "call anthropic gateway: read tcp: connection reset by peer", true},
		{"plain EOF without gateway", "read tcp 10.0.0.1:443: EOF", false},
		{"gateway 503 — no transport drop", "gateway returned HTTP 503: service unavailable", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientGatewayError(tc.text); got != tc.want {
				t.Fatalf("isTransientGatewayError(%q)=%v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// rtFunc adapts a function to http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func okResponse() *http.Response {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     make(http.Header),
	}
}

func newTestGatewayClient(rt http.RoundTripper) *GatewayClient {
	return &GatewayClient{httpClient: &http.Client{Transport: rt}}
}

func TestDoWithRetry_SucceedsAfterTransientBlip(t *testing.T) {
	var calls int
	c := newTestGatewayClient(rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls < 2 {
			return nil, io.EOF // first attempt blips
		}
		return okResponse(), nil
	}))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://gateway/x", bytes.NewReader([]byte(`{"a":1}`)))
	resp, err := c.doWithRetry(req, []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2 (one retry)", calls)
	}
}

func TestDoWithRetry_ExhaustsThenReturnsError(t *testing.T) {
	var calls int
	c := newTestGatewayClient(rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, io.EOF
	}))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://gateway/x", bytes.NewReader([]byte(`{}`)))
	_, err := c.doWithRetry(req, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != gatewayTransientMaxAttempts {
		t.Fatalf("calls=%d, want %d", calls, gatewayTransientMaxAttempts)
	}
}

func TestDoWithRetry_NonTransientNotRetried(t *testing.T) {
	var calls int
	c := newTestGatewayClient(rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("malformed request")
	}))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://gateway/x", bytes.NewReader([]byte(`{}`)))
	_, err := c.doWithRetry(req, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1 (no retry on non-transient)", calls)
	}
}

func TestDoWithRetry_StopsOnCancelledContext(t *testing.T) {
	var calls int
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestGatewayClient(rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel() // cancel after the first failed attempt
		return nil, io.EOF
	}))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://gateway/x", bytes.NewReader([]byte(`{}`)))
	_, err := c.doWithRetry(req, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1 (no retry once context is cancelled)", calls)
	}
}
