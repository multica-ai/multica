//go:build !windows

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPiExecuteRPCSteering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-pi")
	writeTestExecutable(t, path, []byte(`#!/bin/sh
case "$*" in *"--mode rpc"*) ;; *) exit 20;; esac
IFS= read -r initial || exit 21
case "$initial" in *'"type":"prompt"'*) ;; *) exit 22;; esac
printf '%s\n' '{"type":"extension_ui_request","id":"confirm-1","method":"confirm","message":"Allow?"}'
IFS= read -r response || exit 25
case "$response" in *'"cancelled":true'*'"id":"confirm-1"'*'"type":"extension_ui_response"'*) ;; *) exit 26;; esac
printf '%s\n' '{"type":"agent_start"}'
IFS= read -r steer || exit 23
case "$steer" in *'"type":"steer"'*) ;; *) exit 24;; esac
printf '%s\n' '{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"queued"}}'
printf '%s\n' '{"type":"agent_end","willRetry":true,"messages":[{"role":"assistant","stopReason":"error","errorMessage":"transient"}]}' '{"type":"auto_retry_start"}' '{"type":"agent_start"}'
printf '%s\n' '{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"change course"}]}}'
printf '%s\n' '{"type":"turn_start"}' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"steered"}}' '{"type":"agent_end","messages":[]}' '{"type":"agent_settled","aborted":false}'
cat >/dev/null
`))
	backend, err := New("pi", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "initial\nwith quotes \" and --flags", ExecOptions{Cwd: t.TempDir(), EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	for !session.SupplementReady() {
		select {
		case <-ctx.Done():
			t.Fatal("never ready")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := session.Supplement(ctx, "change course"); err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "steered" {
		t.Fatalf("result=%+v", result)
	}
	if session.SupplementReady() {
		t.Fatal("ready after completion")
	}
}

func TestPiExecuteRPCFailures(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"rejected", `printf '%s\n' '{"type":"response","id":"initial-prompt","command":"prompt","success":false,"error":"bad model"}'; cat >/dev/null`, "bad model"},
		{"missing-settlement", `printf '%s\n' '{"type":"agent_start"}' '{"type":"agent_end","messages":[]}'`, "before agent_settled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fake-pi")
			writeTestExecutable(t, path, []byte("#!/bin/sh\nIFS= read -r initial\n"+tc.script+"\n"))
			backend, err := New("pi", Config{ExecutablePath: path, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "initial", ExecOptions{Cwd: t.TempDir(), EnableTaskSupplement: true})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			result := <-session.Result
			if result.Status != "failed" || !strings.Contains(result.Error, tc.want) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
