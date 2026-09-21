package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A test-created Grok ACP process carries provider cost through the real
// backend and runTask conversion, without an installed CLI or model account.
const taskUsageGrokFixture = `#!/bin/sh
case "$*" in *--version*) echo '1.0.0'; exit 0;; esac
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token","name":"Test authentication"}],"agentCapabilities":{"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id";;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session","models":{"currentModelId":"grok-4.6","availableModels":[{"modelId":"grok-4.6","name":"Test model"}]}}}\n' "$id";;
    *'"method":"session/prompt"'*)
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"test-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","_meta":{"modelId":"grok-4.6","usage":{"inputTokens":FIXTURE_INPUT,"costUsdTicks":FIXTURE_COST}}}}\n' "$id";;
    *) if [ -n "$id" ]; then printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"; fi;;
  esac
done
`

func TestRunTaskPreservesProviderCostWithoutTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}
	for _, tc := range []struct {
		name        string
		input, cost int64
		want        []TaskUsageEntry
	}{
		{"cost_only", 0, 100000000, []TaskUsageEntry{{Provider: "grok", Model: "grok-4.6", CostUSDTicks: 100000000}}},
		{"tokens_and_cost", 10, 100000000, []TaskUsageEntry{{Provider: "grok", Model: "grok-4.6", InputTokens: 10, CostUSDTicks: 100000000}}},
		{"tokens_only", 10, 0, []TaskUsageEntry{{Provider: "grok", Model: "grok-4.6", InputTokens: 10}}},
		{"empty", 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			fake := filepath.Join(t.TempDir(), "grok-fixture")
			script := strings.NewReplacer("FIXTURE_INPUT", fmt.Sprint(tc.input), "FIXTURE_COST", fmt.Sprint(tc.cost)).Replace(taskUsageGrokFixture)
			writeTestExecutable(t, fake, []byte(script))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			d := &Daemon{
				client: NewClient(srv.URL), logger: logger,
				workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
				runtimeIndex: map[string]Runtime{"test-rt": {ID: "test-rt", Provider: "grok"}},
				cfg: Config{
					WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second, ServerBaseURL: srv.URL,
					Agents: map[string]AgentEntry{"grok": {Path: fake}},
				},
			}
			task := Task{
				ID: "test-task", WorkspaceID: "test-ws", RuntimeID: "test-rt", IssueID: "test-issue",
				AgentID: "test-agent", AuthToken: "mat_test_usage",
				Agent: &AgentData{ID: "test-agent", Name: "test", Model: "grok-4.6"},
			}
			result, err := d.runTask(context.Background(), task, "grok", 0, logger)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "completed" || result.Comment != "done" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if !reflect.DeepEqual(result.Usage, tc.want) {
				t.Fatalf("usage = %+v, want %+v", result.Usage, tc.want)
			}
		})
	}
}
