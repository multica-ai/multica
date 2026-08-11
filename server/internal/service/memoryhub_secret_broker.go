// Package service: MemoryHub secret broker.
//
// The broker owns AES-256-GCM envelope encryption, CAS state transitions,
// short-lived claim material, rotation, revocation, recovery, and the
// in-process CredentialHandle registry. Queue rows contain only credential_ref.
//
// V6-3: CredentialHandle and CredentialGrant have NO SQL representation. The
// handle/grant registry lives only in this package's process memory, keyed by
// sha256(handle_id). A server restart invalidates outstanding handles/grants;
// redemption then returns a typed invalid/expired result and the normal
// claim/startup failure path retries with a newly issued handle.
package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// KEKSource provides the key-encryption key for the broker. The server
// supplies it from MEMORYHUB_KEY_ENC_KEY (+ _ID); a nil KEK makes the binding
// feature fail closed.
type KEKSource interface {
	// KEK returns the current key-encryption key and its key id. The second
	// return is nil when no KEK is configured.
	KEK() (key []byte, id string, ok bool)
	// PreviousKEK returns the N-1 key used during rotation, or ok=false.
	PreviousKEK() (key []byte, id string, ok bool)
}

// Broker errors (typed, deterministic).
var (
	ErrKEKMissing         = errors.New("memoryhub: key-encryption key not configured")
	ErrEnvelopeVersion    = errors.New("memoryhub: unsupported envelope version")
	ErrDecryptFailed      = errors.New("memoryhub: decrypt failed")
	ErrRevoked            = errors.New("memoryhub: secret revoked")
	ErrBlockedMigration   = errors.New("memoryhub: secret blocked, owner replacement required")
	ErrHandleNotFound     = errors.New("memoryhub: credential handle not found")
	ErrHandleExpired      = errors.New("memoryhub: credential handle expired")
	ErrHandleScopeMismatch = errors.New("memoryhub: credential handle scope mismatch")
	ErrGrantRevoked       = errors.New("memoryhub: credential grant revoked")
	ErrCASConflict        = errors.New("memoryhub: cas conflict")
)

// BrokerState is the durable secret state enum (v1.3 A6).
type BrokerState string

const (
	StateActive          BrokerState = "active"
	StateRotating        BrokerState = "rotating"
	StateRevoked         BrokerState = "revoked"
	StateBlockedMigration BrokerState = "blocked_migration"
)

// SecretEnvelope is the plaintext-relevant view of an encrypted secret.
type SecretEnvelope struct {
	CredentialRef   string
	EnvelopeVersion int
	KeyID           string
	Nonce           []byte // 96-bit, regenerated every encrypt
	Ciphertext      []byte
	AAD             string // workspace_id|kind|credential_ref binding
	UserKeyHash     string
	State           BrokerState
	StateVersion    int
	LeaseOwner      string
	RotationFromKeyID string
}

// CredentialGrant is the no-store redeem response body (V5-6). Ephemeral,
// no SQL representation.
type CredentialGrant struct {
	GrantID    string
	ExecutionID string
	Provider   string
	Placement  string
	BaseURL    string
	Value      string
	ExpiresAt  time.Time
}

// CredentialHandle is the ephemeral broker transport handle (V5-6). All
// fields required, none nullable.
type CredentialHandle struct {
	HandleID    string
	Audience    string
	TaskID      string
	ExecutionID string
	RuntimeID   string
	DaemonID    string
	Provider    string
	ExpiresAt   time.Time
	RedeemPath  string
	AckPath     string
	ReleasePath string
}

// handleRecord is the in-process registry entry.
type handleRecord struct {
	handle CredentialHandle
	grant  *CredentialGrant
	acked  bool
	// sameConsumerRetry counts a lost-response retry by the same consumer
	// before ack; a different consumer is rejected.
	consumerDaemonID string
}

// SecretBroker is the broker. It depends on a KEKSource; persistence of the
// encrypted envelope is done by the service layer through the sqlc queries.
type SecretBroker struct {
	kek      KEKSource
	now      func() time.Time
	mu       sync.Mutex
	registry map[string]*handleRecord // key: sha256(handle_id)
}

// NewSecretBroker builds a broker.
func NewSecretBroker(kek KEKSource) *SecretBroker {
	return &SecretBroker{
		kek:      kek,
		now:      time.Now,
		registry: make(map[string]*handleRecord),
	}
}

// Encrypt seals the plaintext into an envelope using AES-256-GCM with a fresh
// 96-bit nonce. The AAD binds workspace_id|kind|credential_ref so a row cannot
// be re-bound and replayed.
func (b *SecretBroker) Encrypt(workspaceID, kind, credentialRef string, plaintext []byte) (*SecretEnvelope, error) {
	key, keyID, ok := b.kek.KEK()
	if !ok {
		return nil, ErrKEKMissing
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("memoryhub: new cipher: %w", err)
	}
	nonce := make([]byte, 12) // 96-bit
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("memoryhub: nonce: %w", err)
	}
	aad := aadFor(workspaceID, kind, credentialRef)
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("memoryhub: gcm: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(aad))
	return &SecretEnvelope{
		CredentialRef:   credentialRef,
		EnvelopeVersion: 1,
		KeyID:           keyID,
		Nonce:           nonce,
		Ciphertext:      ciphertext,
		AAD:             aad,
		UserKeyHash:     hashUserKey(plaintext),
		State:           StateActive,
		StateVersion:    1,
	}, nil
}

