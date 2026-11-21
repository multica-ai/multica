//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexInitializeParentDeadlineDoesNotPersistOpaqueEnv is the other half of
// the parent-context pair: codex.go treats a parent DEADLINE and a parent
// CANCELLATION as one kind of ending (`contextEnded` in the initialize-failure
// path, codex.go), and both must keep untrusted child stderr out of the Result —
// that stderr can echo opaque Config.Env/auth values which pattern sanitization
// cannot recognize.
//
// Not a transport-timeout test, and it must not be deleted as one: the deadline
// here is the CALLER's own budget, and it is the only thing that exercises the
// DeadlineExceeded arm of `errors.Is(err, context.DeadlineExceeded) ||
// errors.Is(err, context.Canceled)`. The sibling cancellation test covers the
// Canceled arm only.
//
// The fake writes the secret to stderr before reading the handshake, so the
// redaction check never depends on racing our own initialize write. The
// caller's deadline expires only once the fake has written it, and the
// handshake bound is the fallback timeout, so the deadline is the only thing
// that can end the run no matter how slowly the fake starts.
func TestCodexInitializeParentDeadlineDoesNotPersistOpaqueEnv(t *testing.T) {
	t.Parallel()

	const secret = "opaque-init-parent-deadline-sentinel-8842"
	readyFile := filepath.Join(t.TempDir(), "stderr-written")
	fakePath := writeFakeCodexAppServer(t, ""+
		`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
		`touch "`+readyFile+`"`+"\n"+
		`read line`+"\n"+
		`sleep 300`+"\n")

	var logs strings.Builder
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"OPAQUE_AUTH_VALUE": secret},
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := newExpirableContext()
	defer ctx.expire()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: testFallbackTimeout, HandshakeTimeout: testFallbackTimeout})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	waitForTestFile(t, readyFile)
	ctx.expire()
	result := <-session.Result
	persisted := fmt.Sprintf("%+v\n%s", result, logs.String())
	if strings.Contains(persisted, secret) {
		t.Fatalf("opaque env persisted after the parent deadline: %s", persisted)
	}
	if result.Status != "failed" || result.codexInitializeRetrySafe {
		t.Fatalf("parent deadline must fail without initialize retry: %+v", result)
	}
}

func TestCodexInitializeParentCancellationDoesNotPersistOpaqueEnv(t *testing.T) {
	t.Parallel()

	const secret = "opaque-init-parent-context-sentinel-8841"
	readyFile := filepath.Join(t.TempDir(), "stderr-written")
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
		`touch "`+readyFile+`"`+"\n"+
		`sleep 300`+"\n")

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
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: testFallbackTimeout, HandshakeTimeout: testFallbackTimeout})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	waitForTestFile(t, readyFile)
	cancel()
	result := <-session.Result
	persisted := fmt.Sprintf("%+v\n%s", result, logs.String())
	if strings.Contains(persisted, secret) {
		t.Fatalf("opaque env persisted after parent context ended: %s", persisted)
	}
	if result.Status != "failed" || result.codexInitializeRetrySafe {
		t.Fatalf("parent context must fail without initialize retry: %+v", result)
	}
}
