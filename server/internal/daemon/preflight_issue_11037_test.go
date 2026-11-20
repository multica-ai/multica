package daemon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPreflightIssue11037RecoversFromEmptyForbidden(t *testing.T) {
	defer noSleepRetry(t)()
	var workspaceRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			_, _ = w.Write([]byte(`{"renewed":false}`))
		case "/api/daemon/workspaces":
			switch workspaceRequests.Add(1) {
			case 1, 2:
				w.WriteHeader(http.StatusForbidden)
				return
			case 3:
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	d := &Daemon{client: NewClient(server.URL), logger: captureLogger(&bytes.Buffer{})}
	d.client.SetToken("mul_test")
	if err := d.preflightAuth(context.Background()); err != nil {
		t.Fatalf("startup must recover after the transient response: %v", err)
	}
	if got := workspaceRequests.Load(); got != 4 {
		t.Fatalf("workspace requests = %d, want 4", got)
	}
}

func TestPreflightIssue11037PreservesRefusal(t *testing.T) {
	defer noSleepRetry(t)()
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		attempts int32
	}{
		{"persistent empty forbidden", http.StatusForbidden, "", 4},
		{"explicit forbidden", http.StatusForbidden, `{"error":"forbidden"}`, 1},
		{"unauthorized", http.StatusUnauthorized, "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var workspaceRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/tokens/current/renew":
					_, _ = w.Write([]byte(`{"renewed":false}`))
				case "/api/daemon/workspaces":
					workspaceRequests.Add(1)
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)

			d := &Daemon{client: NewClient(server.URL), logger: captureLogger(&bytes.Buffer{})}
			d.client.SetToken("mul_test")
			err := d.preflightAuth(context.Background())
			var response *requestError
			if !errors.As(err, &response) || response.StatusCode != tc.status || response.Body != tc.body {
				t.Fatalf("expected refusal %d %q, got %v", tc.status, tc.body, err)
			}
			if got := workspaceRequests.Load(); got != tc.attempts {
				t.Fatalf("workspace requests = %d, want %d", got, tc.attempts)
			}
		})
	}
}
