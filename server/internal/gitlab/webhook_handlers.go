package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// TaskEnqueuer is the minimal surface the webhook handlers need from the
// task service to enqueue agent work. Defining it locally avoids importing
// internal/service (which would create a package cycle once service grows
// gitlab-aware code) and lets tests supply a no-op/recording stub.
type TaskEnqueuer interface {
	EnqueueTaskForIssue(ctx context.Context, issue db.Issue, triggerCommentID ...pgtype.UUID) (db.AgentTaskQueue, error)
}

// WebhookDeps is what every per-event handler needs. The worker constructs
// this once per event.
//
// Resolver is optional: when non-nil, handlers use it to reverse-resolve
// GitLab user IDs to Multica member UUIDs (and fall back to gitlab_project_member
// for issue assignees). When nil, handlers leave Multica author/actor/assignee
// columns NULL and preserve only the raw gitlab_*_user_id hint — matching the
// pre-Phase-4 behavior.
//
// TaskEnqueuer is optional: when non-nil, ApplyIssueHookEvent enqueues an
// agent task after the upsert if the assignment changed to a (new) agent
// — mirroring the write-through path in handler/issue.go. When nil, the
// webhook path applies cache changes but does NOT spawn agent work.
type WebhookDeps struct {
	Queries      *db.Queries
	WorkspaceID  pgtype.UUID
	ProjectID    int64
	Resolver     *Resolver
	TaskEnqueuer TaskEnqueuer
}

