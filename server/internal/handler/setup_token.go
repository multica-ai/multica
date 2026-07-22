package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SetupTokenTTL bounds how long a minted setup token stays redeemable. Short by
// design: the user pastes `multica setup --token ...` within a minute or two of
// the dialog opening. Long enough to switch to a server terminal and paste,
// short enough that a value left in shell history or the clipboard is dead
// almost immediately. Mirrors GitHub runner registration-token windows.
const SetupTokenTTL = 15 * time.Minute

// setupTokenPATExpiryDays is the lifetime of the PAT minted on exchange. Kept
// in lockstep with the browser login flow (cmd_auth.go issues a 90-day PAT) so
// a machine connected via `setup --token` behaves identically afterwards —
// same renewal window, same listing, same revoke.
const setupTokenPATExpiryDays = 90

type MintSetupTokenRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type MintSetupTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// MintSetupToken issues a short-lived, single-use setup token for the signed-in
// user, scoped to the workspace the connect dialog is open in. The web dialog
// renders it into `multica setup --token <token>` so a headless machine can
// connect with one pasted command instead of a browser round-trip (MUL-5112).
func (h *Handler) MintSetupToken(w http.ResponseWriter, r *http.Request) {
	var req MintSetupTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Membership is the authorization gate: the caller must belong to the
	// workspace they scope the token to. That keeps the redeem-time
	// setup_token:redeemed event from being fanned into a workspace the minter
	// can't otherwise reach. member.WorkspaceID is the resolved UUID we write.
	member, ok := h.requireWorkspaceMember(w, r, req.WorkspaceID, "workspace not found")
	if !ok {
		return
	}

	// Opportunistic reaper — keeps the table bounded without a scheduled GC.
	// Non-fatal: a failed cleanup must never block minting.
	if err := h.Queries.DeleteExpiredSetupTokens(r.Context()); err != nil {
		slog.Warn("setup token: failed to reap expired rows", "error", err)
	}

	rawToken, err := auth.GenerateSetupToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}

	row, err := h.Queries.CreateSetupToken(r.Context(), db.CreateSetupTokenParams{
		UserID:      member.UserID,
		WorkspaceID: member.WorkspaceID,
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(SetupTokenTTL), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create setup token")
		return
	}

	writeJSON(w, http.StatusCreated, MintSetupTokenResponse{
		Token:     rawToken,
		ExpiresAt: timestampToString(row.ExpiresAt),
	})
}

type ExchangeSetupTokenRequest struct {
	Token string `json:"token"`
	// Name labels the PAT that gets minted — the CLI passes its hostname so the
	// resulting token is recognisable in Settings → Tokens. Optional.
	Name string `json:"name"`
}

type ExchangeSetupTokenResponse struct {
	Token string `json:"token"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ExchangeSetupToken redeems a setup token for a normal 90-day mul_ PAT. It is
// deliberately public (no bearer auth): the mst_ token in the body IS the
// credential, exactly like the autopilot webhook path. Single-use is enforced
// atomically inside RedeemSetupToken, so an expired, already-redeemed, or
// unknown token all collapse to one opaque 401 that reveals nothing.
func (h *Handler) ExchangeSetupToken(w http.ResponseWriter, r *http.Request) {
	var req ExchangeSetupTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(req.Token)
	if !strings.HasPrefix(token, auth.SetupTokenPrefix) {
		writeError(w, http.StatusBadRequest, "invalid setup token format")
		return
	}

	redeemed, err := h.Queries.RedeemSetupToken(r.Context(), auth.HashToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "setup token is invalid or expired")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to redeem setup token")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), redeemed.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	patName := strings.TrimSpace(req.Name)
	if patName == "" {
		patName = "CLI"
	}
	rawPAT, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	patPrefix := rawPAT
	if len(patPrefix) > 12 {
		patPrefix = patPrefix[:12]
	}
	if _, err := h.Queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID:      redeemed.UserID,
		Name:        patName,
		TokenHash:   auth.HashToken(rawPAT),
		TokenPrefix: patPrefix,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(setupTokenPATExpiryDays * 24 * time.Hour), Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	// Confirm to the waiting dialog that the pasted command landed — this fires
	// the instant the CLI redeems, ahead of the daemon:register that follows
	// once the daemon boots and registers its runtimes.
	h.publish(protocol.EventSetupTokenRedeemed, uuidToString(redeemed.WorkspaceID), "member", uuidToString(redeemed.UserID), map[string]any{
		"user_id": uuidToString(redeemed.UserID),
	})

	writeJSON(w, http.StatusOK, ExchangeSetupTokenResponse{
		Token: rawPAT,
		Name:  user.Name,
		Email: user.Email,
	})
}
