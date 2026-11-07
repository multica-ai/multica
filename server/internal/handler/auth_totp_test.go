package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp/totp"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
// that the handler rejects code 000000 with 400, whether no setup is in
// progress for the test user or setup ran first and 000000 does not validate
// against the stored secret. Never 401: the web client treats any 401 as an
// expired session and logs the user out.
func TestTOTPSetupVerify_FailsOnInvalidCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)

	body, _ := json.Marshal(map[string]string{"code": "000000"})
	req := newRequest(http.MethodPost, "/api/auth/totp/setup-verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPSetupVerify(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
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
	// 400 whether TOTP is not enabled or the code is wrong; never 401, which
	// the web client treats as an expired session.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
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

	// non-6-digit code → generic 400
	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com", "code": "abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login-totp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPLogin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTOTPLogin_RejectsUnknownEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)

	// well-formed code but user doesn't have TOTP (or doesn't exist) → generic 400
	body, _ := json.Marshal(map[string]string{"email": "nobody-totp-test@example.com", "code": "123456"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login-totp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.TOTPLogin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", w.Code, w.Body.String())
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
			var resp struct {
				Configured bool `json:"configured"`
			}
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

// enrollTOTPUser creates a dedicated user and enables TOTP for it through
// setup-init + setup-verify, so tests do not share TOTP state with the
// fixture user. It returns the user's id, email and base32 secret.
func enrollTOTPUser(t *testing.T) (userID, email, secret string) {
	t.Helper()
	email = fmt.Sprintf("totp-%d@example.com", time.Now().UnixNano())
	userID = dbfx.User(t, "TOTP User", email)

	var init struct {
		Secret string `json:"secret"`
	}
	req := newRequest(http.MethodPost, "/api/auth/totp/setup-init", nil)
	req.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.TOTPSetupInit, req).Want(http.StatusOK).JSON(&init)

	req = newRequest(http.MethodPost, "/api/auth/totp/setup-verify", map[string]string{"code": totpCodeAt(t, init.Secret, time.Now())})
	req.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.TOTPSetupVerify, req).Want(http.StatusOK)
	return userID, email, init.Secret
}

func totpCodeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// wrongTOTPCode returns a six-digit code that is not valid for any step in
// the current acceptance window.
func wrongTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now()
	valid := map[string]bool{}
	for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		valid[totpCodeAt(t, secret, now.Add(offset))] = true
	}
	for _, candidate := range []string{"000000", "111111", "222222", "333333"} {
		if !valid[candidate] {
			return candidate
		}
	}
	t.Fatal("no invalid candidate code")
	return ""
}

func totpLoginRequest(email, code string) *http.Request {
	body, _ := json.Marshal(map[string]string{"email": email, "code": code})
	return httptest.NewRequest(http.MethodPost, "/api/auth/login-totp", bytes.NewReader(body))
}

func TestTOTPLogin_RejectsReplayedCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	_, email, secret := enrollTOTPUser(t)

	// setup-verify consumed the current step; the next one is still inside
	// the acceptance window.
	next := totpCodeAt(t, secret, time.Now().Add(30*time.Second))
	testutil.Call(t, testHandler.TOTPLogin, totpLoginRequest(email, next)).Want(http.StatusOK)
	testutil.Call(t, testHandler.TOTPLogin, totpLoginRequest(email, next)).Want(http.StatusBadRequest)
}

func TestTOTPLogin_RejectsCodeAlreadyUsedForSetup(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	_, email, secret := enrollTOTPUser(t)

	testutil.Call(t, testHandler.TOTPLogin, totpLoginRequest(email, totpCodeAt(t, secret, time.Now()))).Want(http.StatusBadRequest)
}

func TestTOTPLogin_LocksAccountAfterRepeatedFailures(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID, email, secret := enrollTOTPUser(t)

	wrong := wrongTOTPCode(t, secret)
	for range totpMaxFailedAttempts {
		testutil.Call(t, testHandler.TOTPLogin, totpLoginRequest(email, wrong)).Want(http.StatusBadRequest)
	}

	// A correct, unused code is refused while the account is locked, with
	// the same generic response as any other failure.
	next := totpCodeAt(t, secret, time.Now().Add(30*time.Second))
	testutil.Call(t, testHandler.TOTPLogin, totpLoginRequest(email, next)).Want(http.StatusBadRequest)

	var locked bool
	dbfx.QueryRow(t, `SELECT totp_locked_until > now() FROM "user" WHERE id = $1`, userID).Scan(&locked)
	if !locked {
		t.Fatal("account not locked after the attempt budget was spent")
	}
}

func TestTOTPDisable_LockedAccountReturnsTooManyRequests(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID, _, secret := enrollTOTPUser(t)

	disable := func(code string) *http.Request {
		req := newRequest(http.MethodPost, "/api/auth/totp/disable", map[string]string{"code": code})
		req.Header.Set("X-User-ID", userID)
		return req
	}
	wrong := wrongTOTPCode(t, secret)
	for range totpMaxFailedAttempts {
		testutil.Call(t, testHandler.TOTPDisable, disable(wrong)).Want(http.StatusBadRequest)
	}
	next := totpCodeAt(t, secret, time.Now().Add(30*time.Second))
	testutil.Call(t, testHandler.TOTPDisable, disable(next)).Want(http.StatusTooManyRequests)
}

