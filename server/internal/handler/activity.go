package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TimelineEntry represents a single entry in the issue timeline, which can be
// either an activity log record or a comment.
type TimelineEntry struct {
	Type string `json:"type"` // "activity" or "comment"
	ID   string `json:"id"`

	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	CreatedAt string `json:"created_at"`

	// Activity-only fields
	Action  *string         `json:"action,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`

	// Comment-only fields
	Content     *string              `json:"content,omitempty"`
	ParentID    *string              `json:"parent_id,omitempty"`
	UpdatedAt   *string              `json:"updated_at,omitempty"`
	CommentType *string              `json:"comment_type,omitempty"`
	Reactions   []ReactionResponse   `json:"reactions,omitempty"`
	Attachments []AttachmentResponse `json:"attachments,omitempty"`
}

// ListTimeline returns a merged, chronologically-sorted timeline of activities
// and comments for a given issue.
func (h *Handler) ListTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	activities, err := h.Queries.ListActivities(r.Context(), db.ListActivitiesParams{
		IssueID: sql.NullString{String: issue.ID, Valid: true},
		Limit:   200,
		Offset:  0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	comments, err := h.Queries.ListComments(r.Context(), db.ListCommentsParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}

	timeline := make([]TimelineEntry, 0, len(activities)+len(comments))

	for _, a := range activities {
		action := a.Action
		actorType := ""
		if a.ActorType.Valid {
			actorType = a.ActorType.String
		}
		actorID := ""
		if a.ActorID.Valid {
			actorID = a.ActorID.String
		}
		timeline = append(timeline, TimelineEntry{
			Type:      "activity",
			ID:        a.ID,
			ActorType: actorType,
			ActorID:   actorID,
			Action:    &action,
			Details:   json.RawMessage(a.Details),
			CreatedAt: a.CreatedAt,
		})
	}

	// Fetch reactions and attachments for all comments in one batch.
	commentIDs := make([]string, len(comments))
	for i, c := range comments {
		commentIDs[i] = c.ID
	}
	grouped := h.groupReactions(r, commentIDs)
	groupedAtt := h.groupAttachments(r, commentIDs)

	for _, c := range comments {
		content := c.Content
		commentType := c.Type
		updatedAt := c.UpdatedAt
		cid := c.ID
		timeline = append(timeline, TimelineEntry{
			Type:        "comment",
			ID:          cid,
			ActorType:   c.AuthorType,
			ActorID:     c.AuthorID,
			Content:     &content,
			CommentType: &commentType,
			ParentID:    nullStringToPtr(c.ParentID),
			CreatedAt:   c.CreatedAt,
			UpdatedAt:   &updatedAt,
			Reactions:   grouped[cid],
			Attachments: groupedAtt[cid],
		})
	}

	// Sort chronologically (ascending by created_at)
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].CreatedAt < timeline[j].CreatedAt
	})

	writeJSON(w, http.StatusOK, timeline)
}
