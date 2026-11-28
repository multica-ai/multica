package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const strictDomainMessage = "email address not allowed on this instance; allowed domains: @company.com"

// strictLoginHandler returns a DB-backed handler with local email delivery
// and the given signup configuration.
func strictLoginHandler(t *testing.T, cfg Config) *Handler {
	t.Helper()
	h := restrictSignupHandler(t)
	h.cfg = cfg
	return h
}

// googleClientFor answers the Google token exchange and userinfo calls with
// the given email.
func googleClientFor(t *testing.T, email string) *http.Client {
	t.Helper()
	return &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "oauth2.googleapis.com":
			return googleResponse(req, http.StatusOK, `{"access_token":"test-token"}`), nil
		case "www.googleapis.com":
			return googleResponse(req, http.StatusOK, fmt.Sprintf(`{"email":%q}`, email)), nil
		default:
			t.Fatalf("unexpected Google request: %s", req.URL)
			return nil, nil
		}
	})}
}

// strictLoginAttempt runs one sign-in path for email and returns the
// response. verify-code uses a code inserted directly so the attempt does not
// depend on send-code succeeding.
func strictLoginAttempt(t *testing.T, h *Handler, path, email string) *testutil.Response {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)
	switch path {
	case "send-code":
		req := testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": email})
		return testutil.Call(t, h.SendCode, req)
	case "verify-code":
		dbfx.Insert(t, "verification_code", testutil.Cols{
			"email":      email,
			"code":       "123456",
			"expires_at": testutil.Raw("now() + interval '10 minutes'"),
		})
		req := testutil.JSONRequest(http.MethodPost, "/auth/verify-code", map[string]string{"email": email, "code": "123456"})
		return testutil.Call(t, h.VerifyCode, req)
	case "google":
		t.Setenv("GOOGLE_CLIENT_ID", "test-client")
		t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
		h.googleOAuthHTTPClient = googleClientFor(t, email)
		req := testutil.JSONRequest(http.MethodPost, "/auth/google", map[string]string{"code": "test-code", "redirect_uri": "http://localhost/auth/callback"})
		return testutil.Call(t, h.GoogleLogin, req)
	}
	t.Fatalf("unknown path %q", path)
	return nil
}

var strictLoginPaths = []string{"send-code", "verify-code", "google"}

// With the flag off (or on without any allowlist), existing users and invited
// users outside the allowlist keep getting in, as documented upstream.
func TestEmailAllowlistStrictOffKeepsExceptions(t *testing.T) {
	configs := map[string]Config{
		"off":            {AllowSignup: true, AllowedEmailDomains: []string{"company.com"}},
		"off_invitation": {AllowSignup: false, AllowedEmailDomains: []string{"company.com"}},
		"on_no_list":     {AllowSignup: false, EmailAllowlistStrict: true},
	}
	for name, cfg := range configs {
		for _, path := range strictLoginPaths {
			for _, kind := range []string{"existing", "invited"} {
				t.Run(name+"/"+path+"/"+kind, func(t *testing.T) {
					email := fmt.Sprintf("strict-off-%s-%s-%s@example.com", strings.ReplaceAll(name, "_", "-"), path, kind)
					dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
					if kind == "existing" {
						dbfx.User(t, "Strict off", email)
					} else {
						signupInvitation(t, email)
					}
					h := strictLoginHandler(t, cfg)
					strictLoginAttempt(t, h, path, email).Want(http.StatusOK)
					if path != "send-code" && dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email) != 1 {
						t.Fatal("expected the account to exist after sign-in")
					}
				})
			}
		}
	}
}

func TestEmailAllowlistStrictRejectsUnlistedUsers(t *testing.T) {
	for _, path := range strictLoginPaths {
		for _, kind := range []string{"existing", "invited", "new"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				email := fmt.Sprintf("strict-reject-%s-%s@example.com", path, kind)
				dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
				wantUsers := 0
				switch kind {
				case "existing":
					dbfx.User(t, "Strict reject", email)
					wantUsers = 1
				case "invited":
					signupInvitation(t, email)
				}
				h := strictLoginHandler(t, Config{
					AllowSignup:          true,
					AllowedEmailDomains:  []string{"company.com"},
					EmailAllowlistStrict: true,
				})
				resp := strictLoginAttempt(t, h, path, email).Want(http.StatusForbidden)
				got := resp.Map()
				if got["error"] != strictDomainMessage {
					t.Fatalf("error = %v, want %q", got["error"], strictDomainMessage)
				}
				code, hasCode := got["code"]
				if path == "google" && code != googleLoginCodeEmailNotAllowed {
					t.Fatalf("Google error code = %v, want %q", code, googleLoginCodeEmailNotAllowed)
				}
				if path != "google" && hasCode {
					t.Fatal("email-code errors must keep their existing response shape")
				}
				if len(resp.Result().Cookies()) != 0 {
					t.Fatal("rejected sign-in must not establish a session")
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email); n != wantUsers {
					t.Fatalf("users = %d, want %d", n, wantUsers)
				}
				if path == "send-code" && dbfx.Count(t, `SELECT count(*) FROM verification_code WHERE email = $1`, email) != 0 {
					t.Fatal("rejected send-code must not issue a code")
				}
			})
		}
	}
}

