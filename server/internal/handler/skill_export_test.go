package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// insertHandlerTestSkillFile inserts a supporting file for a skill and
// registers its cleanup.
func insertHandlerTestSkillFile(t *testing.T, skillID, filePath, content string) {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill_file (skill_id, path, content)
		VALUES ($1, $2, $3)
		RETURNING id
	`, skillID, filePath, content).Scan(&id); err != nil {
		t.Fatalf("insert skill_file %q: %v", filePath, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill_file WHERE id = $1`, id)
	})
}

func TestExportSkill_ReturnsRoundTrippableTarGz(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}

	namePrefix := "export-skill"
	skillID := insertHandlerTestSkill(t, namePrefix, `---
name: `+namePrefix+`-`+t.Name()+`
description: Exported test skill
---

# Exported
`)
	insertHandlerTestSkillFile(t, skillID, "scripts/run.sh", "echo hi")
	insertHandlerTestSkillFile(t, skillID, "references/g.md", "guide")

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/skills/"+skillID+"/export", nil), "id", skillID)
	testHandler.ExportSkill(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", ct)
	}

	skillName := namePrefix + "-" + t.Name()
	wantFilename := skillName + ".tar.gz"
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="`+wantFilename+`"`) {
		t.Errorf("Content-Disposition = %q, want filename %q", cd, wantFilename)
	}

	// The body must be a real gzip tar with the wrapper layout.
	gz, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	gotEntries := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read entry %q: %v", hdr.Name, err)
		}
		gotEntries[hdr.Name] = string(body)
	}

	root := skillName + "/"
	for _, want := range []string{root + "SKILL.md", root + "scripts/run.sh", root + "references/g.md"} {
		if _, ok := gotEntries[want]; !ok {
			t.Errorf("archive missing entry %q; got %v", want, keys(gotEntries))
		}
	}
	if !strings.Contains(gotEntries[root+"SKILL.md"], "# Exported") {
		t.Errorf("SKILL.md content not preserved: %q", gotEntries[root+"SKILL.md"])
	}
	if gotEntries[root+"scripts/run.sh"] != "echo hi" {
		t.Errorf("scripts/run.sh = %q, want echo hi", gotEntries[root+"scripts/run.sh"])
	}

	// The exported archive must be consumable by the archive importer with
	// the same name/content/files (the whole point of sharing).
	imported, err := parseSkillArchive(w.Body.Bytes(), wantFilename)
	if err != nil {
		t.Fatalf("re-import exported archive: %v", err)
	}
	if imported.name != skillName {
		t.Errorf("re-import name = %q, want %q", imported.name, skillName)
	}
	if imported.description != "Exported test skill" {
		t.Errorf("re-import description = %q", imported.description)
	}
	gotFiles := filePaths(imported)
	if len(gotFiles) != 2 {
		t.Fatalf("re-import files = %v, want 2", gotFiles)
	}
	if c, ok := fileContent(imported, "references/g.md"); !ok || c != "guide" {
		t.Errorf("re-import references/g.md = %q, ok=%v", c, ok)
	}
}

func TestExportSkill_NotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test DB not configured")
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/skills/does-not-exist/export", nil), "id", "00000000-0000-0000-0000-000000000000")
	testHandler.ExportSkill(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestExportSkill_FilenameSanitizesUnsafeName(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}

	name := "unsafe/name\r\nX-Evil: 1"
	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, "fixture", "# Unsafe", testUserID).Scan(&skillID); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/skills/"+skillID+"/export", nil), "id", skillID)
	testHandler.ExportSkill(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if strings.Contains(cd, "\r\n") || strings.Contains(cd, "\n") {
		t.Fatalf("Content-Disposition contains a newline (header injection): %q", cd)
	}
	if !strings.Contains(cd, `filename="unsafe-name-X-Evil-1.tar.gz"`) {
		t.Errorf("Content-Disposition = %q, want sanitized filename", cd)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
