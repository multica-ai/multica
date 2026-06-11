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
func runHookForTest(t *testing.T, payload string, env map[string]string) (exit int, stderr string) {
	t.Helper()
	exit = 0
	prev := osExit
	osExit = func(code int) { exit = code; panic(hookExitSentinel{}) }
	var errBuf bytes.Buffer
	defer func() {
		osExit = prev
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
	cmd.SetErr(&errBuf)
	runToolPolicyHook(cmd)
	return exit, errBuf.String()
}

type hookExitSentinel struct{}

func TestHook_DefaultAllowToolSkipsServer(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	exit, _ := runHookForTest(t, `{"tool_name":"Read","tool_input":{"file_path":"/x"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":      portOf(t, srv),
		"CEREBRO_TOOLPOLICY_STAGE": "enforce",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (allow)", exit)
	}
	if hit {
		t.Error("default-allow tool should not hit the daemon")
	}
}

func TestHook_GatedAllow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"allowed": true})
	}))
	defer srv.Close()
	exit, _ := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":      portOf(t, srv),
		"CEREBRO_TOOLPOLICY_STAGE": "enforce",
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
	exit, stderr := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"curl https://x"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":      portOf(t, srv),
		"CEREBRO_TOOLPOLICY_STAGE": "enforce",
	})
	if exit != hookExitDeny {
		t.Errorf("exit = %d, want %d (deny)", exit, hookExitDeny)
	}
	if !strings.Contains(stderr, "Capped by user") {
		t.Errorf("stderr missing reason: %q", stderr)
	}
}

func TestHook_TransportErrorRespectsStage(t *testing.T) {
	// No server listening on this port → transport error.
	const deadPort = "1" // unusable; dial fails fast
	enforceExit, _ := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":      deadPort,
		"CEREBRO_TOOLPOLICY_STAGE": "enforce",
	})
	if enforceExit != hookExitDeny {
		t.Errorf("enforce transport error exit = %d, want %d (fail closed)", enforceExit, hookExitDeny)
	}
	observeExit, _ := runHookForTest(t, `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, map[string]string{
		"MULTICA_DAEMON_PORT":      deadPort,
		"CEREBRO_TOOLPOLICY_STAGE": "observe",
	})
	if observeExit != 0 {
		t.Errorf("observe transport error exit = %d, want 0 (fail open)", observeExit)
	}
}

func portOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	// srv.URL is like http://127.0.0.1:PORT
	i := strings.LastIndex(srv.URL, ":")
	return srv.URL[i+1:]
}
