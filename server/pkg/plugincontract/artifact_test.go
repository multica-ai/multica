package plugincontract

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

type zipFixtureFile struct {
	name    string
	content []byte
	mode    fs.FileMode
}

func buildArchive(t *testing.T, files []zipFixtureFile) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		mode := file.mode
		if mode == 0 {
			mode = 0o600
		}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", file.name, err)
		}
		if _, err := entry.Write(file.content); err != nil {
			t.Fatalf("write zip entry %s: %v", file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func referenceArchiveFiles(t *testing.T) []zipFixtureFile {
	t.Helper()
	return []zipFixtureFile{
		{name: ManifestFilename, content: manifestJSON(t, validManifest())},
		{name: "skills/review-readiness/SKILL.md", content: []byte("---\nname: review-readiness\n---\nReview the evidence.\n")},
		{name: "skills/review-readiness/references/checklist.md", content: []byte("- Tests\n- Rollback\n")},
	}
}

func TestValidateArtifactAcceptsReferencePlugin(t *testing.T) {
	archive := buildArchive(t, referenceArchiveFiles(t))
	artifact, err := ValidateArtifact(archive)
	if err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
	if artifact.Manifest.Metadata.Key != "ai.multica.software-delivery" {
		t.Fatalf("plugin key = %q", artifact.Manifest.Metadata.Key)
	}
	if !validSHA256Digest(artifact.ArchiveDigest) || !validSHA256Digest(artifact.ArtifactDigest) {
		t.Fatalf("invalid digests: archive=%q artifact=%q", artifact.ArchiveDigest, artifact.ArtifactDigest)
	}
	if len(artifact.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(artifact.Files))
	}
}

func TestValidateArtifactAcceptsRemoteMCPOnlyPlugin(t *testing.T) {
	archive := buildArchive(t, []zipFixtureFile{{
		name: ManifestFilename, content: manifestJSON(t, validRemoteMCPManifest()),
	}})
	artifact, err := ValidateArtifact(archive)
	if err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
	if len(artifact.Files) != 1 || len(artifact.Manifest.Contributes.RemoteMCP) != 1 {
		t.Fatalf("remote-only artifact files = %d, contributions = %d", len(artifact.Files), len(artifact.Manifest.Contributes.RemoteMCP))
	}
}

func TestValidateArtifactAcceptsThirdPartyLicenseFiles(t *testing.T) {
	files := append(referenceArchiveFiles(t), zipFixtureFile{
		name:    ThirdPartyLicensesRoot + "dependency-MIT.txt",
		content: []byte("MIT License\n\nCopyright dependency author\n"),
	})
	artifact, err := ValidateArtifact(buildArchive(t, files))
	if err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
	if len(artifact.Files) != 4 {
		t.Fatalf("files = %d, want 4", len(artifact.Files))
	}
}

func TestArtifactDigestIgnoresZipEntryOrder(t *testing.T) {
	files := referenceArchiveFiles(t)
	reversed := []zipFixtureFile{files[2], files[1], files[0]}

	first, err := ValidateArtifact(buildArchive(t, files))
	if err != nil {
		t.Fatalf("ValidateArtifact first: %v", err)
	}
	second, err := ValidateArtifact(buildArchive(t, reversed))
	if err != nil {
		t.Fatalf("ValidateArtifact second: %v", err)
	}
	if first.ArtifactDigest != second.ArtifactDigest {
		t.Fatalf("artifact digest depends on zip order: %s != %s", first.ArtifactDigest, second.ArtifactDigest)
	}
	if first.ArchiveDigest == second.ArchiveDigest {
		t.Fatal("raw archive digests unexpectedly match across zip order")
	}
}

func TestValidateArtifactRejectsUnsafeOrUnownedFiles(t *testing.T) {
	tests := []struct {
		name    string
		file    zipFixtureFile
		wantErr string
	}{
		{name: "traversal", file: zipFixtureFile{name: "../escape.md", content: []byte("no")}, wantErr: "unsafe"},
		{name: "backslash", file: zipFixtureFile{name: `skills\escape.md`, content: []byte("no")}, wantErr: "unsafe"},
		{name: "unowned", file: zipFixtureFile{name: "README.md", content: []byte("no")}, wantErr: "outside a declared contribution"},
		{name: "primary content descendant", file: zipFixtureFile{name: "skills/review-readiness/SKILL.md/hidden.txt", content: []byte("no")}, wantErr: "reserved primary Skill content"},
		{name: "executable", file: zipFixtureFile{name: "skills/review-readiness/run.sh", content: []byte("echo no"), mode: 0o700}, wantErr: "executable"},
		{name: "symlink", file: zipFixtureFile{name: "skills/review-readiness/link", content: []byte("target"), mode: fs.ModeSymlink | 0o777}, wantErr: "not a regular file"},
		{name: "binary", file: zipFixtureFile{name: "skills/review-readiness/binary", content: []byte{0xff, 0xfe}}, wantErr: "not UTF-8 text"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := append(referenceArchiveFiles(t), test.file)
			_, err := ValidateArtifact(buildArchive(t, files))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateArtifact error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateArtifactRejectsCaseFoldedCollision(t *testing.T) {
	files := append(referenceArchiveFiles(t), zipFixtureFile{
		name:    "skills/review-readiness/skill.md",
		content: []byte("collision"),
	})
	_, err := ValidateArtifact(buildArchive(t, files))
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("ValidateArtifact collision error = %v", err)
	}
}

func TestValidateArtifactRejectsMissingDeclaredEntry(t *testing.T) {
	files := referenceArchiveFiles(t)
	files = []zipFixtureFile{files[0], files[2]}
	_, err := ValidateArtifact(buildArchive(t, files))
	if err == nil || !strings.Contains(err.Error(), "entry") {
		t.Fatalf("ValidateArtifact missing entry error = %v", err)
	}
}

func TestValidateArtifactRejectsInstallAndBuildHooks(t *testing.T) {
	for _, name := range []string{"package.json", "install.sh", "postinstall", "build", "build.ps1", "pnpm-lock.yaml"} {
		t.Run(name, func(t *testing.T) {
			files := append(referenceArchiveFiles(t), zipFixtureFile{
				name:    "skills/review-readiness/" + name,
				content: []byte("declarative-looking but forbidden"),
			})
			_, err := ValidateArtifact(buildArchive(t, files))
			if err == nil || !strings.Contains(err.Error(), "installation or build hook") {
				t.Fatalf("ValidateArtifact error = %v, want install/build hook rejection", err)
			}
		})
	}
}
