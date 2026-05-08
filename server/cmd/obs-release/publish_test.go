package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestPublishBacksUpExistingPrefixThenUploadsArtifacts(t *testing.T) {
	sourceDir := createArtifacts(t)
	store := &fakeStore{
		existing: []RemoteObject{
			{Key: "cli/manifest.json", Size: 10},
			{Key: "cli/releases/old.zip", Size: 20},
		},
	}

	result, err := Publish(context.Background(), store, PublishOptions{
		SourceDir:   sourceDir,
		Bucket:      "multica",
		Prefix:      "cli",
		Concurrency: 1,
		Timestamp:   time.Date(2026, 5, 8, 3, 4, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish error = %v", err)
	}

	if result.BackupPrefix != "cli_bak_20260508030405/" {
		t.Fatalf("BackupPrefix = %q", result.BackupPrefix)
	}
	if result.BackupCount != 2 {
		t.Fatalf("BackupCount = %d, want 2", result.BackupCount)
	}
	if result.Uploaded != 3 {
		t.Fatalf("Uploaded = %d, want 3", result.Uploaded)
	}

	wantCopies := [][2]string{
		{"cli/manifest.json", "cli_bak_20260508030405/manifest.json"},
		{"cli/releases/old.zip", "cli_bak_20260508030405/releases/old.zip"},
	}
	if !reflect.DeepEqual(store.copies, wantCopies) {
		t.Fatalf("copies = %#v, want %#v", store.copies, wantCopies)
	}

	wantDeletes := [][]string{{"cli/manifest.json", "cli/releases/old.zip"}}
	if !reflect.DeepEqual(store.deletes, wantDeletes) {
		t.Fatalf("deletes = %#v, want %#v", store.deletes, wantDeletes)
	}

	uploadedKeys := make([]string, 0, len(store.uploads))
	contentTypes := map[string]string{}
	for _, upload := range store.uploads {
		uploadedKeys = append(uploadedKeys, upload.key)
		contentTypes[upload.key] = upload.contentType
	}
	sort.Strings(uploadedKeys)
	wantUploadedKeys := []string{
		"cli/manifest.json",
		"cli/releases/checksums.txt",
		"cli/releases/multica-cli-1.2.3-linux-amd64.tar.gz",
	}
	if !reflect.DeepEqual(uploadedKeys, wantUploadedKeys) {
		t.Fatalf("uploaded keys = %#v, want %#v", uploadedKeys, wantUploadedKeys)
	}
	if contentTypes["cli/manifest.json"] != "application/json" {
		t.Fatalf("manifest content type = %q", contentTypes["cli/manifest.json"])
	}
	if contentTypes["cli/releases/checksums.txt"] != "text/plain; charset=utf-8" {
		t.Fatalf("checksums content type = %q", contentTypes["cli/releases/checksums.txt"])
	}
	if contentTypes["cli/releases/multica-cli-1.2.3-linux-amd64.tar.gz"] != "application/octet-stream" {
		t.Fatalf("archive content type = %q", contentTypes["cli/releases/multica-cli-1.2.3-linux-amd64.tar.gz"])
	}
}

func TestPublishWithoutExistingPrefixOnlyUploads(t *testing.T) {
	sourceDir := createArtifacts(t)
	store := &fakeStore{}

	result, err := Publish(context.Background(), store, PublishOptions{
		SourceDir:   sourceDir,
		Bucket:      "multica",
		Prefix:      "/cli/",
		Concurrency: 2,
		Timestamp:   time.Date(2026, 5, 8, 3, 4, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish error = %v", err)
	}

	if result.BackupPrefix != "" || result.BackupCount != 0 {
		t.Fatalf("unexpected backup result: %#v", result)
	}
	if len(store.copies) != 0 {
		t.Fatalf("copies = %#v, want none", store.copies)
	}
	if len(store.deletes) != 0 {
		t.Fatalf("deletes = %#v, want none", store.deletes)
	}
	if len(store.uploads) != 3 {
		t.Fatalf("uploads = %d, want 3", len(store.uploads))
	}
}

func TestCollectArtifactFilesRequiresManifestChecksumsAndArchive(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := collectArtifactFiles(dir); err == nil {
			t.Fatal("collectArtifactFiles error = nil, want error")
		}
	})

	t.Run("missing archive", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "manifest.json"), "{}")
		writeFile(t, filepath.Join(dir, "releases", "checksums.txt"), "abc  file\n")
		if _, err := collectArtifactFiles(dir); err == nil {
			t.Fatal("collectArtifactFiles error = nil, want error")
		}
	})
}

func TestBackupKeyRejectsObjectsOutsidePrefix(t *testing.T) {
	if _, err := backupKey("cli", "cli_bak_20260508030405/", "other/file.txt"); err == nil {
		t.Fatal("backupKey error = nil, want error")
	}
}

func createArtifacts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.json"), `{"version":"v1.2.3"}`)
	writeFile(t, filepath.Join(dir, "releases", "checksums.txt"), "abc  multica-cli-1.2.3-linux-amd64.tar.gz\n")
	writeFile(t, filepath.Join(dir, "releases", "multica-cli-1.2.3-linux-amd64.tar.gz"), "archive")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

type fakeStore struct {
	existing []RemoteObject
	copies   [][2]string
	deletes  [][]string
	uploads  []fakeUpload
}

type fakeUpload struct {
	key         string
	path        string
	contentType string
}

func (s *fakeStore) ListObjects(_ context.Context, _, _ string) ([]RemoteObject, error) {
	return append([]RemoteObject(nil), s.existing...), nil
}

func (s *fakeStore) CopyObject(_ context.Context, _ string, sourceKey, destKey string) error {
	s.copies = append(s.copies, [2]string{sourceKey, destKey})
	return nil
}

func (s *fakeStore) DeleteObjects(_ context.Context, _ string, keys []string) error {
	s.deletes = append(s.deletes, append([]string(nil), keys...))
	return nil
}

func (s *fakeStore) PutFile(_ context.Context, _, key, path, contentType string) error {
	s.uploads = append(s.uploads, fakeUpload{key: key, path: path, contentType: contentType})
	return nil
}
