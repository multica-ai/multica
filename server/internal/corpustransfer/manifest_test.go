package corpustransfer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeSourceFile(t *testing.T, root, rel, body string, mtime time.Time) string {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filename, []byte(body), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.Chtimes(filename, mtime, mtime); err != nil {
		t.Fatalf("set source mtime: %v", err)
	}
	return filename
}

func TestStageAndBuildManifestStagesRecentFilesAndMarksReplicas(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root := t.TempDir()
	writeSourceFile(t, root, "z/session.jsonl", "keep raw secret-looking text", now)
	writeSourceFile(t, root, "a/copy.jsonl", "keep raw secret-looking text", now.Add(-time.Minute))
	writeSourceFile(t, root, "old.jsonl", "too old", now.Add(-48*time.Hour))

	staging := filepath.Join(t.TempDir(), "staging")
	manifest, err := StageAndBuildManifest(context.Background(), []SourceRoot{
		{Type: "codex", Name: "desktop", Path: root},
		{Type: "automation", Name: "missing", Path: filepath.Join(root, "absent")},
	}, now.Add(-24*time.Hour), staging)
	if err != nil {
		t.Fatalf("StageAndBuildManifest: %v", err)
	}

	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.PackageID == "" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}
	if manifest.EntryCount != 2 || len(manifest.Entries) != 2 {
		t.Fatalf("entry count = %d/%d, want 2", manifest.EntryCount, len(manifest.Entries))
	}
	if manifest.TotalUncompressedBytes != int64(2*len("keep raw secret-looking text")) {
		t.Fatalf("total bytes = %d", manifest.TotalUncompressedBytes)
	}
	if len(manifest.MissingSources) != 1 || !strings.Contains(manifest.MissingSources[0], "missing") {
		t.Fatalf("missing sources = %#v", manifest.MissingSources)
	}
	if manifest.Entries[0].Path != "codex/desktop/a/copy.jsonl" || manifest.Entries[1].Path != "codex/desktop/z/session.jsonl" {
		t.Fatalf("entries not canonical: %#v", manifest.Entries)
	}
	if manifest.Entries[1].ReplicaOf != manifest.Entries[0].Path {
		t.Fatalf("replica_of = %q, want %q", manifest.Entries[1].ReplicaOf, manifest.Entries[0].Path)
	}
	if _, err := os.Stat(filepath.Join(staging, "codex", "desktop", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old file was staged: %v", err)
	}
	staged, err := os.ReadFile(filepath.Join(staging, "codex", "desktop", "z", "session.jsonl"))
	if err != nil || string(staged) != "keep raw secret-looking text" {
		t.Fatalf("staged raw content = %q, err=%v", staged, err)
	}
	if info, err := os.Stat(staging); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("staging mode = %v, err=%v", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(filepath.Join(staging, "codex", "desktop", "z", "session.jsonl")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("staged file mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestManifestCanonicalJSONSortsEntriesWithoutMutatingCaller(t *testing.T) {
	m := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		PackageID:     "pkg-1",
		CreatedAt:     time.Unix(1, 0).UTC(),
		Entries: []Entry{
			{Path: "z", SHA256: strings.Repeat("a", 64)},
			{Path: "a", SHA256: strings.Repeat("b", 64)},
		},
		EntryCount: 2,
	}
	body, err := m.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	var decoded Manifest
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode canonical manifest: %v", err)
	}
	if decoded.Entries[0].Path != "a" || decoded.Entries[1].Path != "z" {
		t.Fatalf("canonical order = %#v", decoded.Entries)
	}
	if m.Entries[0].Path != "z" {
		t.Fatal("CanonicalJSON mutated caller")
	}
}

func TestManifestValidateRejectsFalseReplicaClaim(t *testing.T) {
	tests := []Manifest{
		{
			SchemaVersion: ManifestSchemaVersion,
			PackageID:     "pkg-1",
			CreatedAt:     time.Now().UTC(),
			Entries: []Entry{
				{Path: "a", SHA256: strings.Repeat("a", 64), SizeBytes: 1},
				{Path: "b", SHA256: strings.Repeat("b", 64), SizeBytes: 1, ReplicaOf: "a"},
			},
			EntryCount:             2,
			TotalUncompressedBytes: 2,
		},
		{
			SchemaVersion:          ManifestSchemaVersion,
			PackageID:              "pkg-2",
			CreatedAt:              time.Now().UTC(),
			Entries:                []Entry{{Path: "a", SHA256: strings.Repeat("a", 64), SizeBytes: 1, ReplicaOf: "a"}},
			EntryCount:             1,
			TotalUncompressedBytes: 1,
		},
	}
	for _, m := range tests {
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "replica_of") {
			t.Fatalf("false replica error = %v", err)
		}
	}
}

func TestManifestValidateRejectsUnsafePackageID(t *testing.T) {
	for _, packageID := range []string{".", "..", "../../escape", `folder\\escape`, "bad\x00id"} {
		m := Manifest{SchemaVersion: ManifestSchemaVersion, PackageID: packageID, CreatedAt: time.Now().UTC()}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "package_id") {
			t.Fatalf("unsafe package_id %q error = %v", packageID, err)
		}
	}
}

func TestManifestValidateReservesEmbeddedManifestPath(t *testing.T) {
	for _, name := range []string{EmbeddedManifestName, "MANIFEST.JSON", "manifest.json"} {
		m := Manifest{
			SchemaVersion:          ManifestSchemaVersion,
			PackageID:              "pkg",
			CreatedAt:              time.Now().UTC(),
			Entries:                []Entry{{Path: name, SHA256: strings.Repeat("a", 64)}},
			EntryCount:             1,
			TotalUncompressedBytes: 0,
		}
		if err := m.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
			t.Fatalf("reserved path %q error = %v", name, err)
		}
	}
}

func TestCopySourceFileRejectsPathSwappedBeforeOpen(t *testing.T) {
	dir := t.TempDir()
	source := writeSourceFile(t, dir, "source", "first", time.Now().UTC().Truncate(time.Second))
	before, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	replacement := writeSourceFile(t, dir, "replacement", "other", before.ModTime())
	if err := os.Rename(replacement, source); err != nil {
		t.Fatal(err)
	}
	_, _, err = copySourceFile(source, filepath.Join(t.TempDir(), "staged"), before)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("path swap error = %v", err)
	}
}

func TestStageAndBuildManifestRejectsUnsafeNamesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "ok.jsonl", "ok", time.Now())
	if _, err := StageAndBuildManifest(context.Background(), []SourceRoot{{Type: "../codex", Name: "desktop", Path: root}}, time.Time{}, filepath.Join(t.TempDir(), "stage")); err == nil {
		t.Fatal("unsafe source type accepted")
	}

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	if err := os.Symlink(filepath.Join(root, "ok.jsonl"), filepath.Join(root, "linked.jsonl")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := StageAndBuildManifest(context.Background(), []SourceRoot{{Type: "codex", Name: "desktop", Path: root}}, time.Time{}, filepath.Join(t.TempDir(), "stage")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}
