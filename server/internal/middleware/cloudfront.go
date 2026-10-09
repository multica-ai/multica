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
func RefreshCloudFrontCookies(signer *auth.CloudFrontSigner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if signer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
