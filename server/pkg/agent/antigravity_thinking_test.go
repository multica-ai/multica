package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAntigravityThinkingCurrentTurn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cid := "bbaabbaa-1111-2222-3333-444444444444"
	transcript := filepath.Join(root, "brain", cid, ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "agy.log")
	if err := os.WriteFile(logPath, []byte("CLI app data directory: "+root+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := `{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":1,"thinking":"old history"}
{"type":"USER_INPUT","step_index":10}
{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":11,"thinking":"Inspecting the implementation.","content":null}
{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":11,"thinking":"Inspecting the implementation."}
{"type":"PLANNER_RESPONSE","source":"MODEL","status":"IN_PROGRESS","step_index":12,"thinking":"partial thought"}
{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":12,"thinking":"Checking the tests.","content":"Final answer must stay on stdout."}
{"type":"PLANNER_RESPONSE","source":"SYSTEM","status":"DONE","step_index":13,"thinking":"system text"}
{"type":"GENERIC","source":"MODEL","status":"DONE","step_index":14,"thinking":"tool text"}
{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","thinking":"missing index"}
{"type":"USER_INPUT","step_index":20}
{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":21,"thinking":"later turn"}
`
	if err := os.WriteFile(transcript, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	messages := make(chan Message, 8)
	writer := &antigravityMessages{messages: messages}
	progress := &antigravityThinkingProgress{logPath: logPath, send: func(msg Message) bool { return writer.send(t.Context(), msg, false) }}
	progress.poll() // No stdout turn boundary: never replay native history.
	if len(messages) != 0 {
		t.Fatal("thinking emitted without a current-turn boundary")
	}
	start := 10
	progress.setScope(cid, &start)
	progress.poll()
	progress.poll()
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want the two settled thoughts from this turn", len(messages))
	}
	for _, want := range []string{"Inspecting the implementation.", "\n\nChecking the tests."} {
		msg := <-messages
		if msg.Type != MessageThinking || msg.Content != want {
			t.Fatalf("got %#v, want thinking %q", msg, want)
		}
	}
}

func TestAntigravityThinkingPartialLineAndBackpressure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cid := "bbaabbaa-1111-2222-3333-444444444444"
	path := filepath.Join(root, "brain", cid, ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "agy.log")
	if err := os.WriteFile(logPath, []byte("CLI app data directory: "+root+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	messages := make(chan Message, 1)
	writer := &antigravityMessages{messages: messages}
	progress := &antigravityThinkingProgress{logPath: logPath, send: func(msg Message) bool { return writer.send(t.Context(), msg, false) }}
	start := 0
	progress.setScope(cid, &start)
	progress.poll() // The transcript can appear after init/user_input.
	line := `{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":1,"thinking":"Live progress."}`
	if err := os.WriteFile(path, []byte(line[:50]), 0o600); err != nil {
		t.Fatal(err)
	}
	progress.poll()
	if len(messages) != 0 || progress.offset != 0 {
		t.Fatal("consumed an incomplete transcript record")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line[50:] + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	messages <- Message{Type: MessageStatus}
	progress.poll()
	if progress.offset != 0 {
		t.Fatal("dropped thinking when the consumer was full")
	}
	<-messages
	progress.poll()
	if len(messages) != 1 || (<-messages).Content != "Live progress." {
		t.Fatal("did not retry the completed native record")
	}
	progress.poll()
	if len(messages) != 0 {
		t.Fatal("replayed an already delivered record")
	}
}

func TestAntigravityThinkingWhitespace(t *testing.T) {
	t.Parallel()
	ch := make(chan Message, 8)
	writer := &antigravityMessages{messages: ch}
	input := []Message{
		{Type: MessageThinking, Content: "First paragraph.\n\nSecond paragraph.\n\n\n"},
		{Type: MessageThinking, Content: "\n  "},
		{Type: MessageThinking, Content: "Next step.\n\n\n"},
		{Type: MessageToolUse, Tool: "read_file"},
		{Type: MessageThinking, Content: "After the tool.\n\n\n"},
	}
	for _, msg := range input {
		if !writer.send(t.Context(), msg, false) {
			t.Fatal("message was not sent")
		}
	}
	first, second, tool, third := <-ch, <-ch, <-ch, <-ch
	if first.Content != "First paragraph.\n\nSecond paragraph." || second.Content != "\n\nNext step." ||
		tool.Type != MessageToolUse || third.Content != "After the tool." || len(ch) != 0 {
		t.Fatalf("unexpected formatting: %#v, %#v, %#v, %#v", first, second, tool, third)
	}
	if strings.HasSuffix(first.Content+second.Content, "\n") || strings.HasSuffix(third.Content, "\n") {
		t.Fatal("thinking block has trailing blank lines")
	}
}

// The fake cannot finish until thinking reaches the consumer. This proves
// live delivery and exercises follower shutdown without any installed CLI.
func TestAntigravityThinkingStreamsBeforeProcessExit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cid := "bbaabbaa-1111-2222-3333-444444444444"
	path := filepath.Join(root, "brain", cid, ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":1,"thinking":"Old turn must not appear."}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(root, "release")
	fakePath := filepath.Join(root, "agy")
	script := fmt.Sprintf(`#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf 'CLI app data directory: %%s\n' %q >> "$log"
printf '%%s\n' '{"event":"init","conversation_id":%q,"init":{}}'
printf '%%s\n' '{"event":"step_update","step_update":{"conversation_id":%q,"step_index":10,"step_type":"user_input","state":"DONE"}}'
printf '%%s\n' '{"type":"USER_INPUT","step_index":10}' '{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":11,"thinking":"Implementing the requested change.","content":null}' >> %q
while [ ! -f %q ]; do sleep 0.01; done
printf '%%s\n' '{"event":"step_update","step_update":{"conversation_id":%q,"step_index":12,"step_type":"agent_response","state":"DONE","text_delta":"Finished."}}'
printf '%%s\n' '{"event":"result","result":{"conversation_id":%q,"status":"SUCCESS","response":"Finished."}}'
`, root, cid, cid, path, release, cid, cid)
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "ignored", ExecOptions{ResumeSessionID: cid})
	if err != nil {
		t.Fatal(err)
	}
	thinkingCount := 0
	for msg := range session.Messages {
		if msg.Type != MessageThinking {
			continue
		}
		thinkingCount++
		if !strings.Contains(msg.Content, "Implementing the requested change.") || strings.Contains(msg.Content, "Old turn") {
			t.Fatalf("wrong native thinking: %#v", msg)
		}
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result := <-session.Result
	if thinkingCount != 1 || result.Status != "completed" || result.Output != "Finished." {
		t.Fatalf("thinking=%d, result=%#v", thinkingCount, result)
	}
}
