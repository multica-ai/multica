package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitScript atomically increments the counter and sets the TTL on
// first access. Using a Lua script ensures INCR and EXPIRE cannot be
// split by a network failure — if INCR succeeds the TTL is guaranteed
// to be set, preventing a stuck key that acts as a permanent ban.
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

// ParseTrustedProxies parses a comma-separated list of CIDRs into a
// slice of *net.IPNet. Invalid entries are warned and skipped.
// Returns nil if raw is empty (default: never trust X-Forwarded-For).
func ParseTrustedProxies(raw string) []*net.IPNet {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var nets []*net.IPNet
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(p)
		if err != nil {
			slog.Warn("ratelimit: invalid trusted proxy CIDR, skipping", "cidr", p, "error", err)
			continue
		}
		nets = append(nets, cidr)
	}
	return nets
}

// RateLimit returns a per-IP fixed-window rate limiter backed by Redis.
// If rdb is nil the middleware is a no-op (fail-open).
//
// trustedProxies controls X-Forwarded-For handling: when the direct
// connection (RemoteAddr) originates from a CIDR in the list, the
// rightmost non-trusted IP in the XFF chain is used as the client IP.
// When the list is empty (default), XFF is never consulted — only
// RemoteAddr is used. This matches the project's conservative trust
// model (see health_realtime.go).
func RateLimit(rdb *redis.Client, limit int, window time.Duration, trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if rdb == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r, trustedProxies)
			key := rateLimitKey(r.URL.Path, ip)
			ctx := r.Context()

			count, err := rateLimitScript.Run(ctx, rdb, []string{key}, int(window.Seconds())).Int64()
			if err != nil {
				slog.Warn("ratelimit: redis error; allowing request", "error", err, "ip", ip)
				next.ServeHTTP(w, r)
				return
			}
			if count > int64(limit) {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractIP determines the client IP for rate limiting purposes.
// It only honors X-Forwarded-For when RemoteAddr is from a trusted proxy.
func extractIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	if len(trustedProxies) > 0 {
		remoteIP := net.ParseIP(remoteHost)
		if remoteIP != nil && isTrustedProxy(remoteIP, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				// Walk right-to-left: the rightmost non-trusted entry is
				// the last hop before the trusted proxy chain.
				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- {
					candidate := net.ParseIP(strings.TrimSpace(parts[i]))
					if candidate != nil && !isTrustedProxy(candidate, trustedProxies) {
						return candidate.String()
					}
				}
			}
		}
	}

	// Default: use RemoteAddr in canonical form.
	if ip := net.ParseIP(remoteHost); ip != nil {
		return ip.String()
	}
	return remoteHost
}

func isTrustedProxy(ip net.IP, cidrs []*net.IPNet) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func rateLimitKey(path, ip string) string {
	sanitized := strings.TrimPrefix(path, "/")
	sanitized = strings.ReplaceAll(sanitized, "/", ":")
	return fmt.Sprintf("mul:ratelimit:%s:%s", sanitized, ip)
}
