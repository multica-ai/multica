package service

import (
	"errors"
	"testing"
	"time"
)

// fakeKEK is a test KEK source.
type fakeKEK struct {
	key []byte
	id  string
	prevKey []byte
	prevID  string
	ok      bool
	prevOK  bool
}

func (f *fakeKEK) KEK() ([]byte, string, bool) {
	return f.key, f.id, f.ok
}
func (f *fakeKEK) PreviousKEK() ([]byte, string, bool) {
	return f.prevKey, f.prevID, f.prevOK
}

func newTestBroker() (*SecretBroker, *fakeKEK) {
	kek := &fakeKEK{key: []byte("0123456789abcdef0123456789abcdef"), id: "kek-1", ok: true}
	return NewSecretBroker(kek), kek
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	b, _ := newTestBroker()
	env, err := b.Encrypt("ws-1", "user_key", "cr-1", []byte("tdai_secret_value"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if env.UserKeyHash == "" || env.Nonce == nil || env.Ciphertext == nil {
		t.Fatal("envelope missing fields")
	}
	plain, err := b.Decrypt(env)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "tdai_secret_value" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestDecryptFailsWithoutKEK(t *testing.T) {
	b, _ := newTestBroker()
	env, err := b.Encrypt("ws-1", "user_key", "cr-1", []byte("x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// No KEK configured -> fail closed, no plaintext fallback.
	b.kek = &fakeKEK{ok: false}
	if _, err := b.Decrypt(env); !errors.Is(err, ErrKEKMissing) {
		t.Fatalf("err = %v, want ErrKEKMissing", err)
	}
	// Encrypt also fails closed.
	if _, err := b.Encrypt("ws-1", "user_key", "cr-2", []byte("y")); !errors.Is(err, ErrKEKMissing) {
		t.Fatalf("encrypt err = %v, want ErrKEKMissing", err)
	}
}

func TestRotationNNMinusOne(t *testing.T) {
	b, kek := newTestBroker()
	env, _ := b.Encrypt("ws-1", "user_key", "cr-1", []byte("secret"))
	// rotate: new key N becomes current, old key N-1
	kek.prevKey = kek.key
	kek.prevID = kek.id
	kek.prevOK = true
	kek.key = []byte("abcdef0123456789abcdef0123456789")
	kek.id = "kek-2"
	// N-1 still decrypts
	plain, err := b.Decrypt(env)
	if err != nil {
		t.Fatalf("N-1 decrypt: %v", err)
	}
	if string(plain) != "secret" {
		t.Fatalf("plain = %q", plain)
	}
	// A third-old key is rejected -> blocked migration
	kek.prevKey = []byte("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	kek.prevID = "kek-0"
	kek.prevOK = true
	if _, err := b.Decrypt(env); !errors.Is(err, ErrBlockedMigration) {
		t.Fatalf("err = %v, want ErrBlockedMigration", err)
	}
}

func TestRevokedNeverDecrypts(t *testing.T) {
	b, _ := newTestBroker()
	env, _ := b.Encrypt("ws-1", "user_key", "cr-1", []byte("secret"))
	env.State = StateRevoked
	if _, err := b.Decrypt(env); !errors.Is(err, ErrRevoked) {
		t.Fatalf("err = %v, want ErrRevoked", err)
	}
}

func TestTamperedAADFails(t *testing.T) {
	b, _ := newTestBroker()
	env, _ := b.Encrypt("ws-1", "user_key", "cr-1", []byte("secret"))
	env.AAD = "ws-2|user_key|cr-1" // re-bound to another workspace
	if _, err := b.Decrypt(env); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("err = %v, want ErrDecryptFailed", err)
	}
}

func TestHandleIssueRedeemAckRelease(t *testing.T) {
	b, _ := newTestBroker()
	now := time.Now()
	handle := CredentialHandle{
		Audience:    "memoryhub-provider-credential",
		TaskID:      "task-1",
		ExecutionID: "exec-1",
		RuntimeID:   "runtime-1",
		DaemonID:    "daemon-1",
		Provider:    "codex",
		ExpiresAt:   now.Add(time.Minute),
		RedeemPath:  "/api/daemon/tasks/task-1/memoryhub-credential/redeem",
		AckPath:     "/api/daemon/tasks/task-1/memoryhub-credential/ack",
		ReleasePath: "/api/daemon/tasks/task-1/memoryhub-credential/release",
	}
	grant := CredentialGrant{
		GrantID:     "grant-1",
		ExecutionID: "exec-1",
		Provider:    "codex",
		Placement:   "mcp_authorization_env",
		BaseURL:     "https://proxy.example/v1",
		Value:       "secret-value",
		ExpiresAt:   now.Add(time.Minute),
	}
	issued, err := b.IssueCredentialHandle(handle, grant)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.HandleID == "" {
		t.Fatal("empty handle id")
	}
	if b.RegistrySize() != 1 {
		t.Fatalf("registry size = %d, want 1", b.RegistrySize())
	}
	// wrong daemon -> revoked
	if _, err := b.Redeem(issued.HandleID, "exec-1", "runtime-1", "daemon-other"); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("err = %v, want ErrGrantRevoked", err)
	}
	// same consumer retry -> same grant
	g, err := b.Redeem(issued.HandleID, "exec-1", "runtime-1", "daemon-1")
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if g.Value != "secret-value" {
		t.Fatalf("grant value mismatch")
	}
	// wrong execution -> scope mismatch
	if _, err := b.Redeem(issued.HandleID, "exec-other", "runtime-1", "daemon-1"); !errors.Is(err, ErrHandleScopeMismatch) {
		t.Fatalf("err = %v, want ErrHandleScopeMismatch", err)
	}
	// ack with wrong grant id -> mismatch
	if err := b.Ack(issued.HandleID, "wrong"); !errors.Is(err, ErrHandleScopeMismatch) {
		t.Fatalf("ack err = %v, want ErrHandleScopeMismatch", err)
	}
	// ack -> grant gone
	if err := b.Ack(issued.HandleID, "grant-1"); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if b.RegistrySize() != 0 {
		t.Fatalf("registry size = %d after ack, want 0", b.RegistrySize())
	}
}

func TestHandleExpiryAndCleanup(t *testing.T) {
	b, _ := newTestBroker()
	base := time.Now()
	handle := CredentialHandle{
		Audience: "memoryhub-provider-credential",
		TaskID:   "task-1", ExecutionID: "exec-1",
		RuntimeID: "runtime-1", DaemonID: "daemon-1",
		Provider: "claude", ExpiresAt: base.Add(-time.Minute),
	}
	issued, _ := b.IssueCredentialHandle(handle, CredentialGrant{GrantID: "g", ExecutionID: "exec-1"})
	if _, err := b.Redeem(issued.HandleID, "exec-1", "runtime-1", "daemon-1"); !errors.Is(err, ErrHandleExpired) {
		t.Fatalf("err = %v, want ErrHandleExpired", err)
	}
	if b.RegistrySize() != 0 {
		t.Fatalf("expired handle not cleaned: size = %d", b.RegistrySize())
	}
	// TTL cleanup
	b2, _ := newTestBroker()
	handle2 := handle
	handle2.ExpiresAt = base.Add(time.Minute)
	issued2, _ := b2.IssueCredentialHandle(handle2, CredentialGrant{GrantID: "g2", ExecutionID: "exec-1"})
	_ = issued2
	// advance clock past expiry
	b2.now = func() time.Time { return base.Add(2 * time.Minute) }
	if n := b2.CleanupExpiredHandles(); n != 1 {
		t.Fatalf("cleanup count = %d, want 1", n)
	}
}

func TestInvalidateAllOnRestart(t *testing.T) {
	b, _ := newTestBroker()
	now := time.Now()
	handle := CredentialHandle{Audience: "memoryhub-provider-credential", TaskID: "t", ExecutionID: "e", RuntimeID: "r", DaemonID: "d", Provider: "kimi", ExpiresAt: now.Add(time.Minute)}
	issued, _ := b.IssueCredentialHandle(handle, CredentialGrant{GrantID: "g", ExecutionID: "e"})
	if n := b.InvalidateAll(); n != 1 {
		t.Fatalf("invalidate = %d, want 1", n)
	}
	if _, err := b.Redeem(issued.HandleID, "e", "r", "d"); !errors.Is(err, ErrHandleNotFound) {
		t.Fatalf("err = %v, want ErrHandleNotFound after restart", err)
	}
}

func TestNonceNeverReused(t *testing.T) {
	b, _ := newTestBroker()
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		env, err := b.Encrypt("ws", "user_key", "cr", []byte("v"))
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		key := string(env.Nonce)
		if seen[key] {
			t.Fatal("nonce reused")
		}
		seen[key] = true
	}
}
