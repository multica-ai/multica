package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newSkillFilesBatchUpsertTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "batch-upsert"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("manifest", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

// writeBatchManifest marshals the manifest as JSON (so Windows path separators
// in local file references are escaped correctly) and writes it to a temp file.
func writeBatchManifest(t *testing.T, manifest map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := t.TempDir() + string(os.PathSeparator) + "manifest.json"
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func writeBatchContentFile(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + string(os.PathSeparator) + name
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write content file %s: %v", name, err)
	}
	return path
}

func TestRunSkillFilesBatchUpsertSendsVerbatimUTF8Content(t *testing.T) {
	var body map[string]any
	srv := newSkillBodyCaptureServer(t, http.MethodPut, "/api/skills/skill-123/files/batch", &body)
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	contentA := "alpha body\nwith \"quotes\" and a literal \\n\n"
	contentB := "# 中文指南\n这是逐字节读取的说明文字。\n"
	pathA := writeBatchContentFile(t, "a.md", contentA)
	pathB := writeBatchContentFile(t, "b.md", contentB)

	manifestPath := writeBatchManifest(t, map[string]any{
		"files": []any{
			map[string]any{"path": "docs/a.md", "file": pathA},
			map[string]any{"path": "notes/b.md", "file": pathB},
		},
	})

	cmd := newSkillFilesBatchUpsertTestCmd()
	_ = cmd.Flags().Set("manifest", manifestPath)
	if _, err := captureStdout(t, func() error { return runSkillFilesBatchUpsert(cmd, []string{"skill-123"}) }); err != nil {
		t.Fatalf("runSkillFilesBatchUpsert: %v", err)
	}

	files, ok := body["files"].([]any)
	if !ok {
		t.Fatalf("body files missing or wrong type: %#v", body["files"])
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	fileA, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("files[0] wrong type: %#v", files[0])
	}
	if fileA["path"] != "docs/a.md" {
		t.Fatalf("files[0] path = %v, want docs/a.md", fileA["path"])
	}
	if fileA["content"] != contentA {
		t.Fatalf("files[0] content = %q, want verbatim %q", fileA["content"], contentA)
	}
	if _, present := fileA["expected_sha256"]; present {
		t.Fatalf("files[0] must omit expected_sha256 when not set: %#v", fileA)
	}
	fileB, ok := files[1].(map[string]any)
	if !ok {
		t.Fatalf("files[1] wrong type: %#v", files[1])
	}
	if fileB["path"] != "notes/b.md" {
		t.Fatalf("files[1] path = %v, want notes/b.md", fileB["path"])
	}
	if fileB["content"] != contentB {
		t.Fatalf("files[1] content = %q, want verbatim UTF-8 %q", fileB["content"], contentB)
	}
}

func TestRunSkillFilesBatchUpsertPassesConcurrencyFields(t *testing.T) {
	var body map[string]any
	srv := newSkillBodyCaptureServer(t, http.MethodPut, "/api/skills/skill-123/files/batch", &body)
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	wantEntrySHA := strings.Repeat("a", 64)
	wantSkillSHA := strings.Repeat("b", 64)
	wantIdempotencyKey := "idem-key-1"

	pathA := writeBatchContentFile(t, "a.md", "content a\n")
	pathB := writeBatchContentFile(t, "b.md", "content b\n")
	manifestPath := writeBatchManifest(t, map[string]any{
		"files": []any{
			map[string]any{"path": "docs/a.md", "file": pathA, "expected_sha256": wantEntrySHA},
			map[string]any{"path": "notes/b.md", "file": pathB},
		},
		"expected_skill_sha256": wantSkillSHA,
		"idempotency_key":       wantIdempotencyKey,
	})

	cmd := newSkillFilesBatchUpsertTestCmd()
	_ = cmd.Flags().Set("manifest", manifestPath)
	if _, err := captureStdout(t, func() error { return runSkillFilesBatchUpsert(cmd, []string{"skill-123"}) }); err != nil {
		t.Fatalf("runSkillFilesBatchUpsert: %v", err)
	}

	if body["expected_skill_sha256"] != wantSkillSHA {
		t.Fatalf("expected_skill_sha256 = %v, want %s", body["expected_skill_sha256"], wantSkillSHA)
	}
	if body["idempotency_key"] != wantIdempotencyKey {
		t.Fatalf("idempotency_key = %v, want %s", body["idempotency_key"], wantIdempotencyKey)
	}
	files, ok := body["files"].([]any)
	if !ok || len(files) != 2 {
		t.Fatalf("body files missing or wrong length: %#v", body["files"])
	}
	fileA, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("files[0] wrong type: %#v", files[0])
	}
	if fileA["expected_sha256"] != wantEntrySHA {
		t.Fatalf("files[0] expected_sha256 = %v, want %s", fileA["expected_sha256"], wantEntrySHA)
	}
	fileB, ok := files[1].(map[string]any)
	if !ok {
		t.Fatalf("files[1] wrong type: %#v", files[1])
	}
	if _, present := fileB["expected_sha256"]; present {
		t.Fatalf("files[1] must omit expected_sha256 when not set: %#v", fileB)
	}
}

