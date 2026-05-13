package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const googleStateTTL = 15 * time.Minute

type StartGoogleBindingRequest struct {
	NextPath    string `json:"next_path"`
	RedirectURI string `json:"redirect_uri"`
}

type StartGoogleBindingResponse struct {
	AuthURL string `json:"auth_url"`
}

type CompleteGoogleBindingRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type CompleteGoogleBindingResponse struct {
	Binding  NotificationBindingResponse `json:"binding"`
	NextPath *string                     `json:"next_path"`
}

func (h *Handler) StartMyGoogleBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	cfg, err := notifyutil.LoadGoogleConfig()
	if err != nil {
		if errors.Is(err, notifyutil.ErrGoogleNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "Google binding is not configured")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load Google configuration")
		return
	}

	var req StartGoogleBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	redirectURI := sanitizeOAuthCallbackRedirectURI(req.RedirectURI)
	if redirectURI == "" {
		redirectURI = cfg.RedirectURL()
	}

	state, err := notifyutil.BuildGoogleBindingState(notifyutil.GoogleBindingState{
		UserID:      userID,
		NextPath:    sanitizeRelativePath(req.NextPath),
		RedirectURI: redirectURI,
		IssuedAt:    time.Now().UTC().Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start Google binding")
		return
	}

	writeJSON(w, http.StatusOK, StartGoogleBindingResponse{
		AuthURL: cfg.AuthorizationURLWithRedirectURI(state, redirectURI),
	})
}

func (h *Handler) CompleteMyGoogleBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	h.completeGoogleBinding(w, r, &userID)
}

func (h *Handler) CompleteGoogleBindingByState(w http.ResponseWriter, r *http.Request) {
	h.completeGoogleBinding(w, r, nil)
}

func (h *Handler) completeGoogleBinding(w http.ResponseWriter, r *http.Request, currentUserID *string) {
	var req CompleteGoogleBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.State) == "" {
		writeError(w, http.StatusBadRequest, "code and state are required")
		return
	}

	state, err := notifyutil.ParseGoogleBindingState(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid Google callback state")
		return
	}
	if currentUserID != nil && state.UserID != *currentUserID {
		writeError(w, http.StatusForbidden, "Google callback state does not match the current user")
		return
	}
	userID := state.UserID
	if time.Since(time.Unix(state.IssuedAt, 0)) > googleStateTTL {
		writeError(w, http.StatusBadRequest, "Google callback state has expired")
		return
	}

	cfg, err := notifyutil.LoadGoogleConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Google configuration")
		return
	}
	redirectURI := sanitizeOAuthCallbackRedirectURI(state.RedirectURI)
	if redirectURI == "" {
		redirectURI = cfg.RedirectURL()
	}

	gToken, err := exchangeGoogleCode(r.Context(), req.Code, cfg.ClientID, cfg.ClientSecret, redirectURI)
	if err != nil {
		var statusErr *googleOAuthStatusError
		if errors.As(err, &statusErr) {
			writeError(w, http.StatusBadGateway, "Google token exchange failed")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to exchange code with Google")
		return
	}

	gUser, err := fetchGoogleUserInfo(r.Context(), gToken.AccessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch user info from Google")
		return
	}

	externalUserID := gUser.ID
	if externalUserID == "" {
		externalUserID = gUser.Email
	}
	if externalUserID == "" {
		writeError(w, http.StatusBadGateway, "Google user info missing identifiers")
		return
	}

	displayName := gUser.Name
	if displayName == "" {
		displayName = gUser.Email
	}

	binding, err := h.Queries.UpsertExternalAccountBinding(r.Context(), db.UpsertExternalAccountBindingParams{
		UserID:                parseUUID(userID),
		Provider:              "google",
		ExternalUserID:        externalUserID,
		DisplayName:           strToText(displayName),
		AccessTokenEncrypted:  pgtype.Text{},
		RefreshTokenEncrypted: pgtype.Text{},
		TokenExpiresAt:        pgtype.Timestamptz{},
		Status:                "active",
		Metadata:              []byte("{}"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist Google binding")
		return
	}

	resp := notificationBindingsToResponse([]db.ExternalAccountBinding{binding})
	writeJSON(w, http.StatusOK, CompleteGoogleBindingResponse{
		Binding: resp[0],
		NextPath: func() *string {
			if next := sanitizeRelativePath(state.NextPath); next != "" {
				return &next
			}
			return nil
		}(),
	})
}
