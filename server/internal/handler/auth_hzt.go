package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/hztauth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const hztFlowCookieName = "multica_hzt_sso_flow"

var hztAdminRoles = map[string]struct{}{
	"admin":                              {},
	"admin_operator_manager":             {},
	"admin_meituan_operator_manager":     {},
	"admin_douyin_operator_manager":      {},
	"admin_xiaohongshu_operator_manager": {},
	"admin_hunliji_operator_manager":     {},
}

func safePostAuthPath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return ""
	}
	return value
}

func hztCookieSecure() bool {
	origin, err := url.Parse(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")))
	return err == nil && strings.EqualFold(origin.Scheme, "https")
}

func setHZTFlowCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: hztFlowCookieName, Value: value, Path: "/auth/hzt", MaxAge: maxAge,
		Expires: time.Now().Add(time.Duration(maxAge) * time.Second), HttpOnly: true,
		Secure: hztCookieSecure(), SameSite: http.SameSiteLaxMode,
	})
}

func clearHZTFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: hztFlowCookieName, Value: "", Path: "/auth/hzt", MaxAge: -1,
		Expires: time.Unix(0, 0), HttpOnly: true, Secure: hztCookieSecure(), SameSite: http.SameSiteLaxMode,
	})
}

func setWebLoggedInMarker(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: "multica_logged_in", Value: "1", Path: "/", MaxAge: 365 * 24 * 60 * 60,
		Expires: time.Now().Add(365 * 24 * time.Hour), Secure: hztCookieSecure(), SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) BeginHZTLogin(w http.ResponseWriter, r *http.Request) {
	if h.HZTAuth == nil || h.cfg.AuthProvider != "hzt_redirect" {
		writeError(w, http.StatusServiceUnavailable, "HZT login is not configured")
		return
	}
	flow, err := h.HZTAuth.NewFlow(safePostAuthPath(r.URL.Query().Get("next")), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start HZT login")
		return
	}
	setHZTFlowCookie(w, flow.Cookie, 10*60)
	http.Redirect(w, r, h.HZTAuth.AuthorizeURL(flow), http.StatusFound)
}

func normalizedHZTEmail(identity hztauth.Identity) string {
	if identity.Email != nil {
		candidate := strings.ToLower(strings.TrimSpace(*identity.Email))
		if parsed, err := mail.ParseAddress(candidate); err == nil && parsed.Address == candidate {
			return candidate
		}
	}
	digest := sha256.Sum256([]byte(identity.ID))
	return "hzt-" + hex.EncodeToString(digest[:12]) + "@localhost.local"
}

func hztDisplayName(identity hztauth.Identity, email string) string {
	if name := strings.TrimSpace(identity.DisplayName); name != "" {
		return name
	}
	if username := strings.TrimSpace(identity.Username); username != "" {
		return username
	}
	return strings.Split(email, "@")[0]
}

func (h *Handler) hztWorkspaceRole(identity hztauth.Identity) string {
	if _, ok := hztAdminRoles[identity.Role]; ok {
		return "admin"
	}
	for _, role := range identity.Roles {
		if _, ok := hztAdminRoles[role.Slug]; ok {
			return "admin"
		}
	}
	return "member"
}

func (h *Handler) findOrCreateHZTUser(r *http.Request, identity hztauth.Identity) (db.User, error) {
	if h.DB == nil {
		return db.User{}, errors.New("database unavailable")
	}
	var mappedUserID pgtype.UUID
	err := h.DB.QueryRow(r.Context(),
		"SELECT user_id FROM hzt_external_identity WHERE external_subject = $1 LIMIT 1", identity.ID,
	).Scan(&mappedUserID)
	if err == nil {
		if user, getErr := h.Queries.GetUser(r.Context(), mappedUserID); getErr == nil {
			if updateErr := h.upsertHZTIdentity(r, identity, user.ID); updateErr != nil {
				return db.User{}, updateErr
			}
			return user, nil
		} else if !isNotFound(getErr) {
			return db.User{}, getErr
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, err
	}

	email := normalizedHZTEmail(identity)
	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if isNotFound(err) {
		user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{
			Name: hztDisplayName(identity, email), Email: email, AvatarUrl: pgtype.Text{},
		})
		if isUniqueViolation(err) {
			user, err = h.Queries.GetUserByEmail(r.Context(), email)
		}
	}
	if err != nil {
		return db.User{}, err
	}
	if err := h.upsertHZTIdentity(r, identity, user.ID); err != nil {
		return db.User{}, err
	}
	return user, nil
}

