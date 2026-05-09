package github

import "testing"

func TestVerifySignature_Valid(t *testing.T) {
	secret := "super-secret"
	body := []byte(`{"action":"opened"}`)
	sig := ComputeSignature(body, secret)
	if err := VerifySignature(body, sig, secret); err != nil {
		t.Fatalf("expected valid signature to verify, got %v", err)
	}
}

func TestVerifySignature_BadSecret(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	sig := ComputeSignature(body, "right-secret")
	if err := VerifySignature(body, sig, "wrong-secret"); err != ErrSignatureInvalid {
		t.Fatalf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_BadBody(t *testing.T) {
	secret := "s"
	sig := ComputeSignature([]byte(`a`), secret)
	if err := VerifySignature([]byte(`b`), sig, secret); err != ErrSignatureInvalid {
		t.Fatalf("expected ErrSignatureInvalid for tampered body, got %v", err)
	}
}

func TestVerifySignature_MissingHeader(t *testing.T) {
	if err := VerifySignature([]byte(`{}`), "", "s"); err != ErrMissingSignature {
		t.Fatalf("expected ErrMissingSignature, got %v", err)
	}
}

func TestVerifySignature_BadAlgo(t *testing.T) {
	// "sha1=..." was the legacy header; we deliberately reject it so
	// nobody accidentally relies on a weaker algorithm.
	if err := VerifySignature([]byte(`{}`), "sha1=abc", "s"); err != ErrSignatureInvalid {
		t.Fatalf("expected ErrSignatureInvalid for sha1 prefix, got %v", err)
	}
}

func TestVerifySignature_NotHex(t *testing.T) {
	if err := VerifySignature([]byte(`{}`), "sha256=zzz", "s"); err != ErrSignatureInvalid {
		t.Fatalf("expected ErrSignatureInvalid for non-hex sig, got %v", err)
	}
}

func TestComputeSignature_Stable(t *testing.T) {
	a := ComputeSignature([]byte("hi"), "k")
	b := ComputeSignature([]byte("hi"), "k")
	if a != b {
		t.Fatalf("HMAC must be deterministic across calls")
	}
}
