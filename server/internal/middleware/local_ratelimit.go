package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
)

// LocalRateLimit adapts a process-local limiter using the same trusted-proxy
// identity as RateLimit. Each URL has its own budget.
func LocalRateLimit(allow func(context.Context, string) bool, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return keyedRateLimit(allow, func(r *http.Request) string { return extractIP(r, trustedProxies) })
}

// UserRateLimit must run after Auth has replaced X-User-ID with the verified identity.
func UserRateLimit(allow func(context.Context, string) bool) func(http.Handler) http.Handler {
	return keyedRateLimit(allow, func(r *http.Request) string { return r.Header.Get("X-User-ID") })
}

func keyedRateLimit(allow func(context.Context, string) bool, key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allow(r.Context(), r.URL.Path+":"+key(r)) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
