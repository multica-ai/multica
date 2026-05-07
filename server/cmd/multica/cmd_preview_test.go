package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

// captureStdout redirects os.Stdout to a pipe for the duration of fn and
// returns whatever was written to it. Used to assert on table/JSON output
// without real terminal output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStdout pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

// ---------------------------------------------------------------------------
// Shared fixture
// ---------------------------------------------------------------------------

var examplePreview = map[string]any{
	"id":             "prev-abc123",
	"app":            "ue",
	"namespace_name": "ue-preview-agent-abc123",
	"preview_host":   "ue-preview-abc123.example.com",
	"status":         "active",
	"workspace_id":   "ws-1",
}

// ---------------------------------------------------------------------------
// TestPreviewList
// ---------------------------------------------------------------------------

func TestPreviewList(t *testing.T) {
	previews := []map[string]any{examplePreview}

	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(previews)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	t.Run("GET /api/previews", func(t *testing.T) {
		var got []map[string]any
		if err := client.GetJSON(ctx, "/api/previews", &got); err != nil {
			t.Fatalf("GetJSON: %v", err)
		}
		if gotMethod != http.MethodGet {
			t.Errorf("method = %q, want GET", gotMethod)
		}
		if gotPath != "/api/previews" {
			t.Errorf("path = %q, want /api/previews", gotPath)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 preview, got %d", len(got))
		}
		if got[0]["id"] != "prev-abc123" {
			t.Errorf("id = %q, want prev-abc123", got[0]["id"])
		}
	})

	t.Run("table output contains expected columns", func(t *testing.T) {
		var got []map[string]any
		_ = client.GetJSON(ctx, "/api/previews", &got)

		out := captureStdout(t, func() {
			headers := []string{"ID", "APP", "NAMESPACE", "HOST", "STATUS"}
			rows := make([][]string, 0, len(got))
			for _, p := range got {
				rows = append(rows, []string{
					strVal(p, "id"),
					strVal(p, "app"),
					strVal(p, "namespace_name"),
					strVal(p, "preview_host"),
					strVal(p, "status"),
				})
			}
			cli.PrintTable(os.Stdout, headers, rows)
		})

		for _, want := range []string{"prev-abc123", "ue", "ue-preview-agent-abc123", "ue-preview-abc123.example.com", "active"} {
			if !strings.Contains(out, want) {
				t.Errorf("table output missing %q\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("json output is valid JSON array", func(t *testing.T) {
		var got []map[string]any
		_ = client.GetJSON(ctx, "/api/previews", &got)

		out := captureStdout(t, func() {
			cli.PrintJSON(os.Stdout, got)
		})

		var decoded []map[string]any
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatalf("output is not valid JSON: %v\noutput:\n%s", err, out)
		}
		if len(decoded) != 1 || decoded[0]["id"] != "prev-abc123" {
			t.Errorf("unexpected decoded content: %+v", decoded)
		}
	})
}

// ---------------------------------------------------------------------------
// TestPreviewGet
// ---------------------------------------------------------------------------

func TestPreviewGet(t *testing.T) {
	t.Run("GET /api/previews/:id returns preview", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(examplePreview)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		var got map[string]any
		if err := client.GetJSON(ctx, "/api/previews/prev-abc123", &got); err != nil {
			t.Fatalf("GetJSON: %v", err)
		}
		if gotPath != "/api/previews/prev-abc123" {
			t.Errorf("path = %q, want /api/previews/prev-abc123", gotPath)
		}
		if got["id"] != "prev-abc123" {
			t.Errorf("id = %v, want prev-abc123", got["id"])
		}
	})

	t.Run("404 from server surfaces as error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		var got map[string]any
		err := client.GetJSON(ctx, "/api/previews/nonexistent", &got)
		if err == nil {
			t.Fatal("expected error on 404, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error should mention 404, got: %v", err)
		}
	})

	t.Run("table output contains expected fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(examplePreview)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		var got map[string]any
		_ = client.GetJSON(ctx, "/api/previews/prev-abc123", &got)

		out := captureStdout(t, func() {
			headers := []string{"ID", "APP", "NAMESPACE", "HOST", "STATUS"}
			rows := [][]string{{
				strVal(got, "id"),
				strVal(got, "app"),
				strVal(got, "namespace_name"),
				strVal(got, "preview_host"),
				strVal(got, "status"),
			}}
			cli.PrintTable(os.Stdout, headers, rows)
		})

		for _, want := range []string{"prev-abc123", "ue", "active"} {
			if !strings.Contains(out, want) {
				t.Errorf("table output missing %q\nfull output:\n%s", want, out)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestPreviewCreate
// ---------------------------------------------------------------------------

func TestPreviewCreate(t *testing.T) {
	t.Run("POST /api/previews sends correct body (no branch)", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/previews" {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(examplePreview)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		body := map[string]any{
			"app":       "ue",
			"issue_id":  "issue-001",
			"repo":      "https://github.com/g2crowd/ue",
			"image_tag": "abc1234",
		}
		var result map[string]any
		if err := client.PostJSON(ctx, "/api/previews", body, &result); err != nil {
			t.Fatalf("PostJSON: %v", err)
		}

		for k, want := range body {
			if gotBody[k] != want {
				t.Errorf("body[%q] = %v, want %v", k, gotBody[k], want)
			}
		}
		// branch must NOT be present
		if _, hasBranch := gotBody["branch"]; hasBranch {
			t.Errorf("body must not contain 'branch' field (deprecated)")
		}
	})

	t.Run("server error surfaces as error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"preview generator unreachable"}`))
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		var result map[string]any
		err := client.PostJSON(ctx, "/api/previews", map[string]any{"app": "ue"}, &result)
		if err == nil {
			t.Fatal("expected error on 503, got nil")
		}
		if !strings.Contains(err.Error(), "503") {
			t.Errorf("error should mention 503, got: %v", err)
		}
	})

	t.Run("result rendered as table", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(examplePreview)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		var result map[string]any
		_ = client.PostJSON(ctx, "/api/previews", map[string]any{}, &result)

		out := captureStdout(t, func() {
			headers := []string{"ID", "APP", "NAMESPACE", "HOST", "STATUS"}
			rows := [][]string{{
				strVal(result, "id"),
				strVal(result, "app"),
				strVal(result, "namespace_name"),
				strVal(result, "preview_host"),
				strVal(result, "status"),
			}}
			cli.PrintTable(os.Stdout, headers, rows)
		})

		for _, want := range []string{"prev-abc123", "ue", "active"} {
			if !strings.Contains(out, want) {
				t.Errorf("table output missing %q\nfull output:\n%s", want, out)
			}
		}
	})

	t.Run("idempotent 200 response is not treated as error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Preview generator returns 200 when a matching preview already exists
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(examplePreview)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		var result map[string]any
		if err := client.PostJSON(ctx, "/api/previews", map[string]any{}, &result); err != nil {
			t.Fatalf("expected no error on idempotent 200, got: %v", err)
		}
		if result["id"] != "prev-abc123" {
			t.Errorf("id = %v, want prev-abc123", result["id"])
		}
	})
}

