package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppCommandExposesCatalogLifecycle(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"app"})
	if err != nil || cmd == rootCmd {
		t.Fatalf("app command is not registered: %v", err)
	}
	for _, name := range []string{"create", "preview", "publish", "rollback", "disable", "list"} {
		if child, _, err := cmd.Find([]string{name}); err != nil || child == cmd {
			t.Errorf("app %s command is not registered", name)
		}
	}
}

func TestReadAppBundleBuildsImmutableFilesAndRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"manifest":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "index.html"), []byte("<h1>App</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := readAppBundle(dir)
	if err != nil {
		t.Fatalf("read app bundle: %v", err)
	}
	if len(files) != 2 || files[0]["path"] != "app.json" || files[1]["path"] != "frontend/index.html" {
		t.Fatalf("unexpected files: %#v", files)
	}
	if files[1]["sha256"] == "" || files[1]["content_base64"] == "" {
		t.Fatalf("bundle hashes or content are missing: %#v", files[1])
	}
	if err := os.Symlink(filepath.Join(dir, "app.json"), filepath.Join(dir, "frontend", "linked.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readAppBundle(dir); err == nil {
		t.Fatal("symlink was accepted")
	}
}
