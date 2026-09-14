package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAntigravityToolTerminalStates(t *testing.T) {
	for _, state := range []string{"DONE", "ERROR", "FAILED", "CANCELLED", "CANCELED", "ABORTED"} {
		t.Run(state, func(t *testing.T) {
			calls := make(map[string]antigravityToolState)
			step := &antigravityStreamStepUpdate{StepType: "tool", StepIndex: 7, State: state, ToolName: "run_command", ToolInfo: &antigravityStreamToolInfo{Error: json.RawMessage(`"failure details"`)}}
			got := step.toolMessages("session", calls)
			if len(got) != 2 || !strings.Contains(got[1].Output, "failure details") {
				t.Fatalf("terminal: %+v", got)
			}
			if duplicate := step.toolMessages("session", calls); len(duplicate) != 0 {
				t.Fatalf("duplicate: %+v", duplicate)
			}
		})
	}
	calls := make(map[string]antigravityToolState)
	for _, step := range []*antigravityStreamStepUpdate{
		{StepType: "agent_response", State: "ACTIVE", ToolName: "run_command"},
		{StepType: "tool", State: "NEW_UNKNOWN_STATE", ToolName: "run_command"},
		{StepType: "tool", State: "ACTIVE"},
	} {
		if got := step.toolMessages("session", calls); len(got) != 0 {
			t.Fatalf("incomplete/non-tool snapshot: %+v", got)
		}
	}
	if got := antigravityToolText(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null output: %q", got)
	}
}

func TestAntigravityToolLifecycle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, path, []byte(`#!/bin/sh
cat <<'JSONL'
{"event":"init","conversation_id":"session-a"}
{"event":"step_update","step_update":{"step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"echo hello"}}}}
{"event":"step_update","step_update":{"step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}}
{"event":"step_update","step_update":{"step_index":3,"state":"ACTIVE","step_type":"tool","tool_info":{"name":"view_file","parameters":{"AbsolutePath":"/tmp/test"}}}}
{"event":"step_update","step_update":{"step_index":3,"state":"ERROR","step_type":"tool","tool_info":{"error":{"type":"TOOL_ERROR","message":"file missing"}}}}
{"event":"step_update","step_update":{"step_index":2,"state":"DONE","step_type":"tool","tool_info":{"output":"hello\n"}}}
{"event":"step_update","step_update":{"step_index":2,"state":"DONE","step_type":"tool","tool_info":{"output":"hello\n"}}}
{"event":"step_update","step_update":{"step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}}
{"event":"step_update","step_update":{"conversation_id":"session-b","step_index":2,"state":"DONE","step_type":"tool","tool_info":{"name":"run_command","output":{"exit_code":0}}}}
{"event":"step_update","step_update":{"step_index":4,"state":"DONE","step_type":"agent_response","text_delta":"Finished."}}
{"event":"result","result":{"status":"SUCCESS","response":"Finished."}}
JSONL
`))
	backend := &antigravityBackend{cfg: Config{ExecutablePath: path, Logger: quietAntigravityLogger()}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "test", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var tools []Message
	for msg := range session.Messages {
		if msg.Type == MessageToolUse || msg.Type == MessageToolResult {
			tools = append(tools, msg)
		}
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "Finished." {
		t.Fatalf("result: %+v", result)
	}
	if len(tools) != 6 {
		t.Fatalf("want exactly 3 starts and 3 results; got %+v", tools)
	}
	if tools[0].Type != MessageToolUse || tools[0].Tool != "run_command" || tools[0].Input["CommandLine"] != "echo hello" {
		t.Fatalf("start: %+v", tools[0])
	}
	if tools[1].Tool != "view_file" || tools[1].Type != MessageToolUse {
		t.Fatalf("second start: %+v", tools[1])
	}
	if tools[2].Type != MessageToolResult || tools[2].CallID != tools[1].CallID || !strings.Contains(tools[2].Output, "file missing") {
		t.Fatalf("error result: %+v", tools[2])
	}
	if tools[3].Type != MessageToolResult || tools[3].CallID != tools[0].CallID || tools[3].Output != "hello\n" {
		t.Fatalf("success result: %+v", tools[3])
	}
	if tools[4].Type != MessageToolUse || tools[5].Type != MessageToolResult || tools[4].CallID != tools[5].CallID || tools[4].CallID == tools[0].CallID {
		t.Fatalf("terminal-only, cross-session identity: %+v", tools[4:])
	}
	if tools[5].Output != `{"exit_code":0}` {
		t.Fatalf("structured output: %q", tools[5].Output)
	}
}

func TestAntigravityToolStartIsLive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "agy")
	writeTestExecutable(t, path, []byte(`#!/bin/sh
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"live","step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}}'
while [ ! -f release ]; do sleep 0.05; done
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"live","step_index":1,"state":"DONE","step_type":"tool","tool_info":{"output":"ok"}}}'
printf '%s\n' '{"event":"result","result":{"status":"SUCCESS","response":"done"}}'
`))
	backend := &antigravityBackend{cfg: Config{ExecutablePath: path, Logger: quietAntigravityLogger()}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "test", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case msg, ok := <-session.Messages:
			if !ok {
				t.Fatal("stream closed before tool start")
			}
			if msg.Type != MessageToolUse {
				continue
			}
			select {
			case <-session.Result:
				t.Fatal("tool start was buffered until completion")
			default:
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("result: %+v", result)
			}
			return
		case <-ctx.Done():
			t.Fatal("tool start not delivered while command was running")
		}
	}
}
