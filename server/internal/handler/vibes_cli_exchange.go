package handler

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/multica-ai/multica/server/internal/vibeshandoff"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var cliOpaqueValuePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type vibesCLIExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	ReceiverID   string `json:"receiver_id"`
	ReceiverURI  string `json:"receiver_uri"`
	Audience     string `json:"audience"`
	DeviceName   string `json:"device_name"`
}

type vibesCLIExchangeResponse struct {
	Token       string `json:"token"`
	WorkspaceID string `json:"workspace_id"`
}

func (h *Handler) VIBESCLIExchange(w http.ResponseWriter, r *http.Request) {
	if h.TagAccessGate == nil || h.TxStarter == nil {
		writeError(w, http.StatusServiceUnavailable, "VIBES CLI exchange unavailable")
		return
	}
	client, err := vibeshandoff.NewCLIClient(h.cfg.VIBESCLIConsumeURL, h.cfg.VIBESCLIServiceSecret)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "VIBES CLI exchange unavailable")
		return
	}
	var request vibesCLIExchangeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Audience != vibeshandoff.CLIAudience ||
		!cliOpaqueValuePattern.MatchString(request.Code) || !cliOpaqueValuePattern.MatchString(request.CodeVerifier) ||
		!cliOpaqueValuePattern.MatchString(request.ReceiverID) || strings.TrimSpace(request.ReceiverURI) == "" ||
		len(request.ReceiverURI) > 2048 || len(request.DeviceName) == 0 || len(request.DeviceName) > 128 {
		writeError(w, http.StatusBadRequest, "invalid CLI exchange")
		return
	}
	identity, err := client.ConsumeCLI(r.Context(), vibeshandoff.CLIConsumeRequest{
		SchemaVersion: vibeshandoff.CLISchemaVersion, Code: request.Code, CodeVerifier: request.CodeVerifier, ReceiverID: request.ReceiverID,
		ReceiverURI: request.ReceiverURI, Audience: request.Audience,
	})
	if err != nil || !identity.SessionExpiresAt.After(time.Now()) {
		writeError(w, http.StatusUnauthorized, "VIBES CLI authorization rejected")
		return
	}
	tagSessionID := tagaccess.CLITagSessionID(identity.UserID, identity.SessionID, request.ReceiverID)
	grantExpiry := time.Now().Add(PATRenewExtension)
	if identity.SessionExpiresAt.Before(grantExpiry) {
		grantExpiry = identity.SessionExpiresAt
	}
	grant := tagaccess.SessionGrant{
		TagSessionID: tagSessionID, VIBESSessionID: identity.SessionID, VIBESUserID: identity.UserID,
		WorkspaceID: identity.WorkspaceID, AccountEpoch: identity.AccountEpoch,
		SessionWorkspaceGeneration: identity.SessionWorkspaceGeneration,
		MembershipGeneration:       identity.MembershipGeneration, AuthorityVersion: identity.AuthorityVersion,
		SessionExpiresAt: identity.SessionExpiresAt, GrantExpiresAt: grantExpiry,
	}
	if err := h.TagAccessGate.GrantSession(r.Context(), grant); err != nil {
		writeError(w, http.StatusForbidden, "VIBES CLI authorization denied")
		return
	}
	decision := h.TagAccessGate.Authorize(r.Context(), tagaccess.AccessRequest{
		TagSessionID: tagSessionID, VIBESSessionID: identity.SessionID, VIBESUserID: identity.UserID,
		WorkspaceID: identity.WorkspaceID, AccountEpoch: identity.AccountEpoch,
		SessionWorkspaceGeneration: identity.SessionWorkspaceGeneration,
		MembershipGeneration:       identity.MembershipGeneration, AuthorityVersion: identity.AuthorityVersion,
	})
	if !decision.Allowed {
		writeError(w, http.StatusForbidden, "VIBES CLI authorization denied")
		return
	}
	user, err := h.mirrorVIBESIdentity(r.Context(), identity.Identity)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "VIBES identity mirror unavailable")
		return
	}
	var multicaWorkspaceID pgtype.UUID
	if err := h.DB.QueryRow(r.Context(), `SELECT multica_workspace_id FROM vibes_workspace_mirror WHERE vibes_workspace_id = $1`, identity.WorkspaceID).Scan(&multicaWorkspaceID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "VIBES identity mirror unavailable")
		return
	}
	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CLI credential")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to create CLI credential")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := h.Queries.WithTx(tx)
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	pat, err := queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID: user.ID, Name: request.DeviceName, TokenHash: auth.HashToken(rawToken), TokenPrefix: prefix,
		ExpiresAt: pgtype.Timestamptz{Time: grantExpiry, Valid: true},
	})
	if err == nil {
		if identity.AccountEpoch > math.MaxInt64 || identity.SessionWorkspaceGeneration > math.MaxInt64 || identity.AuthorityVersion > math.MaxInt64 || identity.MembershipGeneration > math.MaxInt64 {
			err = errors.New("VIBES authority counter out of range")
		} else {
			_, err = queries.CreateVIBESCLIPATBinding(r.Context(), db.CreateVIBESCLIPATBindingParams{
				PatID: pat.ID, MulticaUserID: user.ID, MulticaWorkspaceID: multicaWorkspaceID,
				VibesUserID: identity.UserID, VibesSessionID: identity.SessionID,
				VibesWorkspaceID: identity.WorkspaceID, TagSessionID: tagSessionID,
				AccountEpoch: int64(identity.AccountEpoch), SessionWorkspaceGeneration: int64(identity.SessionWorkspaceGeneration),
				AuthorityVersion: int64(identity.AuthorityVersion), MembershipGeneration: int64(identity.MembershipGeneration),
				SessionExpiresAt: pgtype.Timestamptz{Time: identity.SessionExpiresAt, Valid: true},
			})
		}
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to create CLI credential")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, vibesCLIExchangeResponse{Token: rawToken, WorkspaceID: uuidToString(multicaWorkspaceID)})
}