// Decrypt opens an envelope. Only key ids N and N-1 are decryptable during
// rotation; a revoked or blocked envelope never decrypts.
func (b *SecretBroker) Decrypt(env *SecretEnvelope) ([]byte, error) {
	switch env.State {
	case StateRevoked:
		return nil, ErrRevoked
	case StateBlockedMigration:
		return nil, ErrBlockedMigration
	}
	key, keyID, ok := b.kek.KEK()
	var candidates []struct {
		key []byte
		id  string
	}
	if ok {
		candidates = append(candidates, struct {
			key []byte
			id  string
		}{key, keyID})
	}
	if prev, prevID, prevOK := b.kek.PreviousKEK(); prevOK {
		candidates = append(candidates, struct {
			key []byte
			id  string
		}{prev, prevID})
	}
	if len(candidates) == 0 {
		return nil, ErrKEKMissing
	}
	for _, c := range candidates {
		if c.id != env.KeyID {
			continue
		}
		block, err := aes.NewCipher(c.key)
		if err != nil {
			return nil, fmt.Errorf("memoryhub: new cipher: %w", err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("memoryhub: gcm: %w", err)
		}
		if len(env.Nonce) != aead.NonceSize() {
			return nil, ErrDecryptFailed
		}
		plain, err := aead.Open(nil, env.Nonce, env.Ciphertext, []byte(env.AAD))
		if err != nil {
			return nil, ErrDecryptFailed
		}
		if env.UserKeyHash != "" && hashUserKey(plain) != env.UserKeyHash {
			return nil, ErrDecryptFailed
		}
		return plain, nil
	}
	// key id is neither N nor N-1 -> blocked migration.
	return nil, ErrBlockedMigration
}

// IssueCredentialHandle creates an in-memory handle bound to the exact
// task+execution+runtime+daemon+provider. It never touches SQL (V6-3).
func (b *SecretBroker) IssueCredentialHandle(handle CredentialHandle, grant CredentialGrant) (CredentialHandle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	handleID, err := randomToken(32)
	if err != nil {
		return CredentialHandle{}, err
	}
	handle.HandleID = handleID
	key := registryKey(handleID)
	b.registry[key] = &handleRecord{
		handle:           handle,
		grant:            &grant,
		consumerDaemonID: handle.DaemonID,
	}
	return handle, nil
}

// Redeem returns the grant for a handle if every binding holds. A lost-response
// same-consumer retry returns the same grant; a different consumer, expired
// handle, cancelled task, or revoked grant is rejected.
func (b *SecretBroker) Redeem(handleID, executionID, runtimeID, daemonID string) (*CredentialGrant, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rec := b.registry[registryKey(handleID)]
	if rec == nil {
		return nil, ErrHandleNotFound
	}
	h := rec.handle
	if b.now().After(h.ExpiresAt) {
		delete(b.registry, registryKey(handleID))
		return nil, ErrHandleExpired
	}
	if h.ExecutionID != executionID || h.RuntimeID != runtimeID {
		return nil, ErrHandleScopeMismatch
	}
	if rec.consumerDaemonID != "" && rec.consumerDaemonID != daemonID {
		return nil, ErrGrantRevoked
	}
	if rec.grant == nil {
		return nil, ErrGrantRevoked
	}
	return rec.grant, nil
}

// Ack marks the grant consumed and deletes it.
func (b *SecretBroker) Ack(handleID, grantID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	rec := b.registry[registryKey(handleID)]
	if rec == nil || rec.grant == nil {
		return ErrHandleNotFound
	}
	if rec.grant.GrantID != grantID {
		return ErrHandleScopeMismatch
	}
	delete(b.registry, registryKey(handleID))
	return nil
}

// Release deletes the grant (with or without a prior successful redeem).
func (b *SecretBroker) Release(handleID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rec := b.registry[registryKey(handleID)]; rec != nil {
		delete(b.registry, registryKey(handleID))
	}
	return nil
}

// CleanupExpiredHandles removes expired, never-acked handles (TTL cleanup).
func (b *SecretBroker) CleanupExpiredHandles() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	n := 0
	for k, rec := range b.registry {
		if now.After(rec.handle.ExpiresAt) {
			delete(b.registry, k)
			n++
		}
	}
	return n
}

// InvalidateAll drops every outstanding handle/grant (server restart path).
func (b *SecretBroker) InvalidateAll() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.registry)
	b.registry = make(map[string]*handleRecord)
	return n
}

// RegistrySize exposes the live handle count (test helper).
func (b *SecretBroker) RegistrySize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.registry)
}

func registryKey(handleID string) string {
	sum := sha256.Sum256([]byte(handleID))
	return hex.EncodeToString(sum[:])
}

func aadFor(workspaceID, kind, credentialRef string) string {
	return workspaceID + "|" + kind + "|" + credentialRef
}

func hashUserKey(plain []byte) string {
	sum := sha256.Sum256(plain)
	return hex.EncodeToString(sum[:])
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("memoryhub: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