func TestEnableUserTOTP_OnlyEnablesTheVerifiedSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID := dbfx.User(t, "TOTP Race", fmt.Sprintf("totp-race-%d@example.com", time.Now().UnixNano()))

	setupInit := func() {
		req := newRequest(http.MethodPost, "/api/auth/totp/setup-init", nil)
		req.Header.Set("X-User-ID", userID)
		testutil.Call(t, testHandler.TOTPSetupInit, req).Want(http.StatusOK)
	}
	setupInit()
	var verified []byte
	dbfx.QueryRow(t, `SELECT totp_secret_encrypted FROM "user" WHERE id = $1`, userID).Scan(&verified)

	// A second setup-init replaces the pending secret between the handler's
	// read and its enable write.
	setupInit()
	rows, err := testHandler.Queries.EnableUserTOTP(context.Background(), db.EnableUserTOTPParams{
		ID:                  parseUUID(userID),
		TotpSecretEncrypted: verified,
		Step:                1,
	})
	if err != nil {
		t.Fatalf("EnableUserTOTP: %v", err)
	}
	if rows != 0 {
		t.Fatalf("enabled a secret that was never verified (rows=%d)", rows)
	}
}

func TestConsumeUserTOTPStep_RejectsReplacedSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID, _, _ := enrollTOTPUser(t)

	var verified []byte
	dbfx.QueryRow(t, `SELECT totp_secret_encrypted FROM "user" WHERE id = $1`, userID).Scan(&verified)
	// An admin reset and re-enrollment replace the secret after a login read it.
	dbfx.Exec(t, `UPDATE "user" SET totp_secret_encrypted = $2 WHERE id = $1`, userID, []byte("replaced-secret"))

	rows, err := testHandler.Queries.ConsumeUserTOTPStep(context.Background(), db.ConsumeUserTOTPStepParams{
		ID:                  parseUUID(userID),
		TotpSecretEncrypted: verified,
		Step:                time.Now().Unix()/30 + 1,
	})
	if err != nil {
		t.Fatalf("ConsumeUserTOTPStep: %v", err)
	}
	if rows != 0 {
		t.Fatalf("accepted a code checked against a replaced secret (rows=%d)", rows)
	}
}

func TestDisableUserTOTPSecret_KeepsReplacedSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID, _, _ := enrollTOTPUser(t)

	var verified []byte
	dbfx.QueryRow(t, `SELECT totp_secret_encrypted FROM "user" WHERE id = $1`, userID).Scan(&verified)
	dbfx.Exec(t, `UPDATE "user" SET totp_secret_encrypted = $2 WHERE id = $1`, userID, []byte("replaced-secret"))

	rows, err := testHandler.Queries.DisableUserTOTPSecret(context.Background(), db.DisableUserTOTPSecretParams{
		ID:                  parseUUID(userID),
		TotpSecretEncrypted: verified,
	})
	if err != nil {
		t.Fatalf("DisableUserTOTPSecret: %v", err)
	}
	if rows != 0 {
		t.Fatalf("cleared an authenticator whose code was never checked (rows=%d)", rows)
	}
	var enabled bool
	dbfx.QueryRow(t, `SELECT totp_enabled_at IS NOT NULL FROM "user" WHERE id = $1`, userID).Scan(&enabled)
	if !enabled {
		t.Fatal("replaced authenticator was disabled")
	}
}

func TestTOTPDisable_DisablesWithValidCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID, _, secret := enrollTOTPUser(t)

	req := newRequest(http.MethodPost, "/api/auth/totp/disable", map[string]string{
		"code": totpCodeAt(t, secret, time.Now().Add(30*time.Second)),
	})
	req.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.TOTPDisable, req).Want(http.StatusOK)

	var configured bool
	dbfx.QueryRow(t, `SELECT totp_secret_encrypted IS NOT NULL FROM "user" WHERE id = $1`, userID).Scan(&configured)
	if configured {
		t.Fatal("TOTP secret still stored after disable")
	}
}

// The members list is cached without a staleness bound, so an account-wide
// TOTP change must reach every workspace the user belongs to as
// member:updated; otherwise an admin's "Reset authenticator" action stays
// out of sync until reload.
func TestTOTPDisable_PublishesMemberUpdated(t *testing.T) {
	if testHandler == nil {
		t.Skip("no DB available")
	}
	withRealTOTPService(t)
	userID, _, secret := enrollTOTPUser(t)
	dbfx.Member(t, testWorkspaceID, userID, "member")

	var mu sync.Mutex
	var got []MemberWithUserResponse
	testHandler.Bus.Subscribe(protocol.EventMemberUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || e.WorkspaceID != testWorkspaceID {
			return
		}
		m, ok := payload["member"].(MemberWithUserResponse)
		if !ok || m.UserID != userID {
			return
		}
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	})

	req := newRequest(http.MethodPost, "/api/auth/totp/disable", map[string]string{
		"code": totpCodeAt(t, secret, time.Now().Add(30*time.Second)),
	})
	req.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.TOTPDisable, req).Want(http.StatusOK)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("member:updated events for the user = %d, want 1", len(got))
	}
	if got[0].TotpEnabled {
		t.Fatal("member:updated still reports totp_enabled after disable")
	}
}
