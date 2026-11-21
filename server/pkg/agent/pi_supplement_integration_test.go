//go:build agentintegration

package agent

import (
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPiRealSupplementReachesTheSameSession(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("pi")
	if err != nil {
		t.Skip("pi not on PATH; real same-session consumption is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	version, err := DetectVersion(ctx, NewCommand(path, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !SupportsTaskSupplement("pi", version) {
		t.Skipf("Pi %q does not support steering; need >=1.4.0", version)
	}
	backend, err := New("pi", Config{ExecutablePath: path})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	session, err := backend.Execute(ctx,
		"Run `sleep 5` in the shell, then create objective.txt containing exactly objective-complete. Finally summarize what you did.",
		ExecOptions{Cwd: cwd, Timeout: 170 * time.Second, EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range session.Messages {
		}
	}()
	defer func() {
		cancel()
		<-drained
	}()
	for !session.SupplementReady() {
		select {
		case <-ctx.Done():
			t.Fatal("Pi never became ready for steering")
		case <-drained:
			t.Fatalf("Pi ended before steering: %+v", <-session.Result)
		case <-time.After(25 * time.Millisecond):
		}
	}
	// Only this additional message carries the canary; the initial prompt
	// cannot satisfy the assertion. A queued RPC response alone cannot pass.
	canary := "PI_STEER_" + rand.Text()
	if err := session.Supplement(ctx, "Preserve the original objective. Also create steered.txt containing exactly "+canary+" and include it in your final reply."); err != nil {
		t.Fatalf("same-session supplement was not consumed: %v", err)
	}
	<-drained
	result := <-session.Result
	if result.Status != "completed" || result.SessionID == "" || !strings.Contains(result.Output, canary) {
		t.Fatalf("same-session result did not include guidance: %+v", result)
	}
	for name, want := range map[string]string{"objective.txt": "objective-complete", "steered.txt": canary} {
		data, err := os.ReadFile(filepath.Join(cwd, name))
		if err != nil || strings.TrimSpace(string(data)) != want {
			t.Fatalf("%s = %q, err %v; want %q", name, data, err, want)
		}
	}
	t.Logf("Pi %s consumed additional input in session %s and completed both objectives", version, result.SessionID)
}