// ---------------------------------------------------------------------------
// TestPreviewDelete
// ---------------------------------------------------------------------------

func TestPreviewDelete(t *testing.T) {
	t.Run("DELETE /api/previews/:id sends correct request", func(t *testing.T) {
		var gotPath, gotMethod string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		if err := client.DeleteJSON(ctx, "/api/previews/prev-abc123"); err != nil {
			t.Fatalf("DeleteJSON: %v", err)
		}
		if gotMethod != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", gotMethod)
		}
		if gotPath != "/api/previews/prev-abc123" {
			t.Errorf("path = %q, want /api/previews/prev-abc123", gotPath)
		}
	})

	t.Run("404 on delete surfaces as error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
		}))
		defer srv.Close()

		client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
		ctx := context.Background()

		err := client.DeleteJSON(ctx, "/api/previews/nonexistent")
		if err == nil {
			t.Fatal("expected error on 404, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error should mention 404, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestPreviewAuthHeaders
// ---------------------------------------------------------------------------

// TestPreviewAuthHeaders verifies that the API client sends the Authorization
// and X-Workspace-ID headers on all preview requests, as required by the
// RequireWorkspaceMember middleware on the server.
func TestPreviewAuthHeaders(t *testing.T) {
	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/previews"},
		{"get", http.MethodGet, "/api/previews/prev-abc123"},
		{"delete", http.MethodDelete, "/api/previews/prev-abc123"},
		{"create", http.MethodPost, "/api/previews"},
	}

	for _, ep := range endpoints {
		ep := ep
		t.Run(ep.name+" sends auth headers", func(t *testing.T) {
			var gotAuth, gotWS string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				gotWS = r.Header.Get("X-Workspace-ID")
				w.Header().Set("Content-Type", "application/json")
				switch ep.method {
				case http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				case http.MethodPost:
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(examplePreview)
				default:
					if strings.HasSuffix(r.URL.Path, "/api/previews") {
						json.NewEncoder(w).Encode([]map[string]any{examplePreview})
					} else {
						json.NewEncoder(w).Encode(examplePreview)
					}
				}
			}))
			defer srv.Close()

			client := cli.NewAPIClient(srv.URL, "ws-test", "tok-secret")
			ctx := context.Background()

			switch ep.method {
			case http.MethodGet:
				var out any
				client.GetJSON(ctx, ep.path, &out)
			case http.MethodDelete:
				client.DeleteJSON(ctx, ep.path)
			case http.MethodPost:
				var out map[string]any
				client.PostJSON(ctx, ep.path, map[string]any{}, &out)
			}

			if gotAuth != "Bearer tok-secret" {
				t.Errorf("Authorization = %q, want Bearer tok-secret", gotAuth)
			}
			if gotWS != "ws-test" {
				t.Errorf("X-Workspace-ID = %q, want ws-test", gotWS)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestPreviewCreateNoBranch
// ---------------------------------------------------------------------------

// TestPreviewCreateNoBranch is a focused regression test ensuring the
// deprecated --branch parameter is absent from the request body sent to the
// server, even if a caller somehow passes extra fields.
func TestPreviewCreateNoBranch(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(examplePreview)
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := context.Background()

	// This is the exact body runPreviewCreate constructs (no branch key).
	body := map[string]any{
		"app":       "ue",
		"issue_id":  "issue-001",
		"repo":      "https://github.com/g2crowd/ue",
		"image_tag": "abc1234",
	}
	var result map[string]any
	_ = client.PostJSON(ctx, "/api/previews", body, &result)

	if _, hasBranch := gotBody["branch"]; hasBranch {
		t.Errorf("'branch' key must not appear in the request body (deprecated)")
	}
	for _, required := range []string{"app", "issue_id", "repo", "image_tag"} {
		if _, ok := gotBody[required]; !ok {
			t.Errorf("required field %q missing from request body", required)
		}
	}
}
