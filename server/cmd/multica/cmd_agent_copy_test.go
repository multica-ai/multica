package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newAgentCopyTestCmd builds a standalone cobra.Command carrying the same flags
// runAgentCopy reads (via the shared registrar), plus the persistent --profile
// flag the API-client resolver needs. Tests mutate its flag state in isolation.
func newAgentCopyTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "copy"}
	registerAgentCopyFlags(c)
	c.Flags().String("profile", "", "")
	return c
}

// fullSourceAgent is a representative GET /api/agents/<id> response with every
// portable field populated, so copy tests can assert the whole whitelist.
func fullSourceAgent() map[string]any {
	return map[string]any{
		"id":           "agent-src",
		"name":         "Src",
		"runtime_id":   "runtime-1",
		"description":  "a description",
		"instructions": "some instructions",
		"starter_prompts": []any{
			map[string]any{"label": "Review a PR", "prompt": "Review the open pull request."},
		},
		"avatar_url":           "https://img.example/a.png",
		"custom_args":          []any{"--foo", "--bar"},
		"max_concurrent_tasks": 9,
		"model":                "claude-sonnet-4-6",
		"thinking_level":       "high",
		"service_tier":         "priority",
		"permission_mode":      "public_to",
		"invocation_targets":   []any{map[string]any{"target_type": "workspace"}},
		"skills": []any{
			map[string]any{"id": "skill-1", "name": "One"},
			map[string]any{"id": "skill-2", "name": "Two"},
		},
		// Secret metadata the copy must never turn into real values.
		"has_custom_env":       true,
		"custom_env_key_count": 2,
		"mcp_config_redacted":  true,
	}
}

// copyMockServer serves the source agent on GET and captures the create body on
// POST. The captured body pointer is filled in when the POST fires.
func copyMockServer(t *testing.T, source map[string]any, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents/"+source["id"].(string):
			_ = json.NewEncoder(w).Encode(source)
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents":
			if err := json.NewDecoder(r.Body).Decode(gotBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "agent-new", "name": "Src (copy)"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func setCopyTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	// Run from a fresh temp dir so no daemon-task marker sits in the cwd
	// ancestry: the CLI then treats this as a normal (non-agent) context and
	// accepts the plain test token instead of demanding a task-scoped mat_ one.
	t.Chdir(t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
}

func TestAgentCopySameRuntimeCopiesPortableFields(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}

	if gotBody == nil {
		t.Fatal("create was never called")
	}
	if gotBody["name"] != "Src (copy)" {
		t.Errorf("name = %v, want \"Src (copy)\"", gotBody["name"])
	}
	// No --runtime-id: the copy stays on the source runtime.
	if gotBody["runtime_id"] != "runtime-1" {
		t.Errorf("runtime_id = %v, want runtime-1", gotBody["runtime_id"])
	}
	if gotBody["description"] != "a description" {
		t.Errorf("description = %v", gotBody["description"])
	}
	if gotBody["instructions"] != "some instructions" {
		t.Errorf("instructions = %v", gotBody["instructions"])
	}
	if !reflect.DeepEqual(gotBody["starter_prompts"], []any{
		map[string]any{"label": "Review a PR", "prompt": "Review the open pull request."},
	}) {
		t.Errorf("starter_prompts = %v", gotBody["starter_prompts"])
	}
	if gotBody["avatar_url"] != "https://img.example/a.png" {
		t.Errorf("avatar_url = %v", gotBody["avatar_url"])
	}
	if !reflect.DeepEqual(gotBody["custom_args"], []any{"--foo", "--bar"}) {
		t.Errorf("custom_args = %v", gotBody["custom_args"])
	}
	if gotBody["max_concurrent_tasks"] != float64(9) {
		t.Errorf("max_concurrent_tasks = %v, want 9", gotBody["max_concurrent_tasks"])
	}
	// Same runtime: runtime-specific fields are carried over.
	if gotBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["thinking_level"] != "high" {
		t.Errorf("thinking_level = %v", gotBody["thinking_level"])
	}
	if gotBody["service_tier"] != "priority" {
		t.Errorf("service_tier = %v", gotBody["service_tier"])
	}
	// Invocation permission is copied verbatim.
	if gotBody["permission_mode"] != "public_to" {
		t.Errorf("permission_mode = %v", gotBody["permission_mode"])
	}
	if !reflect.DeepEqual(gotBody["invocation_targets"], []any{map[string]any{"target_type": "workspace"}}) {
		t.Errorf("invocation_targets = %v", gotBody["invocation_targets"])
	}
	// Skills bind in the same create request.
	if !reflect.DeepEqual(gotBody["skill_ids"], []any{"skill-1", "skill-2"}) {
		t.Errorf("skill_ids = %v, want [skill-1 skill-2]", gotBody["skill_ids"])
	}
	// Secrets / machine-local config must never be copied.
	for _, k := range []string{"custom_env", "mcp_config", "runtime_config", "has_custom_env"} {
		if _, ok := gotBody[k]; ok {
			t.Errorf("body must not contain %q, got %v", k, gotBody[k])
		}
	}
}

func TestAgentCopyOmitsInvalidHistoricalConcurrency(t *testing.T) {
	for _, value := range []any{0, -1, 51} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			source := fullSourceAgent()
			source["max_concurrent_tasks"] = value

			var gotBody map[string]any
			srv := copyMockServer(t, source, &gotBody)
			defer srv.Close()
			setCopyTestEnv(t, srv.URL)

			cmd := newAgentCopyTestCmd()
			if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
				t.Fatalf("runAgentCopy: %v", err)
			}
			if _, ok := gotBody["max_concurrent_tasks"]; ok {
				t.Errorf("invalid historical max_concurrent_tasks must be omitted, got %v", gotBody["max_concurrent_tasks"])
			}
		})
	}
}

