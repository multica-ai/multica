package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAntigravityToolsWithSlowConsumer(t *testing.T) {
	for _, cancelWhileFull := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelWhileFull), func(t *testing.T) {
			t.Parallel()
			const calls = 400 // More lifecycle events than the message buffer holds.
			var script strings.Builder
			script.WriteString("#!/bin/sh\n")
			for i := 0; i < calls; i++ {
				fmt.Fprintf(&script, "printf '%%s\\n' '{\"event\":\"step_update\",\"step_update\":{\"step_index\":%d,\"state\":\"DONE\",\"step_type\":\"tool\",\"tool_name\":\"read_file\",\"tool_info\":{\"output\":\"ok\"}}}'\n", i)
			}
			script.WriteString("printf '%s\\n' '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"Finished\"}}'\n")
			fakePath := filepath.Join(t.TempDir(), "agy")
			writeTestExecutable(t, fakePath, []byte(script.String()))
			backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "ignored", ExecOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for len(session.Messages) < cap(session.Messages) {
				select {
				case <-ctx.Done():
					t.Fatal("message buffer never filled")
				case <-time.After(time.Millisecond):
				}
			}
			if cancelWhileFull {
				cancel()
				select {
				case result := <-session.Result:
					if result.Status != "aborted" {
						t.Fatalf("result = %+v, want cancellation", result)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("cancellation blocked behind the full message buffer")
				}
				return
			}
			// Without backpressure the process completes here, having discarded
			// most tool events. A live consumer may temporarily pause just like this.
			select {
			case <-session.Result:
				t.Fatal("execution completed while its tool events could not be delivered")
			case <-time.After(100 * time.Millisecond):
			}
			uses, results := map[string]bool{}, map[string]bool{}
			for message := range session.Messages {
				var seen map[string]bool
				switch message.Type {
				case MessageToolUse:
					seen = uses
				case MessageToolResult:
					seen = results
				default:
					continue
				}
				if message.CallID == "" || seen[message.CallID] {
					t.Fatalf("missing or duplicate call identity: %+v", message)
				}
				seen[message.CallID] = true
			}
			if len(uses) != calls || !reflect.DeepEqual(uses, results) {
				t.Fatalf("tool events lost: uses=%d results=%d, want %d matched pairs", len(uses), len(results), calls)
			}
			if result := <-session.Result; result.Status != "completed" {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestAntigravityToolInputNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, tool  string
		input, want map[string]any
	}{
		{"shell command", "run_command", map[string]any{"CommandLine": "echo hello", "Cwd": "/workspace"}, map[string]any{"Cwd": "/workspace", "command": "echo hello"}},
		{"file read", "view_file", map[string]any{"AbsolutePath": "/workspace/a.go"}, map[string]any{"file_path": "/workspace/a.go"}},
		{
			name: "file write", tool: "write_to_file",
			input: map[string]any{"TargetFile": "/workspace/a.go", "CodeContent": "package main\n"},
			want:  map[string]any{"file_path": "/workspace/a.go", "content": "package main\n"},
		},
		{
			name: "file edit", tool: "replace_file_content",
			input: map[string]any{"TargetFile": "/workspace/a.go", "TargetContent": "before", "ReplacementContent": "after"},
			want:  map[string]any{"file_path": "/workspace/a.go", "old_string": "before", "new_string": "after"},
		},
		{
			name: "empty file", tool: "write_to_file",
			input: map[string]any{"TargetFile": "/workspace/empty.go", "CodeContent": ""},
			want:  map[string]any{"file_path": "/workspace/empty.go", "content": ""},
		},
		{
			name: "delete text", tool: "replace_file_content",
			input: map[string]any{"TargetFile": "/workspace/a.go", "TargetContent": "before", "ReplacementContent": ""},
			want:  map[string]any{"file_path": "/workspace/a.go", "old_string": "before", "new_string": ""},
		},
		{
			name: "insert text", tool: "replace_file_content",
			input: map[string]any{"TargetContent": "", "ReplacementContent": "after"},
			want:  map[string]any{"old_string": "", "new_string": "after"},
		},
		{
			name: "content collisions retained", tool: "custom",
			input: map[string]any{"CodeContent": "other", "content": "", "TargetContent": "before", "old_string": nil, "ReplacementContent": "after", "new_string": false},
			want:  map[string]any{"CodeContent": "other", "content": "", "TargetContent": "before", "old_string": nil, "ReplacementContent": "after", "new_string": false},
		},
		{
			name: "invalid content retained", tool: "custom",
			input: map[string]any{"CodeContent": 123, "TargetContent": nil, "ReplacementContent": false},
			want:  map[string]any{"CodeContent": 123, "TargetContent": nil, "ReplacementContent": false},
		},
		{
			name: "replacement chunks remain opaque", tool: "multi_replace_file_content",
			input: map[string]any{"TargetFile": "/workspace/a.go", "ReplacementChunks": []any{map[string]any{"TargetContent": "before", "ReplacementContent": ""}}},
			want:  map[string]any{"file_path": "/workspace/a.go", "ReplacementChunks": []any{map[string]any{"TargetContent": "before", "ReplacementContent": ""}}},
		},
		{"empty command retained", "run_command", map[string]any{"CommandLine": ""}, map[string]any{"CommandLine": ""}},
		{"empty path retained", "view_file", map[string]any{"AbsolutePath": ""}, map[string]any{"AbsolutePath": ""}},
		{"canonical fields win", "run_command", map[string]any{"CommandLine": "other", "command": "original", "AbsolutePath": "other", "file_path": "original"}, map[string]any{"CommandLine": "other", "command": "original", "AbsolutePath": "other", "file_path": "original"}},
		{"explicit canonical null retained", "run_command", map[string]any{"CommandLine": "other", "command": nil}, map[string]any{"CommandLine": "other", "command": nil}},
		{"invalid aliases ignored", "custom", map[string]any{"CommandLine": 123, "AbsolutePath": nil, "TargetFile": ""}, map[string]any{"CommandLine": 123, "AbsolutePath": nil, "TargetFile": ""}},
		{"unknown parameters retained", "custom", map[string]any{"custom": true}, map[string]any{"custom": true}},
		{"missing parameters", "run_command", nil, nil},
	}
	for _, tt := range tests {
		for _, state := range []string{"ACTIVE", "DONE"} {
			t.Run(tt.name+"/"+state, func(t *testing.T) {
				before, err := json.Marshal(tt.input)
				if err != nil {
					t.Fatal(err)
				}
				index := 0
				step := antigravityStreamStepUpdate{
					StepIndex: &index, State: state, StepType: "tool", ToolName: tt.tool,
					ToolInfo: &antigravityStreamTool{Parameters: tt.input},
				}
				messages := antigravityToolMessages(&step, make(map[int]antigravityToolState))
				if len(messages) == 0 || messages[0].Type != MessageToolUse || !reflect.DeepEqual(messages[0].Input, tt.want) {
					t.Fatalf("messages = %+v, want tool use input %#v", messages, tt.want)
				}
				after, err := json.Marshal(tt.input)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatalf("provider parameters mutated: %s -> %s", before, after)
				}
				if got := antigravityToolInput(messages[0].Input); !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("normalization is not idempotent: %#v", got)
				}
			})
		}
	}
}