func TestEmailAllowlistStrictAdmitsListedUsers(t *testing.T) {
	for _, match := range []string{"domain", "email"} {
		for _, existing := range []bool{false, true} {
			for _, path := range []string{"email-code", "google"} {
				t.Run(fmt.Sprintf("%s/existing=%t/%s", match, existing, path), func(t *testing.T) {
					email := fmt.Sprintf("strict-allow-%s-%t-%s@company.com", match, existing, path)
					cfg := Config{AllowSignup: false, EmailAllowlistStrict: true}
					if match == "domain" {
						cfg.AllowedEmailDomains = []string{"COMPANY.COM"}
					} else {
						// The exact address must win even when its domain is not listed.
						email = strings.Replace(email, "@company.com", "@example.com", 1)
						cfg.AllowedEmails = []string{strings.ToUpper(email)}
						cfg.AllowedEmailDomains = []string{"company.com"}
					}
					dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
					dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)
					if existing {
						dbfx.User(t, "Strict allow", email)
					}
					h := strictLoginHandler(t, cfg)
					var login LoginResponse
					if path == "google" {
						strictLoginAttempt(t, h, "google", email).Want(http.StatusOK).JSON(&login)
					} else {
						testutil.Call(t, h.SendCode, testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": email})).Want(http.StatusOK)
						code, err := h.Queries.GetLatestVerificationCode(context.Background(), email)
						if err != nil {
							t.Fatal(err)
						}
						testutil.Call(t, h.VerifyCode, testutil.JSONRequest(http.MethodPost, "/auth/verify-code", map[string]string{"email": email, "code": code.Code})).Want(http.StatusOK).JSON(&login)
					}
					if login.Token == "" || login.User.Email != email {
						t.Fatalf("listed user did not get a session: %+v", login.User)
					}
				})
			}
		}
	}
}

func TestEmailAllowlistStrictCreateInvitation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    Config
		email  string
		status int
	}{
		{"strict_outside", Config{AllowedEmailDomains: []string{"company.com"}, EmailAllowlistStrict: true}, "strict-invite-outsider@example.com", http.StatusForbidden},
		{"strict_domain", Config{AllowedEmailDomains: []string{"company.com"}, EmailAllowlistStrict: true}, "STRICT-INVITE-DOMAIN@COMPANY.COM", http.StatusCreated},
		{"strict_exact_email", Config{AllowedEmails: []string{"strict-invite-exact@example.com"}, EmailAllowlistStrict: true}, "strict-invite-exact@example.com", http.StatusCreated},
		{"strict_no_list", Config{EmailAllowlistStrict: true}, "strict-invite-nolist@example.com", http.StatusCreated},
		{"off_outside", Config{AllowedEmailDomains: []string{"company.com"}}, "strict-invite-off@example.com", http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := *testHandler
			h.cfg = tc.cfg
			email := strings.ToLower(tc.email)
			dbfx.Cleanup(t, `DELETE FROM workspace_invitation WHERE workspace_id = $1 AND invitee_email = $2`, testWorkspaceID, email)
			req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{Email: tc.email, Role: "member"}), "id", testWorkspaceID)
			resp := testutil.Call(t, h.CreateInvitation, req).Want(tc.status)
			stored := dbfx.Count(t, `SELECT count(*) FROM workspace_invitation WHERE workspace_id = $1 AND invitee_email = $2`, testWorkspaceID, email)
			if tc.status == http.StatusForbidden {
				if got := resp.Map()["error"]; got != strictDomainMessage {
					t.Fatalf("error = %v, want %q", got, strictDomainMessage)
				}
				if stored != 0 {
					t.Fatal("rejected invitation was stored")
				}
			} else if stored != 1 {
				t.Fatalf("stored invitations = %d, want 1", stored)
			}
		})
	}
}

func TestEmailAllowlistStrictMessages(t *testing.T) {
	const secret = "partner@example.net"
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"email_only", Config{AllowedEmails: []string{secret}}, "email address not allowed on this instance; contact your administrator"},
		{"one_domain", Config{AllowedEmailDomains: []string{"Company.com"}}, strictDomainMessage},
		{"two_domains", Config{AllowedEmailDomains: []string{"company.com", "example.com"}}, "email address not allowed on this instance; allowed domains: @company.com, @example.com"},
		{"domain_and_email", Config{AllowedEmailDomains: []string{"company.com"}, AllowedEmails: []string{secret}}, strictDomainMessage},
	} {
		for _, isNew := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/new=%t", tc.name, isNew), func(t *testing.T) {
				cfg := tc.cfg
				cfg.EmailAllowlistStrict = true
				// A pending invitation must not be consulted, let alone honored.
				mock := &mockDB{pendingInvitation: true}
				h := newTestHandler(cfg)
				h.Queries = db.New(mock)
				err := h.checkSignupAllowed(context.Background(), "outsider@other.example", isNew)
				if !errors.Is(err, ErrEmailNotAllowed) {
					t.Fatalf("err = %v, want ErrEmailNotAllowed", err)
				}
				var signupErr SignupError
				if !errors.As(err, &signupErr) || signupErr.Error() != tc.want {
					t.Fatalf("message = %q, want %q", err, tc.want)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("message leaks an ALLOWED_EMAILS entry: %q", err)
				}
				if mock.invitationLookups != 0 {
					t.Fatalf("strict mode looked up invitations %d times", mock.invitationLookups)
				}
			})
		}
	}
}

func TestGoogleLoginActionableErrorKeepsStrictMessage(t *testing.T) {
	err := SignupError{Message: strictDomainMessage, cause: ErrEmailNotAllowed}
	req := testutil.JSONRequest(http.MethodPost, "/auth/google", nil)
	resp := testutil.Call(t, func(w http.ResponseWriter, _ *http.Request) {
		if !writeGoogleLoginActionableError(w, fmt.Errorf("login: %w", err)) {
			t.Fatal("strict allowlist error was not handled")
		}
	}, req).Want(http.StatusForbidden)
	got := resp.Map()
	if got["code"] != googleLoginCodeEmailNotAllowed || got["error"] != strictDomainMessage {
		t.Fatalf("got %v", got)
	}
}
