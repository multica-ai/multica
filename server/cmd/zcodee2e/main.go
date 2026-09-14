// Command zcodee2e is a scratch end-to-end harness for the zcode ACP
// backend: it drives the real zcode-acp-server bridge, the real ZCode
// runtime and the real GLM endpoint through agent.New("zcode", ...).
// Not part of the PR — untracked, for local verification only.
//
// Usage:
//
//	go run ./cmd/zcodee2e run            # happy path + streaming
//	go run ./cmd/zcodee2e resume <sid>   # continue a prior session
//	go run ./cmd/zcodee2e deadresume     # resume a bogus session id
//	go run ./cmd/zcodee2e cancel         # long turn, cancelled mid-flight
//	go run ./cmd/zcodee2e badmodel       # rejected model switch
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func main() {
	bridge := os.Getenv("ZCODE_E2E_BRIDGE")
	if bridge == "" {
		bridge = "/Users/lianxin/Desktop/openproject/zcode-acp/dist/index.js"
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	b, err := agent.New("zcode", agent.Config{
		ExecutablePath: bridge,
		Logger:         logger,
		Env: map[string]string{
			"ZCODE_BIN": "/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs",
		},
	})
	if err != nil {
		fatal("new backend: %v", err)
	}

	mode := "run"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "run":
		execAndReport(b, agent.ExecOptions{
			Cwd:     "/tmp",
			Timeout: 4 * time.Minute,
		}, "Use the shell to run 'echo hello-multica-e2e', then reply with exactly the command's output and nothing else.")
	case "resume":
		if len(os.Args) < 3 {
			fatal("resume needs a session id")
		}
		execAndReport(b, agent.ExecOptions{
			Cwd:            "/tmp",
			Timeout:        4 * time.Minute,
			ResumeSessionID: os.Args[2],
		}, "In my previous message, what exact text did I ask you to echo? Answer with just that text.")
	case "deadresume":
		execAndReport(b, agent.ExecOptions{
			Cwd:             "/tmp",
			Timeout:         time.Minute,
			ResumeSessionID: "00000000-dead-beef-0000-000000000000",
		}, "say hi")
	case "badmodel":
		execAndReport(b, agent.ExecOptions{
			Cwd:     "/tmp",
			Timeout: time.Minute,
			Model:   "definitely-not-a-model",
		}, "say hi")
	case "cancel":
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		start := time.Now()
		s, err := b.Execute(ctx, "Run 'sleep 120' in the shell and wait for it, then reply done.", agent.ExecOptions{
			Cwd:     "/tmp",
			Timeout: 4 * time.Minute,
		})
		if err != nil {
			fatal("execute: %v", err)
		}
		msgCount := 0
		done := make(chan struct{})
		go func() {
			defer close(done)
			for m := range s.Messages {
				msgCount++
				fmt.Printf("[%6.1fs] MSG type=%s tool=%q content=%.80q\n", time.Since(start).Seconds(), m.Type, m.Tool, m.Content)
			}
		}()
		res := <-s.Result
		<-done
		printResult(start, res, msgCount)
	default:
		fatal("unknown mode %q", mode)
	}
}

func execAndReport(b agent.Backend, opts agent.ExecOptions, prompt string) {
	ctx := context.Background()
	start := time.Now()
	s, err := b.Execute(ctx, prompt, opts)
	if err != nil {
		fatal("execute: %v", err)
	}
	msgCount := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range s.Messages {
			msgCount++
			fmt.Printf("[%6.1fs] MSG type=%s tool=%q content=%.80q\n", time.Since(start).Seconds(), m.Type, m.Tool, m.Content)
		}
	}()
	res := <-s.Result
	<-done
	printResult(start, res, msgCount)
}

func printResult(start time.Time, res agent.Result, msgCount int) {
	out, _ := json.MarshalIndent(map[string]any{
		"status":          res.Status,
		"sessionID":       res.SessionID,
		"resumeRejected":  res.ResumeRejected,
		"durationMs":      res.DurationMs,
		"messageCount":    msgCount,
		"error":           res.Error,
		"output":          res.Output,
		"usage":           res.Usage,
	}, "", "  ")
	fmt.Printf("[%6.1fs] RESULT %s\n", time.Since(start).Seconds(), out)
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "zcodee2e: "+f+"\n", a...)
	os.Exit(1)
}
