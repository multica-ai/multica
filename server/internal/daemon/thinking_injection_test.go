package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/jev"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// One fake executable records actual argv and RPC requests across the existing
// launch channels. No installed CLI or provider account participates.
const thinkingInjectionScript = `#!/bin/sh
printf 'ARGV %s\n' "$*" >> "$THINKING_WIRE"
case "$*" in
  *export*) printf '%s\n' '{"messages":[]}'; exit 0;;
esac
case "$THINKING_PROVIDER:$1" in
  opencode:run)
    cat >/dev/null
    printf '%s\n' '{"type":"text","sessionID":"fixture","part":{"type":"text","text":"done"}}' '{"type":"step_finish","sessionID":"fixture","part":{"type":"step-finish","reason":"stop"}}'
    exit 0;;
  pi:*|omp:*)
    if [ "$THINKING_PROVIDER" = omp ]; then
      while [ "$#" -gt 0 ]; do
        case "$1" in --session-dir) dir="$2"; shift ;; esac
        shift
      done
      printf '%s\n' '{"type":"session","id":"fixture"}' > "$dir/fixture.jsonl"
    fi
    cat >/dev/null
    printf '%s\n' '{"type":"agent_start"}' '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"model":"test/model","stopReason":"stop"}}' '{"type":"agent_end","messages":[],"willRetry":false}'
    exit 0;;
esac
opencode_config='[{"type":"select","id":"model","category":"model","currentValue":"test/model","options":[{"value":"test/model","name":"Test"}]},{"type":"select","id":"effort","category":"thought_level","currentValue":"low","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]'
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$THINKING_WIRE"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ "$THINKING_RETRY" = 1 ] && [ ! -f "$THINKING_WIRE.retried" ]; then
        touch "$THINKING_WIRE.retried"
        exec sleep 3
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id";;
    *'"method":"thread/start"'*|*'"method":"thread/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"thread":{"id":"fixture-thread"}}}\n' "$id";;
    *'"method":"turn/start"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      printf '%s\n' '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn"}}}' '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"fixture-thread","item":{"type":"agentMessage","id":"msg","text":"done"}}}' '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn","status":"completed"}}}'
      exit 0;;
    *'"method":"session/new"'*|*'"method":"session/load"'*|*'"method":"session/set_model"'*)
      if [ "$THINKING_PROVIDER" = opencode ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fixture","configOptions":%s}}\n' "$id" "$opencode_config"
        continue
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fixture","models":{"currentModelId":"test/model","availableModels":[{"modelId":"test/model","name":"Test"}]},"configOptions":[{"type":"select","id":"effort","name":"Effort","category":"thought_level","currentValue":"low","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"},{"value":"max","name":"Max"}]}]}}\n' "$id";;
    *'"method":"session/set_config_option"'*'"configId":"model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":%s}}\n' "$id" "$opencode_config";;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"effort","currentValue":"high","options":[{"value":"low"},{"value":"high"},{"value":"max"}]}]}}\n' "$id";;
    *'"method":"session/prompt"'*)
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fixture","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0;;
    *) if [ -n "$id" ]; then printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"; fi;;
  esac
done
`

func TestAdaptiveThinkingInjectionChannels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	for _, tc := range []struct {
		name, provider, version, model, injection string
		retry                                     bool
	}{
		{"codex", "codex", "", "test/model", `"model_reasoning_effort":"high"`, false},
		{"codex-default", "codex", "", "", `"model_reasoning_effort":"high"`, false},
		{"codex-retry", "codex", "", "test/model", `"model_reasoning_effort":"high"`, true},
		{"opencode-v1", "opencode", "1.2.0", "test/model", "--variant high", false},
		{"opencode-v2", "opencode", "opencode v2.0.10", "test/model", "test/model#high", false},
		{"opencode-acp", "opencode", "opencode v2.0.10", "test/model", `"value":"high"`, false},
		{"pi", "pi", "", "test/model", "--thinking high", false},
		{"omp", "omp", "", "test/model", "--thinking high", false},
		{"kimi", "kimi", "", "test/model", `"value":"high"`, false},
		{"reasonix", "reasonix", "", "test/model", `"value":"high"`, false},
		{"hermes", "hermes", "", "test/model", `"value":"high"`, false},
	} {
		for _, invalid := range []bool{false, true} {
			name := tc.name
			if invalid {
				name += "/invalid"
			}
			t.Run(name, func(t *testing.T) {
				stubModelDiscovery(t, map[string]agent.Catalog{tc.provider: adaptTestCatalog()})
				client := &countingThinkingClient{decision: jev.Decision{Level: "high"}}
				if invalid {
					client.decision.Level = agent.ThinkingLevelAdapt
				}
				run := &thinkingRun{Client: client}
				sel := resolveTaskModelSelection(t.Context(), tc.provider, agent.Command{}, taskModelSelection{Model: tc.model, ThinkingLevel: agent.ThinkingLevelAdapt}, quietTaskLog(), run)
				dir := t.TempDir()
				wire := filepath.Join(dir, "wire")
				fake := filepath.Join(dir, "fixture")
				writeTestExecutable(t, fake, []byte(thinkingInjectionScript))
				env := map[string]string{"THINKING_WIRE": wire, "THINKING_PROVIDER": tc.provider}
				if tc.provider == "opencode" {
					env["MULTICA_OPENCODE_TRANSPORT"] = ""
				}
				if tc.name == "opencode-v2" {
					env["MULTICA_OPENCODE_TRANSPORT"] = "run"
				}
				if tc.retry {
					env["THINKING_RETRY"] = "1"
				}
				backend, err := agent.ResolveBackend(tc.provider, agent.Config{ExecutablePath: fake, CLIVersion: tc.version, BuiltinRuntime: true, Logger: quietTaskLog(), Env: env})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				session, err := backend.Execute(ctx, "ping", agent.ExecOptions{Cwd: dir, Model: sel.Model, ThinkingLevel: sel.ThinkingLevel, Timeout: 5 * time.Second, HandshakeTimeout: 100 * time.Millisecond})
				if err != nil {
					t.Fatal(err)
				}
				for range session.Messages {
				}
				result := <-session.Result
				if result.Status != "completed" {
					t.Fatalf("backend failed: %+v", result)
				}
				raw, err := os.ReadFile(wire)
				if err != nil {
					t.Fatal(err)
				}
				got := string(raw)
				if strings.Contains(got, agent.ThinkingLevelAdapt) {
					t.Fatalf("reserved mode reached CLI/ACP: %s", got)
				}
				wantInjection := !invalid && tc.model != ""
				if strings.Contains(got, tc.injection) != wantInjection {
					t.Fatalf("injection=%v expected=%v: %s", strings.Contains(got, tc.injection), wantInjection, got)
				}
				wantCalls := 1
				if tc.model == "" {
					wantCalls = 0
				}
				if client.calls != wantCalls {
					t.Fatalf("Jev calls=%d want=%d", client.calls, wantCalls)
				}
				if tc.provider == "codex" && strings.Contains(got, `"effort":"high"`) != wantInjection {
					t.Fatalf("codex turn effort mismatch: %s", got)
				}
				if tc.retry && strings.Count(got, "ARGV ") != 2 {
					t.Fatalf("did not exercise second codex attempt: %s", got)
				}
			})
		}
	}
}
