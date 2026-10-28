//go:build linux || darwin

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestPiCleanupDetachedTool(t *testing.T) {
	for _, provider := range []string{"pi", "omp"} {
		for _, mode := range []string{"graceful", "ignore", "exit"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				t.Parallel()
				fake, dir, env := testutil.PiCleanupExecutable(t, mode)
				backend, err := ResolveBackend(provider, Config{ExecutablePath: fake, Env: env, Logger: slog.Default()})
				if err != nil {
					t.Fatal(err)
				}
				backend.(*piBackend).terminateGrace = 100 * time.Millisecond
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				session, err := backend.Execute(ctx, "test", ExecOptions{Cwd: dir, ResumeSessionID: filepath.Join(dir, "session.jsonl")})
				if err != nil {
					t.Fatal(err)
				}
				for msg := range session.Messages {
					if msg.Type == MessageToolUse {
						break
					}
				}
				// The fixture has already published its PID/PGID before cancel.
				data, err := os.ReadFile(filepath.Join(dir, "pids.json"))
				var pids []int
				if err != nil || json.Unmarshal(data, &pids) != nil || len(pids) != 2 {
					t.Fatalf("fixture pids=%q err=%v", data, err)
				}
				started := time.Now()
				if mode != "exit" {
					cancel()
				}
				result := waitPiResult(t, session, 4*time.Second)
				if result.Status == "cleanup_unconfirmed" {
					t.Fatalf("cleanup failed: %s", result.Error)
				}
				if time.Since(started) > 3*time.Second {
					t.Fatal("cleanup exceeded the bounded handoff")
				}
				for _, pid := range pids {
					waitProcessGone(t, pid)
				}
				before, err := os.ReadFile(filepath.Join(dir, "receipt"))
				if err != nil || len(before) == 0 {
					t.Fatalf("missing writer receipt: %v", err)
				}
				time.Sleep(100 * time.Millisecond)
				after, _ := os.ReadFile(filepath.Join(dir, "receipt"))
				if len(before) != len(after) {
					t.Fatalf("writes continued after Result: %d -> %d", len(before), len(after))
				}
				if mode == "graceful" {
					if _, err := os.Stat(filepath.Join(dir, "term")); err != nil {
						t.Fatal("Pi never received its graceful-cleanup signal")
					}
				}
			})
		}
	}
}

func TestPiCleanupSuccessfulBackgroundServiceSurvives(t *testing.T) {
	for _, provider := range []string{"pi", "omp"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			fake, dir, env := testutil.PiCleanupExecutable(t, "success")
			backend, err := ResolveBackend(provider, Config{ExecutablePath: fake, Env: env, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			backend.(*piBackend).terminateGrace = 100 * time.Millisecond
			sessionFile := filepath.Join(dir, "session.jsonl")
			if err := os.WriteFile(sessionFile, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(context.Background(), "start a background service", ExecOptions{Cwd: dir, ResumeSessionID: sessionFile})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			result := waitPiResult(t, session, 4*time.Second)
			if result.Status != "completed" {
				t.Fatalf("successful run = %s: %s", result.Status, result.Error)
			}
			before, _ := os.ReadFile(filepath.Join(dir, "receipt"))
			time.Sleep(150 * time.Millisecond)
			after, _ := os.ReadFile(filepath.Join(dir, "receipt"))
			if len(after) <= len(before) {
				t.Fatalf("successful background service stopped at Result: %d -> %d", len(before), len(after))
			}
		})
	}
}
