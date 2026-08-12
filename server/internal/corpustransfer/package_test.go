package corpustransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildZIPEmbedsManifestAndInspectZIPVerifiesEntries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root := t.TempDir()
	writeSourceFile(t, root, "session.jsonl", "one", now)
	writeSourceFile(t, root, "nested/events.jsonl", "two", now)
	staging := filepath.Join(t.TempDir(), "staging")
	manifest, err := StageAndBuildManifest(context.Background(), []SourceRoot{{Type: "codex", Name: "desktop", Path: root}}, time.Time{}, staging)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "package.zip")
	envelope, err := BuildZIP(staging, archivePath, manifest)
	if err != nil {
		t.Fatalf("BuildZIP: %v", err)
	}
	if envelope.Format != "zip" || envelope.SizeBytes <= 0 || len(envelope.SHA256) != 64 || len(envelope.ManifestSHA256) != 64 {
		t.Fatalf("bad envelope: %#v", envelope)
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open built zip: %v", err)
	}
	defer zr.Close()
	foundManifest := false
	for _, f := range zr.File {
		if f.Name == EmbeddedManifestName {
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Fatal("archive has no embedded manifest")
	}

	inspected, inspectedEnvelope, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "codex-export", Name: "legacy"})
	if err != nil {
		t.Fatalf("InspectZIP: %v", err)
	}
	if inspected.PackageID != manifest.PackageID || inspected.EntryCount != manifest.EntryCount {
		t.Fatalf("inspected manifest = %#v, want package %s/%d entries", inspected, manifest.PackageID, manifest.EntryCount)
	}
	if inspectedEnvelope.SHA256 != envelope.SHA256 || inspectedEnvelope.SizeBytes != envelope.SizeBytes {
		t.Fatalf("inspected envelope = %#v, want %#v", inspectedEnvelope, envelope)
	}
}

func TestBuildZIPRejectsStagedContentChangedWithoutSizeChange(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root := t.TempDir()
	writeSourceFile(t, root, "session.jsonl", "one", now)
	staging := filepath.Join(t.TempDir(), "staging")
	manifest, err := StageAndBuildManifest(context.Background(), []SourceRoot{{Type: "codex", Name: "desktop", Path: root}}, time.Time{}, staging)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(manifest.Entries[0].Path)), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = BuildZIP(staging, filepath.Join(t.TempDir(), "package.zip"), manifest)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sha256") {
		t.Fatalf("BuildZIP error = %v, want sha256 mismatch", err)
	}
}

func TestInspectZIPBuildsManifestForExistingArchive(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "legacy.zip")
	writeTestZIP(t, archivePath, []zipTestEntry{
		{name: "sessions/", mode: os.ModeDir | 0o700},
		{name: "sessions/a.jsonl", body: "alpha"},
		{name: "sessions/b.jsonl", body: "alpha"},
	})

	manifest, envelope, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "codex-export", Name: "pipi"})
	if err != nil {
		t.Fatalf("InspectZIP: %v", err)
	}
	if manifest.EntryCount != 2 || manifest.Entries[1].ReplicaOf != manifest.Entries[0].Path {
		t.Fatalf("legacy manifest = %#v", manifest)
	}
	if envelope.Filename != "legacy.zip" || envelope.SizeBytes <= 0 || len(envelope.SHA256) != 64 {
		t.Fatalf("legacy envelope = %#v", envelope)
	}
	second, secondEnvelope, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "codex-export", Name: "pipi"})
	if err != nil {
		t.Fatalf("second InspectZIP: %v", err)
	}
	firstJSON, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := second.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) || envelope.ManifestSHA256 != secondEnvelope.ManifestSHA256 {
		t.Fatalf("existing ZIP inspection is not stable across retries:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestInspectZIPRejectsEmptyEmbeddedManifest(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "empty-manifest.zip")
	writeTestZIP(t, archivePath, []zipTestEntry{{name: EmbeddedManifestName}})
	if _, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "test", Name: "empty"}); err == nil || !strings.Contains(err.Error(), "decode embedded manifest") {
		t.Fatalf("InspectZIP error = %v, want embedded manifest decode failure", err)
	}
}

func TestGoZipWriterEmitsZip64ForLargeEntryCount(t *testing.T) {
	if testing.Short() {
		t.Skip("creates enough entries to require ZIP64")
	}
	archivePath := filepath.Join(t.TempDir(), "zip64.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for i := 0; i < 65_536; i++ {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("e/%05d", i), Method: zip.Store})
		if err != nil {
			t.Fatalf("create entry %d: %v", i, err)
		}
		if _, err := w.Write(nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte{'P', 'K', 6, 6}) || !bytes.Contains(body, []byte{'P', 'K', 6, 7}) {
		t.Fatal("ZIP64 end records are absent")
	}
	if count := binary.LittleEndian.Uint16(body[len(body)-10 : len(body)-8]); count != 0xffff {
		t.Fatalf("classic EOCD count = %#x, want saturated 0xffff", count)
	}
	manifest, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "test", Name: "zip64"})
	if err != nil {
		t.Fatalf("InspectZIP Zip64: %v", err)
	}
	if manifest.EntryCount != 65_536 {
		t.Fatalf("Zip64 entry count = %d", manifest.EntryCount)
	}
}

type zipTestEntry struct {
	name string
	body string
	mode os.FileMode
}

func writeTestZIP(t *testing.T, filename string, entries []zipTestEntry) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}
