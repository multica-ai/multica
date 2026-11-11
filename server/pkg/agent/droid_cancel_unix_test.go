//go:build unix

package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

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
