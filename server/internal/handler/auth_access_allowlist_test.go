package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestLoginAccessAllowlist(t *testing.T) {
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("SMTP_HOST", "")
	a, err := auth.ParseAccessAllowlist("*@allowed.example,solo@other.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	auth.SetAccessAllowlist(a)
	t.Cleanup(func() { auth.SetAccessAllowlist(nil) })

	h := *testHandler
	// Signup itself is open: only the access allowlist decides.
	h.cfg = Config{AllowSignup: true}
	h.EmailService = service.NewEmailService()

	for email, want := range map[string]int{
		"member@allowed.example": http.StatusOK,
		"solo@other.example":     http.StatusOK,
		"else@other.example":     http.StatusForbidden,
		// Existing accounts are gated too, not only new signups.
		handlerTestEmail: http.StatusForbidden,
	} {
		t.Run(email, func(t *testing.T) {
			dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)
			req := testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": email})
			testutil.Call(t, h.SendCode, req).Want(want)
			if want != http.StatusOK {
				if n := dbfx.Count(t, `SELECT count(*) FROM verification_code WHERE email = $1`, email); n != 0 {
					t.Fatal("a rejected email must not receive a code")
				}
			}
		})
	}

	const outsider = "outsider@other.example"
	dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, outsider)
	if _, _, err := h.findOrCreateUser(context.Background(), outsider); !errors.Is(err, ErrEmailNotAllowed) {
		t.Fatalf("expected ErrEmailNotAllowed, got %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, outsider); n != 0 {
		t.Fatal("a rejected email must not create an account")
	}
}
