//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestHermesRealSupplementReachesTheTurn steers a real Hermes Agent run with
// `/steer` while a tool is running and asserts the model acted on the guidance
// in the same turn. It is what catches Hermes changing the `/steer`
// acknowledgement or no longer folding guidance into the running turn.
func TestHermesRealSupplementReachesTheTurn(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("hermes")
	if err != nil {
		t.Skip("hermes not on PATH; skipping real-binary smoke test")
	}
	version, err := exec.Command(path, "acp", "--version").Output()
	if err != nil || !SupportsTaskSupplement("hermes", strings.TrimSpace(string(version))) {
		t.Skipf("hermes %q predates ACP /steer", strings.TrimSpace(string(version)))
	}

	backend, err := New("hermes", Config{ExecutablePath: path, Logger: slog.Default(), BuiltinRuntime: true})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx,
		"Use the terminal tool to run `sleep 15 && echo step-one`. After it finishes, run `echo step-two`. Then reply with a one-line summary.",
		ExecOptions{Cwd: t.TempDir(), Timeout: 230 * time.Second, EnableTaskSupplement: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var transcript strings.Builder
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for msg := range session.Messages {
			transcript.WriteString(msg.Content)
		}
	}()

	for !session.SupplementReady() {
		select {
		case <-ctx.Done():
			t.Fatal("hermes turn never became ready for guidance")
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
	<-drained
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "ORCHID-19") {
		t.Fatalf("final reply ignored the delivered guidance: %q", result.Output)
	}
	if strings.Contains(transcript.String(), hermesSteerAckPrefix) {
		t.Fatalf("the /steer acknowledgement leaked into the transcript: %q", transcript.String())
	}
}
