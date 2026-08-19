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
