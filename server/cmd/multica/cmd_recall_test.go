package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/recall"
)

func TestRunRecallQueryUsesSharedRecallEngine(t *testing.T) {
	vault := t.TempDir()
	notePath := filepath.Join(vault, "notes", "shared memory recall.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notePath, []byte("---\ndescription: Shared memory recall is bounded\ntype: decision\ncreated: 2026-06-24\n---\n\n# Shared memory recall\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recall.BuildIndex(t.Context(), recall.IndexOptions{VaultRoot: vault}); err != nil {
		t.Fatal(err)
	}

	command := newRecallQueryCommand()
	command.SetArgs([]string{
		"--vault", vault,
		"--title", "implement shared memory recall",
		"--max-hits", "5",
		"--budget", "4096",
		"--index-max-age", (24 * time.Hour).String(),
		"--output", "json",
	})
	output, err := captureStdout(t, func() error {
		return command.Execute()
	})
	if err != nil {
		t.Fatalf("execute recall query: %v", err)
	}

	var result recall.Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode recall output: %v\n%s", err, output)
	}
	if result.Status != recall.StatusHit || result.HitCount != 1 {
		t.Fatalf("recall result = %#v", result)
	}
	if result.Hits[0].Path != "notes/shared memory recall.md" {
		t.Fatalf("hit path = %q", result.Hits[0].Path)
	}
}
