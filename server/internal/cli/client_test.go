package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	t.Run("client identity headers", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-Client-Platform"); got != "cli-test" {
				t.Errorf("expected X-Client-Platform cli-test, got %s", got)
			}
			if got := r.Header.Get("X-Client-Version"); got != "9.9.9" {
				t.Errorf("expected X-Client-Version 9.9.9, got %s", got)
			}
			if got := r.Header.Get("X-Client-OS"); got != "linux" {
				t.Errorf("expected X-Client-OS linux, got %s", got)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		client.Platform = "cli-test"
		client.Version = "9.9.9"
		client.OS = "linux"
		if err := client.PostJSON(context.Background(), "/test", reqBody{}, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("client identity headers fall back to package defaults", func(t *testing.T) {
		origPlatform, origVersion, origOS := ClientPlatform, ClientVersion, ClientOS
		ClientPlatform = "cli"
		ClientVersion = "1.2.3-test"
		ClientOS = "macos"
		t.Cleanup(func() {
			ClientPlatform, ClientVersion, ClientOS = origPlatform, origVersion, origOS
		})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-Client-Platform"); got != "cli" {
				t.Errorf("expected X-Client-Platform cli, got %s", got)
			}
			if got := r.Header.Get("X-Client-Version"); got != "1.2.3-test" {
				t.Errorf("expected X-Client-Version 1.2.3-test, got %s", got)
			}
			if got := r.Header.Get("X-Client-OS"); got != "macos" {
				t.Errorf("expected X-Client-OS macos, got %s", got)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		if err := client.PostJSON(context.Background(), "/test", reqBody{}, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestDownloadFile(t *testing.T) {
	t.Run("relative URL is resolved against BaseURL and sent with auth", func(t *testing.T) {
		var gotPath, gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			w.Write([]byte("hello"))
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "test-token")
		data, err := client.DownloadFile(context.Background(), "/uploads/workspaces/abc/file.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("unexpected body: %q", string(data))
		}
		if gotPath != "/uploads/workspaces/abc/file.md" {
			t.Errorf("unexpected path: %q", gotPath)
		}
		if gotAuth != "Bearer test-token" {
			t.Errorf("expected Authorization Bearer test-token, got %q", gotAuth)
		}
	})

	t.Run("absolute URL is used as-is without auth headers", func(t *testing.T) {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Write([]byte("signed-payload"))
		}))
		defer srv.Close()

		client := NewAPIClient("https://api.example.test", "", "test-token")
		data, err := client.DownloadFile(context.Background(), srv.URL+"/signed?sig=abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "signed-payload" {
			t.Errorf("unexpected body: %q", string(data))
		}
		if gotAuth != "" {
			t.Errorf("expected no Authorization header on signed URL, got %q", gotAuth)
		}
	})

	t.Run("relative URL with empty BaseURL returns a helpful error", func(t *testing.T) {
		client := NewAPIClient("", "", "test-token")
		_, err := client.DownloadFile(context.Background(), "/uploads/x.md")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("non-2xx status returns an error with the response body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, "not found")
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "test-token")
		_, err := client.DownloadFile(context.Background(), "/uploads/missing")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestUploadFileWithURL(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "multipart/form-data") {
				t.Errorf("expected multipart content-type, got %s", ct)
			}

			file, header, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("missing file field: %v", err)
			}
			defer file.Close()

			data, _ := io.ReadAll(file)
			if string(data) != "hello" {
				t.Errorf("unexpected file data: %q", string(data))
			}
			if header.Filename != "test.txt" {
				t.Errorf("unexpected filename: %q", header.Filename)
			}

			// Verify no issue_id or comment_id fields are sent.
			if r.FormValue("issue_id") != "" {
				t.Errorf("unexpected issue_id field")
			}
			if r.FormValue("comment_id") != "" {
				t.Errorf("unexpected comment_id field")
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(AttachmentResponse{
				ID:        "att-123",
				URL:       "https://cdn.example.com/file.txt",
				Filename:  "test.txt",
				SizeBytes: 5,
			})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "ws-1", "test-token")
		id, url, err := client.UploadFileWithURL(context.Background(), []byte("hello"), "test.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "att-123" {
			t.Errorf("expected id att-123, got %s", id)
		}
		if url != "https://cdn.example.com/file.txt" {
			t.Errorf("expected url https://cdn.example.com/file.txt, got %s", url)
		}
	})

	t.Run("error status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, "bad request")
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		_, _, err := client.UploadFileWithURL(context.Background(), []byte("x"), "x.txt")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "upload file returned 400") {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})

	t.Run("missing id in response succeeds (fallback path)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"url": "https://example.com"})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		id, url, err := client.UploadFileWithURL(context.Background(), []byte("x"), "x.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty id, got %s", id)
		}
		if url != "https://example.com" {
			t.Errorf("expected url https://example.com, got %s", url)
		}
	})

	t.Run("workspace header sent", func(t *testing.T) {
		var gotWorkspace string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotWorkspace = r.Header.Get("X-Workspace-ID")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(AttachmentResponse{ID: "att-1", URL: "https://example.com"})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "ws-abc", "test-token")
		_, _, err := client.UploadFileWithURL(context.Background(), []byte("x"), "x.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotWorkspace != "ws-abc" {
			t.Errorf("expected X-Workspace-ID ws-abc, got %s", gotWorkspace)
		}
	})

	t.Run("missing url in response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(AttachmentResponse{ID: "att-123"})
		}))
		defer srv.Close()

		client := NewAPIClient(srv.URL, "", "")
		_, _, err := client.UploadFileWithURL(context.Background(), []byte("x"), "x.txt")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "missing attachment url") {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})
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
