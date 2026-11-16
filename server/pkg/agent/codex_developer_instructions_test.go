package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestCodexThreadPassesRunScopedDeveloperInstructions(t *testing.T) {
	for _, mode := range []string{"start", "resume", "resume fallback"} {
		t.Run(mode, func(t *testing.T) {
			c, stdin, _ := newTestCodexClient(t)
			prior := ""
			method := "thread/start"
			if mode != "start" {
				prior = "prior-thread"
				method = "thread/resume"
			}
			assertInstructions := func(t *testing.T, params map[string]any) {
				if params["developerInstructions"] != "Current platform routing" {
					t.Fatalf("developer instructions = %v", params["developerInstructions"])
				}
			}
			script := []rpcResponse{}
			if mode == "resume fallback" {
				script = append(script, rpcResponse{
					method: "thread/resume", errMsg: "unknown thread", errCode: -32602,
					assertFn: assertInstructions,
				})
				method = "thread/start"
			}
			script = append(script, rpcResponse{
				method: method, result: json.RawMessage(`{"thread":{"id":"thread-id"}}`),
				assertFn: assertInstructions,
			})
			wait := drainRPCScript(t, c, stdin, script)
			defer wait()
			threadID, resumed, err := c.startOrResumeThread(context.Background(), ExecOptions{
				ResumeSessionID: prior, DeveloperInstructions: "Current platform routing",
				SystemPrompt: codexRuntimeBriefCanary,
			}, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			if threadID != "thread-id" || resumed != (mode == "resume") {
				t.Fatalf("thread = %q, resumed = %v", threadID, resumed)
			}
		})
	}
}
