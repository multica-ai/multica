// CEREBRO-PATCH(embedded-chat): FIR-2835 verified on-behalf-of API chat boundary.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/embeddedchat"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	embeddedChatSource = "finance"
	embeddedChatFlag   = "cerebro_embedded_chat"
)

var embeddedVerifierCache struct {
	sync.Mutex
	raw      string
	verifier *embeddedchat.Verifier
}

func configuredEmbeddedVerifier() (*embeddedchat.Verifier, error) {
	raw := os.Getenv("CEREBRO_EMBEDDED_CHAT_OIDC_PROVIDERS")
	embeddedVerifierCache.Lock()
	defer embeddedVerifierCache.Unlock()
	if embeddedVerifierCache.verifier != nil && embeddedVerifierCache.raw == raw {
		return embeddedVerifierCache.verifier, nil
	}
	providers, err := embeddedchat.ParseProviders(raw)
	if err != nil {
		return nil, err
	}
	embeddedVerifierCache.raw = raw
	embeddedVerifierCache.verifier = embeddedchat.NewVerifier(providers)
	return embeddedVerifierCache.verifier, nil
}

func (h *Handler) MountEmbeddedChatRoutes(r chi.Router) {
	r.Route("/api/cerebro/embedded-chat", func(r chi.Router) {
		r.Use(h.requireEmbeddedChatIdentity)
		r.Post("/sessions", h.CreateEmbeddedChatSession)
		r.Route("/sessions/{sessionId}", func(r chi.Router) {
			r.Use(h.requireEmbeddedChatSession)
			r.Get("/messages", h.ListChatMessages)
			r.Post("/messages", h.SendChatMessage)
			r.Get("/stream", h.StreamChatSessionRun)
		})
	})
}

func (h *Handler) requireEmbeddedChatIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifier, err := configuredEmbeddedVerifier()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "embedded chat is not configured")
			return
		}
		identity, err := verifier.Verify(r.Context(), embeddedChatSource,
			r.Header.Get(embeddedchat.HeaderUserAssertion))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "verified Google identity required")
			return
		}
		user, err := h.Queries.GetUserByEmail(r.Context(), identity.Email)
		if err != nil {
			writeError(w, http.StatusForbidden, "verified user is not a Multica member")
			return
		}
		workspaceID := parseUUID(ctxWorkspaceID(r.Context()))
		member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID: user.ID, WorkspaceID: workspaceID,
		})
		if err != nil || !h.embeddedChatFeatureEnabled(r, workspaceID, user.ID) {
			writeError(w, http.StatusForbidden, "embedded chat is not enabled for this member")
			return
		}
		r.Header.Set("X-User-ID", uuidToString(user.ID))
		ctx := middleware.SetMemberContext(r.Context(), uuidToString(workspaceID), member)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) embeddedChatFeatureEnabled(r *http.Request, workspaceID, userID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	rows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(r.Context(), workspaceID)
	if err != nil {
		return false
	}
	var enabled, set, locked bool
	for _, row := range rows {
		if row.FlagKey == embeddedChatFlag {
			enabled, set, locked = row.Enabled, true, row.Locked
			break
		}
	}
	if locked {
		return enabled
	}
	personal, err := h.CerebroQueries.GetCerebroFeatureFlag(r.Context(), cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID, UserID: userID, FlagKey: embeddedChatFlag,
	})
	if err == nil {
		return personal
	}
	return errors.Is(err, pgx.ErrNoRows) && set && enabled
}

func (h *Handler) requireEmbeddedChatSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var exists bool
		err := h.DB.QueryRow(r.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM cerebro_chat_session_context
				WHERE chat_session_id = $1 AND kind = 'api' AND source = $2
			)
		`, chi.URLParam(r, "sessionId"), embeddedChatSource).Scan(&exists)
		if err != nil || !exists {
			writeError(w, http.StatusNotFound, "embedded chat session not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) CreateEmbeddedChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	var req CreateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	workspaceUUID := parseUUID(workspaceID)
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}
	if !h.canAccessPrivateAgent(r.Context(), agent, "member", userID, workspaceID) {
		writeError(w, http.StatusForbidden, "cannot start chat with private agent")
		return
	}
	if !h.cerebroRequireAgentAccess(w, r, workspaceID, agent.ID, agent.OwnerID) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start embedded chat")
		return
	}
	defer tx.Rollback(r.Context())
	queries := db.New(tx)
	session, err := queries.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: workspaceUUID, AgentID: agentID, CreatorID: parseUUID(userID), Title: req.Title,
	})
	if err == nil {
		_, err = tx.Exec(r.Context(), `
			INSERT INTO cerebro_chat_session_context (chat_session_id, kind, source)
			VALUES ($1, 'api', $2)
		`, session.ID, embeddedChatSource)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create embedded chat session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create embedded chat session")
		return
	}
	writeJSON(w, http.StatusCreated, h.chatSessionResponse(r.Context(), session))
}
