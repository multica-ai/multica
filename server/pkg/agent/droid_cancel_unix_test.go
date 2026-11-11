//go:build unix

package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDroidTerminatesOwnedProcessTree(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "timeout"}[timeout], func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pids")
			path := filepath.Join(dir, "droid")
			writeTestExecutable(t, path, []byte("#!/bin/sh\nsleep 300 &\nprintf '%s %s\\n' \"$$\" \"$!\" > \"$DROID_PIDS\"\nwait\n"))
			backend, err := New("droid", Config{ExecutablePath: path, Env: map[string]string{"DROID_PIDS": pidFile}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := ExecOptions{Timeout: 15 * time.Second}
			want := "aborted"
			if timeout {
				opts.Timeout = 5 * time.Second
				want = "timeout"
			}
			session, err := backend.Execute(ctx, "task", opts)
			if err != nil {
				t.Fatal(err)
			}
			pids := waitForPids(t, pidFile)
			if !timeout {
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
