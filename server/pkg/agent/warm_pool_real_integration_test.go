//go:build agentintegration

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// Runs a real two-turn conversation with the warm pool disabled and enabled.
// Turn 2 resumes turn 1 and must recall its code word, so it proves the resume
// worked in both modes; the pooled run must also serve turn 2 from the parked
// process.
//
//	MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags agentintegration -run 'RealWarmPoolParity' -v ./pkg/agent/
//
// MULTICA_REAL_PARITY_MODEL optionally overrides the provider's default model.
func TestCodexRealWarmPoolParity(t *testing.T) {
	runRealWarmPoolParity(t, "codex")
}

func TestClaudeRealWarmPoolParity(t *testing.T) {
	runRealWarmPoolParity(t, "claude")
}

func runRealWarmPoolParity(t *testing.T, provider string) {
	requireRealAgentSmoke(t)
	model := os.Getenv("MULTICA_REAL_PARITY_MODEL")
	codeWord := fmt.Sprintf("PINEAPPLE-%d", time.Now().UnixNano()%100000)

	for _, pooled := range []bool{false, true} {
		name := "pool disabled"
		if pooled {
			name = "pool miss then hit"
		}
		t.Run(name, func(t *testing.T) {
			var logs synchronizedBuffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
			var pool *WarmSessionPool
			if pooled {
				pool = NewWarmSessionPool(WarmSessionPoolConfig{IdleTTL: 5 * time.Minute, MaxIdle: 2, Logger: logger})
				defer pool.Close()
			}
			dir := t.TempDir()

			runTurn := func(taskID, prompt, resume string) Result {
				t.Helper()
				b, err := New(provider, Config{
					Logger:   logger,
					TaskID:   taskID,
					WarmPool: pool,
					Env:      map[string]string{"MULTICA_TASK_ID": taskID},
				})
				if err != nil {
					t.Fatalf("New(%s): %v", provider, err)
				}
				started := time.Now()
				sess, err := b.Execute(context.Background(), prompt, ExecOptions{
					Cwd:             dir,
					Model:           model,
					ResumeSessionID: resume,
					Timeout:         4 * time.Minute,
				})
				if err != nil {
					t.Fatalf("Execute(%s): %v", taskID, err)
				}
				var text strings.Builder
				for msg := range sess.Messages {
					if msg.Type == MessageText {
						text.WriteString(msg.Content)
					}
				}
				res := <-sess.Result
				t.Logf("%s: status=%s session=%s duration=%s usage=%+v output=%q streamed=%q error=%q",
					taskID, res.Status, res.SessionID, time.Since(started).Round(time.Millisecond), res.Usage, res.Output, text.String(), res.Error)
				if res.Status != "completed" {
					t.Fatalf("%s: status = %q, error = %q", taskID, res.Status, res.Error)
				}
				if res.SessionID == "" {
					t.Fatalf("%s: no session id", taskID)
				}
				if len(res.Usage) == 0 {
					t.Fatalf("%s: no usage reported", taskID)
				}
				if strings.TrimSpace(text.String()) == "" {
					t.Fatalf("%s: nothing streamed", taskID)
				}
				return res
			}

			r1 := runTurn("task-1", "Remember the code word "+codeWord+". Reply with exactly: OK", "")
			if pooled && pool.Len() != 1 {
				t.Fatalf("pool holds %d sessions after turn 1, want 1", pool.Len())
			}
			r2 := runTurn("task-2", "What was the code word I gave you? Reply with only the code word.", r1.SessionID)
			if r2.SessionID != r1.SessionID {
				t.Fatalf("turn 2 session = %q, want resumed %q", r2.SessionID, r1.SessionID)
			}
			if !strings.Contains(r2.Output, codeWord) {
				t.Fatalf("turn 2 output = %q, want it to recall %q", r2.Output, codeWord)
			}
			reused := strings.Contains(logs.String(), provider+" warm session reused")
			if reused != pooled {
				t.Fatalf("warm session reused = %v, want %v", reused, pooled)
			}
		})
	}
}
