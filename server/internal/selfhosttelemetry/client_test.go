package selfhosttelemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProductionHTTPClientIsFixedAndBounded(t *testing.T) {
	t.Parallel()
	client := newHTTPClient()
	defer client.Close()
	if client.endpoint != "https://telemetry.multica.ai/v1/telemetry/events" {
		t.Fatalf("endpoint = %q", client.endpoint)
	}
	if client.client.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", client.client.Timeout)
	}
	if client.client.Jar != nil {
		t.Fatal("telemetry client must not have a cookie jar")
	}
}

func TestHTTPClientRequestAndResponseClassification(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		want   deliveryDisposition
	}{
		{"accepted", http.StatusAccepted, deliverySuccess},
		{"rate limited", http.StatusTooManyRequests, deliveryRetry},
		{"server error", http.StatusServiceUnavailable, deliveryRetry},
		{"bad request", http.StatusBadRequest, deliveryStopForDay},
		{"unexpected success", http.StatusOK, deliveryStopForDay},
		{"redirect", http.StatusTemporaryRedirect, deliveryStopForDay},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var redirected atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirected" {
					redirected.Store(true)
					w.WriteHeader(http.StatusAccepted)
					return
				}
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q", got)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("request carried authentication or cookies")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if string(body) != `{"safe":true}` {
					t.Errorf("body = %s", body)
				}
				if tt.status == http.StatusTemporaryRedirect {
					w.Header().Set("Location", "/redirected")
				}
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			httpClient := server.Client()
			httpClient.Timeout = time.Second
			httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			client := newHTTPClientForTest(server.URL, httpClient)
			if got := client.Send(context.Background(), []byte(`{"safe":true}`)); got != tt.want {
				t.Fatalf("disposition = %v, want %v", got, tt.want)
			}
			if redirected.Load() {
				t.Fatal("telemetry followed a redirect")
			}
		})
	}
}

type blockingTransport struct {
	calls atomic.Int32
}

func (t *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestHTTPClientTimeoutAndCancellationRetry(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
	}{
		{"timeout", 10 * time.Millisecond, false},
		{"cancellation", time.Second, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			transport := &blockingTransport{}
			client := newHTTPClientForTest("https://local.invalid", &http.Client{
				Transport: transport,
				Timeout:   tt.timeout,
			})
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancel {
				cancel()
			} else {
				defer cancel()
			}
			if got := client.Send(ctx, []byte(`{}`)); got != deliveryRetry {
				t.Fatalf("disposition = %v, want retry", got)
			}
			if transport.calls.Load() != 1 {
				t.Fatalf("transport calls = %d, want 1", transport.calls.Load())
			}
		})
	}
}
