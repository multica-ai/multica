package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOIDCFlowCookieRoundTrip(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.com")
	flow, err := NewOIDCFlow("next:/invite/123", "pkce-verifier")
	if err != nil {
		t.Fatalf("NewOIDCFlow: %v", err)
	}
	w := httptest.NewRecorder()
	if err := SetOIDCFlowCookie(w, flow); err != nil {
		t.Fatalf("SetOIDCFlowCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: want 1, got %d", len(cookies))
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("OIDC flow cookie must be HttpOnly and Secure: %#v", cookies[0])
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc", nil)
	req.AddCookie(cookies[0])
	got, err := ReadOIDCFlowCookie(req)
	if err != nil {
		t.Fatalf("ReadOIDCFlowCookie: %v", err)
	}
	if got.State != flow.State || got.Nonce != flow.Nonce || got.CodeVerifier != flow.CodeVerifier || got.AppState != flow.AppState {
		t.Fatalf("flow round trip mismatch: got %#v, want %#v", got, flow)
	}
}

func TestOIDCFlowCookieRejectsTampering(t *testing.T) {
	flow, err := NewOIDCFlow("", "pkce-verifier")
	if err != nil {
		t.Fatalf("NewOIDCFlow: %v", err)
	}
	w := httptest.NewRecorder()
	if err := SetOIDCFlowCookie(w, flow); err != nil {
		t.Fatalf("SetOIDCFlowCookie: %v", err)
	}
	cookie := w.Result().Cookies()[0]
	cookie.Value += "tampered"
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc", nil)
	req.AddCookie(cookie)
	if _, err := ReadOIDCFlowCookie(req); err == nil {
		t.Fatal("ReadOIDCFlowCookie accepted a tampered cookie")
	}
}
