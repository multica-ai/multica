package handler

import (
	"crypto/aes"
	"crypto/cipher"
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

// mockDingTalkServer creates an httptest server that mocks the DingTalk OAuth APIs.
// tokenStatus/userStatus control what HTTP status each endpoint returns.
// tokenResp/userResp are the JSON bodies.
func mockDingTalkServer(t *testing.T, tokenStatus int, tokenResp any, userStatus int, userResp any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/oauth2/userAccessToken", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(tokenStatus)
		json.NewEncoder(w).Encode(tokenResp)
	})
	mux.HandleFunc("/v1.0/contact/users/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-acs-dingtalk-access-token") == "" {
			http.Error(w, "missing access token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(userStatus)
		json.NewEncoder(w).Encode(userResp)
	})
	return httptest.NewServer(mux)
}

func TestDingTalkLogin_TokenExchangeFails(t *testing.T) {
	srv := mockDingTalkServer(t, http.StatusBadRequest,
		map[string]string{"error": "invalid_code"}, 200, nil)
	defer srv.Close()

	origBase := dingtalkBaseURL
	dingtalkBaseURL = srv.URL
	defer func() { dingtalkBaseURL = origBase }()

	t.Setenv("DINGTALK_CLIENT_ID", "test-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "test-client-secret")

	h := newTestHandler(Config{AllowSignup: true})
	body := `{"code":"bad-code"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDingTalkLogin_EmptyAccessToken(t *testing.T) {
	srv := mockDingTalkServer(t, http.StatusOK,
		dingtalkTokenResponse{AccessToken: "", ExpireIn: 7200},
		200, nil)
	defer srv.Close()

	origBase := dingtalkBaseURL
	dingtalkBaseURL = srv.URL
	defer func() { dingtalkBaseURL = origBase }()

	t.Setenv("DINGTALK_CLIENT_ID", "test-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "test-client-secret")

	h := newTestHandler(Config{AllowSignup: true})
	body := `{"code":"valid-code"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDingTalkLogin_UserInfoFails(t *testing.T) {
	srv := mockDingTalkServer(t, http.StatusOK,
		dingtalkTokenResponse{AccessToken: "test-access-token", ExpireIn: 7200},
		http.StatusInternalServerError, map[string]string{"error": "server error"})
	defer srv.Close()

	origBase := dingtalkBaseURL
	dingtalkBaseURL = srv.URL
	defer func() { dingtalkBaseURL = origBase }()

	t.Setenv("DINGTALK_CLIENT_ID", "test-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "test-client-secret")

	h := newTestHandler(Config{AllowSignup: true})
	body := `{"code":"valid-code"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDingTalkLogin_MissingUnionID(t *testing.T) {
	srv := mockDingTalkServer(t, http.StatusOK,
		dingtalkTokenResponse{AccessToken: "test-access-token", ExpireIn: 7200},
		http.StatusOK,
		dingtalkUserInfo{OpenID: "openid-123", Nick: "Test", Email: "test@example.com"})
	defer srv.Close()

	origBase := dingtalkBaseURL
	dingtalkBaseURL = srv.URL
	defer func() { dingtalkBaseURL = origBase }()

	t.Setenv("DINGTALK_CLIENT_ID", "test-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "test-client-secret")

	h := newTestHandler(Config{AllowSignup: true})
	body := `{"code":"valid-code"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/dingtalk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.DingTalkLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	json.NewDecoder(rec.Body).Decode(&errResp)
	if !strings.Contains(errResp["error"], "unionId") {
		t.Fatalf("expected unionId error, got %s", errResp["error"])
	}
}

func TestEncryptToken_Roundtrip(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-that-is-long-enough-32bytes!")
	t.Setenv("DINGTALK_TOKEN_ENCRYPTION_KEY", "")

	plaintext := "my-super-secret-access-token"
	encrypted := encryptToken(plaintext)
	if encrypted == "" {
		t.Fatal("encrypted token should not be empty")
	}

	// Decrypt and verify
	key := tokenEncryptionKey()
	ciphertext, err := hex.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		t.Fatal("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	decrypted, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Fatalf("roundtrip failed: got %q, want %q", string(decrypted), plaintext)
	}
}
