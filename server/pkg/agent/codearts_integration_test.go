//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCodeArtsRealSmoke(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run an authenticated CodeArts smoke test")
	}

	backend, err := ResolveBackend("codearts", Config{
		ExecutablePath: os.Getenv("MULTICA_CODEARTS_PATH"),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx, "Reply with exactly CODEARTS_SMOKE_OK and nothing else.", ExecOptions{
		Cwd:     t.TempDir(),
		Model:   os.Getenv("MULTICA_CODEARTS_MODEL"),
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q, output = %q", result.Status, result.Error, result.Output)
	}
	if !strings.Contains(result.Output, "CODEARTS_SMOKE_OK") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
