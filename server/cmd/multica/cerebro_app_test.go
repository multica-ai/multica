package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestAppPublishPostsTheImmutableBundleContract(t *testing.T) {
	var method, path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"deployment_status": "provisioning"})
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"manifest":{"schema_version":"1","name":"Test App","version":"0.1.0","frontend":{"entry":"frontend/index.html"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "index.html"), []byte("<h1>Test App</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetWorkflowFlags(t, appPublishCmd, "dir", "version", "release-notes", "output")
	_ = appPublishCmd.Flags().Set("dir", dir)
	_ = appPublishCmd.Flags().Set("version", "0.1.0")
	_ = appPublishCmd.Flags().Set("release-notes", "First release")

	withCLIEnv(t, server.URL, "ws-1", func() {
		if err := runAppPublish(appPublishCmd, []string{"app-1"}); err != nil {
			t.Fatalf("runAppPublish: %v", err)
		}
	})

	if method != http.MethodPost || path != "/api/cerebro/apps/app-1/publish" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body["version"] != "0.1.0" || body["release_notes"] != "First release" {
		t.Fatalf("publish metadata = %#v", body)
	}
	if _, legacy := body["snapshot"]; legacy {
		t.Fatalf("legacy snapshot wrapper leaked into publish request: %#v", body)
	}
	files, ok := body["files"].([]any)
	if !ok || len(files) != 2 {
		t.Fatalf("publish files = %#v", body["files"])
	}
	appFile, ok := files[0].(map[string]any)
	if !ok || appFile["path"] != "app.json" || appFile["content_base64"] == "" || appFile["sha256"] == "" {
		t.Fatalf("app.json bundle entry = %#v", files[0])
	}
}

func TestLegacyAppWorkflowCommandPointsToTheWorkflowsProduct(t *testing.T) {
	command, args, err := rootCmd.Find([]string{"app", "workflow", "create"})
	if err != nil {
		t.Fatalf("find legacy app workflow command: %v", err)
	}
	if command != appWorkflowCreateCmd {
		t.Fatalf("legacy app workflow resolved to %q instead of the migration command", command.CommandPath())
	}
	if !command.Hidden {
		t.Fatal("legacy app workflow command is still advertised")
	}
	if command.RunE == nil {
		t.Fatal("legacy app workflow command has no migration guidance")
	}
	resetWorkflowFlags(t, command, "app", "name", "version", "file")
	if err := command.ParseFlags([]string{"--app", "app-1", "--name", "Legacy", "--version", "1.0.0", "--file", "legacy.json"}); err != nil {
		t.Fatalf("parse legacy invocation: %v", err)
	}
	err = command.RunE(command, args)
	if err == nil || !strings.Contains(err.Error(), "multica workflow create --file") || !strings.Contains(err.Error(), "/api/cerebro/workflows") {
		t.Fatalf("legacy app workflow guidance = %v", err)
	}
}
