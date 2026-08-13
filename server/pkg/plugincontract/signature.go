package plugincontract

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrUnknownSigningKey = errors.New("plugin release signing key is not trusted")
	ErrInvalidSignature  = errors.New("plugin release signature is invalid")
)

// ReleaseEnvelope is the complete immutable identity signed by an official
// publisher. It binds the manifest to both the uploaded archive and the
// canonical extracted artifact.
type ReleaseEnvelope struct {
	PluginKey      string `json:"plugin_key"`
	Version        string `json:"version"`
	ManifestDigest string `json:"manifest_digest"`
	ArchiveDigest  string `json:"archive_digest"`
	ArtifactDigest string `json:"artifact_digest"`
}

func (e ReleaseEnvelope) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	content, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("canonicalize plugin release envelope: %w", err)
	}
	return content, nil
}

func (e ReleaseEnvelope) Validate() error {
	if err := validatePluginKey(e.PluginKey); err != nil {
		return err
	}
	if !semverPattern.MatchString(e.Version) {
		return fmt.Errorf("release envelope version is invalid")
	}
	for field, digest := range map[string]string{
		"manifest_digest": e.ManifestDigest,
		"archive_digest":  e.ArchiveDigest,
		"artifact_digest": e.ArtifactDigest,
	} {
		if !validSHA256Digest(digest) {
			return fmt.Errorf("release envelope %s is invalid", field)
		}
	}
	return nil
}

type ReleaseSigner interface {
	KeyID() string
	SignRelease(ReleaseEnvelope) ([]byte, error)
}

type ReleaseVerifier interface {
	VerifyRelease(keyID string, envelope ReleaseEnvelope, signature []byte) error
}

type Ed25519Signer struct {
	keyID      string
	privateKey ed25519.PrivateKey
}

func NewEd25519Signer(keyID string, privateKey ed25519.PrivateKey) (*Ed25519Signer, error) {
	if keyID == "" {
		return nil, fmt.Errorf("plugin release signing key id is empty")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("plugin release private key has invalid size")
	}
	return &Ed25519Signer{keyID: keyID, privateKey: append(ed25519.PrivateKey(nil), privateKey...)}, nil
}

func (s *Ed25519Signer) KeyID() string {
	return s.keyID
}

func (s *Ed25519Signer) SignRelease(envelope ReleaseEnvelope) ([]byte, error) {
	content, err := envelope.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(s.privateKey, content), nil
}

// Ed25519TrustStore supports overlapping public keys during release-key
// rotation. Private keys never enter the server trust store.
type Ed25519TrustStore struct {
	mu   sync.RWMutex
	keys map[string]ed25519.PublicKey
}

func NewEd25519TrustStore(keys map[string]ed25519.PublicKey) (*Ed25519TrustStore, error) {
	store := &Ed25519TrustStore{keys: make(map[string]ed25519.PublicKey, len(keys))}
	for keyID, publicKey := range keys {
		if err := store.AddKey(keyID, publicKey); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Ed25519TrustStore) AddKey(keyID string, publicKey ed25519.PublicKey) error {
	if keyID == "" {
		return fmt.Errorf("plugin release trust key id is empty")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("plugin release public key %q has invalid size", keyID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[keyID] = append(ed25519.PublicKey(nil), publicKey...)
	return nil
}

func (s *Ed25519TrustStore) RemoveKey(keyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, keyID)
}

func (s *Ed25519TrustStore) VerifyRelease(keyID string, envelope ReleaseEnvelope, signature []byte) error {
	content, err := envelope.CanonicalBytes()
	if err != nil {
		return err
	}
	s.mu.RLock()
	publicKey, ok := s.keys[keyID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownSigningKey, keyID)
	}
	if !ed25519.Verify(publicKey, content, signature) {
		return ErrInvalidSignature
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != len("sha256:")+sha256HexSize {
		return false
	}
	if value[:len("sha256:")] != "sha256:" {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

const sha256HexSize = 64
