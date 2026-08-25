package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"
)

// instantRetrySleep removes the backoff wait so tests exercise the retry loop
// without burning the schedule's wall-clock time.
func instantRetrySleep(t *testing.T) {
	t.Helper()
	prev := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { retrySleep = prev })
}

// fakeRoundTripper fails its first failures calls, then succeeds.
type fakeRoundTripper struct {
	failures int
	err      error
	attempts int
	bodies   []string
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.attempts++
	if req.Body != nil && req.Body != http.NoBody {
		data, _ := io.ReadAll(req.Body)
		f.bodies = append(f.bodies, string(data))
	}
	if f.attempts <= f.failures {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}, nil
}

func newTestRetryTransport(base http.RoundTripper) *retryTransport {
	return &retryTransport{base: base, schedule: defaultRetrySchedule}
}

func TestRetryTransportRetriesInterruptedGET(t *testing.T) {
	instantRetrySleep(t)
	base := &fakeRoundTripper{failures: 2, err: syscall.ECONNRESET}
	rt := newTestRetryTransport(base)

	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/api/issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	defer resp.Body.Close()
	if base.attempts != 3 {
		t.Errorf("attempts = %d, want 3 (1 initial + 2 retries)", base.attempts)
	}
}

func TestRetryTransportGivesUpAfterSchedule(t *testing.T) {
	instantRetrySleep(t)
	base := &fakeRoundTripper{failures: 99, err: syscall.ECONNRESET}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/api/issues", nil)
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("expected error once the schedule is exhausted")
	}
	if want := len(defaultRetrySchedule) + 1; base.attempts != want {
		t.Errorf("attempts = %d, want %d", base.attempts, want)
	}
}

// A POST that already reached the wire must not be replayed: the server may
// have applied it and only the response was lost. Posting a comment twice is
// worse than surfacing the error.
func TestRetryTransportDoesNotReplayPOSTAfterWrite(t *testing.T) {
	instantRetrySleep(t)
	base := &fakeRoundTripper{failures: 1, err: syscall.ECONNRESET}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/api/comments", strings.NewReader(`{"a":1}`))
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("expected the POST error to surface, not a replay")
	}
	if base.attempts != 1 {
		t.Errorf("attempts = %d, want 1 (POST must not be replayed)", base.attempts)
	}
}

// A dial failure proves the request never reached the server, so even a POST
// is safe to replay.
func TestRetryTransportReplaysPOSTOnDialFailure(t *testing.T) {
	instantRetrySleep(t)
	dialErr := &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	base := &fakeRoundTripper{failures: 1, err: dialErr}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/api/comments", strings.NewReader(`{"a":1}`))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected replay to succeed, got %v", err)
	}
	defer resp.Body.Close()
	if base.attempts != 2 {
		t.Errorf("attempts = %d, want 2", base.attempts)
	}
	// Both attempts must carry the full body, not a truncated remnant.
	for i, b := range base.bodies {
		if b != `{"a":1}` {
			t.Errorf("attempt %d body = %q, want the full payload", i+1, b)
		}
	}
}

// TLS and DNS failures fail identically on the next attempt; retrying only
// delays the error the user needs.
func TestRetryTransportDoesNotRetryPermanentFailures(t *testing.T) {
	instantRetrySleep(t)
	for name, err := range map[string]error{
		"tls": errors.New("x509: certificate signed by unknown authority"),
		"dns": &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true},
	} {
		t.Run(name, func(t *testing.T) {
			base := &fakeRoundTripper{failures: 99, err: err}
			rt := newTestRetryTransport(base)
			req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/api/issues", nil)
			if _, rtErr := rt.RoundTrip(req); rtErr == nil {
				t.Fatal("expected error")
			}
			if base.attempts != 1 {
				t.Errorf("attempts = %d, want 1", base.attempts)
			}
		})
	}
}

func TestRetryTransportStopsOnCanceledContext(t *testing.T) {
	instantRetrySleep(t)
	base := &fakeRoundTripper{failures: 99, err: syscall.ECONNRESET}
	rt := newTestRetryTransport(base)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/api/issues", nil)
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("expected error")
	}
	if base.attempts != 1 {
		t.Errorf("attempts = %d, want 1 (canceled context must not retry)", base.attempts)
	}
}

