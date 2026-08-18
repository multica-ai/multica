package plugincontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func TestValidateReleaseCandidateRequiresAndVerifiesBundledSignature(t *testing.T) {
	archive := buildArchive(t, referenceArchiveFiles(t))
	unsigned := ReleaseCandidate{
		Archive:    archive,
		SourceKind: SourceBundled,
		SourceRef:  "embedded://ai.multica.software-delivery/1.0.0",
	}
	if _, err := ValidateReleaseCandidate(unsigned, nil); err == nil {
		t.Fatal("unsigned bundled release was accepted")
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, _ := NewEd25519Signer("release-2026-01", privateKey)
	store, _ := NewEd25519TrustStore(map[string]ed25519.PublicKey{"release-2026-01": publicKey})
	artifact, err := ValidateArtifact(archive)
	if err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
	envelope := ReleaseEnvelope{
		PluginKey:      artifact.Manifest.Metadata.Key,
		Version:        artifact.Manifest.Metadata.Version,
		ManifestDigest: DigestBytes(artifact.CanonicalManifest),
		ArchiveDigest:  artifact.ArchiveDigest,
		ArtifactDigest: artifact.ArtifactDigest,
	}
	signature, err := signer.SignRelease(envelope)
	if err != nil {
		t.Fatalf("SignRelease: %v", err)
	}

	validated, err := ValidateReleaseCandidate(ReleaseCandidate{
		Archive:        archive,
		SourceKind:     SourceBundled,
		SourceRef:      unsigned.SourceRef,
		SignatureKeyID: signer.KeyID(),
		Signature:      signature,
	}, store)
	if err != nil {
		t.Fatalf("ValidateReleaseCandidate: %v", err)
	}
	if len(validated.Contributions) != 1 {
		t.Fatalf("contributions = %d, want 1", len(validated.Contributions))
	}
	if len(validated.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(validated.Files))
	}
	contribution := validated.Contributions[0]
	if contribution.Type != ContributionAgentSkillV1 || contribution.EntryDigest == "" {
		t.Fatalf("validated contribution = %#v", contribution)
	}

	tampered := append([]byte(nil), signature...)
	tampered[0] ^= 0xff
	_, err = ValidateReleaseCandidate(ReleaseCandidate{
		Archive:        archive,
		SourceKind:     SourceBundled,
		SourceRef:      unsigned.SourceRef,
		SignatureKeyID: signer.KeyID(),
		Signature:      tampered,
	}, store)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered signature error = %v", err)
	}
}

func TestValidateReleaseCandidateAllowsUnsignedPrivateDev(t *testing.T) {
	validated, err := ValidateReleaseCandidate(ReleaseCandidate{
		Archive:    buildArchive(t, referenceArchiveFiles(t)),
		SourceKind: SourcePrivateDev,
		SourceRef:  "local://software-delivery",
	}, nil)
	if err != nil {
		t.Fatalf("ValidateReleaseCandidate: %v", err)
	}
	if validated.SourceKind != SourcePrivateDev || validated.SignatureKeyID != "" {
		t.Fatalf("validated private-dev release = %#v", validated)
	}
}

func TestValidateReleaseCandidateCompilesRemoteMCPAdapter(t *testing.T) {
	archive := buildArchive(t, []zipFixtureFile{{name: ManifestFilename, content: manifestJSON(t, validRemoteMCPManifest())}})
	validated, err := ValidateReleaseCandidate(ReleaseCandidate{
		Archive: archive, SourceKind: SourcePrivateDev, SourceRef: "private://fixture",
	}, nil)
	if err != nil {
		t.Fatalf("ValidateReleaseCandidate: %v", err)
	}
	if len(validated.Contributions) != 1 {
		t.Fatalf("contributions = %d, want 1", len(validated.Contributions))
	}
	got := validated.Contributions[0]
	if got.Type != ContributionRemoteMCPV1 || got.EntryPath != ManifestFilename || got.EntryDigest != validated.ManifestDigest {
		t.Fatalf("remote MCP contribution = %#v", got)
	}
}
