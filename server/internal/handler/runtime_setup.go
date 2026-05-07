package handler

// CEREBRO-PATCH(runtime-setup-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Setup tokens have their own short prefix so the install script can validate
// shape client-side and so they're easy to tell apart from PATs in logs.
const (
	setupTokenPrefix = "mst_"
	setupTokenTTL    = 30 * time.Minute
)

type CreateRuntimeSetupTokenRequest struct {
	DeviceLabel string `json:"device_label"`
}

type CreateRuntimeSetupTokenResponse struct {
	Token          string `json:"token"`
	ExpiresAt      string `json:"expires_at"`
	InstallCommand string `json:"install_command"`
	ServerURL      string `json:"server_url"`
	WorkspaceID    string `json:"workspace_id"`
	WorkspaceSlug  string `json:"workspace_slug"`
}

func (h *Handler) CreateRuntimeSetupToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace context required")
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	var req CreateRuntimeSetupTokenRequest
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	rawToken, err := generateSetupToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	expiresAt := pgtype.Timestamptz{Time: time.Now().Add(setupTokenTTL), Valid: true}

	if _, err := h.Queries.CreateRuntimeSetupToken(r.Context(), db.CreateRuntimeSetupTokenParams{
		TokenHash:    auth.HashToken(rawToken),
		TokenPrefix:  rawToken[:12],
		UserID:       parseUUID(userID),
		WorkspaceID:  parseUUID(workspaceID),
		DeviceLabel:  strings.TrimSpace(req.DeviceLabel),
		ExpiresAt:    expiresAt,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create setup token")
		return
	}

	serverURL := publicServerURL(r)

	writeJSON(w, http.StatusCreated, CreateRuntimeSetupTokenResponse{
		Token:          rawToken,
		ExpiresAt:      timestampToString(expiresAt),
		InstallCommand: fmt.Sprintf("curl -fsSL %s/install-runtime.sh | bash -s -- --token %s", serverURL, rawToken),
		ServerURL:      serverURL,
		WorkspaceID:    workspaceID,
		WorkspaceSlug:  ws.Slug,
	})
}

type ExchangeRuntimeSetupTokenRequest struct {
	Token       string `json:"token"`
	DeviceLabel string `json:"device_label"`
}

type ExchangeRuntimeSetupTokenResponse struct {
	ServerURL     string `json:"server_url"`
	AppURL        string `json:"app_url"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceSlug string `json:"workspace_slug"`
	WorkspaceName string `json:"workspace_name"`
	UserID        string `json:"user_id"`
	DaemonToken   string `json:"daemon_token"`
}

func (h *Handler) ExchangeRuntimeSetupToken(w http.ResponseWriter, r *http.Request) {
	var req ExchangeRuntimeSetupTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	setup, err := h.Queries.GetRuntimeSetupTokenByHash(r.Context(), auth.HashToken(req.Token))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid setup token")
		return
	}
	if setup.UsedAt.Valid {
		writeError(w, http.StatusGone, "setup token already used")
		return
	}
	if setup.ExpiresAt.Valid && time.Now().After(setup.ExpiresAt.Time) {
		writeError(w, http.StatusGone, "setup token expired")
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), setup.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace lookup failed")
		return
	}

	patRaw, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate PAT")
		return
	}
	patName := strings.TrimSpace(req.DeviceLabel)
	if patName == "" {
		patName = strings.TrimSpace(setup.DeviceLabel)
	}
	if patName == "" {
		patName = "Runtime daemon"
	}
	pat, err := h.Queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID:      setup.UserID,
		Name:        patName,
		TokenHash:   auth.HashToken(patRaw),
		TokenPrefix: patRaw[:12],
		ExpiresAt:   pgtype.Timestamptz{},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue daemon token")
		return
	}

	if err := h.Queries.ConsumeRuntimeSetupToken(r.Context(), db.ConsumeRuntimeSetupTokenParams{
		ID:        setup.ID,
		UsedPatID: pgtype.UUID{Bytes: pat.ID.Bytes, Valid: true},
	}); err != nil {
		// Token already consumed by a concurrent caller — refuse to leak the PAT.
		_, _ = h.Queries.RevokePersonalAccessToken(r.Context(), db.RevokePersonalAccessTokenParams{
			ID:     pat.ID,
			UserID: setup.UserID,
		})
		writeError(w, http.StatusGone, "setup token already used")
		return
	}

	writeJSON(w, http.StatusOK, ExchangeRuntimeSetupTokenResponse{
		ServerURL:     publicServerURL(r),
		AppURL:        publicAppURL(r),
		WorkspaceID:   uuidToString(ws.ID),
		WorkspaceSlug: ws.Slug,
		WorkspaceName: ws.Name,
		UserID:        uuidToString(setup.UserID),
		DaemonToken:   patRaw,
	})
}

// ServeInstallRuntimeScript returns the one-line installer bash script.
// The script is templated with the public server URL so it knows where to
// call /api/runtime-setup/exchange.
func (h *Handler) ServeInstallRuntimeScript(w http.ResponseWriter, r *http.Request) {
	serverURL := publicServerURL(r)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, renderInstallScript(serverURL))
}

func generateSetupToken() (string, error) {
	raw, err := auth.GeneratePATToken()
	if err != nil {
		return "", err
	}
	// Strip the "mul_" prefix from the random PAT helper and replace with our setup-specific prefix.
	return setupTokenPrefix + strings.TrimPrefix(raw, "mul_"), nil
}

func publicServerURL(r *http.Request) string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_APP_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")); v != "" {
		return strings.TrimRight(v, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func publicAppURL(r *http.Request) string { return publicServerURL(r) }
