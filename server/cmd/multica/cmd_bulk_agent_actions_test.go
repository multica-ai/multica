package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

// Request-shape tests for the two bulk agent commands (MUL-5758). They assert
// what actually leaves the machine — method, path, body — because both
// commands carry secret material or move live work, and a silently dropped
// field would be invisible in the printed output.
//
// MULTICA_TOKEN is a `mat_` token rather than the plain "test-token" the older
// CLI tests use: these commands are expected to run inside daemon-managed agent
// tasks, where newAPIClient requires a task-scoped token. Using one here also
// keeps the tests runnable inside an agent task, not just on a clean checkout.

func newAgentEnvMergeTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "merge"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("custom-env", "", "")
	cmd.Flags().Bool("custom-env-stdin", false, "")
	cmd.Flags().String("custom-env-file", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newRuntimeMigrateAgentsTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "migrate-agents"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().StringArray("agent", nil, "")
	cmd.Flags().String("from", "", "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

// captureRequest records the one request the command sends and replies with
// `reply`.
func captureRequest(t *testing.T, reply any) (*httptest.Server, *struct {
	Method string
	Path   string
	Body   map[string]any
}) {
	t.Helper()

	got := &struct {
		Method string
		Path   string
		Body   map[string]any
	}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Method = r.Method
		got.Path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func setCLITestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_TOKEN", "mat_test-task-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
}

func TestRunAgentEnvMergeSendsMergePayload(t *testing.T) {
	srv, got := captureRequest(t, map[string]any{
		"results": []map[string]any{{
			"agent_id":         "agent-1",
			"name":             "Lambda",
			"added_keys":       []string{"NEW_KEY"},
			"overwritten_keys": []string{},
			"key_count":        1,
		}},
		"skipped": []any{},
	})
	setCLITestEnv(t, srv.URL)

	cmd := newAgentEnvMergeTestCmd()
	if err := cmd.Flags().Set("custom-env", `{"NEW_KEY":"secret"}`); err != nil {
		t.Fatalf("set --custom-env: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runAgentEnvMerge(cmd, []string{"agent-1", "agent-2"})
	}); err != nil {
		t.Fatalf("runAgentEnvMerge: %v", err)
	}

	// PATCH on the collection, not PUT on one agent: the merge endpoint is
	// what makes injection possible without reading existing values back.
	if got.Method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", got.Method)
	}
	if got.Path != "/api/agents/env" {
		t.Errorf("path = %s, want /api/agents/env", got.Path)
	}
	ids, _ := got.Body["agent_ids"].([]any)
	if len(ids) != 2 || ids[0] != "agent-1" || ids[1] != "agent-2" {
		t.Errorf("agent_ids = %v, want both positional args", got.Body["agent_ids"])
	}
	set, _ := got.Body["set"].(map[string]any)
	if set["NEW_KEY"] != "secret" {
		t.Errorf("set = %v, want the submitted key", got.Body["set"])
	}
	// The wholesale-replace field must never appear here — sending it would
	// mean "this is the entire env" and delete every other key.
	if _, present := got.Body["custom_env"]; present {
		t.Error("merge must not send custom_env; that is the replace contract")
	}
}

func TestRunAgentEnvMergeRequiresKeys(t *testing.T) {
	srv, _ := captureRequest(t, map[string]any{})
	setCLITestEnv(t, srv.URL)

	cmd := newAgentEnvMergeTestCmd()
	if err := runAgentEnvMerge(cmd, []string{"agent-1"}); err == nil {
		t.Fatal("expected an error when no env input channel was supplied")
	}
}

func TestRunRuntimeMigrateAgentsSendsTargetAndAgents(t *testing.T) {
	srv, got := captureRequest(t, map[string]any{
		"target_runtime_id":    "rt-target",
		"dry_run":              false,
		"migrated":             []any{},
		"skipped":              []any{},
		"tasks_migrated":       0,
		"tasks_staying_active": 0,
	})
	setCLITestEnv(t, srv.URL)

	cmd := newRuntimeMigrateAgentsTestCmd()
	if err := cmd.Flags().Set("agent", "agent-1"); err != nil {
		t.Fatalf("set --agent: %v", err)
	}
	if err := cmd.Flags().Set("agent", "agent-2"); err != nil {
		t.Fatalf("set --agent: %v", err)
	}
	if err := cmd.Flags().Set("from", "rt-source"); err != nil {
		t.Fatalf("set --from: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runRuntimeMigrateAgents(cmd, []string{"rt-target"})
	}); err != nil {
		t.Fatalf("runRuntimeMigrateAgents: %v", err)
	}

	if got.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.Method)
	}
	// The runtime in the path is the TARGET; getting this backwards would
	// migrate agents the wrong way.
	if got.Path != "/api/runtimes/rt-target/migrate-agents" {
		t.Errorf("path = %s, want the target runtime in the path", got.Path)
	}
	ids, _ := got.Body["agent_ids"].([]any)
	if len(ids) != 2 {
		t.Errorf("agent_ids = %v, want both --agent values", got.Body["agent_ids"])
	}
	// --from is the stale-plan guard; dropping it would let the server migrate
	// a set the caller never saw.
	if got.Body["expected_source_runtime_id"] != "rt-source" {
		t.Errorf("expected_source_runtime_id = %v, want rt-source", got.Body["expected_source_runtime_id"])
	}
	if got.Body["dry_run"] != false {
		t.Errorf("dry_run = %v, want false", got.Body["dry_run"])
	}
}

func TestRunRuntimeMigrateAgentsOmitsSourceWhenNotGiven(t *testing.T) {
	srv, got := captureRequest(t, map[string]any{
		"target_runtime_id":    "rt-target",
		"dry_run":              true,
		"migrated":             []any{},
		"skipped":              []any{},
		"tasks_migrated":       2,
		"tasks_staying_active": 1,
	})
	setCLITestEnv(t, srv.URL)

	cmd := newRuntimeMigrateAgentsTestCmd()
	if err := cmd.Flags().Set("agent", "agent-1"); err != nil {
		t.Fatalf("set --agent: %v", err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set --dry-run: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runRuntimeMigrateAgents(cmd, []string{"rt-target"})
	}); err != nil {
		t.Fatalf("runRuntimeMigrateAgents: %v", err)
	}

	// Absent rather than empty-string: an empty expected_source_runtime_id
	// would be parsed as a malformed UUID and rejected.
	if _, present := got.Body["expected_source_runtime_id"]; present {
		t.Errorf("expected_source_runtime_id must be omitted without --from, body = %v", got.Body)
	}
	if got.Body["dry_run"] != true {
		t.Errorf("dry_run = %v, want true", got.Body["dry_run"])
	}
}

func TestRunRuntimeMigrateAgentsRequiresAtLeastOneAgent(t *testing.T) {
	srv, _ := captureRequest(t, map[string]any{})
	setCLITestEnv(t, srv.URL)

	cmd := newRuntimeMigrateAgentsTestCmd()
	if err := runRuntimeMigrateAgents(cmd, []string{"rt-target"}); err == nil {
		t.Fatal("expected an error when no --agent was supplied")
	}
}
