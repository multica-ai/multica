package storage

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestSignAndVerifyLocalUploadURL_RoundTrip is the happy-path sanity check:
// a URL signed with secret S verifies under the same secret, before expiry.
func TestSignAndVerifyLocalUploadURL_RoundTrip(t *testing.T) {
	secret := []byte("test-jwt-secret")
	key := "workspaces/abc/foo.png"
	rawURL := "/uploads/" + key
	expiry := time.Now().Add(30 * time.Minute)

	signed := SignLocalUploadURL(rawURL, key, secret, expiry)
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	exp, sig := LocalUploadSignatureFromQuery(u.Query())
	if exp == "" || sig == "" {
		t.Fatalf("signed URL missing exp/sig: %s", signed)
	}
	if !VerifyLocalUploadSignature(key, exp, sig, secret, time.Now()) {
		t.Fatalf("freshly signed URL did not verify: %s", signed)
	}
}

// TestVerifyLocalUploadSignature_RejectsExpired confirms that we treat the
// expiry as a hard cutoff. Without this, signed URLs would survive
// indefinitely once leaked.
func TestVerifyLocalUploadSignature_RejectsExpired(t *testing.T) {
	secret := []byte("test-jwt-secret")
	key := "workspaces/abc/foo.png"
	expired := time.Now().Add(-1 * time.Minute)

	signed := SignLocalUploadURL("/uploads/"+key, key, secret, expired)
	u, _ := url.Parse(signed)
	exp, sig := LocalUploadSignatureFromQuery(u.Query())

	if VerifyLocalUploadSignature(key, exp, sig, secret, time.Now()) {
		t.Fatalf("expired URL must not verify: %s", signed)
	}
}

// TestVerifyLocalUploadSignature_RejectsTamperedSig confirms that any byte
// flip in the signature value breaks verification. HMAC + constant-time
// compare guarantees this; the test enforces it as a regression guard.
func TestVerifyLocalUploadSignature_RejectsTamperedSig(t *testing.T) {
	secret := []byte("test-jwt-secret")
	key := "workspaces/abc/foo.png"

	signed := SignLocalUploadURL("/uploads/"+key, key, secret, time.Now().Add(5*time.Minute))
	u, _ := url.Parse(signed)
	exp, sig := LocalUploadSignatureFromQuery(u.Query())

	// Flip the last char of the sig.
	tampered := sig[:len(sig)-1] + flipChar(sig[len(sig)-1])
	if VerifyLocalUploadSignature(key, exp, tampered, secret, time.Now()) {
		t.Fatalf("tampered sig must not verify: %s", tampered)
	}

	// Empty / random sigs must not verify.
	if VerifyLocalUploadSignature(key, exp, "", secret, time.Now()) {
		t.Errorf("empty sig must not verify")
	}
	if VerifyLocalUploadSignature(key, exp, "AAAAAAAAAAAA", secret, time.Now()) {
		t.Errorf("random sig must not verify")
	}
}

// TestVerifyLocalUploadSignature_BoundToKey is the security-critical check:
// a signature minted for key A must not authorize a request for key B. This
// is what guarantees that leaking one signed URL does not grant the holder
// access to the entire workspace's uploads.
func TestVerifyLocalUploadSignature_BoundToKey(t *testing.T) {
	secret := []byte("test-jwt-secret")
	keyA := "workspaces/abc/file-a.png"
	keyB := "workspaces/abc/file-b.png"

	signed := SignLocalUploadURL("/uploads/"+keyA, keyA, secret, time.Now().Add(5*time.Minute))
	u, _ := url.Parse(signed)
	exp, sig := LocalUploadSignatureFromQuery(u.Query())

	// Sig was minted for keyA; it must NOT verify for keyB even though
	// the URL came from the same workspace and is unexpired.
	if VerifyLocalUploadSignature(keyB, exp, sig, secret, time.Now()) {
		t.Fatalf("sig for %q must not verify against %q", keyA, keyB)
	}
	// Sanity: it does verify for the original key.
	if !VerifyLocalUploadSignature(keyA, exp, sig, secret, time.Now()) {
		t.Fatalf("sig for %q failed to verify against itself", keyA)
	}
}

// TestVerifyLocalUploadSignature_RejectsWrongSecret confirms that a signed
// URL minted under a different secret does NOT verify. Protects against
// the case where the JWT_SECRET env var changes (e.g. operator rotation):
// in-flight signed URLs become invalid, never silently fall back to
// "trust anyway."
func TestVerifyLocalUploadSignature_RejectsWrongSecret(t *testing.T) {
	key := "workspaces/abc/foo.png"
	signed := SignLocalUploadURL("/uploads/"+key, key, []byte("old-secret"), time.Now().Add(5*time.Minute))
	u, _ := url.Parse(signed)
	exp, sig := LocalUploadSignatureFromQuery(u.Query())

	if VerifyLocalUploadSignature(key, exp, sig, []byte("new-secret"), time.Now()) {
		t.Fatalf("URL signed under old secret must not verify under new secret")
	}
}

// TestSignLocalUploadURL_PreservesExistingQuery is a regression guard: if
// for any reason the stored URL already had query params (e.g. a future
// CDN-facing URL with a cache-buster), signing must not drop them. Our
// signed params win on collision so a stale exp/sig in the input gets
// replaced.
func TestSignLocalUploadURL_PreservesExistingQuery(t *testing.T) {
	secret := []byte("test-jwt-secret")
	key := "workspaces/abc/foo.png"
	rawURL := "/uploads/" + key + "?v=42"

	signed := SignLocalUploadURL(rawURL, key, secret, time.Now().Add(5*time.Minute))
	if !strings.Contains(signed, "v=42") {
		t.Errorf("signed URL must preserve existing query params: %s", signed)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	exp, sig := LocalUploadSignatureFromQuery(u.Query())
	if !VerifyLocalUploadSignature(key, exp, sig, secret, time.Now()) {
		t.Fatalf("signed URL with pre-existing query failed to verify")
	}
}

// TestLocalUploadSignatureFromQuery_EmptyOnAbsence ensures the helper
// returns "" for missing keys rather than panicking — callers
// distinguish "no signed-query auth attempted" from "broken signed-query
// auth" by both being empty.
func TestLocalUploadSignatureFromQuery_EmptyOnAbsence(t *testing.T) {
	v := url.Values{}
	exp, sig := LocalUploadSignatureFromQuery(v)
	if exp != "" || sig != "" {
		t.Errorf("empty query must yield empty exp/sig, got %q/%q", exp, sig)
	}

	v.Set("exp", "1700000000")
	exp, sig = LocalUploadSignatureFromQuery(v)
	if exp != "1700000000" || sig != "" {
		t.Errorf("partial query: exp=%q sig=%q", exp, sig)
	}
}

func flipChar(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
