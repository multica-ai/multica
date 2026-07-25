package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyCodeLimitsConfiguredStaticLoginToCerebroStaging(t *testing.T) {
	const staticCode = "654321"

	tests := []struct {
		name       string
		email      string
		appURL     string
		appEnv     string
		wantStatus int
	}{
		{
			name:       "accepts on Cerebro staging even with production-like app env",
			email:      "static-login-staging-test@firtal.com",
			appURL:     "https://cerebro.firtal.com",
			appEnv:     "production",
			wantStatus: http.StatusOK,
		},
		{
			name:       "rejects on Multica production",
			email:      "static-login-production-test@firtal.com",
			appURL:     "https://multica.firtal.com",
			appEnv:     "production",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "keeps explicit local development login",
			email:      "static-login-local-test@firtal.com",
			appURL:     "http://localhost:3000",
			appEnv:     "development",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			t.Cleanup(func() {
				_, _ = testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, tt.email)
				_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, tt.email)
			})

			t.Setenv("MULTICA_DEV_MASTER_CODE", staticCode)
			t.Setenv("MULTICA_APP_URL", tt.appURL)
			t.Setenv("APP_ENV", tt.appEnv)

			var body bytes.Buffer
			if err := json.NewEncoder(&body).Encode(map[string]string{
				"email": tt.email,
				"code":  staticCode,
			}); err != nil {
				t.Fatalf("encode request: %v", err)
			}

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/auth/verify-code", &body)
			request.Header.Set("Content-Type", "application/json")
			testHandler.VerifyCode(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("VerifyCode status = %d, want %d: %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestCerebroStaticLoginAllowed(t *testing.T) {
	tests := []struct {
		name   string
		appURL string
		appEnv string
		want   bool
	}{
		{
			name:   "Cerebro staging HTTPS",
			appURL: "https://cerebro.firtal.com/",
			appEnv: "production",
			want:   true,
		},
		{
			name:   "Cerebro staging HTTPS with port",
			appURL: "https://CEREBRO.FIRTAL.COM:443",
			appEnv: "production",
			want:   true,
		},
		{
			name:   "Cerebro staging HTTP",
			appURL: "http://cerebro.firtal.com",
			appEnv: "development",
			want:   false,
		},
		{
			name:   "Multica production",
			appURL: "https://multica.firtal.com",
			appEnv: "production",
			want:   false,
		},
		{
			name:   "lookalike staging host",
			appURL: "https://cerebro.firtal.com.example.org",
			appEnv: "production",
			want:   false,
		},
		{
			name:   "staging host in URL user info",
			appURL: "https://cerebro.firtal.com@example.org",
			appEnv: "production",
			want:   false,
		},
		{
			name:   "localhost development",
			appURL: "http://localhost:3000",
			appEnv: "development",
			want:   true,
		},
		{
			name:   "loopback development",
			appURL: "http://127.0.0.1:3000",
			appEnv: "",
			want:   true,
		},
		{
			name:   "localhost production",
			appURL: "http://localhost:3000",
			appEnv: "production",
			want:   false,
		},
		{
			name:   "empty URL development",
			appURL: "",
			appEnv: "development",
			want:   true,
		},
		{
			name:   "empty URL production",
			appURL: "",
			appEnv: "production",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MULTICA_APP_URL", tt.appURL)
			t.Setenv("APP_ENV", tt.appEnv)

			if got := cerebroStaticLoginAllowed(); got != tt.want {
				t.Fatalf("cerebroStaticLoginAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
