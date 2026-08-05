package corpustransfer

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractVerifiedRejectsExcessRawArchiveEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("creates enough entries to exercise the raw archive limit")
	}
	archivePath := filepath.Join(t.TempDir(), "too-many.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for i := 0; i < MaxArchiveEntries+1; i++ {
		header := &zip.FileHeader{Name: fmt.Sprintf("f/%06d", i), Method: zip.Store}
		if _, err := zw.CreateHeader(header); err != nil {
			t.Fatalf("create entry %d: %v", i, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: ManifestSchemaVersion, PackageID: "pkg", CreatedAt: time.Now()}
	if err := ExtractVerified(archivePath, filepath.Join(t.TempDir(), "out"), manifest); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("ExtractVerified error = %v, want raw entry limit", err)
	}
}

func TestExtractVerifiedInstallsPackageOnlyAfterHashValidation(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "legacy.zip")
	writeTestZIP(t, archivePath, []zipTestEntry{{name: "sessions/a.jsonl", body: "alpha"}})
	manifest, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "codex-export", Name: "pipi"})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "received")
	if err := ExtractVerified(archivePath, destination, manifest); err != nil {
		t.Fatalf("ExtractVerified: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "sessions", "a.jsonl"))
	if err != nil || string(body) != "alpha" {
		t.Fatalf("installed body = %q, err=%v", body, err)
	}

	bad := manifest
	bad.Entries = append([]Entry(nil), manifest.Entries...)
	bad.Entries[0].SHA256 = strings.Repeat("0", 64)
	badDestination := filepath.Join(t.TempDir(), "bad")
	if err := ExtractVerified(archivePath, badDestination, bad); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("hash mismatch error = %v", err)
	}
	if _, err := os.Stat(badDestination); !os.IsNotExist(err) {
		t.Fatalf("bad destination was published: %v", err)
	}
}

func TestVerifyExtractedValidatesExistingInstallForACKRetry(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "legacy.zip")
	writeTestZIP(t, archivePath, []zipTestEntry{{name: "sessions/a.jsonl", body: "alpha"}})
	manifest, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "codex-export", Name: "pipi"})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "received")
	if err := ExtractVerified(archivePath, destination, manifest); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExtracted(destination, manifest); err != nil {
		t.Fatalf("VerifyExtracted valid install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "sessions", "a.jsonl"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExtracted(destination, manifest); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("VerifyExtracted tampered error = %v", err)
	}
}

func TestVerifyExtractedRejectsExtraFilesAndSymlinks(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "legacy.zip")
	writeTestZIP(t, archivePath, []zipTestEntry{{name: "sessions/a.jsonl", body: "alpha"}})
	manifest, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "codex-export", Name: "pipi"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		add  func(t *testing.T, destination string)
	}{
		{name: "extra file", add: func(t *testing.T, destination string) {
			if err := os.WriteFile(filepath.Join(destination, "extra"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", add: func(t *testing.T, destination string) {
			if err := os.Symlink("sessions/a.jsonl", filepath.Join(destination, "link")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "received")
			if err := ExtractVerified(archivePath, destination, manifest); err != nil {
				t.Fatal(err)
			}
			test.add(t, destination)
			if err := VerifyExtracted(destination, manifest); err == nil {
				t.Fatal("VerifyExtracted accepted an unexpected installed entry")
			}
		})
	}
}

func TestInspectAndExtractRejectUnsafeZIPEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipTestEntry
		want    string
	}{
		{name: "traversal", entries: []zipTestEntry{{name: "../escape", body: "x"}}, want: "unsafe"},
		{name: "absolute", entries: []zipTestEntry{{name: "/escape", body: "x"}}, want: "unsafe"},
		{name: "backslash", entries: []zipTestEntry{{name: `dir\\escape`, body: "x"}}, want: "unsafe"},
		{name: "drive", entries: []zipTestEntry{{name: "C:/escape", body: "x"}}, want: "unsafe"},
		{name: "nul", entries: []zipTestEntry{{name: "bad\x00name", body: "x"}}, want: "unsafe"},
		{name: "duplicate", entries: []zipTestEntry{{name: "a", body: "x"}, {name: "a", body: "y"}}, want: "duplicate"},
		{name: "case fold", entries: []zipTestEntry{{name: "A.txt", body: "x"}, {name: "a.txt", body: "y"}}, want: "collision"},
		{name: "unicode fold", entries: []zipTestEntry{{name: "e\u0301.txt", body: "x"}, {name: "é.txt", body: "y"}}, want: "collision"},
		{name: "symlink", entries: []zipTestEntry{{name: "link", body: "target", mode: os.ModeSymlink | 0o777}}, want: "regular"},
		{name: "special", entries: []zipTestEntry{{name: "pipe", body: "x", mode: os.ModeNamedPipe | 0o600}}, want: "regular"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "unsafe.zip")
			writeTestZIP(t, archivePath, tt.entries)
			_, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "test", Name: "unsafe"})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("InspectZIP error = %v, want %q", err, tt.want)
			}
			empty := Manifest{SchemaVersion: ManifestSchemaVersion, PackageID: "pkg", CreatedAt: time.Now()}
			err = ExtractVerified(archivePath, filepath.Join(t.TempDir(), "out"), empty)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("ExtractVerified error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestExtractVerifiedRejectsDuplicateAndSpecialManifestEntriesDirectly(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipTestEntry
		want    string
	}{
		{name: "duplicate embedded manifest", entries: []zipTestEntry{{name: EmbeddedManifestName, body: "{}"}, {name: EmbeddedManifestName, body: "{}"}}, want: "duplicate"},
		{name: "special embedded manifest", entries: []zipTestEntry{{name: EmbeddedManifestName, body: "{}", mode: os.ModeSymlink | 0o777}}, want: "regular"},
		{name: "duplicate directory", entries: []zipTestEntry{{name: "dir/", mode: os.ModeDir | 0o700}, {name: "dir/", mode: os.ModeDir | 0o700}}, want: "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "bad.zip")
			writeTestZIP(t, archivePath, tt.entries)
			manifest := Manifest{SchemaVersion: ManifestSchemaVersion, PackageID: "pkg", CreatedAt: time.Now(), Entries: []Entry{}, EntryCount: 0}
			err := ExtractVerified(archivePath, filepath.Join(t.TempDir(), "out"), manifest)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("ExtractVerified error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestExtractVerifiedRejectsManifestExpansionAndExistingDestination(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "legacy.zip")
	writeTestZIP(t, archivePath, []zipTestEntry{{name: "a", body: "alpha"}})
	manifest, _, err := InspectZIP(archivePath, SourceInfo{Adapter: "zip", Type: "test", Name: "x"})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	manifest.TotalUncompressedBytes--
	if err := ExtractVerified(archivePath, filepath.Join(t.TempDir(), "expanded"), manifest); err == nil || !strings.Contains(err.Error(), "total") {
		t.Fatalf("expansion error = %v", err)
	}

	destination := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest.TotalUncompressedBytes++
	manifest.CreatedAt = time.Now()
	if err := ExtractVerified(archivePath, destination, manifest); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("existing destination error = %v", err)
	}
}
