package main

import (
	"strings"
	"testing"
)

func TestTagProjectionAccessConfigFailsClosed(t *testing.T) {
	t.Setenv("TAG_PROJECTION_HMAC_KEY_ID", "")
	t.Setenv("TAG_PROJECTION_HMAC_KEY_BASE64", "")
	access, err := tagProjectionAccessFromEnv(nil)
	if err != nil || access != nil {
		t.Fatalf("disabled access = %#v, err = %v", access, err)
	}

	t.Setenv("TAG_PROJECTION_HMAC_KEY_ID", "vibes-primary")
	if _, err := tagProjectionAccessFromEnv(nil); err == nil {
		t.Fatal("partial projection authentication config was accepted")
	}

	secret := "not-valid-base64"
	t.Setenv("TAG_PROJECTION_HMAC_KEY_BASE64", secret)
	_, err = tagProjectionAccessFromEnv(nil)
	if err == nil {
		t.Fatal("invalid projection authentication key was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("configuration error disclosed the authentication key")
	}
}

func TestTagHTTPAssertionConfigFailsClosed(t *testing.T) {
	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_ID", "")
	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_BASE64", "")
	// The pre-#299 names must not remain as a fallback configuration path.
	t.Setenv("TAG_HTTP_ASSERTION_ISSUER", "legacy-issuer")
	t.Setenv("TAG_HTTP_ASSERTION_AUDIENCE", "legacy-audience")
	t.Setenv("TAG_HTTP_ASSERTION_HMAC_KEY_ID", "legacy-key")
	t.Setenv("TAG_HTTP_ASSERTION_HMAC_KEY_BASE64", "bGVnYWN5LWtleS10aGF0LW11c3Qtbm90LWJlLXVzZWQ=")
	verifier, err := tagHTTPAssertionVerifierFromEnv()
	if err != nil || verifier != nil {
		t.Fatalf("disabled verifier = %#v, err = %v", verifier, err)
	}

	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_ID", "gateway-primary")
	if _, err := tagHTTPAssertionVerifierFromEnv(); err == nil {
		t.Fatal("partial assertion configuration was accepted")
	}

	secret := "not-valid-base64"
	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_BASE64", secret)
	_, err = tagHTTPAssertionVerifierFromEnv()
	if err == nil {
		t.Fatal("invalid assertion key was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("configuration error disclosed the assertion key")
	}

	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_BASE64", "Z2F0ZXdheS1hc3NlcnRpb24ta2V5LWF0LWxlYXN0LTMyLWJ5dGVz")
	verifier, err = tagHTTPAssertionVerifierFromEnv()
	if err != nil || verifier == nil {
		t.Fatalf("configured verifier = %#v, err = %v", verifier, err)
	}
}
