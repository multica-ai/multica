package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newRuntimeUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("target-version", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("wait", false, "")
	cmd.Flags().Bool("latest", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.PersistentFlags().String("profile", "", "")
	return cmd
}

func captureStdoutRuntime(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	runErr := fn()
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close stdout writer: %v", cerr)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out), runErr
}

func TestRuntimeUpdate_MissingVersion(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()

	err := runRuntimeUpdate(cmd, []string{"rt-1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--target-version") || !strings.Contains(err.Error(), "--latest") {
		t.Fatalf("error should mention --target-version or --latest, got: %v", err)
	}
}

func TestRuntimeUpdate_MutuallyExclusiveAllAndRuntimeID(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("all", "true")

	err := runRuntimeUpdate(cmd, []string{"rt-1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error should mention mutual exclusivity, got: %v", err)
	}
}

func TestRuntimeUpdate_MissingRuntimeIDWithoutAll(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")

	err := runRuntimeUpdate(cmd, []string{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "runtime-id") {
		t.Fatalf("error should mention runtime-id, got: %v", err)
	}
}

func TestRuntimeUpdate_LatestResolvesVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes/rt-1/update" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			// The target_version should NOT have the "v" prefix.
			tv, ok := body["target_version"].(string)
			if !ok {
				t.Fatalf("target_version missing from body")
			}
			if strings.HasPrefix(tv, "v") {
				t.Fatalf("target_version should not have 'v' prefix, got: %s", tv)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-1",
				"status": "pending",
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	// Mock FetchLatestRelease
	orig := fetchLatestReleaseFn
	defer func() { fetchLatestReleaseFn = orig }()
	fetchLatestReleaseFn = func() (*cli.GitHubRelease, error) {
		return &cli.GitHubRelease{TagName: "v2.0.0"}, nil
	}

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("latest", "true")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if result["status"] != "pending" {
		t.Fatalf("status = %v, want pending", result["status"])
	}
}

func TestRuntimeUpdate_SingleRuntimeReturnsBareObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes/rt-1/update" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-1",
				"status": "pending",
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	// Verify it's a bare object, not an array.
	trimmed := strings.TrimSpace(out)
	if strings.HasPrefix(trimmed, "[") {
		t.Fatalf("single-runtime JSON should be a bare object, not an array: %s", out)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if result["id"] != "upd-1" {
		t.Fatalf("id = %v, want upd-1", result["id"])
	}
}

func TestRuntimeUpdate_AllReturnsArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/runtimes":
			if r.Method == http.MethodGet {
				json.NewEncoder(w).Encode([]map[string]any{
					{"id": "rt-1", "status": "active"},
					{"id": "rt-2", "status": "active"},
					{"id": "rt-3", "status": "offline"},
				})
			} else {
				http.NotFound(w, r)
			}
		case "/api/runtimes/rt-1/update", "/api/runtimes/rt-2/update", "/api/runtimes/rt-3/update":
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-" + strings.TrimPrefix(r.URL.Path, "/api/runtimes/"),
				"status": "pending",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("all", "true")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("--all JSON should be an array, got: %s", out)
	}

	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestRuntimeUpdate_NoRuntimesForAllExitsGracefully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes" {
			json.NewEncoder(w).Encode([]map[string]any{})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("all", "true")

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	err := runRuntimeUpdate(cmd, []string{})
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close stderr writer: %v", cerr)
	}
	out, _ := io.ReadAll(r)

	if err != nil {
		t.Fatalf("expected nil error for empty runtime list, got: %v", err)
	}
	if !strings.Contains(string(out), "No runtimes found") {
		t.Fatalf("stderr should mention 'No runtimes found', got: %s", string(out))
	}
}

func TestRuntimeUpdate_AllSkipsEmptyIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes" {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "rt-good", "status": "active"},
				{"id": "", "status": "bad"},
				{"status": "no-id-key"},
			})
		} else if strings.Contains(r.URL.Path, "/update") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-good",
				"status": "pending",
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("all", "true")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (skipping empty/missing IDs), got %d", len(results))
	}
}

func TestRuntimeUpdate_PostFailureIncludedInResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"error":"server error"}`)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if _, ok := result["error"]; !ok {
		t.Fatalf("result should contain error key: %s", out)
	}
}

func TestRuntimeUpdate_TargetVersionStripsVPrefix(t *testing.T) {
	var receivedVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes/rt-1/update" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			receivedVersion = body["target_version"].(string)
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-1",
				"status": "pending",
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "v1.5.0")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	if receivedVersion != "1.5.0" {
		t.Fatalf("target_version sent to server = %q, want %q", receivedVersion, "1.5.0")
	}
}

func TestRuntimeUpdate_TableOutputForSingleRuntime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes/rt-1/update" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-1",
				"status": "pending",
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("output", "table")

	out, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}

	if !strings.Contains(out, "rt-1") {
		t.Fatalf("table output should contain runtime ID: %s", out)
	}
	if !strings.Contains(out, "upd-1") {
		t.Fatalf("table output should contain update ID: %s", out)
	}
}

func TestRuntimeUpdate_LatestAndTargetVersionBoth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runtimes/rt-1/update" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			tv := body["target_version"].(string)
			// --latest takes precedence, so version should come from the mock, not the flag.
			if tv != "3.0.0" {
				t.Fatalf("target_version = %q, want 3.0.0 from --latest (should override --target-version)", tv)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "upd-1",
				"status": "pending",
			})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	orig := fetchLatestReleaseFn
	defer func() { fetchLatestReleaseFn = orig }()
	fetchLatestReleaseFn = func() (*cli.GitHubRelease, error) {
		return &cli.GitHubRelease{TagName: "v3.0.0"}, nil
	}

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("latest", "true")
	_ = cmd.Flags().Set("target-version", "1.0.0")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdoutRuntime(t, func() error {
		return runRuntimeUpdate(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeUpdate: %v", err)
	}
}

func TestRuntimeUpdate_LatestFetchFails(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	orig := fetchLatestReleaseFn
	defer func() { fetchLatestReleaseFn = orig }()
	fetchLatestReleaseFn = func() (*cli.GitHubRelease, error) {
		return nil, fmt.Errorf("GitHub API unavailable")
	}

	cmd := newRuntimeUpdateTestCmd()
	_ = cmd.Flags().Set("latest", "true")

	err := runRuntimeUpdate(cmd, []string{"rt-1"})
	if err == nil {
		t.Fatal("expected error when --latest fetch fails, got nil")
	}
	if !strings.Contains(err.Error(), "fetch latest release") {
		t.Fatalf("error should mention 'fetch latest release', got: %v", err)
	}
}
