package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildTestZip packs the given path->content map into an in-memory zip and
// returns its bytes. A path ending in "/" is written as a directory entry.
func buildTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		if strings.HasSuffix(name, "/") {
			if _, err := zw.Create(name); err != nil {
				t.Fatalf("create dir entry %q: %v", name, err)
			}
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func filePaths(imported *importedSkill) []string {
	paths := make([]string, 0, len(imported.files))
	for _, f := range imported.files {
		paths = append(paths, f.path)
	}
	return paths
}

func fileContent(imported *importedSkill, path string) (string, bool) {
	for _, f := range imported.files {
		if f.path == path {
			return f.content, true
		}
	}
	return "", false
}

const testSkillMd = `---
name: review-helper
description: Reviews code changes
---

# Review Helper

Do the review.
`

func TestParseSkillArchive_NestedWrapper(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"review-helper/":                "",
		"review-helper/SKILL.md":        testSkillMd,
		"review-helper/scripts/run.sh":  "echo hi",
		"review-helper/references/g.md": "guide",
	})

	imported, err := parseSkillArchive(data, "review-helper.skill")
	if err != nil {
		t.Fatalf("parseSkillArchive: %v", err)
	}
	if imported.name != "review-helper" {
		t.Errorf("name = %q, want review-helper", imported.name)
	}
	if imported.description != "Reviews code changes" {
		t.Errorf("description = %q", imported.description)
	}
	if !strings.Contains(imported.content, "# Review Helper") {
		t.Errorf("content missing SKILL.md body: %q", imported.content)
	}
	got := filePaths(imported)
	want := map[string]bool{"scripts/run.sh": true, "references/g.md": true}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want keys %v", got, want)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected file %q (SKILL.md must not be a supporting file)", p)
		}
	}
	if c, ok := fileContent(imported, "scripts/run.sh"); !ok || c != "echo hi" {
		t.Errorf("scripts/run.sh content = %q, ok=%v", c, ok)
	}
}

func TestParseSkillArchive_RootLayout(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"SKILL.md":          testSkillMd,
		"references/doc.md": "doc",
	})
	imported, err := parseSkillArchive(data, "anything.zip")
	if err != nil {
		t.Fatalf("parseSkillArchive: %v", err)
	}
	if imported.name != "review-helper" {
		t.Errorf("name = %q, want review-helper", imported.name)
	}
	if got := filePaths(imported); len(got) != 1 || got[0] != "references/doc.md" {
		t.Errorf("files = %v, want [references/doc.md]", got)
	}
}

func TestParseSkillArchive_NoSkillMd(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"my-skill/notes.md": "hello",
	})
	if _, err := parseSkillArchive(data, "x.skill"); err == nil {
		t.Fatal("expected error when archive has no SKILL.md")
	}
}

func TestParseSkillArchive_InvalidZip(t *testing.T) {
	if _, err := parseSkillArchive([]byte("not a zip"), "x.skill"); err == nil {
		t.Fatal("expected error for non-zip data")
	}
}

func TestParseSkillArchive_RejectsUnsafeSkillMdPath(t *testing.T) {
	// A SKILL.md whose only candidate path is absolute or traversal must not be
	// accepted as the primary content; the archive is treated as having none.
	for _, name := range []string{"../escape/SKILL.md", "/abs/SKILL.md"} {
		data := buildTestZip(t, map[string]string{name: testSkillMd})
		if _, err := parseSkillArchive(data, "x.skill"); err == nil {
			t.Errorf("expected rejection for unsafe SKILL.md path %q", name)
		}
	}
}

func TestParseSkillArchive_DropsTraversalAndJunk(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"s/SKILL.md":     testSkillMd,
		"s/../evil.sh":   "pwn",       // zip-slip out of the skill root
		"s/.git/config":  "secret",    // dotfile dir
		"s/.DS_Store":    "junk",      // dotfile
		"__MACOSX/s/._x": "applemeta", // mac noise (outside root anyway)
		"s/LICENSE":      "MIT",       // license excluded
		"s/keep.md":      "real",      // legitimate asset
	})
	imported, err := parseSkillArchive(data, "s.skill")
	if err != nil {
		t.Fatalf("parseSkillArchive: %v", err)
	}
	got := filePaths(imported)
	if len(got) != 1 || got[0] != "keep.md" {
		t.Fatalf("files = %v, want only [keep.md]", got)
	}
}

