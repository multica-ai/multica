package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyFileCreatesPrivateDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "nested", "dest")
	if err := os.Mkdir(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err == nil {
		// The source is intentionally outside the working directory; this
		// confirms callers cannot smuggle arbitrary absolute paths into copyFile.
		t.Fatal("expected unsafe source path to be rejected")
	}
	// Use relative paths within a temporary working directory for the success case.
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := copyFile("source", "dest"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat("dest")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("destination mode = %o, want 600", info.Mode().Perm())
	}
	content, err := os.ReadFile("dest")
	if err != nil || string(content) != "secret" {
		t.Fatalf("destination content = %q, err=%v", content, err)
	}
}

func TestCopyFileRejectsTraversalAndExistingDestination(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyFile("../source", filepath.Join(dir, "dest")); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("traversal error = %v", err)
	}
	if err := copyFile(filepath.Join(dir, "source"), filepath.Join(dir, "dest")); err == nil {
		// Absolute paths are rejected before opening, proving the helper is
		// intentionally scoped to startup-relative paths.
		t.Fatal("expected absolute source path to be rejected")
	}
}

func TestCheckFirstRunCreatesDirectoriesAndDoesNotOverwriteEnv(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.WriteFile(".env.example", []byte("SECRET=example\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkFirstRun(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"data/.runtime", "data/.runtime/secrets", "data/90_运行数据/teams", "data/00_系统"} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0755 {
			t.Fatalf("directory %s: info=%v err=%v", path, info, err)
		}
	}
	if err := os.WriteFile(".env", []byte("SECRET=existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkFirstRun(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(".env")
	if err != nil || string(content) != "SECRET=existing\n" {
		t.Fatalf("existing env overwritten: %q, err=%v", content, err)
	}
}

func TestCheckFirstRunSoftFailsWhenDatabaseUnavailable(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := checkFirstRun(); err != nil {
		t.Fatalf("database unavailability must be non-fatal: %v", err)
	}
}
