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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func writeSkillDirFile(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

type uploadedSkillArchive struct {
	filename   string
	onConflict string
	files      map[string]string
}

// newSkillArchiveUploadServer records the multipart archive a CLI import sends
// and answers with a created skill.
func newSkillArchiveUploadServer(t *testing.T, got *uploadedSkillArchive) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/skills/import" {
			t.Errorf("request = %s %s, want POST /api/skills/import", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		got.onConflict = r.FormValue("on_conflict")
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		got.filename = header.Filename
		data, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("read upload: %v", err)
			return
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Errorf("upload is not a zip: %v", err)
			return
		}
		got.files = map[string]string{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Errorf("open %s: %v", f.Name, err)
				return
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Errorf("read %s: %v", f.Name, err)
				return
			}
			got.files[f.Name] = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "created",
			"skill":  map[string]any{"id": "skill-123", "name": "review-helper"},
		})
	}))
}

func setSkillImportTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	t.Setenv("MULTICA_SERVER_URL", serverURL)
}

func TestRunSkillImportDirUploadsFolderAsArchive(t *testing.T) {
	var got uploadedSkillArchive
	srv := newSkillArchiveUploadServer(t, &got)
	defer srv.Close()
	setSkillImportTestEnv(t, srv.URL)

	dir := filepath.Join(t.TempDir(), "review-helper")
	writeSkillDirFile(t, dir, "SKILL.md", []byte("---\nname: review-helper\n---\nReview carefully.\n"))
	writeSkillDirFile(t, dir, "scripts/run.sh", []byte("#!/bin/sh\necho run\n"))
	writeSkillDirFile(t, dir, "references/deep/notes.md", []byte("deep notes\n"))
	// Entries the server and the web folder import drop.
	writeSkillDirFile(t, dir, ".git/config", []byte("[core]\n"))
	writeSkillDirFile(t, dir, "scripts/.cache/state", []byte("cache\n"))
	writeSkillDirFile(t, dir, ".DS_Store", []byte("noise"))
	writeSkillDirFile(t, dir, "__MACOSX/._SKILL.md", []byte("noise"))
	writeSkillDirFile(t, dir, "LICENSE", []byte("MIT\n"))
	writeSkillDirFile(t, dir, "nested/SKILL.md", []byte("not a supporting file\n"))
	writeSkillDirFile(t, dir, "assets/logo.png", []byte{0x89, 'P', 'N', 'G'})
	writeSkillDirFile(t, dir, "big.txt", bytes.Repeat([]byte("x"), skillDirMaxFileSize+1))

	cmd := newSkillImportTestCmd()
	_ = cmd.Flags().Set("dir", dir)
	_ = cmd.Flags().Set("on-conflict", "rename")

	out, err := captureStdout(t, func() error { return runSkillImport(cmd, nil) })
	if err != nil {
		t.Fatalf("runSkillImport returned error: %v", err)
	}
	if !strings.Contains(out, `"created"`) {
		t.Fatalf("stdout = %q, want the structured import result", out)
	}

	if got.onConflict != "rename" {
		t.Fatalf("on_conflict = %q, want rename", got.onConflict)
	}
	if got.filename != "review-helper.skill" {
		t.Fatalf("upload filename = %q, want review-helper.skill", got.filename)
	}
	want := map[string]string{
		"review-helper/SKILL.md":                 "---\nname: review-helper\n---\nReview carefully.\n",
		"review-helper/scripts/run.sh":           "#!/bin/sh\necho run\n",
		"review-helper/references/deep/notes.md": "deep notes\n",
	}
	if len(got.files) != len(want) {
		t.Fatalf("archive entries = %v, want %v", sortedKeys(got.files), sortedKeys(want))
	}
	for name, content := range want {
		if got.files[name] != content {
			t.Fatalf("archive entry %q = %q, want %q (entries: %v)", name, got.files[name], content, sortedKeys(got.files))
		}
	}
}