func TestRunSkillFilesBatchUpsertMissingManifestFlag(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	cmd := newSkillFilesBatchUpsertTestCmd()
	err := runSkillFilesBatchUpsert(cmd, []string{"skill-123"})
	if err == nil {
		t.Fatal("expected missing --manifest to return an error")
	}
	if !strings.Contains(err.Error(), "--manifest is required") {
		t.Fatalf("error = %v, want mention of --manifest is required", err)
	}
	if hits != 0 {
		t.Fatalf("expected no HTTP request for a missing flag, got %d", hits)
	}
}

func TestRunSkillFilesBatchUpsertEmptyManifestListedNoFiles(t *testing.T) {
	setSkillServerEnv(t, "http://127.0.0.1:1")

	manifestPath := writeBatchManifest(t, map[string]any{"files": []any{}})

	cmd := newSkillFilesBatchUpsertTestCmd()
	_ = cmd.Flags().Set("manifest", manifestPath)
	err := runSkillFilesBatchUpsert(cmd, []string{"skill-123"})
	if err == nil {
		t.Fatal("expected empty manifest to return an error")
	}
	if !strings.Contains(err.Error(), "lists no files") {
		t.Fatalf("error = %v, want mention of lists no files", err)
	}
}

func TestRunSkillFilesBatchUpsertEntryMissingFileField(t *testing.T) {
	setSkillServerEnv(t, "http://127.0.0.1:1")

	manifestPath := writeBatchManifest(t, map[string]any{
		"files": []any{map[string]any{"path": "docs/a.md"}},
	})

	cmd := newSkillFilesBatchUpsertTestCmd()
	_ = cmd.Flags().Set("manifest", manifestPath)
	err := runSkillFilesBatchUpsert(cmd, []string{"skill-123"})
	if err == nil {
		t.Fatal("expected entry without file to return an error")
	}
	if !strings.Contains(err.Error(), `missing "file"`) {
		t.Fatalf("error = %v, want mention of missing \"file\"", err)
	}
}

func TestRunSkillFilesBatchUpsertConflictFromServer(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/api/skills/skill-123/files/batch" {
			t.Fatalf("path = %q, want /api/skills/skill-123/files/batch", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "expected_skill_sha256 does not match current skill files",
		})
	}))
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	contentPath := writeBatchContentFile(t, "a.md", "conflicting body\n")
	manifestPath := writeBatchManifest(t, map[string]any{
		"files": []any{map[string]any{"path": "docs/a.md", "file": contentPath}},
	})

	cmd := newSkillFilesBatchUpsertTestCmd()
	_ = cmd.Flags().Set("manifest", manifestPath)
	if _, err := captureStdout(t, func() error { return runSkillFilesBatchUpsert(cmd, []string{"skill-123"}) }); err == nil {
		t.Fatal("expected 409 from server to return an error")
	} else if !strings.Contains(err.Error(), "batch upsert skill files") {
		t.Fatalf("error = %v, want wrap mentioning batch upsert skill files", err)
	}
	if hits != 1 {
		t.Fatalf("server hit %d times, want exactly one PUT attempt", hits)
	}
}

func TestRunSkillFilesBatchUpsertTableOutputPrintsPathsAndAggregate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []any{
				map[string]any{"id": "f1", "path": "docs/SKILL.md"},
				map[string]any{"id": "f2", "path": "notes/idea.md"},
			},
			"skill_files_sha256": strings.Repeat("c", 64),
		})
	}))
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	contentPath := writeBatchContentFile(t, "a.md", "table body\n")
	manifestPath := writeBatchManifest(t, map[string]any{
		"files": []any{map[string]any{"path": "docs/SKILL.md", "file": contentPath}},
	})

	cmd := newSkillFilesBatchUpsertTestCmd()
	_ = cmd.Flags().Set("manifest", manifestPath)
	_ = cmd.Flags().Set("output", "table")

	out, err := captureStdout(t, func() error { return runSkillFilesBatchUpsert(cmd, []string{"skill-123"}) })
	if err != nil {
		t.Fatalf("runSkillFilesBatchUpsert: %v", err)
	}
	for _, want := range []string{"docs/SKILL.md", "notes/idea.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output %q must contain upserted path %s", out, want)
		}
	}
	wantAggregate := "Skill files sha256: " + strings.Repeat("c", 64)
	if !strings.Contains(out, wantAggregate) {
		t.Fatalf("table output %q must contain aggregate line %q", out, wantAggregate)
	}
}
