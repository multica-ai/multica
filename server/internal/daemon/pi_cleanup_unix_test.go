//go:build linux || darwin

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/agent"
)

type cancelPiAtTool struct {
	agent.Backend
	cancel context.CancelFunc
}

func (b cancelPiAtTool) Execute(ctx context.Context, prompt string, opts agent.ExecOptions) (*agent.Session, error) {
	s, err := b.Backend.Execute(ctx, prompt, opts)
	if err != nil || b.cancel == nil {
		return s, err
	}
	input, output := s.Messages, make(chan agent.Message, 16)
	s.Messages = output
	go func() {
		defer close(output)
		for msg := range input {
			output <- msg
			if msg.Type == agent.MessageToolUse {
				b.cancel()
			}
		}
	}()
	return s, nil
}

func TestPiCleanupWatchdogAndManualCancel(t *testing.T) {
	for _, provider := range []string{"pi", "omp"} {
		for _, trigger := range []string{"watchdog", "manual"} {
			t.Run(provider+"/"+trigger, func(t *testing.T) {
				t.Parallel()
				fake, dir, env := testutil.PiCleanupExecutable(t, "ignore")
				backend, err := agent.ResolveBackend(provider, agent.Config{ExecutablePath: fake, Env: env, Logger: slog.Default()})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				var onTool context.CancelFunc
				want := "idle_watchdog"
				if trigger == "manual" {
					onTool, want = cancel, "cancelled"
				}
				d := newTestDaemon(t)
				d.cfg.AgentIdleWatchdog, d.cfg.AgentToolWatchdog = 3*time.Second, 100*time.Millisecond
				started := time.Now()
				result, _, err := d.executeAndDrain(ctx, cancelPiAtTool{Backend: backend, cancel: onTool}, "test", agent.ExecOptions{
					Cwd: dir, ResumeSessionID: filepath.Join(dir, "session.jsonl"),
				}, slog.Default(), "pi-cleanup", "", new(atomic.Int32))
				if err != nil || result.Status != want {
					t.Fatalf("result=%+v err=%v, want %s", result, err, want)
				}
				if time.Since(started) > 6*time.Second {
					t.Fatal("cleanup did not return within its budget")
				}
				data, _ := os.ReadFile(filepath.Join(dir, "pids.json"))
				var pids []int
				if json.Unmarshal(data, &pids) != nil || len(pids) != 2 {
					t.Fatal("fixture did not record the processes before cancellation")
				}
				for _, pid := range pids {
					until := time.Now().Add(2 * time.Second)
					for syscall.Kill(pid, 0) == nil && time.Now().Before(until) {
						time.Sleep(20 * time.Millisecond)
					}
					if syscall.Kill(pid, 0) == nil {
						t.Fatalf("process %d still alive after daemon result", pid)
					}
				}
				before, _ := os.ReadFile(filepath.Join(dir, "receipt"))
				time.Sleep(100 * time.Millisecond)
				after, _ := os.ReadFile(filepath.Join(dir, "receipt"))
				if len(before) == 0 || len(before) != len(after) {
					t.Fatalf("writer still active after handoff: %d -> %d", len(before), len(after))
				}
			})
		}
	}
}
