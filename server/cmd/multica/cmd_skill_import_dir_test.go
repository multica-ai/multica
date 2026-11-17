package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func writeSkillDirFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unzipToMap(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}

func newSkillDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "review-helper")
	writeSkillDirFile(t, dir, "SKILL.md", "---\nname: review-helper\n---\nbody")
	return dir
}

func TestRunSkillImportDirUploadsNestedFiles(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")

	dir := newSkillDir(t)
	writeSkillDirFile(t, dir, "scripts/run.sh", "echo hi\n")
	writeSkillDirFile(t, dir, "references/deep/notes.md", "notes")
	// Left out, matching the server and browser importers.
	writeSkillDirFile(t, dir, ".git/config", "[core]")
	writeSkillDirFile(t, dir, ".DS_Store", "x")
	writeSkillDirFile(t, dir, "LICENSE", "MIT")
	writeSkillDirFile(t, dir, "assets/logo.png", "png")
	writeSkillDirFile(t, dir, "nested/SKILL.md", "nested")
	writeSkillDirFile(t, dir, "big.md", strings.Repeat("x", skillDirMaxFileBytes+1))

	var gotFiles map[string]string
	var gotFilename, gotConflict string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/skills/import" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		data, _ := io.ReadAll(file)
		gotFiles = unzipToMap(t, data)
		gotFilename = header.Filename
		gotConflict = r.FormValue("on_conflict")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "created",
			"skill":  map[string]any{"id": "skill-1", "name": "review-helper"},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newSkillImportTestCmd()
	_ = cmd.Flags().Set("dir", dir)
	_ = cmd.Flags().Set("on-conflict", "rename")

	out, err := captureStdout(t, func() error { return runSkillImport(cmd, nil) })
	if err != nil {
		t.Fatalf("runSkillImport: %v", err)
	}
	if !strings.Contains(out, `"created"`) {
		t.Fatalf("stdout = %q", out)
	}
	if gotFilename != "review-helper.zip" {
		t.Fatalf("filename = %q", gotFilename)
	}
	if gotConflict != "rename" {
		t.Fatalf("on_conflict = %q", gotConflict)
	}
	want := map[string]string{
		"review-helper/SKILL.md":                 "---\nname: review-helper\n---\nbody",
		"review-helper/scripts/run.sh":           "echo hi\n",
		"review-helper/references/deep/notes.md": "notes",
	}
	if !reflect.DeepEqual(gotFiles, want) {
		names := make([]string, 0, len(gotFiles))
		for n := range gotFiles {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("archive entries = %v, want %v", names, want)
	}
}

func TestRunSkillImportDirFailsBeforeUpload(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected upload to %s", r.URL.Path)
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	noSkillMd := t.TempDir()
	writeSkillDirFile(t, noSkillMd, "sub/SKILL.md", "nested only")

	cases := []struct {
		name  string
		flags map[string]string
		want  string
	}{
		{"missing SKILL.md", map[string]string{"dir": noSkillMd}, "does not contain a SKILL.md"},
		{"dir and file", map[string]string{"dir": newSkillDir(t), "file": "x.zip"}, "exactly one of --url, --file or --dir"},
		{"dir and url", map[string]string{"dir": newSkillDir(t), "url": "https://skills.sh/a/b"}, "exactly one of --url, --file or --dir"},
		{"no source", map[string]string{}, "exactly one of --url, --file or --dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newSkillImportTestCmd()
			for k, v := range tc.flags {
				_ = cmd.Flags().Set(k, v)
			}
			err := runSkillImport(cmd, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestPackSkillDirectorySymlinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("link inside the directory is packed", func(t *testing.T) {
		dir := newSkillDir(t)
		writeSkillDirFile(t, dir, "docs/real.md", "real")
		if err := os.Symlink("real.md", filepath.Join(dir, "docs", "alias.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		packed, err := packSkillDirectory(dir)
		if err != nil {
			t.Fatalf("pack: %v", err)
		}
		if got := unzipToMap(t, packed.data)["review-helper/docs/alias.md"]; got != "real" {
			t.Fatalf("alias.md = %q", got)
		}
	})

	t.Run("link outside the directory is rejected", func(t *testing.T) {
		dir := newSkillDir(t)
		if err := os.Symlink(outside, filepath.Join(dir, "leak.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		_, err := packSkillDirectory(dir)
		if err == nil || !strings.Contains(err.Error(), "leak.md") {
			t.Fatalf("err = %v, want rejection naming leak.md", err)
		}
	})

	t.Run("link to a directory is rejected", func(t *testing.T) {
		dir := newSkillDir(t)
		writeSkillDirFile(t, dir, "docs/real.md", "real")
		if err := os.Symlink("docs", filepath.Join(dir, "docs-link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		_, err := packSkillDirectory(dir)
		if err == nil || !strings.Contains(err.Error(), "only supported for regular files") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPackSkillDirectoryCaps(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		dir := newSkillDir(t)
		for i := 0; i <= skillDirMaxFileCount; i++ {
			writeSkillDirFile(t, dir, "refs/f"+strconv.Itoa(i)+".md", "x")
		}
		_, err := packSkillDirectory(dir)
		if err == nil || !strings.Contains(err.Error(), "supporting files") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("aggregate size", func(t *testing.T) {
		dir := newSkillDir(t)
		chunk := strings.Repeat("x", skillDirMaxFileBytes)
		for i := 0; i <= skillDirMaxBundleBytes/skillDirMaxFileBytes; i++ {
			writeSkillDirFile(t, dir, "refs/f"+strconv.Itoa(i)+".md", chunk)
		}
		_, err := packSkillDirectory(dir)
		if err == nil || !strings.Contains(err.Error(), "byte limit") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("not a directory", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "skill.zip")
		if err := os.WriteFile(f, []byte("zip"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := packSkillDirectory(f)
		if err == nil || !strings.Contains(err.Error(), "use --file") {
			t.Fatalf("err = %v", err)
		}
	})
}