func TestRetryTransportReturnsCancellationDuringBackoff(t *testing.T) {
	previousSleep := retrySleep
	retrySleep = func(ctx context.Context, _ time.Duration) error {
		return context.Canceled
	}
	t.Cleanup(func() { retrySleep = previousSleep })

	base := &fakeRoundTripper{failures: 99, err: syscall.ECONNRESET}
	rt := newTestRetryTransport(base)
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/api/issues", nil)

	_, err := rt.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled from the interrupted backoff", err)
	}
	if base.attempts != 1 {
		t.Errorf("attempts = %d, want 1 (canceled backoff must stop retries)", base.attempts)
	}
}

// statusRoundTripper serves the given statuses in order, then 200.
type statusRoundTripper struct {
	statuses []int
	attempts int
	methods  []string
}

func (s *statusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.methods = append(s.methods, req.Method)
	code := http.StatusOK
	if s.attempts < len(s.statuses) {
		code = s.statuses[s.attempts]
	}
	s.attempts++
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader("body")),
		Request:    req,
	}, nil
}

// A cold-starting stack answers 502 through the proxy until the app is up.
// An idempotent GET should ride that out instead of failing the command.
func TestRetryTransportRetriesGatewayStatusForGET(t *testing.T) {
	instantRetrySleep(t)
	base := &statusRoundTripper{statuses: []int{http.StatusBadGateway, http.StatusServiceUnavailable}}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/api/inbox", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after riding out the gateway errors", resp.StatusCode)
	}
	if base.attempts != 3 {
		t.Errorf("attempts = %d, want 3", base.attempts)
	}
}

// A 502 says nothing about whether the write behind it was applied, so a POST
// must surface it rather than replay.
func TestRetryTransportDoesNotRetryGatewayStatusForPOST(t *testing.T) {
	instantRetrySleep(t)
	base := &statusRoundTripper{statuses: []int{http.StatusBadGateway, http.StatusBadGateway}}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/api/inbox/x/read", strings.NewReader("{}"))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want the 502 surfaced", resp.StatusCode)
	}
	if base.attempts != 1 {
		t.Errorf("attempts = %d, want 1 (POST must not be replayed on 502)", base.attempts)
	}
}

// 500 is an application error, not a blip; retrying only delays the failure.
func TestRetryTransportDoesNotRetry500(t *testing.T) {
	instantRetrySleep(t)
	base := &statusRoundTripper{statuses: []int{http.StatusInternalServerError, http.StatusInternalServerError}}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/api/issues", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 surfaced", resp.StatusCode)
	}
	if base.attempts != 1 {
		t.Errorf("attempts = %d, want 1", base.attempts)
	}
}

// The response handed back must still have a readable body — the retry path
// drains intermediate responses, and draining the final one would give the
// caller an empty payload.
func TestRetryTransportFinalResponseBodyIsIntact(t *testing.T) {
	instantRetrySleep(t)
	base := &statusRoundTripper{statuses: []int{http.StatusBadGateway}}
	rt := newTestRetryTransport(base)

	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/api/inbox", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != "body" {
		t.Errorf("body = %q, want %q", string(data), "body")
	}
}

func TestRetryScheduleHonorsEnv(t *testing.T) {
	t.Setenv("MULTICA_HTTP_RETRIES", "0")
	if got := retrySchedule(); len(got) != 0 {
		t.Errorf("MULTICA_HTTP_RETRIES=0 should disable retries, got %v", got)
	}
	t.Setenv("MULTICA_HTTP_RETRIES", "4")
	if got := retrySchedule(); len(got) != 4 {
		t.Errorf("MULTICA_HTTP_RETRIES=4 should yield 4 retries, got %d", len(got))
	}
	t.Setenv("MULTICA_HTTP_RETRIES", "nonsense")
	if got := retrySchedule(); len(got) != len(defaultRetrySchedule) {
		t.Errorf("invalid value should fall back to the default, got %d", len(got))
	}
}

// End-to-end through a real http.Client: the first response is killed
// mid-flight, and the CLI's client must still return the retried success
// without the caller knowing anything happened.
func TestAPIClientRetriesThroughRealServer(t *testing.T) {
	instantRetrySleep(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			// Hijack and drop the connection so the client sees a transport
			// error rather than an HTTP status.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "ws", "token")
	var out struct {
		ID string `json:"id"`
	}
	if err := c.GetJSON(context.Background(), "/api/issues/abc", &out); err != nil {
		t.Fatalf("expected the retry to hide the dropped connection, got %v", err)
	}
	if out.ID != "abc" {
		t.Errorf("id = %q, want abc", out.ID)
	}
	if hits < 2 {
		t.Errorf("server hits = %d, want at least 2", hits)
	}
}
