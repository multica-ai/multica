package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// F1: `multica agent create/update` must accept an explicit ordered-pool flag
// (--runtime-ids, a JSON array), wire it to the runtime_ids field the handler
// already accepts, and be mutually exclusive with the legacy scalar
// --runtime-id. These tests pin the CLI wire contract and mutual exclusion; the
// handler owns the pool semantics (two-provider-family rule, non-empty on
// create), covered by agent_runtime_pool_test.go.

func newAgentCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("runtime-ids", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}

func newAgentUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("runtime-ids", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}

func setAgentTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	// A task-scoped mat_ token so the test also passes inside an agent workdir,
	// where a daemon task marker makes newAPIClient reject a plain token.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
}

func TestParseRuntimeIDs(t *testing.T) {
	got, err := parseRuntimeIDs(`["a","b"]`)
	if err != nil {
		t.Fatalf("parseRuntimeIDs: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v, want [a b]", got)
	}
	// "[]" must yield a non-nil empty slice so it JSON-encodes as [] (the
	// server reads that as "clear the pool"), not null.
	empty, err := parseRuntimeIDs(`[]`)
	if err != nil {
		t.Fatalf("parseRuntimeIDs([]): %v", err)
	}
	if empty == nil {
		t.Fatal("parseRuntimeIDs([]) returned nil; want non-nil empty slice")
	}
	if b, _ := json.Marshal(empty); string(b) != "[]" {
		t.Fatalf("empty pool marshals to %s, want []", b)
	}
	if _, err := parseRuntimeIDs(`not json`); err == nil {
		t.Fatal("parseRuntimeIDs(not json) = nil error, want error")
	}
}

func TestAgentCreateSendsRuntimeIDs(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-123", "name": "PoolAgent"})
	}))
	defer srv.Close()
	setAgentTestEnv(t, srv.URL)

	cmd := newAgentCreateTestCmd()
	_ = cmd.Flags().Set("name", "PoolAgent")
	_ = cmd.Flags().Set("runtime-ids", `["rt-a","rt-b"]`)

	if err := runAgentCreate(cmd, nil); err != nil {
		t.Fatalf("runAgentCreate: %v", err)
	}
	ids, ok := gotBody["runtime_ids"].([]any)
	if !ok {
		t.Fatalf("body missing runtime_ids array; got %v", gotBody)
	}
	if len(ids) != 2 || ids[0] != "rt-a" || ids[1] != "rt-b" {
		t.Fatalf("runtime_ids = %v, want [rt-a rt-b]", ids)
	}
	if _, present := gotBody["runtime_id"]; present {
		t.Fatalf("runtime_id must not be sent alongside runtime_ids; got %v", gotBody)
	}
}

func TestAgentCreateRuntimeFlagsMutualExclusion(t *testing.T) {
	setAgentTestEnv(t, "http://127.0.0.1:0")
	cmd := newAgentCreateTestCmd()
	_ = cmd.Flags().Set("name", "PoolAgent")
	_ = cmd.Flags().Set("runtime-id", "rt-a")
	_ = cmd.Flags().Set("runtime-ids", `["rt-a","rt-b"]`)

	err := runAgentCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want mutual-exclusion error", err)
	}
}

func TestAgentCreateRequiresRuntime(t *testing.T) {
	setAgentTestEnv(t, "http://127.0.0.1:0")
	cmd := newAgentCreateTestCmd()
	_ = cmd.Flags().Set("name", "PoolAgent")

	err := runAgentCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v, want required error", err)
	}
}

func TestAgentUpdateSendsRuntimeIDs(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-123"})
	}))
	defer srv.Close()
	setAgentTestEnv(t, srv.URL)

	cmd := newAgentUpdateTestCmd()
	_ = cmd.Flags().Set("runtime-ids", `["rt-a","rt-b"]`)

	if err := runAgentUpdate(cmd, []string{"agent-123"}); err != nil {
		t.Fatalf("runAgentUpdate: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/agents/agent-123" {
		t.Fatalf("path = %q, want /api/agents/agent-123", gotPath)
	}
	ids, ok := gotBody["runtime_ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("runtime_ids body = %v, want [rt-a rt-b]", gotBody["runtime_ids"])
	}
}

// TestAgentUpdateClearsRuntimeIDs pins that "[]" reaches the wire as an empty
// array (clear/unbind), not null and not omitted.
func TestAgentUpdateClearsRuntimeIDs(t *testing.T) {
	var rawBody map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-123"})
	}))
	defer srv.Close()
	setAgentTestEnv(t, srv.URL)

	cmd := newAgentUpdateTestCmd()
	_ = cmd.Flags().Set("runtime-ids", `[]`)

	if err := runAgentUpdate(cmd, []string{"agent-123"}); err != nil {
		t.Fatalf("runAgentUpdate: %v", err)
	}
	raw, ok := rawBody["runtime_ids"]
	if !ok {
		t.Fatalf("body missing runtime_ids key; got %v", rawBody)
	}
	if string(raw) != "[]" {
		t.Fatalf("runtime_ids raw = %s, want []", raw)
	}
}

func TestAgentUpdateRuntimeFlagsMutualExclusion(t *testing.T) {
	setAgentTestEnv(t, "http://127.0.0.1:0")
	cmd := newAgentUpdateTestCmd()
	_ = cmd.Flags().Set("runtime-id", "rt-a")
	_ = cmd.Flags().Set("runtime-ids", `["rt-a","rt-b"]`)

	err := runAgentUpdate(cmd, []string{"agent-123"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want mutual-exclusion error", err)
	}
}

// TestAgentRuntimeIDsFlagHelp guards the help text so operators learn the
// ordering, the two-provider-family rule, and mutual exclusion.
func TestAgentRuntimeIDsFlagHelp(t *testing.T) {
	for _, name := range []string{"create", "update"} {
		var usage string
		switch name {
		case "create":
			if agentCreateCmd.Flag("runtime-ids") == nil {
				t.Fatalf("agent create must expose --runtime-ids")
			}
			usage = agentCreateCmd.Flag("runtime-ids").Usage
		case "update":
			if agentUpdateCmd.Flag("runtime-ids") == nil {
				t.Fatalf("agent update must expose --runtime-ids")
			}
			usage = agentUpdateCmd.Flag("runtime-ids").Usage
		}
		for _, want := range []string{"priority", "provider families", "Mutually exclusive"} {
			if !strings.Contains(usage, want) {
				t.Errorf("agent %s --runtime-ids help = %q, want to mention %q", name, usage, want)
			}
		}
	}
}
