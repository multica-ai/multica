//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCursorRealSupplementReachesTheTurn steers a real cursor-agent run through
// the real multica hook relay and asserts the model acted on the guidance in
// the same turn. It is what catches Cursor changing hook delivery, or a model
// starting to reject tagged guidance as a prompt injection.
func TestCursorRealSupplementReachesTheTurn(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("cursor-agent")
	if err != nil {
		t.Skip("cursor-agent not on PATH; skipping real-binary smoke test")
	}
	version, err := exec.Command(path, "--version").Output()
	if err != nil || !SupportsTaskSupplement("cursor", strings.TrimSpace(string(version))) {
		t.Skipf("cursor-agent %q predates hook additional_context", strings.TrimSpace(string(version)))
	}
	relay := filepath.Join(t.TempDir(), "multica")
	if out, err := exec.Command("go", "build", "-o", relay, "github.com/multica-ai/multica/server/cmd/multica").CombinedOutput(); err != nil {
		t.Fatalf("build multica relay: %v\n%s", err, out)
	}

	backend, err := New("cursor", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new cursor backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx,
		"Run `sleep 5 && echo step-one` in the shell. After it finishes, run `echo step-two` in the shell. Then reply with a one-line summary.",
		ExecOptions{
			Cwd:                  t.TempDir(),
			Timeout:              230 * time.Second,
			EnableTaskSupplement: true,
			CursorHookCommand:    []string{relay, CursorHookHelperArg},
		})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	for !session.SupplementReady() {
		select {
		case <-ctx.Done():
			t.Fatal("cursor turn never became ready for guidance")
		case <-time.After(50 * time.Millisecond):
		}
	}
	// The daemon's own framing (formatTaskSupplementInstruction).
	guidance := `[ADDITIONAL GUIDANCE] Human "Jiayuan" added guidance while you were working.

Treat this as additional guidance for the same active task, not as a replacement:
- Preserve and complete the original objective.
- Incorporate this guidance into the work and the turn's single final response.
- Do not send a separate acknowledgement.
- Replace or cancel the original objective only if the human explicitly asks for replacement or cancellation.

Human message:
Also include the codeword ORCHID-19 in your final reply.`
	if err := session.Supplement(ctx, guidance); err != nil {
		t.Fatalf("supplement was not delivered: %v", err)
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "ORCHID-19") {
		t.Fatalf("final reply ignored the delivered guidance: %q", result.Output)
	}
}
