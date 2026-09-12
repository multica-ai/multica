package agent

// Tests for the workbuddy built-in runtime identity (WorkBuddy's bundled
// CodeBuddy CLI) hosted by the codebuddy protocol family.

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestNewRuntimeWorkbuddyHostsCodebuddyFamily pins the descriptor wiring:
// workbuddy must resolve through NewRuntime (identity factory), land on the
// codebuddy backend, and receive the descriptor's executable/label overrides
// — the fail-closed contract backendOverrideApplicator exists to enforce.
func TestNewRuntimeWorkbuddyHostsCodebuddyFamily(t *testing.T) {
	b, err := NewRuntime("workbuddy", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("NewRuntime(workbuddy): %v", err)
	}
	cb, ok := b.(*codebuddyBackend)
	if !ok {
		t.Fatalf("NewRuntime(workbuddy) returned %T, want *codebuddyBackend", b)
	}
	if cb.defaultExecutable != "codebuddy" {
		t.Errorf("defaultExecutable = %q, want %q (the bundled CLI's binary name)", cb.defaultExecutable, "codebuddy")
	}
	if cb.providerLabel != "workbuddy" {
		t.Errorf("providerLabel = %q, want %q", cb.providerLabel, "workbuddy")
	}
}

// TestWorkbuddyIsBuiltinRuntimeIdentity verifies the identity-vs-family
// split: workbuddy is a built-in runtime (never a SupportedTypes family),
// while codebuddy remains the protocol family.
func TestWorkbuddyIsBuiltinRuntimeIdentity(t *testing.T) {
	if !IsBuiltinRuntime("workbuddy") {
		t.Error("workbuddy should be a built-in runtime identity")
	}
	if IsSupportedType("workbuddy") {
		t.Error("workbuddy must NOT be a protocol family in SupportedTypes")
	}
	if !IsSupportedType("codebuddy") {
		t.Error("codebuddy must remain a supported protocol family")
	}
}

// TestWorkbuddyErrorLabelNamesIdentity verifies that a launch failure on a
// workbuddy runtime reports the identity, not the family — the error-label
// override applyBuiltinRuntimeOverrides installs.
func TestWorkbuddyErrorLabelNamesIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("missing-executable fixture path semantics differ on windows")
	}
	backend, err := ResolveBackend("workbuddy", Config{
		// Point at a guaranteed-missing executable so Execute fails at the
		// LookPath gate without spawning anything.
		ExecutablePath: filepath.Join(t.TempDir(), "no-such-workbuddy"),
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("ResolveBackend(workbuddy): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = backend.Execute(ctx, "prompt", ExecOptions{})
	if err == nil {
		t.Fatal("expected a launch error for a missing workbuddy executable")
	}
	if err.Error()[:9] != "workbuddy" {
		t.Errorf("error = %q, want it to name the workbuddy identity", err.Error())
	}
}
