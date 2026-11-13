package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// UserRateLimit is a per-user fixed-window limiter for authenticated routes
// whose cost scales with use (each call bills an upstream model). It keys on
// the X-User-ID the auth middleware sets, so it must run after Auth; a request
// without one passes through for the handler to reject. Like RateLimit, a nil
// or failing Redis fails open.
func UserRateLimit(rdb redis.UniversalClient, name string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if rdb == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}
			key := fmt.Sprintf("mul:ratelimit:user:%s:%s", name, userID)
			count, err := rateLimitScript.Run(r.Context(), rdb, []string{key}, int(window.Seconds())).Int64()
			if err != nil {
				slog.Warn("user ratelimit: redis error; allowing request", "error", err, "limit", name)
				next.ServeHTTP(w, r)
				return
			}
			if count > int64(limit) {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
