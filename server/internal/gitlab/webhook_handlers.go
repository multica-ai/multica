package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// WebhookDeps is what every per-event handler needs. The worker constructs
// this once per event.
type WebhookDeps struct {
	Queries     *db.Queries
	WorkspaceID pgtype.UUID
	ProjectID   int64
}

// issueHookPayload is the subset of the Issue Hook body we read.
type issueHookPayload struct {
	ObjectAttributes struct {
		IID         int      `json:"iid"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		State       string   `json:"state"`
		UpdatedAt   string   `json:"updated_at"`
		DueDate     string   `json:"due_date"`
		Labels      []struct {
			Title string `json:"title"`
		} `json:"labels"`
	} `json:"object_attributes"`
}

// ApplyIssueHookEvent applies one Issue Hook event to the cache. Reuses the
// same translator + upsert as Phase 2a's initial sync.
func ApplyIssueHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p issueHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode issue hook: %w", err)
	}
	updatedAt, _ := time.Parse(time.RFC3339, p.ObjectAttributes.UpdatedAt)

	// Stale-event check: if cache row exists and is at least as new, skip.
	existing, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.ObjectAttributes.IID), Valid: true},
	})
	if err == nil && existing.ExternalUpdatedAt.Valid && !existing.ExternalUpdatedAt.Time.Before(updatedAt) {
		return nil
	}

	labels := make([]string, 0, len(p.ObjectAttributes.Labels))
	for _, l := range p.ObjectAttributes.Labels {
		labels = append(labels, l.Title)
	}
	apiIssue := gitlabapi.Issue{
		IID:         p.ObjectAttributes.IID,
		Title:       p.ObjectAttributes.Title,
		Description: p.ObjectAttributes.Description,
		State:       p.ObjectAttributes.State,
		Labels:      labels,
		DueDate:     p.ObjectAttributes.DueDate,
		UpdatedAt:   p.ObjectAttributes.UpdatedAt,
	}

	agentMap, err := buildAgentSlugMap(ctx, deps.Queries, deps.WorkspaceID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}
	values := TranslateIssue(apiIssue, &TranslateContext{AgentBySlug: agentMap})

	if _, err := deps.Queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(deps.WorkspaceID, deps.ProjectID, apiIssue, values)); err != nil {
		return fmt.Errorf("upsert issue: %w", err)
	}

	// Note: label associations (issue_gitlab_label) are not updated here.
	// The webhook payload includes label titles but not the gitlab_label_id,
	// so we'd need an extra ListLabels lookup. Phase 2b's reconciler handles
	// label associations via re-syncing labels every tick.
	return nil
}

type noteHookPayload struct {
	ObjectAttributes struct {
		ID           int64  `json:"id"`
		Note         string `json:"note"`
		System       bool   `json:"system"`
		UpdatedAt    string `json:"updated_at"`
		NoteableType string `json:"noteable_type"`
	} `json:"object_attributes"`
	Issue struct {
		IID int `json:"iid"`
	} `json:"issue"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

// ApplyNoteHookEvent caches a comment delta. Filters out non-issue notes
// (MR / snippet comments) — Multica only mirrors issues.
func ApplyNoteHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p noteHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode note hook: %w", err)
	}
	if p.ObjectAttributes.NoteableType != "Issue" {
		return nil
	}
	if p.Issue.IID == 0 {
		return fmt.Errorf("note hook missing issue.iid")
	}

	parent, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.Issue.IID), Valid: true},
	})
	if err != nil {
		// The webhook arrived before we cached the parent issue. Returning
		// the error keeps the event in the queue for the worker to retry;
		// the next reconciler pass will create the issue and the worker
		// will retry this note.
		return fmt.Errorf("parent issue not yet cached (iid=%d): %w", p.Issue.IID, err)
	}

	apiNote := gitlabapi.Note{
		ID:        p.ObjectAttributes.ID,
		Body:      p.ObjectAttributes.Note,
		System:    p.ObjectAttributes.System,
		Author:    gitlabapi.User{ID: p.User.ID},
		UpdatedAt: p.ObjectAttributes.UpdatedAt,
	}
	nv := TranslateNote(apiNote)

	var authorType pgtype.Text
	var authorID pgtype.UUID
	if nv.AuthorType == "agent" {
		agentMap, err := buildAgentSlugMap(ctx, deps.Queries, deps.WorkspaceID)
		if err != nil {
			return fmt.Errorf("agent map: %w", err)
		}
		if uuidStr, ok := agentMap[nv.AuthorSlug]; ok {
			authorType = pgtype.Text{String: "agent", Valid: true}
			_ = authorID.Scan(uuidStr)
		}
	}
	var glUser pgtype.Int8
	if nv.GitlabUserID != 0 {
		glUser = pgtype.Int8{Int64: nv.GitlabUserID, Valid: true}
	}

	if _, err := deps.Queries.UpsertCommentFromGitlab(ctx, db.UpsertCommentFromGitlabParams{
		WorkspaceID:        deps.WorkspaceID,
		IssueID:            parent.ID,
		AuthorType:         authorType,
		AuthorID:           authorID,
		GitlabAuthorUserID: glUser,
		Content:            nv.Body,
		Type:               nv.Type,
		GitlabNoteID:       pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true},
		ExternalUpdatedAt:  parseTS(nv.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("upsert comment: %w", err)
	}
	return nil
}

