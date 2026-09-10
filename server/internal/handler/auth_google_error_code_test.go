package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type googleRoundTripper func(*http.Request) (*http.Response, error)

func (fn googleRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func googleResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestGoogleLoginTokenFailures(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	tests := []struct {
		name       string
		status     int
		body       string
		readErr    error
		requestErr error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid grant", status: 400, body: `{"error":"invalid_grant"}`, wantStatus: 400, wantCode: "oauth_code_invalid"},
		{name: "invalid client", status: 401, body: `{"error":"invalid_client"}`, wantStatus: 502},
		{name: "redirect mismatch", status: 400, body: `{"error":"redirect_uri_mismatch"}`, wantStatus: 502},
		{name: "unauthorized client", status: 400, body: `{"error":"unauthorized_client"}`, wantStatus: 502},
		{name: "deleted client", status: 401, body: `{"error":"deleted_client"}`, wantStatus: 502},
		{name: "unknown provider error", status: 400, body: `{"error":"future_error"}`, wantStatus: 502},
		{name: "rate limited", status: 429, body: `{"error":"rate_limit_exceeded"}`, wantStatus: 502},
		{name: "provider unavailable", status: 503, body: `{"error":"temporarily_unavailable"}`, wantStatus: 502},
		{name: "5xx takes precedence over invalid grant", status: 500, body: `{"error":"invalid_grant"}`, wantStatus: 502},
		{name: "malformed provider error", status: 400, body: `<html>bad gateway</html>`, wantStatus: 502},
		{name: "partially decoded provider error", status: 400, body: `{"error":"invalid_grant","error":42}`, wantStatus: 502},
		{name: "empty provider error", status: 400, wantStatus: 502},
		{name: "malformed token", status: 200, body: `{`, wantStatus: 502},
		{name: "missing access token", status: 200, body: `{}`, wantStatus: 502},
		{name: "null token", status: 200, body: `null`, wantStatus: 502},
		{name: "empty access token", status: 200, body: `{"access_token":""}`, wantStatus: 502},
		{name: "wrong access token type", status: 200, body: `{"access_token":42}`, wantStatus: 502},
		{name: "read failure", status: 200, readErr: io.ErrUnexpectedEOF, wantStatus: 502},
		{name: "transport failure", requestErr: errors.New("provider unavailable"), wantStatus: 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(Config{})
			h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "oauth2.googleapis.com" {
					t.Fatalf("token failure must not fetch userinfo: %s", req.URL)
				}
				if tt.requestErr != nil {
					return nil, tt.requestErr
				}
				resp := googleResponse(req, tt.status, tt.body)
				if tt.readErr != nil {
					resp.Body = io.NopCloser(iotest.ErrReader(tt.readErr))
				}
				return resp, nil
			})}
			req := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"code":"test-code"}`))
			var got struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			testutil.Call(t, h.GoogleLogin, req).Want(tt.wantStatus).JSON(&got)
			if got.Code != tt.wantCode || got.Error == "" {
				t.Fatalf("got code=%q error=%q, want code=%q and a fallback message", got.Code, got.Error, tt.wantCode)
			}
		})
	}
}

func TestGoogleLoginUserInfoFailures(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	tests := []struct {
		name       string
		status     int
		body       string
		readErr    error
		requestErr error
		wantStatus int
		wantCode   string
	}{
		{name: "unauthorized", status: 401, body: `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, wantStatus: 502},
		{name: "forbidden", status: 403, body: `{"error":{"code":403}}`, wantStatus: 502},
		{name: "rate limited", status: 429, body: `{"error":{"code":429}}`, wantStatus: 502},
		{name: "provider unavailable", status: 503, body: `{"error":{"code":503}}`, wantStatus: 502},
		{name: "error status with profile fields", status: 500, body: `{"email":"user@example.com"}`, wantStatus: 502},
		{name: "non-JSON provider failure", status: 502, body: `<html>bad gateway</html>`, wantStatus: 502},
		{name: "missing email on a successful response", status: 200, body: `{"name":"No Email"}`, wantStatus: 400, wantCode: "google_account_no_email"},
		{name: "blank email on a successful response", status: 200, body: `{"email":"   "}`, wantStatus: 400, wantCode: "google_account_no_email"},
		{name: "null profile", status: 200, body: `null`, wantStatus: 502},
		{name: "malformed profile", status: 200, body: `{`, wantStatus: 502},
		{name: "wrong email type", status: 200, body: `{"email":42}`, wantStatus: 502},
		{name: "empty profile response", status: 200, wantStatus: 502},
		{name: "read failure", status: 200, readErr: io.ErrUnexpectedEOF, wantStatus: 502},
		{name: "unreadable provider failure", status: 503, readErr: io.ErrUnexpectedEOF, wantStatus: 502},
		{name: "transport failure", requestErr: errors.New("provider unavailable"), wantStatus: 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(Config{})
			h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Host {
				case "oauth2.googleapis.com":
					return googleResponse(req, http.StatusOK, `{"access_token":"test-token"}`), nil
				case "www.googleapis.com":
					if tt.requestErr != nil {
						return nil, tt.requestErr
					}
					resp := googleResponse(req, tt.status, tt.body)
					if tt.readErr != nil {
						resp.Body = io.NopCloser(iotest.ErrReader(tt.readErr))
					}
					return resp, nil
				default:
					t.Fatalf("unexpected Google OAuth request: %s", req.URL)
					return nil, nil
				}
			})}
			req := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"code":"test-code"}`))
			var got struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			testutil.Call(t, h.GoogleLogin, req).Want(tt.wantStatus).JSON(&got)
			if got.Code != tt.wantCode || got.Error == "" {
				t.Fatalf("got code=%q error=%q, want code=%q and a fallback message", got.Code, got.Error, tt.wantCode)
			}
		})
	}
}

func TestGoogleLoginActionableErrorCodes(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")

	tests := []struct {
		name       string
		cfg        Config
		userBody   string
		wantStatus int
		wantCode   string
		wantError  string
	}{
		{
			name:       "signup prohibited",
			cfg:        Config{AllowSignup: false},
			userBody:   `{"email":"new@example.com"}`,
			wantStatus: http.StatusForbidden,
			wantCode:   "signup_prohibited",
			wantError:  "user registration is disabled on this self-hosted instance",
		},
		{
			name:       "email not allowed",
			cfg:        Config{AllowSignup: true, AllowedEmailDomains: []string{"company.com"}},
			userBody:   `{"email":"new@example.com"}`,
			wantStatus: http.StatusForbidden,
			wantCode:   "email_not_allowed",
			wantError:  "email address or domain not allowed on this instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(tt.cfg)
			h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

			h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Host {
				case "oauth2.googleapis.com":
					return googleResponse(req, http.StatusOK, `{"access_token":"test-token"}`), nil
				case "www.googleapis.com":
					return googleResponse(req, http.StatusOK, tt.userBody), nil
				default:
					t.Fatalf("unexpected Google OAuth request: %s", req.URL)
					return nil, nil
				}
			})}

			req := httptest.NewRequest(
				http.MethodPost,
				"/auth/google",
				bytes.NewBufferString(`{"code":"test-code","redirect_uri":"http://localhost/auth/callback"}`),
			)
			var got struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			testutil.Call(t, h.GoogleLogin, req).Want(tt.wantStatus).JSON(&got)
			if got.Code != tt.wantCode || got.Error != tt.wantError {
				t.Fatalf("got code=%q error=%q, want code=%q error=%q", got.Code, got.Error, tt.wantCode, tt.wantError)
			}
		})
	}
}

func TestGoogleLoginActionableErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantError string
	}{
		{"account disabled", auth.ErrTemporarilyDisabledUser, "account_disabled", "account disabled"},
		{"signup prohibited", ErrSignupProhibited, "signup_prohibited", ErrSignupProhibited.Error()},
		{"email not allowed", ErrEmailNotAllowed, "email_not_allowed", ErrEmailNotAllowed.Error()},
		{"wrapped signup restriction", fmt.Errorf("signup: %w", ErrEmailNotAllowed), "email_not_allowed", ErrEmailNotAllowed.Error()},
		{"future signup restriction", SignupError{Message: "another signup restriction"}, "", "another signup restriction"},
		{"wrapped future signup restriction", fmt.Errorf("signup: %w", SignupError{Message: "another signup restriction"}), "", "another signup restriction"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
			testutil.Call(t, func(w http.ResponseWriter, _ *http.Request) {
				if !writeGoogleLoginActionableError(w, tt.err) {
					t.Fatalf("actionable error was not handled: %v", tt.err)
				}
			}, req).Want(http.StatusForbidden).JSON(&got)
			if got.Code != tt.wantCode || got.Error != tt.wantError {
				t.Fatalf("got code=%q error=%q, want code=%q error=%q", got.Code, got.Error, tt.wantCode, tt.wantError)
			}
		})
	}
}

func TestGoogleLoginSuccessfulExistingUser(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	email := "google-login-success@example.com"
	userID := dbfx.User(t, "Google OAuth User", email)
	// Existing users can still sign in when new registrations are restricted.
	h := newTestHandler(Config{AllowSignup: false, AllowedEmailDomains: []string{"company.com"}})
	h.Queries = testHandler.Queries
	requests := 0
	h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		switch req.URL.Host {
		case "oauth2.googleapis.com":
			if err := req.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if req.Form.Get("code") != "test-code" || req.Form.Get("redirect_uri") != "http://localhost/auth/callback" {
				t.Fatalf("unexpected token exchange form: %v", req.Form)
			}
			return googleResponse(req, http.StatusOK, `{"access_token":"test-token"}`), nil
		case "www.googleapis.com":
			if req.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatal("userinfo request did not use the exchanged token")
			}
			return googleResponse(req, http.StatusOK, `{"email":" GOOGLE-LOGIN-SUCCESS@EXAMPLE.COM "}`), nil
		default:
			t.Fatalf("unexpected Google OAuth request: %s", req.URL)
			return nil, nil
		}
	})}
	req := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"code":"test-code","redirect_uri":"http://localhost/auth/callback"}`))
	var got LoginResponse
	resp := testutil.Call(t, h.GoogleLogin, req).Want(http.StatusOK).JSON(&got)
	if requests != 2 || got.Token == "" || got.User.ID != userID || got.User.Email != email {
		t.Fatalf("unexpected successful login: requests=%d, token present=%t, user=%+v", requests, got.Token != "", got.User)
	}
	var authCookie, csrfCookie *http.Cookie
	for _, cookie := range resp.Result().Cookies() {
		switch cookie.Name {
		case auth.AuthCookieName:
			authCookie = cookie
		case auth.CSRFCookieName:
			csrfCookie = cookie
		}
	}
	if authCookie == nil || authCookie.Value != got.Token || csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatal("successful Google login must return matching auth and CSRF cookies")
	}

	// Exercise the issued JWT through the same middleware that accepts browser sessions.
	meReq := httptest.NewRequest(http.MethodGet, "/users/me", nil)
	meReq.AddCookie(authCookie)
	protected := middleware.Auth(h.Queries, nil, nil)(http.HandlerFunc(h.GetMe))
	var me UserResponse
	testutil.Call(t, protected.ServeHTTP, meReq).Want(http.StatusOK).JSON(&me)
	if me.ID != userID || me.Email != email {
		t.Fatalf("Google session resolved to the wrong user: %+v", me)
	}

	csrfReq := httptest.NewRequest(http.MethodPost, "/users/me", nil)
	csrfReq.AddCookie(authCookie)
	csrfReq.Header.Set("X-CSRF-Token", csrfCookie.Value)
	if !auth.ValidateCSRF(csrfReq) {
		t.Fatal("Google login cookies must allow authenticated browser writes")
	}
}