func TestAgentCopyValidConcurrencyOverrideRepairsInvalidSource(t *testing.T) {
	source := fullSourceAgent()
	source["max_concurrent_tasks"] = 0

	var gotBody map[string]any
	srv := copyMockServer(t, source, &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("max-concurrent-tasks", "12")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["max_concurrent_tasks"] != float64(12) {
		t.Errorf("max_concurrent_tasks = %v, want 12", gotBody["max_concurrent_tasks"])
	}
}

func TestAgentCopyRejectsInvalidConcurrencyOverrideBeforeRequest(t *testing.T) {
	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("max-concurrent-tasks", "51")

	err := runAgentCopy(cmd, []string{"agent-src"})
	if err == nil {
		t.Fatal("expected invalid --max-concurrent-tasks error")
	}
	if !strings.Contains(err.Error(), "between 1 and 50") {
		t.Fatalf("error = %q, want 1-50 range", err.Error())
	}
}

func TestAgentCopyCrossRuntimeRequiresModel(t *testing.T) {
	postCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(fullSourceAgent())
			return
		}
		postCalled = true
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "agent-new"})
	}))
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("runtime-id", "runtime-2")

	err := runAgentCopy(cmd, []string{"agent-src"})
	if err == nil {
		t.Fatal("expected error when copying across runtimes without --model")
	}
	if !strings.Contains(err.Error(), "--model") {
		t.Errorf("error = %q, want it to mention --model", err.Error())
	}
	if postCalled {
		t.Error("create must not be called when validation fails")
	}
}

func TestAgentCopyCrossRuntimeDropsRuntimeSpecificFields(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("runtime-id", "runtime-2")
	_ = cmd.Flags().Set("model", "openai/gpt-4o")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["runtime_id"] != "runtime-2" {
		t.Errorf("runtime_id = %v, want runtime-2", gotBody["runtime_id"])
	}
	if gotBody["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v, want openai/gpt-4o", gotBody["model"])
	}
	// thinking_level / service_tier are runtime-specific and must NOT ride
	// across a runtime change unless set explicitly.
	if _, ok := gotBody["thinking_level"]; ok {
		t.Errorf("thinking_level must be dropped on a runtime change, got %v", gotBody["thinking_level"])
	}
	if _, ok := gotBody["service_tier"]; ok {
		t.Errorf("service_tier must be dropped on a runtime change, got %v", gotBody["service_tier"])
	}
	// Skills still travel across the runtime change.
	if !reflect.DeepEqual(gotBody["skill_ids"], []any{"skill-1", "skill-2"}) {
		t.Errorf("skill_ids = %v", gotBody["skill_ids"])
	}
}

// Passing --model "" across a runtime change is an explicit "accept the target
// runtime default" and must satisfy the required-model gate.
func TestAgentCopyCrossRuntimeEmptyModelIsAllowed(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("runtime-id", "runtime-2")
	_ = cmd.Flags().Set("model", "")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["model"] != "" {
		t.Errorf("model = %v, want empty string", gotBody["model"])
	}
}

func TestAgentCopyNoSkillsOmitsSkillIDs(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("no-skills", "true")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if _, ok := gotBody["skill_ids"]; ok {
		t.Errorf("skill_ids must be omitted with --no-skills, got %v", gotBody["skill_ids"])
	}
}

// A permission override flag fully defines the copy's permission and must not
// mix with the source's copied permission_mode / allow-list.
func TestAgentCopyPermissionOverrideReplacesSource(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("permission-mode", "private")

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	if gotBody["permission_mode"] != "private" {
		t.Errorf("permission_mode = %v, want private", gotBody["permission_mode"])
	}
	// applyAgentPermissionFlags emits an explicit empty allow-list; the source's
	// workspace target must be gone.
	if got, ok := gotBody["invocation_targets"].([]any); !ok || len(got) != 0 {
		t.Errorf("invocation_targets = %v, want empty list", gotBody["invocation_targets"])
	}
}

// custom_env is never read from the source, but an explicit --custom-env sets a
// fresh map on the copy.
func TestAgentCopyAcceptsExplicitCustomEnv(t *testing.T) {
	var gotBody map[string]any
	srv := copyMockServer(t, fullSourceAgent(), &gotBody)
	defer srv.Close()
	setCopyTestEnv(t, srv.URL)

	cmd := newAgentCopyTestCmd()
	_ = cmd.Flags().Set("custom-env", `{"API_KEY":"fresh"}`)

	if err := runAgentCopy(cmd, []string{"agent-src"}); err != nil {
		t.Fatalf("runAgentCopy: %v", err)
	}
	ce, ok := gotBody["custom_env"].(map[string]any)
	if !ok {
		t.Fatalf("custom_env = %v, want a JSON object", gotBody["custom_env"])
	}
	if ce["API_KEY"] != "fresh" {
		t.Errorf("custom_env[API_KEY] = %v, want fresh", ce["API_KEY"])
	}
}

// The copy command must expose the same secret-safe input channels as create so
// scripts can keep secrets off the command line.
func TestAgentCopyExposesSecretSafeFlags(t *testing.T) {
	for _, name := range []string{
		"custom-env-stdin", "custom-env-file",
		"mcp-config-stdin", "mcp-config-file",
	} {
		if agentCopyCmd.Flag(name) == nil {
			t.Errorf("agent copy is missing the %q flag", name)
		}
	}
}