func TestParseSkillArchive_SkipsBinaryAssets(t *testing.T) {
	data := buildTestZip(t, map[string]string{
		"s/SKILL.md": testSkillMd,
		"s/logo.png": "\x89PNG\x00binary",
		"s/note.txt": "text",
	})
	imported, err := parseSkillArchive(data, "s.skill")
	if err != nil {
		t.Fatalf("parseSkillArchive: %v", err)
	}
	if got := filePaths(imported); len(got) != 1 || got[0] != "note.txt" {
		t.Errorf("files = %v, want [note.txt] (binary png dropped)", got)
	}
}

func TestParseSkillArchive_NameFallbackToWrapperDir(t *testing.T) {
	noName := "# Title only\n\nNo frontmatter name here.\n"
	data := buildTestZip(t, map[string]string{
		"cool-skill/SKILL.md": noName,
	})
	imported, err := parseSkillArchive(data, "ignored.skill")
	if err != nil {
		t.Fatalf("parseSkillArchive: %v", err)
	}
	if imported.name != "cool-skill" {
		t.Errorf("name = %q, want cool-skill (wrapper dir fallback)", imported.name)
	}
}

func TestParseSkillArchive_NameFallbackToFilename(t *testing.T) {
	noName := "# Title only\n"
	data := buildTestZip(t, map[string]string{
		"SKILL.md": noName,
	})
	imported, err := parseSkillArchive(data, "My-Thing.skill")
	if err != nil {
		t.Fatalf("parseSkillArchive: %v", err)
	}
	if imported.name != "My-Thing" {
		t.Errorf("name = %q, want My-Thing (filename fallback)", imported.name)
	}
}

// --- Handler-level tests: the multipart /api/skills/import archive path ---

func skillMdWithName(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n\nBody.\n"
}

func newSkillArchiveImportRequest(userID string, archive []byte, filename, onConflict string) *http.Request {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", filename)
	_, _ = part.Write(archive)
	if onConflict != "" {
		_ = writer.WriteField("on_conflict", onConflict)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/skills/import", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func TestImportSkill_ArchiveUploadCreatesSkill(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	name := "archive-create-" + t.Name()
	archive := buildTestZip(t, map[string]string{
		name + "/SKILL.md":       skillMdWithName(name, "From archive"),
		name + "/scripts/run.sh": "echo hi",
	})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
	})

	w := httptest.NewRecorder()
	testHandler.ImportSkill(w, newSkillArchiveImportRequest(testUserID, archive, name+".skill", "fail"))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var body SkillImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "created" || body.Skill == nil {
		t.Fatalf("body = %#v", body)
	}
	if body.Skill.Name != name {
		t.Errorf("name = %q, want %q", body.Skill.Name, name)
	}
	found := false
	for _, f := range body.Skill.Files {
		if f.Path == "scripts/run.sh" {
			found = true
		}
	}
	if !found {
		t.Errorf("scripts/run.sh missing from imported files: %#v", body.Skill.Files)
	}
}

func TestImportSkill_ArchiveUploadConflictSkip(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	namePrefix := "archive-skip"
	skillName := namePrefix + "-" + t.Name()
	existingID := insertHandlerTestSkill(t, namePrefix, "# Existing")
	archive := buildTestZip(t, map[string]string{
		skillName + "/SKILL.md": skillMdWithName(skillName, "From archive"),
	})

	w := httptest.NewRecorder()
	testHandler.ImportSkill(w, newSkillArchiveImportRequest(testUserID, archive, skillName+".skill", "skip"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body SkillImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "skipped" {
		t.Fatalf("status = %q, want skipped", body.Status)
	}
	if body.ExistingSkill == nil || body.ExistingSkill.ID != existingID {
		t.Fatalf("existing_skill = %#v", body.ExistingSkill)
	}
}

func TestImportSkill_ArchiveUploadRejectsNonZip(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	w := httptest.NewRecorder()
	testHandler.ImportSkill(w, newSkillArchiveImportRequest(testUserID, []byte("not a zip at all"), "bad.skill", "fail"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
