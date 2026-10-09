package governance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeHook(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseWorkspaceHooks_ServerDefaults(t *testing.T) {
	root := t.TempDir()
	hooks := ParseWorkspaceHooks(nil, Config{Root: root, Timeout: 10 * time.Second})
	wantComment := filepath.Join(root, defaultPreComment)
	wantStatus := filepath.Join(root, defaultPreStatus)
	if hooks.PreComment != wantComment {
		t.Fatalf("PreComment = %q, want %q", hooks.PreComment, wantComment)
	}
	if hooks.PreStatus != wantStatus {
		t.Fatalf("PreStatus = %q, want %q", hooks.PreStatus, wantStatus)
	}
	if hooks.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %v, want 10s", hooks.Timeout)
	}
}

func TestParseWorkspaceHooks_NoConfigSkips(t *testing.T) {
	hooks := ParseWorkspaceHooks([]byte(`{"foo":"bar"}`), Config{})
	if hooks.PreComment != "" || hooks.PreStatus != "" {
		t.Fatalf("expected empty hooks, got %+v", hooks)
	}
}

func TestRunPreComment_AllowAndDeny(t *testing.T) {
	dir := t.TempDir()
	allow := writeHook(t, dir, "allow.sh", "#!/usr/bin/env bash\nexit 0\n")
	deny := writeHook(t, dir, "deny.sh", "#!/usr/bin/env bash\necho blocked by gate >&2\nexit 1\n")

	commentFile := filepath.Join(dir, "comment.md")
	if err := os.WriteFile(commentFile, []byte("STATUS: DONE\nok"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	env := map[string]string{
		"MULTICA_COMMENT_FILE": commentFile,
		"MULTICA_AUTHOR_ID":    "agent-1",
	}

	if err := RunPreComment(ctx, WorkspaceHooks{PreComment: allow, Timeout: time.Second}, env, commentFile); err != nil {
		t.Fatalf("allow hook: %v", err)
	}

	err := RunPreComment(ctx, WorkspaceHooks{PreComment: deny, Timeout: time.Second}, env, commentFile)
	var denied *HookDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("deny hook: expected HookDeniedError, got %T: %v", err, err)
	}
	if !strings.Contains(denied.Stderr, "blocked by gate") {
		t.Fatalf("stderr = %q", denied.Stderr)
	}
}

func TestRunPreComment_MissingScriptFailsClosed(t *testing.T) {
	err := RunPreComment(context.Background(), WorkspaceHooks{
		PreComment: filepath.Join(t.TempDir(), "missing.sh"),
		Timeout:    time.Second,
	}, nil, filepath.Join(t.TempDir(), "c.md"))
	var failed *HookFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("expected HookFailedError, got %T: %v", err, err)
	}
}

func TestRunPreComment_TimeoutFailsClosed(t *testing.T) {
	dir := t.TempDir()
	slow := writeHook(t, dir, "slow.sh", "#!/usr/bin/env bash\nsleep 2\n")
	commentFile := filepath.Join(dir, "comment.md")
	_ = os.WriteFile(commentFile, []byte("x"), 0o644)

	err := RunPreComment(context.Background(), WorkspaceHooks{
		PreComment: slow,
		Timeout:    50 * time.Millisecond,
	}, nil, commentFile)
	var failed *HookFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("expected HookFailedError, got %T: %v", err, err)
	}
	if !strings.Contains(failed.Error(), "timed out") {
		t.Fatalf("error = %v", failed)
	}
}

func TestRunPreComment_UnconfiguredSkips(t *testing.T) {
	if err := RunPreComment(context.Background(), WorkspaceHooks{}, nil, ""); err != nil {
		t.Fatalf("unconfigured hook should skip: %v", err)
	}
}