type emojiHookPayload struct {
	ObjectAttributes struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		AwardableType string `json:"awardable_type"`
		AwardableIID  int    `json:"awardable_id"`
		UpdatedAt     string `json:"updated_at"`
	} `json:"object_attributes"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

// ApplyEmojiHookEvent caches an issue-level award emoji.
// Note-level awards (reactions on comments) are NOT mirrored.
func ApplyEmojiHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p emojiHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode emoji hook: %w", err)
	}
	if p.ObjectAttributes.AwardableType != "Issue" {
		return nil
	}
	parent, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.ObjectAttributes.AwardableIID), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("parent issue not yet cached (iid=%d): %w", p.ObjectAttributes.AwardableIID, err)
	}
	var glUser pgtype.Int8
	if p.User.ID != 0 {
		glUser = pgtype.Int8{Int64: p.User.ID, Valid: true}
	}
	if _, err := deps.Queries.UpsertIssueReactionFromGitlab(ctx, db.UpsertIssueReactionFromGitlabParams{
		WorkspaceID:       deps.WorkspaceID,
		IssueID:           parent.ID,
		ActorType:         pgtype.Text{},
		ActorID:           pgtype.UUID{},
		GitlabActorUserID: glUser,
		Emoji:             p.ObjectAttributes.Name,
		GitlabAwardID:     pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true},
		ExternalUpdatedAt: parseTS(p.ObjectAttributes.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("upsert reaction: %w", err)
	}
	return nil
}

type labelHookPayload struct {
	ObjectAttributes struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Color       string `json:"color"`
		Description string `json:"description"`
		UpdatedAt   string `json:"updated_at"`
		Action      string `json:"action"` // "create", "update", "delete"
	} `json:"object_attributes"`
}

// ApplyLabelHookEvent maintains the gitlab_label cache.
func ApplyLabelHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p labelHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode label hook: %w", err)
	}
	if p.ObjectAttributes.ID == 0 {
		return fmt.Errorf("label hook missing object_attributes.id")
	}
	if p.ObjectAttributes.Action == "delete" {
		return deps.Queries.DeleteGitlabLabel(ctx, db.DeleteGitlabLabelParams{
			WorkspaceID:   deps.WorkspaceID,
			GitlabLabelID: p.ObjectAttributes.ID,
		})
	}
	if _, err := deps.Queries.UpsertGitlabLabel(ctx, db.UpsertGitlabLabelParams{
		WorkspaceID:       deps.WorkspaceID,
		GitlabLabelID:     p.ObjectAttributes.ID,
		Name:              p.ObjectAttributes.Title,
		Color:             p.ObjectAttributes.Color,
		Description:       p.ObjectAttributes.Description,
		ExternalUpdatedAt: parseTS(p.ObjectAttributes.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("upsert label: %w", err)
	}
	return nil
}
