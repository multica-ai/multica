package main

// CEREBRO-PATCH(cerebro-tool-policy-hook-cmd): TECH-2563 — tests for the PreToolUse hook subcommand.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runHookForTest drives runToolPolicyHook with a payload + env, capturing the
// exit code (0 = allow / no explicit exit, since allow returns without calling
// osExit). It restores osExit and the relevant env afterwards.
func runHookForTest(t *testing.T, payload string, env map[string]string) (exit int, stdout, stderr string) {
	t.Helper()
	exit = 0
	prev := osExit
	osExit = func(code int) { exit = code; panic(hookExitSentinel{}) }
	var outBuf, errBuf bytes.Buffer
	defer func() {
		osExit = prev
		stdout = outBuf.String()
		stderr = errBuf.String() // set the named return even when osExit panicked
		if r := recover(); r != nil {
			if _, ok := r.(hookExitSentinel); !ok {
				panic(r)
			}
		}
	}()
	for k, v := range env {
		t.Setenv(k, v)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(payload))
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	runToolPolicyHook(cmd)
	return exit, outBuf.String(), errBuf.String()
}

type hookExitSentinel struct{}

func TestHook_ReadToolUsesServer(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		json.NewEncoder(w).Encode(map[string]any{"allowed": true})
	}))
	defer srv.Close()
	exit, stdout, _ := runHookForTest(t, `{"tool_name":"Read","tool_input":{"file_path":"/x"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":     portOf(t, srv),
		"MULTICA_AGENT_PROVIDER":  "claude",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (allow)", exit)
	}
	if stdout != "" {
		t.Errorf("claude allow must keep stdout empty, got %q", stdout)
	}
	if !hit {
		t.Error("every named tool must hit the daemon")
	}
}

func TestHook_ForwardsTaskMandateGeneration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode hook request: %v", err)
		}
		if generation := body["claim_generation"]; generation != float64(13) {
			t.Errorf("claim_generation = %v, want 13", generation)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"allowed": true})
	}))
	defer srv.Close()
	exit, _, _ := runHookForTest(t, `{"tool_name":"Read","tool_input":{"file_path":"/x"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":             portOf(t, srv),
		"MULTICA_TASK_MANDATE_GENERATION": "13",
		"MULTICA_AGENT_PROVIDER":          "claude",
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
}

func TestHook_GatedAllow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"allowed": true})
	}))
	defer srv.Close()
	exit, _, _ := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":    portOf(t, srv),
		"MULTICA_AGENT_PROVIDER": "claude",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (allow)", exit)
	}
}

func TestHook_GatedDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"allowed": false, "reason": "Capped by user"})
	}))
	defer srv.Close()
	exit, _, stderr := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"curl https://x"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":    portOf(t, srv),
		"MULTICA_AGENT_PROVIDER": "claude",
	})
	if exit != hookExitDeny {
		t.Errorf("exit = %d, want %d (deny)", exit, hookExitDeny)
	}
	if !strings.Contains(stderr, "Capped by user") {
		t.Errorf("stderr missing reason: %q", stderr)
	}
}

func TestHook_TransportErrorFailsClosed(t *testing.T) {
	// No server listening on this port → transport error.
	const deadPort = "1" // unusable; dial fails fast
	exit, _, _ := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":    deadPort,
		"MULTICA_AGENT_PROVIDER": "claude",
	})
	if exit != hookExitDeny {
		t.Errorf("transport error exit = %d, want %d (fail closed)", exit, hookExitDeny)
	}

	malformedExit, _, _ := runHookForTest(t, `not-json`, map[string]string{
		"MULTICA_AGENT_PROVIDER": "claude",
	})
	if malformedExit != hookExitDeny {
		t.Errorf("malformed payload exit = %d, want %d", malformedExit, hookExitDeny)
	}
}

func TestHook_CursorAllowEmitsPermissionJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"allowed": true})
	}))
	defer srv.Close()
	// Cursor payload with cursor_version (not tool_use_id alone — Claude sends that too).
	payload := `{"tool_name":"Shell","tool_input":{"command":"printf ok"},"tool_use_id":"abc","cursor_version":"2026.07.20","cwd":"/tmp"}`
	exit, stdout, stderr := runHookForTest(t, payload, map[string]string{
		"MULTICA_DAEMON_PORT": portOf(t, srv),
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if stderr != "" {
		t.Fatalf("cursor allow must not write stderr, got %q", stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("stdout not JSON: %q (%v)", stdout, err)
	}
	if out["permission"] != "allow" {
		t.Fatalf("permission = %v, want allow", out["permission"])
	}
}

func TestHook_CursorProtocolFlagForcesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"allowed": true})
	}))
	defer srv.Close()
	prev := hookProtocolFlag
	hookProtocolFlag = "cursor"
	defer func() { hookProtocolFlag = prev }()
	// No cursor_version, no MULTICA_AGENT_PROVIDER — flag alone must force JSON.
	exit, stdout, _ := runHookForTest(t, `{"tool_name":"Shell","tool_input":{"command":"ls"}}`, map[string]string{
		"MULTICA_DAEMON_PORT": portOf(t, srv),
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("stdout not JSON: %q (%v)", stdout, err)
	}
	if out["permission"] != "allow" {
		t.Fatalf("permission = %v, want allow", out["permission"])
	}
}

func TestHook_CursorDenyEmitsPermissionJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"allowed": false, "reason": "not allowed"})
	}))
	defer srv.Close()
	exit, stdout, _ := runHookForTest(t, `{"tool_name":"Shell","tool_input":{"command":"rm -rf /"},"cursor_version":"1"}`, map[string]string{
		"MULTICA_DAEMON_PORT":    portOf(t, srv),
		"MULTICA_AGENT_PROVIDER": "cursor",
	})
	if exit != 0 {
		t.Fatalf("cursor deny must exit 0 with JSON, got %d stdout=%q", exit, stdout)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("stdout not JSON: %q (%v)", stdout, err)
	}
	if out["permission"] != "deny" {
		t.Fatalf("permission = %v, want deny", out["permission"])
	}
	if !strings.Contains(out["agent_message"].(string), "not allowed") {
		t.Fatalf("agent_message missing reason: %v", out["agent_message"])
	}
}

func TestHook_CursorTransportErrorJSONDeny(t *testing.T) {
	exit, stdout, _ := runHookForTest(t, `{"tool_name":"Shell","tool_input":{"command":"ls"},"hook_event_name":"preToolUse"}`, map[string]string{
		"MULTICA_DAEMON_PORT": "1",
	})
	if exit != 0 {
		t.Fatalf("cursor fail-closed must exit 0 with JSON deny, got %d", exit)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("stdout not JSON: %q", stdout)
	}
	if out["permission"] != "deny" {
		t.Fatalf("permission = %v, want deny", out["permission"])
	}
}

func portOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	// srv.URL is like http://127.0.0.1:PORT
	i := strings.LastIndex(srv.URL, ":")
	return srv.URL[i+1:]
}
