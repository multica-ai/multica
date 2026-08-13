package plugincontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func testReleaseEnvelope() ReleaseEnvelope {
	return ReleaseEnvelope{
		PluginKey:      "ai.multica.software-delivery",
		Version:        "1.0.0",
		ManifestDigest: DigestBytes([]byte("manifest")),
		ArchiveDigest:  DigestBytes([]byte("archive")),
		ArtifactDigest: DigestBytes([]byte("artifact")),
	}
}

func TestEd25519ReleaseSignatureRoundTripAndTamper(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewEd25519Signer("release-2026-01", privateKey)
	if err != nil {
		t.Fatalf("NewEd25519Signer: %v", err)
	}
	trustStore, err := NewEd25519TrustStore(map[string]ed25519.PublicKey{
		signer.KeyID(): publicKey,
	})
	if err != nil {
		t.Fatalf("NewEd25519TrustStore: %v", err)
	}

	envelope := testReleaseEnvelope()
	signature, err := signer.SignRelease(envelope)
	if err != nil {
		t.Fatalf("SignRelease: %v", err)
	}
	if err := trustStore.VerifyRelease(signer.KeyID(), envelope, signature); err != nil {
		t.Fatalf("VerifyRelease: %v", err)
	}

	tampered := envelope
	tampered.ArtifactDigest = DigestBytes([]byte("tampered"))
	if err := trustStore.VerifyRelease(signer.KeyID(), tampered, signature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyRelease tampered error = %v", err)
	}
}

func TestEd25519TrustStoreSupportsKeyRotation(t *testing.T) {
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey old: %v", err)
	}
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey new: %v", err)
	}
	oldSigner, _ := NewEd25519Signer("old", oldPrivate)
	newSigner, _ := NewEd25519Signer("new", newPrivate)
	store, err := NewEd25519TrustStore(map[string]ed25519.PublicKey{"old": oldPublic})
	if err != nil {
		t.Fatalf("NewEd25519TrustStore: %v", err)
	}
	if err := store.AddKey("new", newPublic); err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	envelope := testReleaseEnvelope()
	oldSignature, _ := oldSigner.SignRelease(envelope)
	newSignature, _ := newSigner.SignRelease(envelope)
	if err := store.VerifyRelease("old", envelope, oldSignature); err != nil {
		t.Fatalf("verify old during overlap: %v", err)
	}
	if err := store.VerifyRelease("new", envelope, newSignature); err != nil {
		t.Fatalf("verify new during overlap: %v", err)
	}

	store.RemoveKey("old")
	if err := store.VerifyRelease("old", envelope, oldSignature); !errors.Is(err, ErrUnknownSigningKey) {
		t.Fatalf("verify removed key error = %v", err)
	}
	if err := store.VerifyRelease("new", envelope, newSignature); err != nil {
		t.Fatalf("verify new after rotation: %v", err)
	}
}
