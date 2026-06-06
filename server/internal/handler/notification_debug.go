package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type NotificationDebugEventResponse struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	RecipientUserID string          `json:"recipient_user_id"`
	Type            string          `json:"type"`
	Severity        string          `json:"severity"`
	IssueID         *string         `json:"issue_id"`
	CommentID       *string         `json:"comment_id"`
	ActorType       *string         `json:"actor_type"`
	ActorID         *string         `json:"actor_id"`
	Title           string          `json:"title"`
	Body            *string         `json:"body"`
	Link            *string         `json:"link"`
	Details         json.RawMessage `json:"details"`
	CreatedAt       string          `json:"created_at"`
}

type NotificationDebugDeliveryResponse struct {
	ID              string          `json:"id"`
	Channel         string          `json:"channel"`
	Status          string          `json:"status"`
	AttemptCount    int32           `json:"attempt_count"`
	LastError       *string         `json:"last_error"`
	PayloadSnapshot json.RawMessage `json:"payload_snapshot"`
	SentAt          *string         `json:"sent_at"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	TargetType      *string         `json:"target_type"`
	TargetID        *string         `json:"target_id"`
}

type NotificationDebugRowResponse struct {
	NotificationEvent NotificationDebugEventResponse     `json:"notification_event"`
	Delivery          *NotificationDebugDeliveryResponse `json:"delivery"`
}

type ListNotificationDebugRowsResponse struct {
	Rows  []NotificationDebugRowResponse `json:"rows"`
	Total int                            `json:"total"`
}

func optionalUUIDQueryParam(w http.ResponseWriter, value string, fieldName string) (pgtype.UUID, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, value, fieldName)
}

func optionalTextQueryParam(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func notificationDebugRowToResponse(row db.ListNotificationDebugRowsRow) NotificationDebugRowResponse {
	resp := NotificationDebugRowResponse{
		NotificationEvent: NotificationDebugEventResponse{
			ID:              uuidToString(row.NotificationEventID),
			WorkspaceID:     uuidToString(row.WorkspaceID),
			RecipientUserID: uuidToString(row.RecipientUserID),
			Type:            row.Type,
			Severity:        row.Severity,
			IssueID:         uuidToPtr(row.IssueID),
			CommentID:       uuidToPtr(row.CommentID),
			ActorType:       textToPtr(row.ActorType),
			ActorID:         uuidToPtr(row.ActorID),
			Title:           row.Title,
			Body:            textToPtr(row.Body),
			Link:            textToPtr(row.Link),
			Details:         json.RawMessage(row.Details),
			CreatedAt:       timestampToString(row.EventCreatedAt),
		},
	}
	if row.DeliveryID.Valid {
		resp.Delivery = &NotificationDebugDeliveryResponse{
			ID:              uuidToString(row.DeliveryID),
			Channel:         row.Channel.String,
			Status:          row.Status.String,
			AttemptCount:    row.AttemptCount.Int32,
			LastError:       textToPtr(row.LastError),
			PayloadSnapshot: json.RawMessage(row.PayloadSnapshot),
			SentAt:          timestampToPtr(row.SentAt),
			CreatedAt:       timestampToString(row.DeliveryCreatedAt),
			UpdatedAt:       timestampToString(row.DeliveryUpdatedAt),
			TargetType:      textToPtr(row.TargetType),
			TargetID:        uuidToPtr(row.TargetID),
		}
	}
	return resp
}

func (h *Handler) ListNotificationDebugRows(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}

	issueID, ok := optionalUUIDQueryParam(w, r.URL.Query().Get("issue_id"), "issue_id")
	if !ok {
		return
	}
	recipientID, ok := optionalUUIDQueryParam(w, r.URL.Query().Get("recipient_id"), "recipient_id")
	if !ok {
		return
	}
	commentID, ok := optionalUUIDQueryParam(w, r.URL.Query().Get("comment_id"), "comment_id")
	if !ok {
		return
	}

	limit := int32(100)
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = int32(parsed)
	}

	rows, err := h.Queries.ListNotificationDebugRows(r.Context(), db.ListNotificationDebugRowsParams{
		WorkspaceID:     workspaceID,
		IssueID:         issueID,
		RecipientUserID: recipientID,
		CommentID:       commentID,
		EventType:       optionalTextQueryParam(r.URL.Query().Get("event_type")),
		Channel:         optionalTextQueryParam(r.URL.Query().Get("channel")),
		Limit:           limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notification debug rows")
		return
	}

	respRows := make([]NotificationDebugRowResponse, 0, len(rows))
	for _, row := range rows {
		respRows = append(respRows, notificationDebugRowToResponse(row))
	}
	writeJSON(w, http.StatusOK, ListNotificationDebugRowsResponse{Rows: respRows, Total: len(respRows)})
}
