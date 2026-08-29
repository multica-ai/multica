package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
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

func TestGoogleLoginActionableErrorCodes(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")

	tests := []struct {
		name        string
		cfg         Config
		tokenStatus int
		userBody    string
		wantStatus  int
		wantCode    string
		wantError   string
	}{
		{
			name:        "oauth code invalid",
			tokenStatus: http.StatusBadRequest,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "oauth_code_invalid",
			wantError:   "failed to exchange code with Google",
		},
		{
			name:        "Google account has no email",
			tokenStatus: http.StatusOK,
			userBody:    `{"name":"No Email"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "google_account_no_email",
			wantError:   "Google account has no email",
		},
		{
			name:        "signup prohibited",
			cfg:         Config{AllowSignup: false},
			tokenStatus: http.StatusOK,
			userBody:    `{"email":"new@example.com"}`,
			wantStatus:  http.StatusForbidden,
			wantCode:    "signup_prohibited",
			wantError:   "user registration is disabled on this self-hosted instance",
		},
		{
			name:        "email not allowed",
			cfg:         Config{AllowSignup: true, AllowedEmailDomains: []string{"company.com"}},
			tokenStatus: http.StatusOK,
			userBody:    `{"email":"new@example.com"}`,
			wantStatus:  http.StatusForbidden,
			wantCode:    "email_not_allowed",
			wantError:   "email address or domain not allowed on this instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(tt.cfg)
			h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

			h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Host {
				case "oauth2.googleapis.com":
					body := `{"access_token":"test-token"}`
					if tt.tokenStatus != http.StatusOK {
						body = `{"error":"invalid_grant"}`
					}
					return googleResponse(req, tt.tokenStatus, body), nil
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
