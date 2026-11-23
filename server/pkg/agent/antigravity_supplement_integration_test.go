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

// TestAntigravityRealSupplementReachesTheTurn steers a real agy print-mode run
// through the real multica hook relay and asserts the model acted on the
// guidance in the same turn. It is what catches agy changing PreInvocation
// injection, or no longer reporting the injected user_input step that settles
// delivery.
func TestAntigravityRealSupplementReachesTheTurn(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("agy not on PATH; skipping real-binary smoke test")
	}
	version, err := exec.Command(path, "--version").Output()
	if err != nil || !SupportsTaskSupplement("antigravity", strings.TrimSpace(string(version))) {
		t.Skipf("agy %q predates PreInvocation steering", strings.TrimSpace(string(version)))
	}
	relay := filepath.Join(t.TempDir(), "multica")
	if out, err := exec.Command("go", "build", "-o", relay, "github.com/multica-ai/multica/server/cmd/multica").CombinedOutput(); err != nil {
		t.Fatalf("build multica relay: %v\n%s", err, out)
	}

	backend, err := New("antigravity", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx,
		"Run `sleep 10 && echo step-one` in the shell. After it finishes, run `echo step-two` in the shell. Then reply with a one-line summary.",
		ExecOptions{
			Cwd:                   t.TempDir(),
			Timeout:               230 * time.Second,
			EnableTaskSupplement:  true,
			SupplementHookCommand: []string{relay, AntigravityHookHelperArg},
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
			t.Fatal("agy turn never became ready for guidance")
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
