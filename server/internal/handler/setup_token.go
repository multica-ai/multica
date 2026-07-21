package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// SetupTokenTTL keeps copied setup commands useful long enough to run on a
	// remote machine without turning them into durable account credentials.
	SetupTokenTTL          = 30 * time.Minute
	setupTokenPrefixLength = 12
)

type SetupTokenResponse struct {
	ID                string  `json:"id"`
	Token             string  `json:"token,omitempty"`
	ExpiresAt         string  `json:"expires_at"`
	RedeemedAt        *string `json:"redeemed_at"`
	DaemonConnectedAt *string `json:"daemon_connected_at"`
	DaemonID          *string `json:"daemon_id"`
	RuntimeCount      int32   `json:"runtime_count"`
}

func setupTokenToResponse(row db.SetupToken) SetupTokenResponse {
	var daemonID *string
	if row.DaemonID.Valid && row.DaemonID.String != "" {
		value := row.DaemonID.String
		daemonID = &value
	}
	return SetupTokenResponse{
		ID:                uuidToString(row.ID),
		ExpiresAt:         timestampToString(row.ExpiresAt),
		RedeemedAt:        timestampToPtr(row.RedeemedAt),
		DaemonConnectedAt: timestampToPtr(row.DaemonConnectedAt),
		DaemonID:          daemonID,
		RuntimeCount:      row.RuntimeCount,
	}
}

// CreateSetupToken mints a short-lived, single-use credential for one
// workspace. The raw token is returned once and never persisted.
func (h *Handler) CreateSetupToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	rawToken, err := auth.GenerateSetupToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate setup token")
		return
	}
	prefix := rawToken
	if len(prefix) > setupTokenPrefixLength {
		prefix = prefix[:setupTokenPrefixLength]
	}

	row, err := h.Queries.CreateSetupToken(r.Context(), db.CreateSetupTokenParams{
		UserID:      member.UserID,
		WorkspaceID: member.WorkspaceID,
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
		ExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(SetupTokenTTL),
			Valid: true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create setup token")
		return
	}

	// Pruning is maintenance, not part of the successful mint contract.
	if err := h.Queries.DeleteExpiredSetupTokens(r.Context()); err != nil {
		slog.Warn("prune expired setup tokens failed", append(logger.RequestAttrs(r), "error", err)...)
	}

	response := setupTokenToResponse(row)
	response.Token = rawToken
	writeJSON(w, http.StatusCreated, response)
}

// GetSetupToken returns observable setup progress without ever returning the
// raw setup token again.
func (h *Handler) GetSetupToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	setupTokenID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "setupTokenId"), "setup token id")
	if !ok {
		return
	}

	row, err := h.Queries.GetSetupTokenForUser(r.Context(), db.GetSetupTokenForUserParams{
		ID:          setupTokenID,
		WorkspaceID: member.WorkspaceID,
		UserID:      member.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "setup token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get setup token")
		return
	}

	writeJSON(w, http.StatusOK, setupTokenToResponse(row))
}

type RedeemSetupTokenRequest struct {
	Token      string `json:"token"`
	DeviceName string `json:"device_name"`
	CLIVersion string `json:"cli_version"`
}

type RedeemSetupTokenResponse struct {
	Token          string `json:"token"`
	SetupSessionID string `json:"setup_session_id"`
	WorkspaceID    string `json:"workspace_id"`
	ExpiresAt      string `json:"expires_at"`
}

// RedeemSetupToken atomically consumes an mst_ token and mints the normal
// renewable 90-day PAT used by the CLI and daemon. It is intentionally the
// only unauthenticated endpoint that accepts setup credentials.
func (h *Handler) RedeemSetupToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req RedeemSetupTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if !strings.HasPrefix(req.Token, "mst_") || len(req.Token) > 128 {
		writeError(w, http.StatusGone, "setup token is invalid, expired, or already used")
		return
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "unknown device"
	}
	if len(deviceName) > 120 || len(req.CLIVersion) > 64 {
		writeError(w, http.StatusBadRequest, "device metadata is too long")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	setup, err := qtx.ConsumeSetupToken(r.Context(), auth.HashToken(req.Token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusGone, "setup token is invalid, expired, or already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}

	// Membership may have changed after the command was copied. Revalidate it
	// inside the same transaction before minting a durable credential.
	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      setup.UserID,
		WorkspaceID: setup.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusGone, "setup token is invalid, expired, or already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}

	rawPAT, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}
	patPrefix := rawPAT
	if len(patPrefix) > setupTokenPrefixLength {
		patPrefix = patPrefix[:setupTokenPrefixLength]
	}
	patName := fmt.Sprintf("CLI (%s)", deviceName)
	pat, err := qtx.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID:      setup.UserID,
		Name:        patName,
		TokenHash:   auth.HashToken(rawPAT),
		TokenPrefix: patPrefix,
		ExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(PATRenewExtension),
			Valid: true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}

	workspaceID := uuidToString(setup.WorkspaceID)
	userID := uuidToString(setup.UserID)
	h.publish(protocol.EventSetupProgress, workspaceID, "member", userID, map[string]any{
		"setup_session_id": uuidToString(setup.ID),
		"status":           "redeemed",
	})
	writeJSON(w, http.StatusOK, RedeemSetupTokenResponse{
		Token:          rawPAT,
		SetupSessionID: uuidToString(setup.ID),
		WorkspaceID:    workspaceID,
		ExpiresAt:      timestampToString(pat.ExpiresAt),
	})
}