func (h *Handler) upsertHZTIdentity(r *http.Request, identity hztauth.Identity, userID pgtype.UUID) error {
	roles, err := json.Marshal(identity.Roles)
	if err != nil {
		return err
	}
	_, err = h.DB.Exec(r.Context(),
		`INSERT INTO hzt_external_identity
		   (external_subject, user_id, username_snapshot, email_snapshot, role_snapshot, roles_snapshot, last_verified_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now())
		 ON CONFLICT (external_subject)
		 DO UPDATE SET user_id = excluded.user_id,
		               username_snapshot = excluded.username_snapshot,
		               email_snapshot = excluded.email_snapshot,
		               role_snapshot = excluded.role_snapshot,
		               roles_snapshot = excluded.roles_snapshot,
		               last_verified_at = now(),
		               updated_at = now()`,
		identity.ID, userID, identity.Username, identity.Email, identity.Role, roles,
	)
	return err
}

func (h *Handler) syncHZTWorkspaceMembership(r *http.Request, userID pgtype.UUID, role string) error {
	if h.cfg.HZTDefaultWorkspace == "" {
		return nil
	}
	result, err := h.DB.Exec(r.Context(),
		`INSERT INTO member (workspace_id, user_id, role)
		 SELECT id, $1, $2 FROM workspace WHERE slug = $3
		 ON CONFLICT (workspace_id, user_id)
		 DO UPDATE SET role = CASE WHEN member.role = 'owner' THEN member.role ELSE excluded.role END`,
		userID, role, h.cfg.HZTDefaultWorkspace,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("HZT default workspace %q not found", h.cfg.HZTDefaultWorkspace)
	}
	return nil
}

func (h *Handler) HZTCallback(w http.ResponseWriter, r *http.Request) {
	if h.HZTAuth == nil || h.cfg.AuthProvider != "hzt_redirect" {
		writeError(w, http.StatusServiceUnavailable, "HZT login is not configured")
		return
	}
	flowCookie, err := r.Cookie(hztFlowCookieName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "HZT login flow cookie is missing")
		return
	}
	flow, err := h.HZTAuth.ParseFlow(flowCookie.Value, r.URL.Query().Get("state"), time.Now())
	if err != nil {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusBadRequest, "HZT login flow is invalid or expired")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusBadRequest, "HZT authorization code is missing")
		return
	}
	identity, err := h.HZTAuth.Exchange(r.Context(), code, flow.Verifier)
	if err != nil {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusUnauthorized, "HZT login verification failed")
		return
	}
	user, err := h.findOrCreateHZTUser(r, identity)
	if err != nil {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusInternalServerError, "failed to bind HZT identity")
		return
	}
	if err := h.syncHZTWorkspaceMembership(r, user.ID, h.hztWorkspaceRole(identity)); err != nil {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusInternalServerError, "failed to grant Multica workspace access")
		return
	}
	token, err := h.issueJWT(user)
	if err != nil {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusInternalServerError, "failed to create Multica session")
		return
	}
	if err := auth.SetAuthCookies(w, token); err != nil {
		clearHZTFlowCookie(w)
		writeError(w, http.StatusInternalServerError, "failed to create Multica session")
		return
	}
	clearHZTFlowCookie(w)
	setWebLoggedInMarker(w)
	destination := flow.Next
	if destination == "" {
		destination = "/login"
	}
	http.Redirect(w, r, destination, http.StatusFound)
}
