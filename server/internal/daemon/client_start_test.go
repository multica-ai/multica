package daemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type startTaskTransport func(*http.Request) (*http.Response, error)

func (f startTaskTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStartTaskRetries(t *testing.T) {
	defer noSleepRetry(t)()
	for _, tc := range []struct {
		name      string
		failure   error
		status    int
		failures  int
		wantCalls int
		wantError bool
	}{
		{name: "EOF", failure: io.EOF, failures: 1, wantCalls: 2},
		{name: "unexpected EOF", failure: io.ErrUnexpectedEOF, failures: 2, wantCalls: 3},
		{name: "gateway outage", status: http.StatusBadGateway, failures: 1, wantCalls: 2},
		{name: "persistent outage", failure: io.ErrUnexpectedEOF, failures: 10, wantCalls: 3, wantError: true},
		{name: "unauthorized", status: http.StatusUnauthorized, failures: 10, wantCalls: 1, wantError: true},
		{name: "invalid task state", status: http.StatusBadRequest, failures: 10, wantCalls: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := NewClient("https://daemon.test")
			client.client.Transport = startTaskTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/daemon/tasks/task-1/start" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != "{}" {
					t.Fatalf("start body = %q, error = %v", body, err)
				}
				status := http.StatusOK
				if calls <= tc.failures {
					if tc.failure != nil {
						return nil, tc.failure
					}
					status = tc.status
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			})
			err := client.StartTask(context.Background(), "task-1")
			if (err != nil) != tc.wantError || calls != tc.wantCalls {
				t.Fatalf("StartTask error = %v, calls = %d; want error = %v, calls = %d", err, calls, tc.wantError, tc.wantCalls)
			}
		})
	}
}

func TestStartTaskCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previous := retrySleep
	t.Cleanup(func() { retrySleep = previous })
	retrySleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	calls := 0
	client := NewClient("https://daemon.test")
	client.client.Transport = startTaskTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, io.ErrUnexpectedEOF
	})
	err := client.StartTask(ctx, "task-1")
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) || calls != 1 {
		t.Fatalf("error = %v, context = %v, calls = %d; want cancelled backoff and one attempt", err, ctx.Err(), calls)
	}
}