func TestRunSkillImportDirFollowsLinksInsideTheFolder(t *testing.T) {
	var got uploadedSkillArchive
	srv := newSkillArchiveUploadServer(t, &got)
	defer srv.Close()
	setSkillImportTestEnv(t, srv.URL)

	dir := filepath.Join(t.TempDir(), "review-helper")
	writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
	writeSkillDirFile(t, dir, "references/notes.md", []byte("notes\n"))
	symlinkOrSkip(t, filepath.Join("references", "notes.md"), filepath.Join(dir, "notes-link.md"))

	cmd := newSkillImportTestCmd()
	_ = cmd.Flags().Set("dir", dir)
	if _, err := captureStdout(t, func() error { return runSkillImport(cmd, nil) }); err != nil {
		t.Fatalf("runSkillImport returned error: %v", err)
	}
	if got.files["review-helper/notes-link.md"] != "notes\n" {
		t.Fatalf("linked file content = %q, want the target's content (entries: %v)", got.files["review-helper/notes-link.md"], sortedKeys(got.files))
	}
}

func TestRunSkillImportDirFailsBeforeUpload(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		flags   map[string]string
		wantErr string
	}{
		{
			name:    "missing SKILL.md",
			setup:   func(t *testing.T, dir string) { writeSkillDirFile(t, dir, "notes.md", []byte("x")) },
			wantErr: "no SKILL.md",
		},
		{
			name: "SKILL.md only in a subfolder",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "inner/SKILL.md", []byte("x"))
			},
			wantErr: "no SKILL.md",
		},
		{
			name: "dir with file",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
			},
			flags:   map[string]string{"file": "other.skill"},
			wantErr: "mutually exclusive",
		},
		{
			name: "dir with url",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
			},
			flags:   map[string]string{"url": "https://skills.sh/acme/x"},
			wantErr: "mutually exclusive",
		},
		{
			name: "symlink outside the folder",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
				outside := filepath.Join(filepath.Dir(dir), "secret.txt")
				if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, outside, filepath.Join(dir, "secret.md"))
			},
			wantErr: "secret.md is a link that points outside the folder",
		},
		{
			name: "symlink to a directory",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
				writeSkillDirFile(t, dir, "real/a.md", []byte("a"))
				symlinkOrSkip(t, "real", filepath.Join(dir, "linked"))
			},
			wantErr: "linked",
		},
		{
			name: "SKILL.md over the per-file limit",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", bytes.Repeat([]byte("x"), skillDirMaxFileSize+1))
			},
			wantErr: "SKILL.md",
		},
		{
			name: "too many supporting files",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
				for i := 0; i <= skillDirMaxFileCount; i++ {
					writeSkillDirFile(t, dir, "refs/f-"+strconv.Itoa(i)+".md", []byte("x"))
				}
			},
			wantErr: "512",
		},
		{
			name: "supporting files over the total limit",
			setup: func(t *testing.T, dir string) {
				writeSkillDirFile(t, dir, "SKILL.md", []byte("x"))
				chunk := bytes.Repeat([]byte("x"), skillDirMaxFileSize)
				for i := 0; i < skillDirMaxTotalSize/skillDirMaxFileSize+1; i++ {
					writeSkillDirFile(t, dir, "refs/part-"+strconv.Itoa(i)+".md", chunk)
				}
			},
			wantErr: "8 MiB",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s: the import must fail before uploading", r.Method, r.URL.Path)
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}))
			defer srv.Close()
			setSkillImportTestEnv(t, srv.URL)

			dir := filepath.Join(t.TempDir(), "skill")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			tc.setup(t, dir)

			cmd := newSkillImportTestCmd()
			_ = cmd.Flags().Set("dir", dir)
			for name, value := range tc.flags {
				_ = cmd.Flags().Set(name, value)
			}
			_, err := captureStdout(t, func() error { return runSkillImport(cmd, nil) })
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("runSkillImport error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestRunSkillImportDirRejectsAFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	setSkillImportTestEnv(t, srv.URL)

	file := filepath.Join(t.TempDir(), "review-helper.skill")
	if err := os.WriteFile(file, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newSkillImportTestCmd()
	_ = cmd.Flags().Set("dir", file)
	_, err := captureStdout(t, func() error { return runSkillImport(cmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("runSkillImport error = %v, want a hint to use --file for an archive", err)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
