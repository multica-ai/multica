package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newRuntimeDeleteTestCmd(serverURL string) *cobra.Command {
	cmd := &cobra.Command{Use: "delete"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().Bool("cascade", false, "")
	cmd.Flags().String("output", "table", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	return cmd
}

func captureRuntimeStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out), runErr
}

func TestRunRuntimeDeleteStrictSuccessPrintsJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")

	var deleteCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/runtimes/rt-1" {
			t.Fatalf("path = %q, want /api/runtimes/rt-1", r.URL.Path)
		}
		if r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Fatalf("X-Workspace-ID = %q, want ws-1", r.Header.Get("X-Workspace-ID"))
		}
		deleteCount++
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer srv.Close()

	cmd := newRuntimeDeleteTestCmd(srv.URL)
	_ = cmd.Flags().Set("output", "json")

	out, err := captureRuntimeStdout(t, func() error {
		return runRuntimeDelete(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeDelete: %v", err)
	}
	if deleteCount != 1 {
		t.Fatalf("deleteCount = %d, want 1", deleteCount)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if got["id"] != "rt-1" || got["deleted"] != true {
		t.Fatalf("stdout = %#v, want deleted result for rt-1", got)
	}
}

func TestRunRuntimeDeleteConflictSuggestsCascade(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/runtimes/rt-1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":  "runtime_has_active_agents",
			"error": "cannot delete runtime: it has active agents bound to it.",
			"active_agents": []map[string]any{
				{"id": "agent-1", "name": "Codex"},
			},
		})
	}))
	defer srv.Close()

	err := runRuntimeDelete(newRuntimeDeleteTestCmd(srv.URL), []string{"rt-1"})
	if err == nil {
		t.Fatal("expected active-agent conflict")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Codex (agent-1)") || !strings.Contains(msg, "--cascade") {
		t.Fatalf("error = %q, want agent name and --cascade guidance", msg)
	}
}

func TestRunRuntimeDeleteCascadeConfirmsActiveAgentSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")

	var gotExpectedIDs []string
	var deleteCount, cascadeCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/runtimes/rt-1":
			deleteCount++
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":  "runtime_has_active_agents",
				"error": "cannot delete runtime: it has active agents bound to it.",
				"active_agents": []map[string]any{
					{"id": "agent-1", "name": "Codex"},
					{"id": "agent-2", "name": "Claude"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/runtimes/rt-1/archive-agents-and-delete":
			cascadeCount++
			var body struct {
				ExpectedActiveAgentIDs []string `json:"expected_active_agent_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode cascade body: %v", err)
			}
			gotExpectedIDs = body.ExpectedActiveAgentIDs
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":          "ok",
				"agents_archived": 2,
				"tasks_cancelled": 1,
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := newRuntimeDeleteTestCmd(srv.URL)
	_ = cmd.Flags().Set("cascade", "true")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureRuntimeStdout(t, func() error {
		return runRuntimeDelete(cmd, []string{"rt-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeDelete: %v", err)
	}
	if deleteCount != 1 || cascadeCount != 1 {
		t.Fatalf("deleteCount/cascadeCount = %d/%d, want 1/1", deleteCount, cascadeCount)
	}
	if strings.Join(gotExpectedIDs, ",") != "agent-1,agent-2" {
		t.Fatalf("expected_active_agent_ids = %#v", gotExpectedIDs)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if got["id"] != "rt-1" || got["deleted"] != true || got["agents_archived"] != float64(2) {
		t.Fatalf("stdout = %#v, want cascade result", got)
	}
}
