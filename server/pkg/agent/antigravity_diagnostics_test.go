package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This probe establishes adapter behavior for an error_message step already
// recognized by its recovery bookkeeping. It does not establish which retry
// diagnostics a particular installed agy version emits on stdout vs stderr.
func TestAntigravityReportsRuntimeErrorStepWhileRunning(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
printf '%s\n' '{"event":"step_update","step_update":{"step_index":10,"state":"DONE","step_type":"user_input"}}'
printf '%s\n' '{"event":"step_update","step_update":{"step_index":11,"state":"DONE","step_type":"error_message"}}'
# Remain live so a diagnostic delivered only after exit cannot satisfy the test.
sleep 30
`))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "ignored", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message, ok := <-session.Messages:
			if !ok {
				t.Fatal("session ended before reporting its runtime error")
			}
			if message.Type == MessageDiagnostic && strings.TrimSpace(message.Content) != "" {
				return
			}
		case <-session.Result:
			t.Fatal("diagnostic was not delivered while the runtime was live")
		case <-timer.C:
			t.Fatal("runtime error step produced no live diagnostic")
		}
	}
}

func TestAntigravityRuntimeDiagnosticsPreserveRecoveryAndDeduplicateSnapshots(t *testing.T) {
	step := func(index int, kind, state string) string {
		return fmt.Sprintf("printf '%%s\\n' '{\"event\":\"step_update\",\"step_update\":{\"step_index\":%d,\"step_type\":%q,\"state\":%q}}'\n", index, kind, state)
	}
	for _, tc := range []struct {
		name, events string
		want         int
	}{
		{"historical error", step(1, "error_message", "DONE") + step(10, "user_input", "DONE"), 0},
		{"repeated snapshot", step(10, "user_input", "DONE") + step(11, "error_message", "ACTIVE") + step(11, "error_message", "DONE") + step(11, "error_message", "DONE"), 1},
		{"distinct errors", step(10, "user_input", "DONE") + step(11, "error_message", "DONE") + step(12, "error_message", "DONE"), 2},
		{"replayed earlier step", step(10, "user_input", "DONE") + step(1, "error_message", "DONE"), 0},
		{"conflicting replayed boundary", step(10, "user_input", "DONE") + step(0, "user_input", "DONE") + step(1, "error_message", "DONE"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakePath := filepath.Join(t.TempDir(), "agy")
			writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+tc.events+"printf '%s\\n' '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"Recovered.\"}}'\n"))
			backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "ignored", ExecOptions{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for msg := range session.Messages {
				if msg.Type == MessageDiagnostic {
					count++
					if strings.TrimSpace(msg.Content) == "" {
						t.Error("empty runtime diagnostic")
					}
				}
			}
			if count != tc.want {
				t.Errorf("diagnostics = %d, want %d", count, tc.want)
			}
			result := <-session.Result
			if result.Status != "completed" || result.Error != "" || result.Output != "Recovered." {
				t.Errorf("a recovered runtime error changed the final result: %+v", result)
			}
		})
	}
}
