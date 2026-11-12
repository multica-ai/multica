//go:build unix

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBBTaskCredentialsTravelOnStdin(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "bb")
	requestPath := filepath.Join(dir, "request.json")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RECORD.args\"\ncat > \"$RECORD\"\nprintf '%s\\n' '{\"session_id\":\"thr_test\",\"output\":\"computed result\"}'\n"))
	backend, err := New("bb", Config{ExecutablePath: executable, Env: map[string]string{"RECORD": requestPath}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "read the task", ExecOptions{Cwd: dir, TaskToken: "mat_test_secret"})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.SessionID != "thr_test" || result.Output != "computed result" {
		t.Fatalf("unexpected result: %+v", result)
	}
	args, err := os.ReadFile(requestPath + ".args")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "mat_test_secret") {
		t.Fatal("credential leaked into argv")
	}
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request bbRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if request.Env["MULTICA_TOKEN"] != "mat_test_secret" || request.Prompt != "read the task" {
		t.Fatal("task context was not forwarded")
	}
}

func TestBBRejectsFailedCommandWithPartialOutput(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "bb")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"session_id\":\"thr_test\",\"output\":\"partial result\"}'\nexit 1\n"))
	backend := &bbBackend{cfg: Config{ExecutablePath: executable}}
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	for message := range session.Messages {
		if message.Type == MessageText {
			t.Fatal("published a failed command's partial output")
		}
	}
	if result := <-session.Result; result.Status != "failed" {
		t.Fatalf("status = %s", result.Status)
	}
}

func TestBBCancellationRequiresStopAcknowledgement(t *testing.T) {
	for _, acknowledgement := range []bool{true, false} {
		t.Run(fmt.Sprint(acknowledgement), func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "bb")
			stopCode := "0"
			if !acknowledgement {
				stopCode = "1"
			}
			writeTestExecutable(t, executable, []byte("#!/bin/sh\nif [ \"$2\" = cancel ]; then touch \"$RECORD.stopped\"; exit "+stopCode+"; fi\ncat >\"$RECORD\"\nwhile :; do :; done\n"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(dir, "request")
			backend := &bbBackend{cfg: Config{ExecutablePath: executable, Env: map[string]string{"RECORD": path}}}
			session, err := backend.Execute(ctx, "task", ExecOptions{Cwd: dir})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				if _, err := os.Stat(path); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("BB did not start")
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			for range session.Messages {
			}
			result := <-session.Result
			expected := "aborted"
			if !acknowledgement {
				expected = "cleanup_unconfirmed"
			}
			if result.Status != expected {
				t.Fatalf("status=%s, expected %s", result.Status, expected)
			}
			if _, err := os.Stat(path + ".stopped"); err != nil {
				t.Fatal("returned before requesting remote stop")
			}
		})
	}
}
