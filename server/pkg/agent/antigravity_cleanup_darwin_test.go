package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAntigravityCleansUpAfterTerminalSuccess(t *testing.T) {
	for _, leader := range []string{"waits", "closes-pipes", "exits"} {
		t.Run(leader, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "agy")
			wait := `wait "$child"`
			switch leader {
			case "closes-pipes":
				wait = "exec >/dev/null 2>&1\n" + wait
			case "exits":
				wait = "exit 0"
			}
			child := `sleep 60 &
child=$!
printf '%s' "$child" > child.pid
`
			if leader == "closes-pipes" {
				child = strings.Replace(child, "sleep 60 &", "sleep 60 >/dev/null 2>&1 &", 1)
			}
			writeTestExecutable(t, fake, []byte("#!/bin/sh\n"+child+`cat <<'JSONL'
{"event":"step_update","step_update":{"step_index":0,"state":"DONE","step_type":"agent_response","text_delta":"Complete report."}}
{"event":"result","result":{"status":"SUCCESS","response":"Complete report."}}
JSONL
`+wait+"\n"))

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			backend := &antigravityBackend{
				cfg:               Config{ExecutablePath: fake, Logger: quietAntigravityLogger()},
				terminalExitGrace: 150 * time.Millisecond,
			}
			session, err := backend.Execute(ctx, "ignored", ExecOptions{Cwd: dir})
			if err != nil {
				t.Fatal(err)
			}
			var result Result
			select {
			case result = <-session.Result:
			case <-ctx.Done():
				t.Fatal("terminal result stayed pending until run cancellation")
			}
			if result.Status != "completed" || result.Output != "Complete report." {
				t.Errorf("result = %+v, want completed with the preserved report", result)
			}
			pidText, err := os.ReadFile(filepath.Join(dir, "child.pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(pidText))
			if err != nil {
				t.Fatal(err)
			}
			if !waitForProcessGone(pid, time.Second) {
				t.Fatalf("child %d survived cleanup", pid)
			}
		})
	}
}

func TestAntigravityKeepsRequiredBackgroundWork(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "agy")
	writeTestExecutable(t, fake, []byte(fmt.Sprintf(`#!/bin/sh
cat <<'JSONL'
{"event":"step_update","step_update":{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command"}}
{"event":"step_update","step_update":{"step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"Waiting for the command."}}
{"event":"result","result":{"status":"SUCCESS","response":"Waiting for the command."}}
JSONL
sleep 0.6
touch %q
cat <<'JSONL'
{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"tool","tool_name":"run_command"}}
{"event":"result","result":{"status":"SUCCESS","response":"Command finished."}}
JSONL
`, filepath.Join(dir, "required-work-done"))))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	backend := &antigravityBackend{
		cfg:               Config{ExecutablePath: fake, Logger: quietAntigravityLogger()},
		terminalExitGrace: 150 * time.Millisecond,
	}
	session, err := backend.Execute(ctx, "ignored", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "Command finished." {
		t.Fatalf("result = %+v, want the result after background work", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "required-work-done")); err != nil {
		t.Fatalf("required background work did not finish: %v", err)
	}
}

func TestAntigravityKeepsNativeExitFailureAfterTerminalSuccess(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "agy")
	writeTestExecutable(t, fake, []byte(`#!/bin/sh
sleep 60 &
printf '%s\n' '{"event":"result","result":{"status":"SUCCESS","response":"Report."}}'
exit 7
`))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	backend := &antigravityBackend{
		cfg:               Config{ExecutablePath: fake, Logger: quietAntigravityLogger()},
		terminalExitGrace: 150 * time.Millisecond,
	}
	session, err := backend.Execute(ctx, "ignored", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "failed" || result.Output != "Report." || !strings.Contains(result.Error, "exit status 7") {
			t.Fatalf("result = %+v, want native exit failure with the report", result)
		}
	case <-ctx.Done():
		t.Fatal("CLI failure stayed pending until run cancellation")
	}
}

func TestAntigravityPreservesReportWhenCleanupIsUnconfirmed(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "agy")
	state := filepath.Join(dir, "holder.pid")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the pipe-holder fixture that detaches itself from the CLI's group.
	writeTestExecutable(t, fake, []byte(fmt.Sprintf(`#!/bin/sh
%s=%q %q -test.run='^TestHermesEscapedPipeHolderHelper$' &
while [ ! -f %q ]; do sleep 0.02; done
cat <<'JSONL'
{"event":"step_update","step_update":{"step_index":0,"state":"DONE","step_type":"agent_response","text_delta":"Complete "}}
{"event":"result","result":{"status":"SUCCESS","response":"Complete report."}}
JSONL
`, hermesEscapedHolderEnv, state, testBinary, state)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backend := &antigravityBackend{
		cfg:               Config{ExecutablePath: fake, Logger: quietAntigravityLogger()},
		terminalExitGrace: 150 * time.Millisecond,
	}
	session, err := backend.Execute(ctx, "ignored", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	pid, pgid := readEscapedHolderIdentity(t, state)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if pgid != pid {
		t.Fatalf("fixture did not detach from the runtime group: pid=%d pgid=%d", pid, pgid)
	}
	select {
	case result := <-session.Result:
		if result.Status != "cleanup_unconfirmed" || result.Output != "Complete report." {
			t.Fatalf("result = %+v, want cleanup warning with the preserved report", result)
		}
	case <-ctx.Done():
		t.Fatal("escaped pipe holder wedged cleanup")
	}
	var streamed strings.Builder
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed.WriteString(msg.Content)
		}
	}
	if streamed.String() != "Complete report." || !processAlive(pid) {
		t.Fatalf("transcript = %q or escaped process was stopped", streamed.String())
	}
}
