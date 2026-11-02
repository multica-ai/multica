package middleware

import (
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

// RefreshCloudFrontCookies is middleware that refreshes CloudFront signed cookies
// on authenticated requests when the cookie is missing (expired or first request
// after login). This prevents 403s from the CDN when cookies expire before the
// user's session does.
//
// Renewal-time re-signing is NOT done here — it belongs to the auth
// middleware, which is the only place that knows a session was renewed and is
// mounted on every group that can renew one. This middleware only skips its
// own work when that already happened, so a single response does not carry
// two sets of CDN cookies (MUL-7436).
//
// Non-browser clients (CLI, daemon) are skipped entirely: they never keep a
// cookie jar, so the cookie is "missing" on every request and each one paid an
// RSA signature for Set-Cookie headers that were thrown away — ~18% of API CPU
// in production (MUL-8190). Trusting the client-controlled X-Client-Platform
// header is safe here because a spoofed value can only make the server
// withhold CDN cookies from the spoofer; it never grants anything.
func RefreshCloudFrontCookies(signer *auth.CloudFrontSigner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if signer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isCookielessClient(r) {
				next.ServeHTTP(w, r)
				return
			}
			_, err := r.Cookie("CloudFront-Policy")
			if err != nil && !SessionRenewed(r) {
				ttl := auth.AuthTokenTTL()
				for _, cookie := range signer.SignedCookies(time.Now().Add(ttl)) {
					http.SetCookie(w, cookie)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// cookielessClientPlatforms are X-Client-Platform values sent by first-party
// clients that never store cookies (server/internal/cli and
// server/internal/daemon). Browser-based clients (web, desktop, mobile) are
// intentionally absent: they rely on the CDN cookies to load attachments.
var cookielessClientPlatforms = map[string]struct{}{
	"cli":    {},
	"daemon": {},
}

// isCookielessClient reads the header directly rather than the
// ClientMetadata context value so the check does not depend on middleware
// ordering.
func isCookielessClient(r *http.Request) bool {
	_, ok := cookielessClientPlatforms[r.Header.Get(HeaderClientPlatform)]
	return ok
}
