package plugincontract

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePackFixture(t *testing.T, root string) {
	t.Helper()
	manifest := manifestJSON(t, validManifest())
	if err := os.WriteFile(filepath.Join(root, ManifestFilename), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root, "skills", "review-readiness")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review-readiness\n---\nReview the evidence.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackDirectoryIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writePackFixture(t, root)

	firstArchive, firstArtifact, err := PackDirectory(root)
	if err != nil {
		t.Fatalf("PackDirectory first: %v", err)
	}
	if err := os.Chtimes(filepath.Join(root, ManifestFilename), deterministicZipTime.Add(10), deterministicZipTime.Add(20)); err != nil {
		t.Fatal(err)
	}
	secondArchive, secondArtifact, err := PackDirectory(root)
	if err != nil {
		t.Fatalf("PackDirectory second: %v", err)
	}
	if !bytes.Equal(firstArchive, secondArchive) {
		t.Fatal("archive bytes changed across identical packs")
	}
	if firstArtifact.ArchiveDigest != secondArtifact.ArchiveDigest || firstArtifact.ArtifactDigest != secondArtifact.ArtifactDigest {
		t.Fatalf("pack digests changed: first=%+v second=%+v", firstArtifact, secondArtifact)
	}
}

func TestPackDirectoryRejectsExecutableFile(t *testing.T) {
	root := t.TempDir()
	writePackFixture(t, root)
	path := filepath.Join(root, "skills", "review-readiness", "run.sh")
	if err := os.WriteFile(path, []byte("echo no\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := PackDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("PackDirectory error = %v, want executable rejection", err)
	}
}

func TestPackDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writePackFixture(t, root)
	link := filepath.Join(root, "skills", "review-readiness", "link")
	if err := os.Symlink("SKILL.md", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := PackDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("PackDirectory error = %v, want symlink rejection", err)
	}
}
