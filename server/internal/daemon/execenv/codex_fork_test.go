package execenv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishCodexForkReplaysPublishedChild(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	const child = "019fe3de-56a2-7792-8913-ee53664dfa0e"
	called := 0
	stage := func(dir string) (string, error) {
		called++
		if _, err := publishCodexFork(store, "request-1", func(string) (string, error) {
			t.Error("overlapping request called the provider")
			return "", errors.New("overlap")
		}); err == nil {
			t.Error("overlapping request was accepted")
		}
		if err := os.WriteFile(filepath.Join(dir, "sessions", "rollout-2026-11-20T00-00-00-"+child+".jsonl"), []byte("{}"), 0o600); err != nil {
			return "", err
		}
		return child, nil
	}
	for range 2 {
		got, err := publishCodexFork(store, "request-1", stage)
		if err != nil || got != child {
			t.Fatalf("child=%q, error=%v, want %q", got, err, child)
		}
	}
	if called != 1 {
		t.Fatalf("native fork ran %d times", called)
	}
	if _, err := publishCodexFork(store, "another-request", stage); err == nil || called != 1 {
		t.Fatalf("existing child was accepted for another operation: %v, calls=%d", err, called)
	}
}

func TestPublishCodexForkAbortsFailedStaging(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	failure := errors.New("native fork failed")
	if _, err := publishCodexFork(store, "request-1", func(string) (string, error) { return "", failure }); !errors.Is(err, failure) {
		t.Fatalf("error=%v, want %v", err, failure)
	}
	if _, err := os.Stat(filepath.Join(store, "fork")); !os.IsNotExist(err) {
		t.Fatalf("failed fork exposed its store: %v", err)
	}
}

func TestStageCodexForkCopiesOnlySelectedSource(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	stage := t.TempDir()
	const sourceID = "019fe3de-56a2-7792-8913-ee53664dfa0e"
	name := "rollout-2026-11-20T00-00-00-" + sourceID + ".jsonl"
	for _, file := range []string{name, "unrelated.jsonl"} {
		if err := os.WriteFile(filepath.Join(source, file), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	copied, err := stageCodexForkSource(source, stage, sourceID)
	if err != nil || filepath.Base(copied) != name {
		t.Fatalf("copied=%q error=%v", copied, err)
	}
	if _, err := os.Stat(filepath.Join(stage, "sessions", "unrelated.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("staging included unrelated history: %v", err)
	}
	if err := os.WriteFile(copied, []byte("changed copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(source, name)); err != nil || string(data) != name {
		t.Fatalf("source was changed: %q, error=%v", data, err)
	}
}