func TestAntigravityToolMessages(t *testing.T) {
	t.Parallel()
	verboseOutput := strings.Repeat("x", 9000)
	tests := []struct {
		name  string
		steps []string
		want  []Message
	}{
		{
			name: "repeated and interleaved snapshots",
			steps: []string{
				`{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_name":"read_file","tool_info":{"parameters":{"path":"a.go"}}}`,
				`{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_info":{"name":"run_command"}}`,
				`{"step_index":1,"state":"DONE","step_type":"tool","tool_info":{"output":"ok"}}`,
				`{"step_index":0,"state":"DONE","step_type":"tool","tool_info":{"output":{"text":"source"}}}`,
				`{"step_index":0,"state":"DONE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}`,
			},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-0", Tool: "read_file", Input: map[string]any{"path": "a.go"}},
				{Type: MessageToolUse, CallID: "agy-step-1", Tool: "run_command"},
				{Type: MessageToolResult, CallID: "agy-step-1", Tool: "run_command", Output: "ok"},
				{Type: MessageToolResult, CallID: "agy-step-0", Tool: "read_file", Output: `{"text":"source"}`},
			},
		},
		{
			name:  "done only tool failure remains a tool result",
			steps: []string{`{"step_index":2,"state":"DONE","step_type":"tool","tool_info":{"name":"run_command","output":"partial","error":{"type":"exit","message":"exit 1"}}}`},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-2", Tool: "run_command"},
				{Type: MessageToolResult, CallID: "agy-step-2", Tool: "run_command", Output: "Tool error: exit: exit 1\npartial"},
			},
		},
		{
			name:  "failure detail survives prefix truncation of verbose output",
			steps: []string{fmt.Sprintf(`{"step_index":2,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"output":%q,"error":{"message":"exit 1"}}}`, verboseOutput)},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-2", Tool: "run_command"},
				{Type: MessageToolResult, CallID: "agy-step-2", Tool: "run_command", Output: "Tool error: exit 1\n" + verboseOutput},
			},
		},
		{
			name: "missing metadata can arrive later",
			steps: []string{
				`{"step_index":3,"state":"ACTIVE","step_type":"tool"}`,
				`{"step_index":3,"state":"DONE","step_type":"tool","tool_name":"list_dir","tool_info":{"output":null}}`,
			},
			want: []Message{
				{Type: MessageToolUse, CallID: "agy-step-3", Tool: "list_dir"},
				{Type: MessageToolResult, CallID: "agy-step-3", Tool: "list_dir"},
			},
		},
		{
			name: "ignore missing indices unknown states and non tools",
			steps: []string{
				`{"state":"DONE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":-1,"state":"DONE","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":0,"state":"QUEUED","step_type":"tool","tool_name":"read_file"}`,
				`{"step_index":0,"state":"DONE","step_type":"agent_response","text_delta":"hello"}`,
			},
		},
		{
			name:  "unfinished tool is not fabricated as complete",
			steps: []string{`{"step_index":4,"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}`},
			want:  []Message{{Type: MessageToolUse, CallID: "agy-step-4", Tool: "read_file"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			states := make(map[int]antigravityToolState)
			var got []Message
			for _, raw := range tt.steps {
				var step antigravityStreamStepUpdate
				if err := json.Unmarshal([]byte(raw), &step); err != nil {
					t.Fatal(err)
				}
				got = append(got, antigravityToolMessages(&step, states)...)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("messages = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The fake stays alive until the test receives its tool event. This proves
// delivery is live, rather than reconstructed after process completion.
func TestAntigravityToolsStreamBeforeProcessExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	fakePath := filepath.Join(dir, "agy")
	writeTestExecutable(t, fakePath, []byte(fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"event":"step_update","step_update":{"step_index":4,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"echo hello"}}}}'
while [ ! -f %q ]; do sleep 0.01; done
printf '%%s\n' '{"event":"step_update","step_update":{"step_index":4,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"output":"hello\n"}}}'
printf '%%s\n' '{"event":"result","result":{"status":"SUCCESS","response":"Finished"}}'
`, release)))
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
	var use, result *Message
	for msg := range session.Messages {
		switch msg.Type {
		case MessageToolUse:
			if use != nil {
				t.Fatal("duplicate tool use")
			}
			copy := msg
			use = &copy
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		case MessageToolResult:
			copy := msg
			result = &copy
		}
	}
	if use == nil || result == nil {
		t.Fatalf("missing live tool lifecycle: use=%+v result=%+v", use, result)
	}
	if use.Tool != "run_command" || use.Input["command"] != "echo hello" || use.CallID == "" {
		t.Fatalf("unexpected tool use: %+v", use)
	}
	if _, exists := use.Input["CommandLine"]; exists {
		t.Fatalf("provider command was duplicated in normalized input: %+v", use.Input)
	}
	if result.CallID != use.CallID || result.Tool != use.Tool || result.Output != "hello\n" {
		t.Fatalf("unexpected tool result: %+v", result)
	}
	if got := <-session.Result; got.Status != "completed" || got.Output != "Finished" {
		t.Fatalf("unexpected execution result: %+v", got)
	}
}
