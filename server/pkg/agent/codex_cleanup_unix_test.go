//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexInitializeParentCancellationDoesNotPersistOpaqueEnv(t *testing.T) {
	t.Parallel()

	const secret = "opaque-init-parent-context-sentinel-8841"
	readyFile := filepath.Join(t.TempDir(), "stderr-written")
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
		`touch "`+readyFile+`"`+"\n"+
		`sleep 5`+"\n")

	var logs strings.Builder
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"OPAQUE_AUTH_VALUE": secret},
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 10 * time.Second, HandshakeTimeout: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake app-server did not write stderr before cancellation")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	result := <-session.Result
	if _, err := os.Stat(readyFile); err != nil {
		t.Fatalf("fake app-server never wrote the secret to stderr, so the redaction check would pass vacuously: %v", err)
	}
	persisted := fmt.Sprintf("%+v\n%s", result, logs.String())
	if strings.Contains(persisted, secret) {
		t.Fatalf("opaque env persisted after parent context ended: %s", persisted)
	}
	if result.Status != "failed" || result.codexInitializeRetrySafe {
		t.Fatalf("parent context must fail without initialize retry: %+v", result)
	}
}
