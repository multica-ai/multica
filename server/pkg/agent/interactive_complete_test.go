package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Used only inside re-executed fake providers; no real agent is launched.
func waitTestSettleGate(path string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestInteractiveAgentCompletionPreservesFinalReply(t *testing.T) {
	for _, provider := range []string{"pi", "codex"} {
		for _, steer := range []bool{false, true} {
			t.Run(provider+map[bool]string{false: "/complete", true: "/human-correction"}[steer], func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				gate := filepath.Join(t.TempDir(), "settle")
				cfg := Config{ExecutablePath: os.Args[0], CLIVersion: "0.87.0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{"MULTICA_TEST_SETTLE_GATE": gate}}
				opts := ExecOptions{Cwd: t.TempDir(), KeepInteractiveOpen: true}
				if provider == "pi" {
					cfg.Env["MULTICA_TEST_PI_RPC"] = "1"
					opts.ResumeSessionID = filepath.Join(t.TempDir(), "session.jsonl")
					if err := os.WriteFile(opts.ResumeSessionID, []byte("{\"type\":\"session\"}\n"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					cfg.Env["MULTICA_TEST_CODEX_INTERACTIVE"] = "1"
				}
				backend, err := New(provider, cfg)
				if err != nil {
					t.Fatal(err)
				}
				s, err := backend.(InteractiveBackend).ExecuteInteractive(ctx, "work", opts)
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					for range s.Messages {
					}
				}()
				for s.Control.Snapshot().State == InteractionStarting {
					select {
					case <-ctx.Done():
						t.Fatal("startup timeout")
					case <-time.After(time.Millisecond):
					}
				}
				if _, err := s.Control.Submit(ctx, InteractionCommand{ID: "complete", Kind: "complete", Activity: 1}); err != nil {
					t.Fatal(err)
				}
				select {
				case result := <-s.Result:
					t.Fatalf("premature completion: %+v", result)
				case <-time.After(30 * time.Millisecond):
				}
				if steer {
					if _, err := s.Control.Submit(ctx, InteractionCommand{ID: "correction", Kind: "input", Activity: 1, Text: "wait for my review"}); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(gate, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if steer {
					for s.Control.Snapshot().State != InteractionAwaitingInput {
						select {
						case result := <-s.Result:
							t.Fatalf("human correction did not cancel completion: %+v", result)
						case <-ctx.Done():
							t.Fatal("settle timeout")
						case <-time.After(time.Millisecond):
						}
					}
					if _, err := s.Control.Submit(ctx, InteractionCommand{ID: "complete-again", Kind: "complete", Activity: s.Control.Snapshot().Activity}); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case result := <-s.Result:
					if result.Status != "completed" || !strings.Contains(result.Output, "final reply preserved") {
						t.Fatalf("lost final reply: %+v", result)
					}
				case <-ctx.Done():
					t.Fatal("completion timeout")
				}
			})
		}
	}
}
