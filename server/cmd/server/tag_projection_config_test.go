package main

import (
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestTagProjectionAccessConfigFailsClosed(t *testing.T) {
	t.Setenv("TAG_PROJECTION_HMAC_KEY_ID", "")
	t.Setenv("TAG_PROJECTION_HMAC_KEY_BASE64", "")
	access, err := tagProjectionAccessFromEnv(nil, nil)
	if err != nil || access != nil {
		t.Fatalf("disabled access = %#v, err = %v", access, err)
	}

	t.Setenv("TAG_PROJECTION_HMAC_KEY_ID", "vibes-primary")
	if _, err := tagProjectionAccessFromEnv(nil, nil); err == nil {
		t.Fatal("partial projection authentication config was accepted")
	}

	secret := "not-valid-base64"
	t.Setenv("TAG_PROJECTION_HMAC_KEY_BASE64", secret)
	_, err = tagProjectionAccessFromEnv(nil, nil)
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

func TestTagWebSocketAssertionConfigUsesSameGatewayKeyMaterial(t *testing.T) {
	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_ID", "gateway-primary")
	t.Setenv("TAG_GATEWAY_ASSERTION_HMAC_KEY_BASE64", "Z2F0ZXdheS1hc3NlcnRpb24ta2V5LWF0LWxlYXN0LTMyLWJ5dGVz")
	verifier, err := tagWebSocketAssertionVerifierFromEnv()
	if err != nil || verifier == nil {
		t.Fatalf("configured WS verifier = %#v, err = %v", verifier, err)
	}
}

func TestTagRevocationRedisDefaultsToExistingRelayAndAllowsOptionalOverride(t *testing.T) {
	relay := &redis.Options{Addr: "shared-redis.internal:6379", DB: 4, Username: "relay"}
	t.Setenv("TAG_REVOCATION_REDIS_URL", "")
	opts, dedicated, err := tagRevocationRedisOptionsFromEnv(relay)
	if err != nil || dedicated || opts != relay {
		t.Fatalf("default options = %#v, dedicated=%v, err=%v", opts, dedicated, err)
	}

	t.Setenv("TAG_REVOCATION_REDIS_URL", "redis://revocation-user:secret@shared-redis.internal:6379/7")
	opts, dedicated, err = tagRevocationRedisOptionsFromEnv(relay)
	if err != nil || !dedicated || opts == relay || opts.DB != 7 || opts.Username != "revocation-user" {
		t.Fatalf("override options = %#v, dedicated=%v, err=%v", opts, dedicated, err)
	}

	t.Setenv("TAG_REVOCATION_REDIS_URL", "://not-a-url")
	if _, _, err := tagRevocationRedisOptionsFromEnv(relay); err == nil {
		t.Fatal("invalid revocation override was accepted")
	}
	t.Setenv("TAG_REVOCATION_REDIS_URL", "")
	if _, _, err := tagRevocationRedisOptionsFromEnv(nil); err == nil {
		t.Fatal("missing relay was accepted for Tag revocation")
	}
}
