package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func setAccessAllowlist(t *testing.T, raw string, lookup auth.UserEmailLookup) {
	t.Helper()
	a, err := auth.ParseAccessAllowlist(raw, lookup)
	if err != nil {
		t.Fatal(err)
	}
	auth.SetAccessAllowlist(a)
	t.Cleanup(func() { auth.SetAccessAllowlist(nil) })
}

func TestAuth_AccessAllowlistJWT(t *testing.T) {
	setAccessAllowlist(t, "*@multica.ai,guest@example.com", nil)
	for email, want := range map[string]int{
		"test@multica.ai":   http.StatusOK,
		"guest@example.com": http.StatusOK,
		"other@example.com": http.StatusForbidden,
	} {
		t.Run(email, func(t *testing.T) {
			claims := validClaims()
			claims["email"] = email
			handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest("GET", "/api/me", nil)
			req.Header.Set("Authorization", "Bearer "+generateToken(claims, auth.JWTSecret()))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != want {
				t.Fatalf("expected %d, got %d: %s", want, w.Code, w.Body.String())
			}
			if want == http.StatusForbidden {
				var body map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["code"] != auth.AccessNotAllowedCode {
					t.Fatalf("denial must carry code %q for clients to end the session, got %s", auth.AccessNotAllowedCode, w.Body.String())
				}
			}
		})
	}
}

// Credentials without an email claim (PATs, task, daemon and cloud tokens)
// resolve the owner's email through the registered lookup.
func TestRejectDeniedUser_ResolvesEmailForUserIDCredentials(t *testing.T) {
	setAccessAllowlist(t, "*@multica.ai", func(_ context.Context, userID string) (string, error) {
		switch userID {
		case "listed":
			return "a@multica.ai", nil
		case "unlisted":
			return "a@example.com", nil
		}
		return "", errors.New("db down")
	})
	for userID, want := range map[string]int{
		"listed":   0,
		"unlisted": http.StatusForbidden,
		"broken":   http.StatusServiceUnavailable,
	} {
		t.Run(userID, func(t *testing.T) {
			w := httptest.NewRecorder()
			rejected := rejectDeniedUser(w, httptest.NewRequest("GET", "/api/me", nil), userID, "", "pat")
			if want == 0 {
				if rejected {
					t.Fatalf("listed user rejected: %d %s", w.Code, w.Body.String())
				}
				return
			}
			if !rejected || w.Code != want {
				t.Fatalf("expected %d, got rejected=%t code=%d", want, rejected, w.Code)
			}
		})
	}
}
