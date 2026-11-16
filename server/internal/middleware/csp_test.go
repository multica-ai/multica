package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentSecurityPolicy(t *testing.T) {
	handler := ContentSecurityPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	assertCSPDirectives(t, csp, []string{
		"script-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	})
}

func TestContentSecurityPolicyAllowsSameOriginAttachmentPreviews(t *testing.T) {
	handler := ContentSecurityPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{
		"/api/attachments/019f0dae-0315-79b7-b653-f55d6af90403/download",
		"/api/attachments/019f0dae-0315-79b7-b653-f55d6af90403/content",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			csp := rec.Header().Get("Content-Security-Policy")
			assertCSPDirectives(t, csp, []string{
				"script-src 'self'",
				"object-src 'none'",
				"frame-ancestors 'self'",
				"base-uri 'self'",
				"form-action 'self'",
			})
			if strings.Contains(csp, "frame-ancestors 'none'") {
				t.Fatalf("attachment preview CSP must not block same-origin iframe embedding; got: %s", csp)
			}
		})
	}
}

func assertCSPDirectives(t *testing.T, csp string, required []string) {
	t.Helper()
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
	for _, directive := range required {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP missing directive %q; got: %s", directive, csp)
		}
	}
}
