package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndSearch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Example() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := ReadFile(root, "main.go", 2, 2)
	if err != nil || len(result) != 1 || result[0].Line != 2 || result[0].Text != "func Example() {}" {
		t.Fatalf("read: %+v %v", result, err)
	}
	matches, err := Search(context.Background(), root, "Example", 10)
	if err != nil || len(matches) != 1 || matches[0].Path != "main.go" || matches[0].Line != 2 {
		t.Fatalf("search: %+v %v", matches, err)
	}
}

func TestReadRejectsEscapesAndGitMetadata(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", filepath.Join(outside, "secret"), ".git/config", "escape"} {
		if _, err := ReadFile(root, path, 1, 20); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
}
