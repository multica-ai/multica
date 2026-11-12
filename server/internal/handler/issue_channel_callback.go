package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueChannelCallbackDeliveryResponse is one ledger entry: a single event
// sent (or not) to the callback's chat thread.
type IssueChannelCallbackDeliveryResponse struct {
	Event             string `json:"event"`
	Status            string `json:"status"`
	ProviderMessageID string `json:"provider_message_id,omitempty"`
	Error             string `json:"error,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// IssueChannelCallbackResponse is a chat thread the issue reports back to,
// with its delivery history.
type IssueChannelCallbackResponse struct {
	ID           string                                 `json:"id"`
	ChannelType  string                                 `json:"channel_type"`
	ChatID       string                                 `json:"chat_id"`
	ChatType     string                                 `json:"chat_type"`
	ThreadID     string                                 `json:"thread_id,omitempty"`
	RegisteredBy string                                 `json:"registered_by"`
	CreatedAt    string                                 `json:"created_at"`
	RevokedAt    *string                                `json:"revoked_at"`
	Deliveries   []IssueChannelCallbackDeliveryResponse `json:"deliveries"`
}

func issueChannelCallbackToResponse(cb db.IssueChannelCallback, deliveries []db.IssueChannelCallbackDelivery) IssueChannelCallbackResponse {
	resp := IssueChannelCallbackResponse{
		ID:           uuidToString(cb.ID),
		ChannelType:  cb.ChannelType,
		ChatID:       cb.ChatID,
		ChatType:     cb.ChatType,
		ThreadID:     cb.ThreadID,
		RegisteredBy: uuidToString(cb.RegisteredBy),
		CreatedAt:    timestampToString(cb.CreatedAt),
		Deliveries:   make([]IssueChannelCallbackDeliveryResponse, 0, len(deliveries)),
	}
	if cb.RevokedAt.Valid {
		revoked := timestampToString(cb.RevokedAt)
		resp.RevokedAt = &revoked
	}
	for _, d := range deliveries {
		resp.Deliveries = append(resp.Deliveries, IssueChannelCallbackDeliveryResponse{
			Event:             d.Event,
			Status:            d.Status,
			ProviderMessageID: d.ProviderMessageID,
			Error:             d.Error,
			CreatedAt:         timestampToString(d.CreatedAt),
			UpdatedAt:         timestampToString(d.UpdatedAt),
		})
	}
	return resp
}

// ListIssueChannelCallbacks returns the chat threads the issue reports back
// to, revoked ones included, each with its delivery history.
func (h *Handler) ListIssueChannelCallbacks(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	callbacks, err := h.Queries.ListIssueChannelCallbacks(r.Context(), db.ListIssueChannelCallbacksParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channel callbacks")
		return
	}
	ids := make([]pgtype.UUID, 0, len(callbacks))
	for _, cb := range callbacks {
		ids = append(ids, cb.ID)
	}
	deliveries, err := h.Queries.ListIssueChannelCallbackDeliveries(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channel callbacks")
		return
	}
	byCallback := make(map[pgtype.UUID][]db.IssueChannelCallbackDelivery, len(callbacks))
	for _, d := range deliveries {
		byCallback[d.CallbackID] = append(byCallback[d.CallbackID], d)
	}
	resp := make([]IssueChannelCallbackResponse, 0, len(callbacks))
	for _, cb := range callbacks {
		resp = append(resp, issueChannelCallbackToResponse(cb, byCallback[cb.ID]))
	}
	writeJSON(w, http.StatusOK, resp)
}

// RevokeIssueChannelCallback stops future sends to one chat thread. Any member
// who can see the issue may revoke: it only ever narrows what leaves Multica.
func (h *Handler) RevokeIssueChannelCallback(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	callbackID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "callbackID"), "callback id")
	if !ok {
		return
	}
	cb, err := h.Queries.RevokeIssueChannelCallback(r.Context(), db.RevokeIssueChannelCallbackParams{
		ID:          callbackID,
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel callback not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke channel callback")
		return
	}
	deliveries, err := h.Queries.ListIssueChannelCallbackDeliveries(r.Context(), []pgtype.UUID{cb.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke channel callback")
		return
	}
	writeJSON(w, http.StatusOK, issueChannelCallbackToResponse(cb, deliveries))
}
