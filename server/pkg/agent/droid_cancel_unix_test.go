//go:build unix

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDroidInheritedOutputPipes(t *testing.T) {
	for _, tc := range []struct {
		name, child, final, want string
		exitCode                 string
		beforeExit               string
		longPrompt               bool
		captureInput             bool
		timeout                  time.Duration
	}{
		{name: "stdout and stderr without timeout", child: "sleep 300", final: "Done", want: "completed"},
		{name: "stdout and stderr with timeout", child: "sleep 300", final: "Done", timeout: 750 * time.Millisecond, want: "completed"},
		{name: "stderr only", child: "sleep 300 >/dev/null", final: "Done", want: "completed"},
		{name: "unread stdin", child: "sleep 300", final: "Done", longPrompt: true, want: "completed"},
		{name: "missing completion", child: "sleep 300", want: "failed"},
		{name: "nonzero exit after completion", child: "sleep 300", final: "Done", beforeExit: "sleep 0.1\n", exitCode: "7", want: "failed"},
		{name: "completion from descendant after wrapper exits", child: `(sleep 0.1; printf '%s\n' '{"type":"completion","finalText":"Done"}'; exec sleep 300)`, want: "completed"},
		{name: "descendant reads prompt after wrapper exits", child: `(sleep 0.1; cat > "$DROID_INPUT"; printf '%s\n' '{"type":"completion","finalText":"Done"}'; exec sleep 300)`, longPrompt: true, captureInput: true, want: "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pids")
			inputFile := filepath.Join(dir, "input")
			path := filepath.Join(dir, "droid")
			// Noninteractive shells otherwise replace background stdin with
			// /dev/null; explicitly pass the prompt pipe through fd 3.
			script := "#!/bin/sh\nexec 3<&0\n" + tc.child + " <&3 &\nprintf '%s %s\\n' \"$$\" \"$!\" > \"$DROID_PIDS\"\nexec 3<&-\n"
			if tc.final != "" {
				script += "printf '%s\\n' '{\"type\":\"completion\",\"finalText\":\"" + tc.final + "\"}'\n"
			}
			if tc.exitCode != "" {
				script += tc.beforeExit + "exit " + tc.exitCode + "\n"
			}
			writeTestExecutable(t, path, []byte(script))
			backend, err := New("droid", Config{ExecutablePath: path, Env: map[string]string{"DROID_PIDS": pidFile, "DROID_INPUT": inputFile}})
			if err != nil {
				t.Fatal(err)
			}
			// The outer deadline is a test safety net. A finished direct child
			// must settle on its own, including when the task has no timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			started := time.Now()
			prompt := "task"
			if tc.longPrompt {
				prompt = strings.Repeat("task", 100000)
			}
			session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: tc.timeout})
			if err != nil {
				t.Fatal(err)
			}
			pids := waitForPids(t, pidFile)
			_, result := collectDroid(t, session)
			elapsed := time.Since(started)
			for _, pid := range pids {
				waitProcessGone(t, pid)
			}
			if result.Status != tc.want || elapsed > 3*time.Second {
				t.Fatalf("result after %s = %+v, want %s within 3s", elapsed, result, tc.want)
			}
			if tc.want == "completed" && result.Output != "Done" {
				t.Fatalf("completion lost: %+v", result)
			}
			if tc.captureInput {
				got, err := os.ReadFile(inputFile)
				if err != nil || string(got) != prompt {
					t.Fatalf("prompt truncated after wrapper exit: got %d of %d bytes, error=%v", len(got), len(prompt), err)
				}
			}
			if tc.want == "failed" && (result.Output != "" || result.Error == "") {
				t.Fatalf("failed run exposed output or lost error: %+v", result)
			}
			if tc.name == "missing completion" && !strings.Contains(result.Error, "without terminal result") {
				t.Fatalf("missing terminal result misclassified: %+v", result)
			}
		})
	}
}

func TestDroidTerminatesOwnedProcessTree(t *testing.T) {
	for _, stage := range []string{"cancel", "timeout", "completed", "failed"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pids")
			path := filepath.Join(dir, "droid")
			script := "#!/bin/sh\nsleep 300 >/dev/null 2>&1 &\nprintf '%s %s\\n' \"$$\" \"$!\" > \"$DROID_PIDS\"\n"
			if stage == "completed" {
				script += "printf '%s\\n' '{\"type\":\"completion\",\"finalText\":\"Done\"}'\n"
			} else if stage != "failed" {
				script += "wait\n"
			}
			writeTestExecutable(t, path, []byte(script))
			backend, err := New("droid", Config{ExecutablePath: path, Env: map[string]string{"DROID_PIDS": pidFile}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := ExecOptions{Timeout: 15 * time.Second}
			want := "aborted"
			if stage == "timeout" {
				opts.Timeout = 5 * time.Second
				want = "timeout"
			} else if stage == "completed" || stage == "failed" {
				want = stage
			}
			session, err := backend.Execute(ctx, "task", opts)
			if err != nil {
				t.Fatal(err)
			}
			pids := waitForPids(t, pidFile)
			if stage == "cancel" {
				cancel()
			}
			_, result := collectDroid(t, session)
			if result.Status != want {
				t.Fatalf("result = %+v", result)
			}
			for _, pid := range pids {
				waitProcessGone(t, pid)
			}
		})
	}
}