// issueHookPayload is the subset of the Issue Hook body we read.
type issueHookPayload struct {
	ObjectAttributes struct {
		IID         int    `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       string `json:"state"`
		UpdatedAt   string `json:"updated_at"`
		DueDate     string `json:"due_date"`
		Labels      []struct {
			Title string `json:"title"`
		} `json:"labels"`
	} `json:"object_attributes"`
	// Assignees is the native GitLab assignee list. Populated from the
	// top-level `assignees` array in the hook payload. We use the first entry
	// as the assignee (GitLab issues support multiple assignees on some
	// tiers; Multica mirrors a single slot).
	Assignees []struct {
		ID int64 `json:"id"`
	} `json:"assignees"`
	// User is the issue author (creator) — GitLab sends this as a sibling
	// of `object_attributes`, not inside it.
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

// ApplyIssueHookEvent applies one Issue Hook event to the cache. Reuses the
// same translator + upsert as Phase 2a's initial sync.
func ApplyIssueHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p issueHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode issue hook: %w", err)
	}
	updatedAt, _ := time.Parse(time.RFC3339, p.ObjectAttributes.UpdatedAt)

	// Load prior cache state (for stale-event check + post-upsert
	// change-detection used by the agent-task enqueue gate). pgx.ErrNoRows
	// means the issue is brand-new in cache; any other error is a real DB
	// problem and must be propagated rather than silently masked.
	existing, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.ObjectAttributes.IID), Valid: true},
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load existing issue cache row: %w", err)
	}
	// Stale-event check: if cache row exists and is at least as new, skip.
	if err == nil && existing.ExternalUpdatedAt.Valid && !existing.ExternalUpdatedAt.Time.Before(updatedAt) {
		return nil
	}

	labels := make([]string, 0, len(p.ObjectAttributes.Labels))
	for _, l := range p.ObjectAttributes.Labels {
		labels = append(labels, l.Title)
	}
	assignees := make([]gitlabapi.User, 0, len(p.Assignees))
	for _, a := range p.Assignees {
		assignees = append(assignees, gitlabapi.User{ID: a.ID})
	}
	apiIssue := gitlabapi.Issue{
		IID:         p.ObjectAttributes.IID,
		Title:       p.ObjectAttributes.Title,
		Description: p.ObjectAttributes.Description,
		State:       p.ObjectAttributes.State,
		Labels:      labels,
		Assignees:   assignees,
		Author:      gitlabapi.User{ID: p.User.ID},
		DueDate:     p.ObjectAttributes.DueDate,
		UpdatedAt:   p.ObjectAttributes.UpdatedAt,
	}

	agentMap, err := buildAgentSlugMap(ctx, deps.Queries, deps.WorkspaceID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}
	values := TranslateIssue(apiIssue, &TranslateContext{AgentBySlug: agentMap})

	// Reverse-resolve native GitLab assignee when no agent::<slug> label set it.
	// Agent labels always win (Multica semantics prefer agent assignment).
	if values.AssigneeType == "" && values.GitlabAssigneeUserID != 0 && deps.Resolver != nil {
		ut, uid, memID, rErr := deps.Resolver.ResolveMulticaUserFromGitlabUserID(ctx, uuidString(deps.WorkspaceID), values.GitlabAssigneeUserID)
		if rErr != nil {
			return fmt.Errorf("resolve assignee: %w", rErr)
		}
		switch ut {
		case "member":
			values.AssigneeType = "member"
			values.AssigneeID = uid
		case "gitlab_user":
			values.AssigneeType = "gitlab_user"
			values.AssigneeID = memID
		}
		// "" → unmapped; leave values.AssigneeType empty, cache stays NULL.
	}

	creatorType, creatorID, err := resolveCreatorFromGitlabID(ctx, deps.Resolver, uuidString(deps.WorkspaceID), values.CreatorGitlabUserID)
	if err != nil {
		return fmt.Errorf("resolve creator: %w", err)
	}

	cacheRow, err := deps.Queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(deps.WorkspaceID, deps.ProjectID, apiIssue, values, creatorType, creatorID))
	if err != nil {
		return fmt.Errorf("upsert issue: %w", err)
	}

	// Enqueue an agent task when the GitLab-originated update JUST moved the
	// issue into an agent assignment (or switched to a different agent). This
	// closes the "human adds ~agent::<slug> from gitlab.com" gap that Phase 2a
	// left open — the write-through path in handler/issue.go already enqueues
	// on the POST/PATCH route, but a webhook-only assignment never reached the
	// task queue until Phase 4. Change detection compares prior cache row vs.
	// fresh upsert row so webhook replays and unrelated updates (title-only,
	// description-only) don't spawn duplicate tasks.
	if deps.TaskEnqueuer != nil && shouldEnqueueAgentOnWebhook(existing, cacheRow) {
		if _, err := deps.TaskEnqueuer.EnqueueTaskForIssue(ctx, cacheRow); err != nil {
			// Match the write-through tail: a failed enqueue must not fail the
			// webhook event (the cache update already landed). Log and move on;
			// the reconciler / next webhook delivery will retry naturally.
			slog.Error("webhook issue hook: enqueue agent task failed",
				"workspace_id", uuidString(deps.WorkspaceID),
				"issue_id", uuidString(cacheRow.ID),
				"err", err)
		}
	}

	// Note: this handler doesn't update issue_gitlab_label junction rows. The
	// webhook payload only carries label titles (not gitlab_label_ids), so we'd
	// need an extra ListLabels round-trip to resolve. Junction rows are kept in
	// sync by Phase 2a's initial sync (which uses ClearIssueLabels +
	// AddIssueLabels) and by subsequent Issue Hook deliveries that include the
	// full label set in the payload — the next Issue Hook will rewrite the
	// associations. Phase 3 may add a label-by-name resolver here if real-time
	// per-issue label drift becomes a UX problem.
	return nil
}

// shouldEnqueueAgentOnWebhook decides whether the post-upsert cache row
// represents a fresh agent assignment worth spawning a task for. Returns
// true only when the current row is agent-assigned, NOT in backlog, AND
// (the prior row was NOT an agent, OR it was a different agent).
// Same-agent replays are no-ops.
//
// Backlog status gate matches the handler's write-through path
// (shouldEnqueueAgentTask): issues parked in backlog don't auto-run even
// when an agent is assigned. The webhook payload's labels drive status
// via TranslateIssue in the same step, so the gate sees the authoritative
// new-state status. TaskEnqueuer.EnqueueTaskForIssue additionally requires
// a non-archived agent with a runtime — unready agents silently no-op.
func shouldEnqueueAgentOnWebhook(prior, cur db.Issue) bool {
	if !cur.AssigneeType.Valid || cur.AssigneeType.String != "agent" {
		return false
	}
	if !cur.AssigneeID.Valid {
		return false
	}
	if cur.Status == "backlog" {
		return false
	}
	// Brand-new issue OR prior wasn't agent-assigned → enqueue.
	if !prior.AssigneeType.Valid || prior.AssigneeType.String != "agent" || !prior.AssigneeID.Valid {
		return true
	}
	// Swapped to a different agent → enqueue.
	return prior.AssigneeID != cur.AssigneeID
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
	} else if nv.GitlabUserID != 0 && deps.Resolver != nil {
		// Human-authored note: reverse-resolve the GitLab user ID to a
		// Multica member when possible. The comment table's author_type
		// CHECK constraint only permits 'member' or 'agent' (not
		// 'gitlab_user'), so unmapped GitLab users leave author_type NULL
		// and rely on gitlab_author_user_id for UI display.
		ut, uid, _, rErr := deps.Resolver.ResolveMulticaUserFromGitlabUserID(ctx, uuidString(deps.WorkspaceID), nv.GitlabUserID)
		if rErr != nil {
			return fmt.Errorf("resolve author: %w", rErr)
		}
		if ut == "member" {
			authorType = pgtype.Text{String: "member", Valid: true}
			_ = authorID.Scan(uid)
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
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// pgx.ErrNoRows here means the clobber guard short-circuited: the
		// cache already has a newer-or-equal row (webhook replay or race with
		// a Phase 3c write-through). Treat it as success — the stored copy
		// stays authoritative.
		return fmt.Errorf("upsert comment: %w", err)
	}
	return nil
}

type emojiHookPayload struct {
	ObjectAttributes struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		AwardableType string `json:"awardable_type"`
		// GitLab payload field is `awardable_id` which is the GLOBAL issue
		// id (not the per-project IID). We look up the cached issue via
		// GetIssueByGitlabID, not GetIssueByGitlabIID.
		AwardableID int64  `json:"awardable_id"`
		UpdatedAt   string `json:"updated_at"`
	} `json:"object_attributes"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

// ApplyEmojiHookEvent caches an award emoji. Supports both Issue-level and
// Note-level awards (the latter via comment_reaction).
func ApplyEmojiHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p emojiHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode emoji hook: %w", err)
	}

	// Resolve actor once — same logic for issue vs. note awards. The
	// issue_reaction / comment_reaction tables both permit NULL actor refs,
	// so "gitlab_user" and unmapped both leave Multica actor columns NULL
	// and rely on gitlab_actor_user_id for display.
	var actorType pgtype.Text
	var actorID pgtype.UUID
	if p.User.ID != 0 && deps.Resolver != nil {
		ut, uid, _, rErr := deps.Resolver.ResolveMulticaUserFromGitlabUserID(ctx, uuidString(deps.WorkspaceID), p.User.ID)
		if rErr != nil {
			return fmt.Errorf("resolve actor: %w", rErr)
		}
		if ut == "member" {
			actorType = pgtype.Text{String: "member", Valid: true}
			_ = actorID.Scan(uid)
		}
	}
	var glUser pgtype.Int8
	if p.User.ID != 0 {
		glUser = pgtype.Int8{Int64: p.User.ID, Valid: true}
	}

	switch p.ObjectAttributes.AwardableType {
	case "Issue":
		parent, err := deps.Queries.GetIssueByGitlabID(ctx, db.GetIssueByGitlabIDParams{
			WorkspaceID:   deps.WorkspaceID,
			GitlabIssueID: pgtype.Int8{Int64: p.ObjectAttributes.AwardableID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("parent issue not yet cached (gitlab_id=%d): %w", p.ObjectAttributes.AwardableID, err)
		}
		if _, err := deps.Queries.UpsertIssueReactionFromGitlab(ctx, db.UpsertIssueReactionFromGitlabParams{
			WorkspaceID:       deps.WorkspaceID,
			IssueID:           parent.ID,
			ActorType:         actorType,
			ActorID:           actorID,
			GitlabActorUserID: glUser,
			Emoji:             p.ObjectAttributes.Name,
			GitlabAwardID:     pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true},
			ExternalUpdatedAt: parseTS(p.ObjectAttributes.UpdatedAt),
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("upsert issue reaction: %w", err)
		}
	case "Note":
		// Note-level awards: look up the parent comment by gitlab_note_id
		// (awardable_id is the note id for Note-type awards).
		parent, err := deps.Queries.GetCommentByGitlabNoteID(ctx, pgtype.Int8{Int64: p.ObjectAttributes.AwardableID, Valid: true})
		if err != nil {
			return fmt.Errorf("parent comment not yet cached (gitlab_note_id=%d): %w", p.ObjectAttributes.AwardableID, err)
		}
		if _, err := deps.Queries.UpsertCommentReactionFromGitlab(ctx, db.UpsertCommentReactionFromGitlabParams{
			WorkspaceID:       deps.WorkspaceID,
			CommentID:         parent.ID,
			ActorType:         actorType,
			ActorID:           actorID,
			GitlabActorUserID: glUser,
			Emoji:             p.ObjectAttributes.Name,
			GitlabAwardID:     pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true},
			ExternalUpdatedAt: parseTS(p.ObjectAttributes.UpdatedAt),
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("upsert comment reaction: %w", err)
		}
	default:
		// MergeRequest / Snippet / unknown awardables — Multica only mirrors
		// issues + their comments.
		return nil
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
