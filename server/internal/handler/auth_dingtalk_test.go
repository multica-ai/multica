package handler

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDingTalkLogin_MissingCode(t *testing.T) {
	h := newTestHandler(Config{AllowSignup: true})
	body := `{"code":"","redirect_uri":"http://localhost:3000/auth/callback"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDingTalkLogin_NotConfigured(t *testing.T) {
	os.Unsetenv("DINGTALK_CLIENT_ID")
	os.Unsetenv("DINGTALK_CLIENT_SECRET")
	h := newTestHandler(Config{AllowSignup: true})
	body := `{"code":"test-code","redirect_uri":"http://localhost:3000/auth/callback"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestDingTalkLogin_InvalidBody(t *testing.T) {
	h := newTestHandler(Config{AllowSignup: true})
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestEncryptToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-that-is-long-enough-32bytes!")

	encrypted := encryptToken("my-access-token")
	if encrypted == "" {
		t.Fatal("encrypted token should not be empty")
	}
	if encrypted == "my-access-token" {
		t.Fatal("encrypted token should differ from plaintext")
	}

	// Verify it's valid hex
	_, err := hex.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("encrypted token should be valid hex: %v", err)
	}
}

func TestEncryptToken_Empty(t *testing.T) {
	result := encryptToken("")
	if result != "" {
		t.Fatalf("empty plaintext should return empty string, got %q", result)
	}
}

func TestTokenEncryptionKey_FromEnv(t *testing.T) {
	// 32-byte key in hex = 64 hex chars
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	t.Setenv("DINGTALK_TOKEN_ENCRYPTION_KEY", hexKey)
	t.Setenv("JWT_SECRET", "fallback-secret")

	key := tokenEncryptionKey()
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}
	if hex.EncodeToString(key) != hexKey {
		t.Fatal("key should match DINGTALK_TOKEN_ENCRYPTION_KEY")
	}
}

func TestTokenEncryptionKey_FallbackToJWT(t *testing.T) {
	t.Setenv("DINGTALK_TOKEN_ENCRYPTION_KEY", "")
	t.Setenv("JWT_SECRET", "a]very-long-secret-key-for-jwt!!")

	key := tokenEncryptionKey()
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}
}

func TestDingTalkLoginRequest_JSON(t *testing.T) {
	input := `{"code":"auth_code_123","redirect_uri":"http://localhost:3000/auth/callback"}`
	var req DingTalkLoginRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if req.Code != "auth_code_123" {
		t.Fatalf("expected code auth_code_123, got %s", req.Code)
	}
	if req.RedirectURI != "http://localhost:3000/auth/callback" {
		t.Fatalf("expected redirect_uri, got %s", req.RedirectURI)
	}
}
