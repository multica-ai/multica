package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidToString(u pgtype.UUID) string { return util.UUIDToString(u) }

// Auth middleware validates JWT tokens or Personal Access Tokens.
// Token sources are tried in order:
//  1. Authorization: Bearer <token> header (PAT or JWT)
//  2. multica_auth HttpOnly cookie (JWT) — requires valid CSRF token for state-changing requests
//
// A stale bearer token must not shadow a valid browser session cookie. This
// happens during the localStorage-to-cookie migration when old web bundles or
// OAuth callback pages still carry a legacy token in memory.
//
// Sets X-User-ID and X-User-Email headers on the request for downstream handlers.
func Auth(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			candidates := extractTokenCandidates(r)
			if len(candidates) == 0 {
				slog.Debug("auth: no token found", "path", r.URL.Path)
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			lastStatus := http.StatusUnauthorized
			lastBody := `{"error":"invalid token"}`

			for _, candidate := range candidates {
				tokenString := candidate.token

				// Cookie-based auth requires CSRF validation for state-changing methods.
				if candidate.fromCookie && !auth.ValidateCSRF(r) {
					slog.Debug("auth: CSRF validation failed", "path", r.URL.Path)
					lastStatus = http.StatusForbidden
					lastBody = `{"error":"CSRF validation failed"}`
					continue
				}

				// PAT: tokens starting with "mul_"
				if strings.HasPrefix(tokenString, "mul_") {
					if queries == nil {
						continue
					}
					hash := auth.HashToken(tokenString)
					pat, err := queries.GetPersonalAccessTokenByHash(r.Context(), hash)
					if err != nil {
						slog.Warn("auth: invalid PAT", "path", r.URL.Path, "error", err)
						continue
					}

					r.Header.Set("X-User-ID", uuidToString(pat.UserID))

					// Best-effort: update last_used_at
					go queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)

					next.ServeHTTP(w, r)
					return
				}

				// JWT
				token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return auth.JWTSecret(), nil
				})
				if err != nil || !token.Valid {
					slog.Warn("auth: invalid token", "path", r.URL.Path, "source", candidate.source, "error", err)
					continue
				}

				claims, ok := token.Claims.(jwt.MapClaims)
				if !ok {
					slog.Warn("auth: invalid claims", "path", r.URL.Path, "source", candidate.source)
					lastBody = `{"error":"invalid claims"}`
					continue
				}

				sub, ok := claims["sub"].(string)
				if !ok || strings.TrimSpace(sub) == "" {
					slog.Warn("auth: invalid claims", "path", r.URL.Path, "source", candidate.source)
					lastBody = `{"error":"invalid claims"}`
					continue
				}
				r.Header.Set("X-User-ID", sub)
				if email, ok := claims["email"].(string); ok {
					r.Header.Set("X-User-Email", email)
				}

				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, lastBody, lastStatus)
		})
	}
}

type tokenCandidate struct {
	token      string
	fromCookie bool
	source     string
}

// extractTokenCandidates returns candidate tokens in preferred order.
func extractTokenCandidates(r *http.Request) []tokenCandidate {
	candidates := make([]tokenCandidate, 0, 2)
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString != authHeader {
			candidates = append(candidates, tokenCandidate{
				token:  tokenString,
				source: "authorization",
			})
		}
	}

	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		candidates = append(candidates, tokenCandidate{
			token:      cookie.Value,
			fromCookie: true,
			source:     "cookie",
		})
	}

	return candidates
}
