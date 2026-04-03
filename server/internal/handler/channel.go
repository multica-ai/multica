package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- Response types ---

type ChannelResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	CreatedAt string `json:"created_at"`
}

type IssueChannelResponse struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	HasThread bool   `json:"has_thread"`
	CreatedAt string `json:"created_at"`
}

type ChannelMessageResponse struct {
	ID         string `json:"id"`
	Direction  string `json:"direction"`
	Content    string `json:"content"`
	SenderType string `json:"sender_type"`
	CreatedAt  string `json:"created_at"`
}

// --- Channel CRUD ---

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	var req struct {
		Name     string          `json:"name"`
		Provider string          `json:"provider"`
		Config   json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Provider == "" {
		writeError(w, http.StatusBadRequest, "name and provider are required")
		return
	}

	// Validate provider config.
	provider, ok := channel.GetProvider(req.Provider)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported provider: "+req.Provider)
		return
	}
	if err := provider.ValidateConfig(r.Context(), req.Config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid config: "+err.Error())
		return
	}

	ch, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Provider:    req.Provider,
		Config:      req.Config,
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	writeJSON(w, http.StatusCreated, channelToResponse(ch))
}

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	channels, err := h.Queries.ListChannelsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	resp := make([]ChannelResponse, len(channels))
	for i, ch := range channels {
		resp[i] = channelToResponse(ch)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	// Verify channel belongs to workspace.
	if _, err := h.Queries.GetChannelInWorkspace(r.Context(), db.GetChannelInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	var req struct {
		Name   *string          `json:"name"`
		Config *json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ch, err := h.Queries.UpdateChannel(r.Context(), db.UpdateChannelParams{
		ID:     parseUUID(id),
		Name:   ptrToText(req.Name),
		Config: ptrToRawJSON(req.Config),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}

	writeJSON(w, http.StatusOK, channelToResponse(ch))
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	if _, err := h.Queries.GetChannelInWorkspace(r.Context(), db.GetChannelInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	if err := h.Queries.DeleteChannel(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Issue-channel assignment ---

func (h *Handler) AssignChannel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	var req struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ic, err := h.Queries.AssignChannelToIssue(r.Context(), db.AssignChannelToIssueParams{
		IssueID:   parseUUID(issueID),
		ChannelID: parseUUID(req.ChannelID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "channel already assigned")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to assign channel")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": uuidToString(ic.ID)})
}

func (h *Handler) UnassignChannel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	channelID := chi.URLParam(r, "channelId")

	if err := h.Queries.UnassignChannelFromIssue(r.Context(), db.UnassignChannelFromIssueParams{
		IssueID:   parseUUID(issueID),
		ChannelID: parseUUID(channelID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unassign channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListIssueChannels(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	rows, err := h.Queries.ListIssueChannels(r.Context(), parseUUID(issueID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue channels")
		return
	}

	resp := make([]IssueChannelResponse, len(rows))
	for i, row := range rows {
		resp[i] = IssueChannelResponse{
			ID:        uuidToString(row.ID),
			IssueID:   uuidToString(row.IssueID),
			ChannelID: uuidToString(row.ChannelID),
			Name:      row.ChannelName,
			Provider:  row.ChannelProvider,
			HasThread: row.ThreadRef.Valid && row.ThreadRef.String != "",
			CreatedAt: timestampToString(row.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Agent interaction ---

func (h *Handler) ChannelAsk(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	var req struct {
		Question string `json:"question"`
		AgentID  string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}

	msgID, err := h.ChannelService.AskQuestion(r.Context(), issueID, req.AgentID, req.Question)
	if err != nil {
		slog.Error("channel ask failed", "issue_id", issueID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send question: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message_id": msgID})
}

func (h *Handler) ChannelPoll(w http.ResponseWriter, r *http.Request) {
	messageID := chi.URLParam(r, "id")

	response, found, err := h.ChannelService.GetResponse(r.Context(), messageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to poll response")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"found":    found,
		"response": response,
	})
}

func (h *Handler) ChannelHistory(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	messages, err := h.ChannelService.GetConversationHistory(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get history")
		return
	}

	resp := make([]ChannelMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = ChannelMessageResponse{
			ID:         uuidToString(m.ID),
			Direction:  m.Direction,
			Content:    m.Content,
			SenderType: m.SenderType,
			CreatedAt:  timestampToString(m.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Slack webhook ---

func (h *Handler) SlackWebhook(w http.ResponseWriter, r *http.Request) {
	signingSecret := os.Getenv("SLACK_SIGNING_SECRET")

	eventType, msg, challenge, err := channel.ParseSlackWebhook(r, signingSecret)
	if err != nil {
		slog.Warn("slack webhook: parse error", "error", err)
		writeError(w, http.StatusBadRequest, "invalid webhook")
		return
	}

	if eventType == "url_verification" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
		return
	}

	if msg != nil {
		if err := h.ChannelService.HandleInboundMessage(
			r.Context(), "slack", msg.ChannelID, msg.ThreadRef, msg.Text, msg.ExternalID, msg.SenderRef,
		); err != nil {
			slog.Warn("slack webhook: handle inbound failed", "error", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// --- Helpers ---

func channelToResponse(ch db.Channel) ChannelResponse {
	return ChannelResponse{
		ID:        uuidToString(ch.ID),
		Name:      ch.Name,
		Provider:  ch.Provider,
		CreatedAt: timestampToString(ch.CreatedAt),
	}
}

func ptrToRawJSON(p *json.RawMessage) []byte {
	if p == nil {
		return nil
	}
	return *p
}
