package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidToString(u pgtype.UUID) string { return util.UUIDToString(u) }

func rejectTemporarilyDisabledUser(w http.ResponseWriter, r *http.Request, userID, email, authPath string) bool {
	if !auth.IsTemporarilyDisabledUser(userID, email) {
		return false
	}
	slog.Warn(
		"auth: temporarily disabled user rejected",
		"path", r.URL.Path,
		"user_id", userID,
		"auth_path", authPath,
	)
	writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
	return true
}

// Auth middleware validates JWT tokens or Personal Access Tokens.
// Token sources (in priority order):
//  1. Authorization: Bearer <token> header (PAT or JWT)
//  2. multica_auth HttpOnly cookie (JWT) — requires valid CSRF token for state-changing requests
//
// Sets X-User-ID and X-User-Email headers on the request for downstream handlers.
//
// patCache is optional; when non-nil, PAT lookups are cached with a short
// TTL (auth.AuthCacheTTL). On cache hit the middleware skips both the DB
// SELECT and the last_used_at UPDATE — last_used_at is therefore refreshed
// at most once per TTL window per token, not per request.
//
// cfSigner is optional; when non-nil, a session renewed here also gets fresh
// CloudFront cookies. It is wired into THIS middleware rather than left to
// RefreshCloudFrontCookies because renewal is what makes the two clocks
// diverge: every route group that can renew must re-sign, and only the
// renewer knows it renewed. A group that mounts Auth without the CDN
// middleware (the plugin bridge) would otherwise slide the session forward
// while leaving the CDN policy pinned to the original login.
func Auth(queries *db.Queries, patCache *auth.PATCache, cfSigner *auth.CloudFrontSigner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// X-Actor-Source is server-set only — any value supplied by
			// the client is untrusted and discarded before the auth
			// branches run. Only the mat_ branch below re-sets it. This
			// is what prevents a client from sending a normal mul_ PAT
			// plus a forged `X-Actor-Source: member` (or anything else)
			// to convince a downstream handler that its request came
			// from a non-task-token path.
			r.Header.Del("X-Actor-Source")

			// Agent identity is server-set for exactly the same reason,
			// and the rest of the codebase already assumes it (see
			// resolveActor, actor_guards.go, CreateIssue). Only the mat_
			// branch below re-stamps these from the token row.
			//
			// Without the strip, resolveActor's pair requirement was not a
			// boundary: BOTH ids are readable by any workspace member
			// (GET /api/issues/{id}/task-runs returns agent_id + task_id),
			// so a member could replay a live pair on their own JWT/PAT and
			// be resolved as that agent — and, since MUL-6951, act with the
			// authority of that run's originator. MUL-3428, reported and
			// fixed in #4313.
			r.Header.Del("X-Agent-ID")
			r.Header.Del("X-Task-ID")

			tokenString, fromCookie := extractToken(r)
			if tokenString == "" {
				slog.Debug("auth: no token found", "path", r.URL.Path)
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			// Cookie-based auth requires CSRF validation for state-changing methods.
			if fromCookie && !auth.ValidateCSRF(r) {
				slog.Debug("auth: CSRF validation failed", "path", r.URL.Path)
				http.Error(w, `{"error":"CSRF validation failed"}`, http.StatusForbidden)
				return
			}

			// Agent task token: "mat_" prefix. Minted by the server at
			// task-claim time and injected by the daemon into the agent
			// process. Authoritative for actor identity — the bound
			// (user_id, agent_id, task_id, workspace_id) triple is
			// written into request headers here, OVERRIDING whatever the
			// client sent, so a downstream actor-resolver cannot be
			// tricked by a client that strips or forges X-Agent-ID /
			// X-Task-ID. Human-only endpoints (e.g. agent env
			// management) reject requests authenticated this way; see
			// `actorSourceFromRequest`. MUL-2600.
			if strings.HasPrefix(tokenString, "mat_") {
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				hash := auth.HashToken(tokenString)
				tt, err := queries.GetTaskTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid task token", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				userID := uuidToString(tt.UserID)
				if rejectTemporarilyDisabledUser(w, r, userID, "", "task_token") {
					return
				}
				boundWorkspaceID := uuidToString(tt.WorkspaceID)
				// An explicit workspace selector that disagrees with the
				// token's binding is rejected here, BEFORE the overwrite
				// below erases what the client asked for. Silently
				// swapping the target let a request for workspace B run
				// against A (GitHub #9403).
				if checkTaskTokenWorkspaceTarget(w, r, queries, boundWorkspaceID) {
					return
				}
				r.Header.Set("X-User-ID", userID)
				r.Header.Set("X-Agent-ID", uuidToString(tt.AgentID))
				r.Header.Set("X-Task-ID", uuidToString(tt.TaskID))
				r.Header.Set("X-Workspace-ID", boundWorkspaceID)
				// X-Actor-Source flags the auth path so resolveActor and
				// any owner-only handler can deny without re-querying the
				// token table. The value "task_token" is the only signal
				// this header is allowed to carry — strip anything else a
				// client tried to send.
				r.Header.Set("X-Actor-Source", "task_token")
				next.ServeHTTP(w, r)
				return
			}

			// Cloud Node PATs ("mcn_") belonged to the retired Multica
			// Cloud Runtime. Reject them here instead of letting them
			// reach the mul_ / JWT branches.
			if strings.HasPrefix(tokenString, "mcn_") {
				slog.Warn("auth: retired mcn_ token presented", "path", r.URL.Path)
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			// PAT: tokens starting with "mul_"
			if strings.HasPrefix(tokenString, "mul_") {
				hash := auth.HashToken(tokenString)

				// Cache hit: TTL has not expired, the token was valid the
				// last time we looked, and nothing has invalidated the
				// entry since. Skip the DB SELECT and the last_used_at
				// UPDATE — last_used_at is bumped once per TTL window.
				if userID, ok := patCache.Get(r.Context(), hash); ok {
					if rejectTemporarilyDisabledUser(w, r, userID, "", "pat_cache") {
						return
					}
					r.Header.Set("X-User-ID", userID)
					next.ServeHTTP(w, r.WithContext(withPersonalAccessToken(r.Context())))
					return
				}

				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				pat, err := queries.GetPersonalAccessTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid PAT", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}

				userID := uuidToString(pat.UserID)
				if rejectTemporarilyDisabledUser(w, r, userID, "", "pat") {
					return
				}
				r.Header.Set("X-User-ID", userID)

				// Clamp cache TTL to the token's remaining lifetime so a
				// PAT expiring in <AuthCacheTTL can't continue passing
				// auth on a cache hit after expires_at.
				var expiresAt time.Time
				if pat.ExpiresAt.Valid {
					expiresAt = pat.ExpiresAt.Time
				}
				patCache.Set(r.Context(), hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))

				// Cache miss = TTL expired (or first use after revoke /
				// process restart). Refresh last_used_at; subsequent hits
				// within the TTL window skip this write entirely.
				go queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)

				next.ServeHTTP(w, r.WithContext(withPersonalAccessToken(r.Context())))
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
			email, _ := claims["email"].(string)
			if rejectTemporarilyDisabledUser(w, r, sub, email, "jwt") {
				return
			}
			r.Header.Set("X-User-ID", sub)
			if email != "" {
				r.Header.Set("X-User-Email", email)
			}

			// Sliding session: a browser that keeps using the app keeps its
			// cookie, instead of being logged out on the anniversary of its
			// login. No-op until the session drops below half its TTL
			// (MUL-7436).
			r = renewCookieSession(w, r, claims, fromCookie, cfSigner)

			next.ServeHTTP(w, r)
		})
	}
}

// extractToken returns the bearer token and whether it came from a cookie.
// Priority: Authorization header > multica_auth cookie.
func extractToken(r *http.Request) (token string, fromCookie bool) {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString != authHeader {
			return tokenString, false
		}
	}

	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}

	return "", false
}
