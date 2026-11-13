package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newServiceAccountCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().Int("expires-in-days", 0, "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func newServiceAccountListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func newServiceAccountTokenMintTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "mint"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().Int("expires-in-days", 0, "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func newServiceAccountTokenRevokeTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "revoke"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	return cmd
}

const serviceAccountTestWorkspace = "44444444-4444-4444-4444-444444444444"

func TestRunServiceAccountCreatePostsNameAndPrintsToken(t *testing.T) {
	var got map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/workspaces/"+serviceAccountTestWorkspace+"/service-accounts" {
			http.NotFound(w, r)
			return
		}
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "55555555-5555-5555-5555-555555555555",
			"name":       "ci-runner",
			"email":      "service-00000000@acme.multica.service",
			"created_at": "2026-11-11T00:00:00Z",
			"token":      "mul_testtoken",
		})
	}))
	defer srv.Close()

	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newServiceAccountCreateTestCmd()
	_ = cmd.Flags().Set("name", "ci-runner")
	_ = cmd.Flags().Set("output", "table")

	out, err := captureStdout(t, func() error {
		return runServiceAccountCreate(cmd, []string{serviceAccountTestWorkspace})
	})
	if err != nil {
		t.Fatalf("runServiceAccountCreate: %v", err)
	}
	if gotPath != "/api/workspaces/"+serviceAccountTestWorkspace+"/service-accounts" {
		t.Errorf("POST path = %q", gotPath)
	}
	if got["name"] != "ci-runner" {
		t.Errorf("body name = %v, want ci-runner", got["name"])
	}
	if _, present := got["expires_in_days"]; present {
		t.Errorf("body must omit expires_in_days when unset, got %v", got["expires_in_days"])
	}
	for _, want := range []string{"mul_testtoken", "ci-runner"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
}

func TestRunServiceAccountCreateRequiresName(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newServiceAccountCreateTestCmd()
	err := runServiceAccountCreate(cmd, []string{serviceAccountTestWorkspace})
	if err == nil || !strings.Contains(err.Error(), "--name is required") {
		t.Fatalf("expected --name error, got %v", err)
	}
}

func TestRunServiceAccountListPrintsTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/workspaces/"+serviceAccountTestWorkspace+"/service-accounts" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":            "55555555-5555-5555-5555-555555555555",
				"name":          "ci-runner",
				"email":         "service-00000000@acme.multica.service",
				"created_at":    "2026-11-11T00:00:00Z",
				"active_tokens": 2,
				"last_used_at":  nil,
			},
		})
	}))
	defer srv.Close()

	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newServiceAccountListTestCmd()
	_ = cmd.Flags().Set("output", "table")

	out, err := captureStdout(t, func() error {
		return runServiceAccountList(cmd, []string{serviceAccountTestWorkspace})
	})
	if err != nil {
		t.Fatalf("runServiceAccountList: %v", err)
	}
	for _, want := range []string{"ci-runner", "service-00000000@acme.multica.service", "2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
}

func TestRunServiceAccountTokenMintPostsToAccountPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "55555555-5555-5555-5555-555555555555",
			"name":       "ci-runner",
			"email":      "service-00000000@acme.multica.service",
			"created_at": "2026-11-11T00:00:00Z",
			"token":      "mul_rotated",
		})
	}))
	defer srv.Close()

	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newServiceAccountTokenMintTestCmd()
	_ = cmd.Flags().Set("output", "table")

	out, err := captureStdout(t, func() error {
		return runServiceAccountTokenMint(cmd, []string{"55555555-5555-5555-5555-555555555555", serviceAccountTestWorkspace})
	})
	if err != nil {
		t.Fatalf("runServiceAccountTokenMint: %v", err)
	}
	want := "/api/workspaces/" + serviceAccountTestWorkspace + "/service-accounts/55555555-5555-5555-5555-555555555555/tokens"
	if gotPath != want {
		t.Errorf("POST path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(out, "mul_rotated") {
		t.Errorf("output %q does not contain the minted token", out)
	}
}

func TestRunServiceAccountTokenRevokeDeletes(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newServiceAccountTokenRevokeTestCmd()

	out, err := captureStdout(t, func() error {
		return runServiceAccountTokenRevoke(cmd, []string{"66666666-6666-6666-6666-666666666666", serviceAccountTestWorkspace})
	})
	if err != nil {
		t.Fatalf("runServiceAccountTokenRevoke: %v", err)
	}
	want := "/api/workspaces/" + serviceAccountTestWorkspace + "/service-accounts/tokens/66666666-6666-6666-6666-666666666666"
	if gotMethod != http.MethodDelete || gotPath != want {
		t.Errorf("revoke = %s %q, want DELETE %q", gotMethod, gotPath, want)
	}
	if !strings.Contains(out, "Token revoked.") {
		t.Errorf("output %q does not confirm revocation", out)
	}
}
