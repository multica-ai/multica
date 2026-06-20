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

// Auth middleware validates JWT tokens or Personal Access Tokens from the Authorization header.
// Sets X-User-ID and X-User-Email headers on the request for downstream handlers.
func Auth(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				slog.Debug("auth: missing authorization header", "path", r.URL.Path)
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				slog.Debug("auth: invalid format", "path", r.URL.Path)
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			// Task token: scoped to one agent task and used by spawned agent CLIs.
			if strings.HasPrefix(tokenString, "mat_") {
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				hash := auth.HashToken(tokenString)
				taskToken, err := queries.GetTaskTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid task token", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}

				r.Header.Set("X-User-ID", uuidToString(taskToken.UserID))
				r.Header.Set("X-Agent-ID", uuidToString(taskToken.AgentID))
				r.Header.Set("X-Task-ID", uuidToString(taskToken.TaskID))
				r.Header.Set("X-Workspace-ID", uuidToString(taskToken.WorkspaceID))

				next.ServeHTTP(w, r)
				return
			}

			// PAT: tokens starting with "mul_"
			if strings.HasPrefix(tokenString, "mul_") {
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				hash := auth.HashToken(tokenString)
				pat, err := queries.GetPersonalAccessTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid PAT", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
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
				slog.Warn("auth: invalid token", "path", r.URL.Path, "error", err)
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				slog.Warn("auth: invalid claims", "path", r.URL.Path)
				http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
				return
			}

			sub, ok := claims["sub"].(string)
			if !ok || strings.TrimSpace(sub) == "" {
				slog.Warn("auth: invalid claims", "path", r.URL.Path)
				http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
				return
			}
			r.Header.Set("X-User-ID", sub)
			if email, ok := claims["email"].(string); ok {
				r.Header.Set("X-User-Email", email)
			}

			next.ServeHTTP(w, r)
		})
	}
}
