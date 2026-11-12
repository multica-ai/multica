package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHermesGuidanceUnnegotiatedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	for _, tc := range []struct {
		name    string
		offer   string
		enabled bool
	}{
		{"legacy peer", `{}`, true},
		{"feature disabled", `{"_meta":{"ai.nous/active-turn-guidance":{"version":1,"statusLookup":true,"literalText":true,"terminalReceipts":true}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "controlled-hermes")
			trace, gate := filepath.Join(dir, "prompt.json"), filepath.Join(dir, "release")
			script := fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":%s}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '%%s' "$line" > "$HERMES_TEST_PROMPT"
      while [ ! -f "$HERMES_TEST_GATE" ]; do sleep 0.01; done
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`, tc.offer)
			writeTestExecutable(t, executable, []byte(script))
			backend := &hermesBackend{cfg: Config{ExecutablePath: executable, Logger: slog.Default(), Env: map[string]string{
				"HERMES_HOME": dir, "HERMES_TEST_PROMPT": trace, "HERMES_TEST_GATE": gate,
			}}}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "ordinary prompt", ExecOptions{Cwd: dir, EnableTaskSupplement: tc.enabled})
			if err != nil {
				t.Fatal(err)
			}
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for range session.Messages {
				}
			}()
			var prompt []byte
			for len(prompt) == 0 && ctx.Err() == nil {
				prompt, _ = os.ReadFile(trace)
				if len(prompt) == 0 {
					select {
					case <-ctx.Done():
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			if len(prompt) == 0 {
				t.Fatal("ordinary prompt was not submitted")
			}
			if strings.Contains(string(prompt), hermesGuidanceCapability) {
				t.Error("unnegotiated prompt contains guidance metadata")
			}
			if session.SupplementReady != nil && session.SupplementReady() {
				t.Error("unnegotiated peer became ready for guidance")
			}
			if tc.enabled && session.Supplement != nil && session.Supplement(ctx, "do not send") == nil {
				t.Error("legacy peer accepted guidance")
			}
			if err := os.WriteFile(gate, nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result:
				if result.Status != "completed" {
					t.Fatalf("ordinary prompt failed: %+v", result)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			<-drained
		})
	}
}
