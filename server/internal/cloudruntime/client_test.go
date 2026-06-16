package cloudruntime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestClientDoForwardsFleetRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/base/api/v1/nodes" {
			t.Fatalf("path = %s, want /base/api/v1/nodes", r.URL.Path)
		}
		if r.URL.RawQuery != "limit=20&offset=0" {
			t.Fatalf("query = %s, want limit=20&offset=0", r.URL.RawQuery)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		if got := r.Header.Get("X-User-ID"); got != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
			t.Fatalf("X-User-ID = %q", got)
		}
		if got := r.Header.Get("X-Request-ID"); got != "request-123" {
			t.Fatalf("X-Request-ID = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != `{"instance_type":"g5.xlarge"}` {
			t.Fatalf("body = %s", got)
		}
		w.Header().Set("X-Request-ID", "fleet-request-456")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"status":"launching"}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL + "/base/"})
	resp, err := client.Do(context.Background(), Request{
		Method:    http.MethodPost,
		Path:      "/api/v1/nodes",
		Query:     url.Values{"limit": []string{"20"}, "offset": []string{"0"}},
		Body:      []byte(`{"instance_type":"g5.xlarge"}`),
		UserID:    "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001",
		RequestID: "request-123",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-ID"); got != "fleet-request-456" {
		t.Fatalf("response X-Request-ID = %q", got)
	}
	if got := string(resp.Body); got != `{"status":"launching"}` {
		t.Fatalf("response body = %s", got)
	}
}

func TestClientDoDisabled(t *testing.T) {
	client := NewClient(Config{})
	_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/healthz"})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestClientDoInvalidBaseURL(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://%"})
	_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/healthz"})
	if !errors.Is(err, ErrInvalidBaseURL) {
		t.Fatalf("err = %v, want ErrInvalidBaseURL", err)
	}
}
