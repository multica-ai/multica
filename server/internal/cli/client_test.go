package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostJSON(t *testing.T) {
	type reqBody struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	type respBody struct {
		ID string `json:"id"`
	}

	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}
			if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
				t.Errorf("expected Authorization Bearer test-token, got %s", auth)
			}

			var body reqBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}
			if body.Name != "alice" || body.Age != 30 {
				t.Errorf("unexpected body: %+v", body)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(respBody{ID: "123"})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "test-token")
		var out respBody
		err := client.PostJSON(context.Background(), "/test", reqBody{Name: "alice", Age: 30}, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.ID != "123" {
			t.Errorf("expected ID 123, got %s", out.ID)
		}
	})

	t.Run("error status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, "bad request")
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "test-token")
		err := client.PostJSON(context.Background(), "/test", reqBody{Name: "bob"}, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got := err.Error(); got != "POST /test returned 400: bad request" {
			t.Errorf("unexpected error message: %s", got)
		}
	})

	t.Run("nil output", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "test-token")
		err := client.PostJSON(context.Background(), "/test", reqBody{Name: "charlie"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("workspace and agent context headers", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ws := r.Header.Get("X-Workspace-ID"); ws != "ws-abc" {
				t.Errorf("expected X-Workspace-ID ws-abc, got %s", ws)
			}
			if agent := r.Header.Get("X-Agent-ID"); agent != "agent-123" {
				t.Errorf("expected X-Agent-ID agent-123, got %s", agent)
			}
			if task := r.Header.Get("X-Task-ID"); task != "task-456" {
				t.Errorf("expected X-Task-ID task-456, got %s", task)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(respBody{ID: "456"})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "ws-abc", "test-token")
		client.AgentID = "agent-123"
		client.TaskID = "task-456"
		var out respBody
		err := client.PostJSON(context.Background(), "/test", reqBody{}, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestDownloadFile guards the bug from ZAR-27 where a relative `download_url`
// was passed straight to http.Get and produced "unsupported protocol scheme".
func TestDownloadFile(t *testing.T) {
	const payload = "hello-bytes"

	t.Run("relative URL resolves against base URL and sends auth headers", func(t *testing.T) {
		var seenAuth, seenWS string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/uploads/workspaces/ws-abc/file.png" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			seenAuth = r.Header.Get("Authorization")
			seenWS = r.Header.Get("X-Workspace-ID")
			io.WriteString(w, payload)
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "ws-abc", "test-token")
		got, err := client.DownloadFile(context.Background(), "/uploads/workspaces/ws-abc/file.png")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != payload {
			t.Errorf("expected body %q, got %q", payload, string(got))
		}
		if seenAuth != "Bearer test-token" {
			t.Errorf("expected Authorization 'Bearer test-token', got %q", seenAuth)
		}
		if seenWS != "ws-abc" {
			t.Errorf("expected X-Workspace-ID 'ws-abc', got %q", seenWS)
		}
	})

	t.Run("absolute URL passes through and does not send auth headers", func(t *testing.T) {
		var seenAuth, seenWS string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenAuth = r.Header.Get("Authorization")
			seenWS = r.Header.Get("X-Workspace-ID")
			io.WriteString(w, payload)
		}))
		defer srv.Close()

		// Configure the client against a different base URL to prove that
		// absolute URLs are not rewritten to point at it.
		client := NewAPIClient("https://api.example.invalid", "ws-abc", "test-token")
		got, err := client.DownloadFile(context.Background(), srv.URL+"/some/external/object?sig=abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != payload {
			t.Errorf("expected body %q, got %q", payload, string(got))
		}
		if seenAuth != "" {
			t.Errorf("expected no Authorization header on absolute URL, got %q", seenAuth)
		}
		if seenWS != "" {
			t.Errorf("expected no X-Workspace-ID header on absolute URL, got %q", seenWS)
		}
	})

	t.Run("error status surfaces server message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, "nope")
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		_, err := client.DownloadFile(context.Background(), "/uploads/x.png")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got := err.Error(); got != "download returned 403: nope" {
			t.Errorf("unexpected error: %s", got)
		}
	})
}
