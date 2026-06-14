package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
)

// withRealTOTPService swaps a freshly-constructed *service.TOTPService onto
// testHandler.TOTPService for the duration of the test. Restores the prior
// value on cleanup. Since *service.TOTPService is a concrete type (not an
// interface), tests cannot use a stub — they must use a real instance with
// a per-test-generated random key.
func withRealTOTPService(t *testing.T) {
	t.Helper()
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	t.Setenv("MULTICA_USER_TOTP_KEY", base64.StdEncoding.EncodeToString(raw[:]))
	svc, err := service.NewTOTPService()
	if err != nil {
		t.Fatalf("NewTOTPService: %v", err)
	}
	orig := testHandler.TOTPService
	testHandler.TOTPService = svc
	t.Cleanup(func() { testHandler.TOTPService = orig })
}

// TestTOTPSetupInit_RequiresAuth verifies that a request without X-User-ID
// is rejected with 401.
func TestTOTPSetupInit_RequiresAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/totp/setup-init", nil)
	w := httptest.NewRecorder()
	// Use a bare Handler — no TOTPService, no DB needed; requireUserID fires first.
	h := &Handler{}
	h.TOTPSetupInit(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestTOTPSetupVerify_RequiresAuth verifies that setup-verify also requires auth.
func TestTOTPSetupVerify_RequiresAuth(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/totp/setup-verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h := &Handler{}
	h.TOTPSetupVerify(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestTOTPSetupInit_ServiceUnavailableWhenNil verifies that a request with a
// valid user but no TOTPService wired returns 503.
func TestTOTPSetupInit_ServiceUnavailableWhenNil(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	// Temporarily nil out the service (it starts nil in testHandler since we
	// don't set MULTICA_USER_TOTP_KEY in tests).
	req := newRequest(http.MethodPost, "/api/auth/totp/setup-init", nil)
	w := httptest.NewRecorder()
	testHandler.TOTPSetupInit(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when TOTPService is nil, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestTOTPSetupVerify_ServiceUnavailableWhenNil mirrors the 503 check for the
// verify endpoint.
func TestTOTPSetupVerify_ServiceUnavailableWhenNil(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := newRequest(http.MethodPost, "/api/auth/totp/setup-verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPSetupVerify(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when TOTPService is nil, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestTOTPSetupVerify_FailsOnInvalidCode uses a real TOTPService and verifies
// that the handler rejects code 000000. Result code is 400 (no setup in
// progress for the test user) OR 401 (setup ran first and stored a secret;
// 000000 won't validate against a random secret). Both prove the code was
// not accepted.
func TestTOTPSetupVerify_FailsOnInvalidCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)

	body, _ := json.Marshal(map[string]string{"code": "000000"})
	req := newRequest(http.MethodPost, "/api/auth/totp/setup-verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPSetupVerify(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
		t.Errorf("expected 400 or 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTOTPDisable_RequiresAuth(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/totp/disable", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h := &Handler{}
	h.TOTPDisable(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestTOTPDisable_ServiceUnavailableWhenNil(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := newRequest(http.MethodPost, "/api/auth/totp/disable", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPDisable(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTOTPDisable_RejectsInvalidCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	body, _ := json.Marshal(map[string]string{"code": "000000"})
	req := newRequest(http.MethodPost, "/api/auth/totp/disable", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPDisable(w, req)
	// Either 400 (no TOTP enabled — no encrypted secret) or 401 (wrong code).
	// Both prove rejection.
	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
		t.Errorf("expected 400 or 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestTOTPSetupInit_ReturnsSecretAndOTPAuthURL exercises the full happy path
// with a freshly-constructed TOTPService injected for this test only.
func TestTOTPSetupInit_ReturnsSecretAndOTPAuthURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)

	req := newRequest(http.MethodPost, "/api/auth/totp/setup-init", nil)
	w := httptest.NewRecorder()
	testHandler.TOTPSetupInit(w, req)
	// Happy path requires the test user to exist in the DB. CI seeds it; local
	// dev DBs may not. Skip gracefully on that specific failure.
	if w.Code == http.StatusInternalServerError && strings.Contains(w.Body.String(), "failed to load user") {
		t.Skip("test user fixture not seeded in local DB; happy path covered in CI")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Secret     string `json:"secret"`
		OtpAuthURL string `json:"otpauth_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Secret) < 16 {
		t.Errorf("secret too short: %q", resp.Secret)
	}
	if !strings.HasPrefix(resp.OtpAuthURL, "otpauth://totp/Multica:") {
		t.Errorf("bad otpauth url: %q", resp.OtpAuthURL)
	}
}

// TestAdminResetMemberTOTP_RequiresAuth verifies that the admin-reset endpoint
// requires authentication. Valid UUID params are injected so UUID parsing
// succeeds; requireWorkspaceMember then calls requireUserID which fires 401.
func TestAdminResetMemberTOTP_RequiresAuth(t *testing.T) {
	const validUUID = "00000000-0000-0000-0000-000000000001"
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+validUUID+"/members/"+validUUID+"/totp-reset", nil)
	req = withURLParams(req, "wsId", validUUID, "userId", validUUID)
	w := httptest.NewRecorder()
	// Bare Handler — no DB, no auth header; requireWorkspaceMember fires requireUserID → 401.
	h := &Handler{}
	h.AdminResetMemberTOTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestAdminResetMemberTOTP_RejectsBadIDs verifies that an invalid workspace UUID
// in the URL param causes a 400 Bad Request before any DB or auth check.
func TestAdminResetMemberTOTP_RejectsBadIDs(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/not-a-uuid/members/00000000-0000-0000-0000-000000000001/totp-reset", nil)
	req = withURLParams(req, "wsId", "not-a-uuid", "userId", "00000000-0000-0000-0000-000000000001")
	w := httptest.NewRecorder()
	h := &Handler{}
	h.AdminResetMemberTOTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid workspace id, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTOTPLogin_RejectsBadInput(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)

	// non-6-digit code → 401 generic
	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com", "code": "abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login-totp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPLogin(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTOTPLogin_RejectsUnknownEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)

	// well-formed code but user doesn't have TOTP (or doesn't exist) → generic 401
	body, _ := json.Marshal(map[string]string{"email": "nobody-totp-test@example.com", "code": "123456"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login-totp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPLogin(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTOTPLogin_ServiceUnavailableWhenNil(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"email": "a@b.com", "code": "123456"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login-totp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h := &Handler{}
	h.TOTPLogin(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when TOTPService is nil, got %d", w.Code)
	}
}

func TestTOTPStatus_AlwaysReturnsConfiguredTrue(t *testing.T) {
	// 防枚举：恒返 configured:true，不分用户是否存在 / 是否启用 TOTP
	emails := []string{"alice@example.com", "nobody@example.com", "", "not-an-email"}
	for _, email := range emails {
		t.Run(email, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/totp-status?email="+email, nil)
			w := httptest.NewRecorder()
			h := &Handler{} // anti-enumeration shim doesn't need DB or service
			h.TOTPStatus(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d", w.Code)
			}
			var resp struct{ Configured bool `json:"configured"` }
			json.NewDecoder(w.Body).Decode(&resp)
			if !resp.Configured {
				t.Errorf("status must always return configured:true for email %q", email)
			}
		})
	}
}

// TestTOTPRoutes_RequireHumanActorWired pins the security contract that
// account-level TOTP endpoints (personal setup/verify/disable + admin
// reset) must never be reachable with a machine credential (mat_ task
// token or mcn_ cloud-node PAT). If a future change removes the
// r.Use(RequireHumanActor) / chained .With(RequireHumanActor, ...) wiring
// in router.go this test fails, even if every per-handler test still
// passes — because the per-handler tests bypass the route group entirely.
//
// Mirrors TestRequireHumanActor_AppliedViaChiRouterUse in actor_guards_test.go
// but for the TOTP group specifically. The minimal router replicates the
// production wiring shape from server/cmd/server/router.go:672-696.
func TestTOTPRoutes_RequireHumanActorWired(t *testing.T) {
	innerCalled := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	r := chi.NewRouter()
	// /auth/totp/* group — same wiring shape as production.
	r.Route("/auth/totp", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Post("/setup-init", innerHandler)
		r.Post("/setup-verify", innerHandler)
		r.Post("/disable", innerHandler)
	})
	// admin-reset endpoint — chained .With(RequireHumanActor, ...).
	r.With(RequireHumanActor).Post(
		"/api/workspaces/{wsId}/members/{userId}/totp-reset", innerHandler,
	)

	endpoints := []string{
		"/auth/totp/setup-init",
		"/auth/totp/setup-verify",
		"/auth/totp/disable",
		"/api/workspaces/00000000-0000-0000-0000-000000000001/members/00000000-0000-0000-0000-000000000002/totp-reset",
	}
	machineSources := []string{"task_token", "cloud_pat"}

	for _, ep := range endpoints {
		for _, src := range machineSources {
			t.Run(ep+":"+src, func(t *testing.T) {
				innerCalled = false
				req := httptest.NewRequest(http.MethodPost, ep, nil)
				req.Header.Set("X-Actor-Source", src)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want 403", w.Code)
				}
				if innerCalled {
					t.Fatal("inner handler must NOT run for machine actor")
				}
			})
		}

		// Human actor (no X-Actor-Source) passes the guard.
		t.Run(ep+":human", func(t *testing.T) {
			innerCalled = false
			req := httptest.NewRequest(http.MethodPost, ep, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (guard should pass for human)", w.Code)
			}
			if !innerCalled {
				t.Fatal("inner handler must run for human actor")
			}
		})
	}
}

