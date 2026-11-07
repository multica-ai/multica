package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestSupplementCodexLiveProcess(t *testing.T) {
	const marker = "MULTICA_STEER_PROCESS_e4f618"
	executable, trace := testutil.FakeSteerCodex(t, marker)
	backend, err := New("codex", Config{ExecutablePath: executable, Logger: slog.Default(), Env: map[string]string{"CODEX_HOME": t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "wait for guidance", ExecOptions{Cwd: filepath.Dir(executable), EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for msg := range session.Messages {
			if strings.Contains(msg.Content, "fixture ready") {
				select {
				case <-ready:
				default:
					close(ready)
				}
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("turn never became ready")
	}
	if session.SupplementReady == nil || !session.SupplementReady() {
		t.Fatal("turn not ready")
	}
	select {
	case result := <-session.Result:
		t.Fatalf("finished without injection: %+v", result)
	default:
	}
	if err := session.Supplement(ctx, marker); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "completed" || result.Output != marker {
			t.Fatalf("result=%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("no final answer")
	}
	<-drained
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected process/start/steer/final: %s", data)
	}
	pid := 0
	for i, line := range lines {
		var event struct {
			Event string
			PID   int
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			pid = event.PID
		}
		if event.Event != []string{"process", "start", "steer", "final"}[i] {
			t.Fatalf("unexpected trace: %s", data)
		}
		if event.PID != pid {
			t.Fatalf("process changed: %s", data)
		}
	}
	t.Logf("same process %d, expectedTurnId verified, marker in final answer", pid)
}
