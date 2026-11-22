//go:build unix

package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAntigravityExecuteSteersRunningTurnThroughHooks(t *testing.T) {
	t.Parallel()
	// "delivered": agy records the injected guidance as the step's user input.
	// "foreign hook input": Multica's reply was not applied; another hook
	// injected its own input, which carries no marker.
	for name, wantDelivered := range map[string]bool{"delivered": true, "foreign hook input": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testAntigravityExecuteSteering(t, wantDelivered)
		})
	}
}

func testAntigravityExecuteSteering(t *testing.T, wantDelivered bool) {
	cwd := t.TempDir()
	appDataDir := t.TempDir()
	transcript := filepath.Join(appDataDir, "brain", "conv-steer", ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'I0630 1 common.go:156] CLI app data directory: %s\n' '` + appDataDir + `' >> "$log"
printf '%s\n%s\n' "$MULTICA_ANTIGRAVITY_HOOK_URL" "$MULTICA_ANTIGRAVITY_HOOK_TOKEN" > hook-env.txt
printf '%s\n' '{"event":"init","conversation_id":"conv-steer","init":{}}'
printf '%s\n' '{"type":"USER_INPUT","step_index":0,"content":"original goal"}' >> '` + transcript + `'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"conv-steer","step_index":0,"state":"DONE","step_type":"user_input"}}'
while [ ! -f injected ]; do sleep 0.02; done
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"conv-steer","step_index":3,"state":"DONE","step_type":"user_input"}}'
while [ ! -f answer ]; do sleep 0.02; done
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"conv-steer","step_index":4,"state":"DONE","step_type":"agent_response","text_delta":"Done, with the smaller fix."}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"conv-steer","status":"ERROR","response":"Done, with the smaller fix.","error":"There was a network issue connecting to the server, please try again."}}'
`
	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(antigravity): %v", err)
	}
	session, err := backend.Execute(t.Context(), "original goal", ExecOptions{
		Cwd:                   cwd,
		Timeout:               30 * time.Second,
		EnableTaskSupplement:  true,
		SupplementHookCommand: testAntigravityHookCommand,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if session.Supplement == nil || session.SupplementReady == nil {
		t.Fatal("negotiated antigravity session exposes no supplement hooks")
	}

	deadline := time.Now().Add(10 * time.Second)
	for !session.SupplementReady() {
		if time.Now().After(deadline) {
			t.Fatal("session never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var envLines []string
	for len(envLines) < 2 {
		data, _ := os.ReadFile(filepath.Join(cwd, "hook-env.txt"))
		envLines = strings.Fields(string(data))
		if time.Now().After(deadline) {
			t.Fatal("agy did not receive the hook environment")
		}
		time.Sleep(10 * time.Millisecond)
	}
	getenv := func(name string) string {
		switch name {
		case antigravityHookURLEnv:
			return envLines[0]
		case antigravityHookTokenEnv:
			return envLines[1]
		}
		return ""
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "hooks.json")); err != nil {
		t.Fatalf("hooks.json missing during the run: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- session.Supplement(t.Context(), "Prefer the smaller fix.") }()
	var injected []string
	var reply map[string]any
	for len(injected) == 0 && time.Now().Before(deadline) {
		reply = relayAntigravityHook(t, getenv, preInvocation("conv-steer"))
		injected = injectedUserMessages(t, reply)
		time.Sleep(10 * time.Millisecond)
	}
	if len(injected) != 1 || injected[0] != "Prefer the smaller fix." {
		t.Fatalf("injected = %q", injected)
	}
	assertAntigravitySupplementWaiting(t, done)
	// agy appends the step's transcript record before it streams the step.
	content := "lint: 3 warnings"
	if wantDelivered {
		content = reply["injectSteps"].([]any)[0].(map[string]any)["userMessage"].(string)
	}
	record := antigravityUserInputRecord(t, 3, content) + "\n"
	if f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
		t.Fatal(err)
	} else if _, err := f.WriteString(record); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}
	if err := os.WriteFile(filepath.Join(cwd, "injected"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if wantDelivered {
		if err := <-done; err != nil {
			t.Fatalf("Supplement = %v, want delivered", err)
		}
	} else {
		assertAntigravitySupplementWaiting(t, done)
	}
	if err := os.WriteFile(filepath.Join(cwd, "answer"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !wantDelivered {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Supplement = %v, want canceled at turn end so the daemon falls back", err)
		}
	}

	// The injected user_input step belongs to the turn, so a trailing transport
	// error after the final answer still recovers as completed.
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if session.SupplementReady() {
		t.Fatal("ready after the terminal result")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "hooks.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hooks.json after the run: %v, want removed", err)
	}
}

func TestAntigravityExecuteWithoutNegotiationInstallsNothing(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "${MULTICA_ANTIGRAVITY_HOOK_URL:-unset}" > hook-env.txt
ls -a .agents > agents-dir.txt 2>&1 || true
printf '%s\n' '{"event":"init","conversation_id":"conv-plain","init":{}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"conv-plain","status":"SUCCESS","response":"done"}}'
`
	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(t.Context(), "goal", ExecOptions{
		Cwd: cwd, Timeout: 30 * time.Second, SupplementHookCommand: testAntigravityHookCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if session.Supplement != nil || session.SupplementReady != nil {
		t.Fatal("unnegotiated run exposes supplement hooks")
	}
	if data, _ := os.ReadFile(filepath.Join(cwd, "hook-env.txt")); strings.TrimSpace(string(data)) != "unset" {
		t.Fatalf("hook env leaked into an unnegotiated run: %q", data)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".agents created for an unnegotiated run: %v", err)
	}
}

func TestAntigravityExecuteWithUnmanagedHooksFailsSupplementsFast(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"user":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\n' '{"event":"init","conversation_id":"conv-own-hooks","init":{}}'
while [ ! -f release ]; do sleep 0.02; done
printf '%s\n' '{"event":"result","result":{"conversation_id":"conv-own-hooks","status":"SUCCESS","response":"done"}}'
`
	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(t.Context(), "goal", ExecOptions{
		Cwd: cwd, Timeout: 30 * time.Second, EnableTaskSupplement: true, SupplementHookCommand: testAntigravityHookCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if session.Supplement == nil || !session.SupplementReady() {
		t.Fatal("negotiated run must settle supplements instead of leaving them pending")
	}
	if err := session.Supplement(t.Context(), "guidance"); err == nil || !strings.Contains(err.Error(), "hooks unavailable") {
		t.Fatalf("Supplement = %v, want hooks-unavailable failure", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if data, _ := os.ReadFile(path); string(data) != `{"user":{}}` {
		t.Fatalf("user hooks.json changed: %s", data)
	}
}
