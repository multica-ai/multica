package gitlab

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// SyncDeps is the set of plumbing the sync orchestrator needs.
type SyncDeps struct {
	Queries *db.Queries
	Client  *gitlabapi.Client
}

// RunInitialSyncInput is the per-call input.
type RunInitialSyncInput struct {
	WorkspaceID string
	ProjectID   int64
	Token       string
}

// RunInitialSync orchestrates a one-shot pull of GitLab project state into
// Multica's cache tables for one workspace, and reports the outcome on the
// workspace_gitlab_connection row.
func RunInitialSync(ctx context.Context, deps SyncDeps, in RunInitialSyncInput) error {
	wsUUID, err := pgUUID(in.WorkspaceID)
	if err != nil {
		return fmt.Errorf("initial sync: workspace_id: %w", err)
	}
	if err := runInitialSyncImpl(ctx, deps, in, wsUUID); err != nil {
		// Best-effort status update — log but don't override the original error.
		_ = deps.Queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
			WorkspaceID:      wsUUID,
			ConnectionStatus: "error",
			StatusMessage:    pgtype.Text{String: err.Error(), Valid: true},
		})
		return err
	}
	return deps.Queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
		WorkspaceID:      wsUUID,
		ConnectionStatus: "connected",
		StatusMessage:    pgtype.Text{},
	})
}

// runInitialSyncImpl is the body of the previous RunInitialSync — same logic,
// just with the wsUUID parameter passed in instead of derived inside.
func runInitialSyncImpl(ctx context.Context, deps SyncDeps, in RunInitialSyncInput, wsUUID pgtype.UUID) error {
	// 1. Bootstrap canonical scoped labels (idempotent).
	if err := BootstrapScopedLabels(ctx, deps.Client, in.Token, in.ProjectID); err != nil {
		return fmt.Errorf("initial sync: bootstrap labels: %w", err)
	}

	// 2. Fetch + upsert all labels.
	labels, err := deps.Client.ListLabels(ctx, in.Token, in.ProjectID)
	if err != nil {
		return fmt.Errorf("initial sync: list labels: %w", err)
	}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	for _, l := range labels {
		if _, err := deps.Queries.UpsertGitlabLabel(ctx, db.UpsertGitlabLabelParams{
			WorkspaceID:       wsUUID,
			GitlabLabelID:     l.ID,
			Name:              l.Name,
			Color:             l.Color,
			Description:       l.Description,
			ExternalUpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("initial sync: upsert label %q: %w", l.Name, err)
		}
	}

	// 3. Fetch + upsert project members.
	members, err := deps.Client.ListProjectMembers(ctx, in.Token, in.ProjectID)
	if err != nil {
		return fmt.Errorf("initial sync: list members: %w", err)
	}
	for _, m := range members {
		if _, err := deps.Queries.UpsertGitlabProjectMember(ctx, db.UpsertGitlabProjectMemberParams{
			WorkspaceID:       wsUUID,
			GitlabUserID:      m.ID,
			Username:          m.Username,
			Name:              m.Name,
			AvatarUrl:         m.AvatarURL,
			ExternalUpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("initial sync: upsert member %q: %w", m.Username, err)
		}
	}

	// 4. Issues + notes + awards.
	if err := syncAllIssues(ctx, deps, in, wsUUID); err != nil {
		return fmt.Errorf("initial sync: issues: %w", err)
	}

	return nil
}

// pgUUID converts a string UUID to pgtype.UUID, returning an error for
// invalid input.
func pgUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, err
	}
	return u, nil
}

// syncAllIssues fetches every project issue and upserts each (with notes,
// awards, and label associations) into the cache. Concurrency bounded at 5.
func syncAllIssues(ctx context.Context, deps SyncDeps, in RunInitialSyncInput, wsUUID pgtype.UUID) error {
	issues, err := deps.Client.ListIssues(ctx, in.Token, in.ProjectID, gitlabapi.ListIssuesParams{State: "all"})
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	// Build the agent-slug → uuid lookup once for the translator.
	// The agent table has no slug column; derive slug from name.
	agentMap, err := buildAgentSlugMap(ctx, deps.Queries, wsUUID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}

	// Build the gitlab_label_id by name lookup so we can set issue_gitlab_label
	// associations from the issue's label-name list.
	labels, err := deps.Queries.ListGitlabLabels(ctx, wsUUID)
	if err != nil {
		return fmt.Errorf("list cached labels: %w", err)
	}
	labelIDByName := make(map[string]int64, len(labels))
	for _, l := range labels {
		labelIDByName[l.Name] = l.GitlabLabelID
	}

	// Cancel sibling workers on first error so a sync against a revoked PAT
	// doesn't hammer GitLab with thousands of identical 401s before reporting.
	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)
	errs := make(chan error, len(issues))
	var wg sync.WaitGroup

	for _, issue := range issues {
		issue := issue
		// Stop launching new workers if we've already cancelled.
		if syncCtx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// Bail early if a sibling already failed and cancelled the context.
			if syncCtx.Err() != nil {
				return
			}
			if err := syncOneIssue(syncCtx, deps, in, wsUUID, issue, agentMap, labelIDByName); err != nil {
				errs <- fmt.Errorf("issue iid=%d: %w", issue.IID, err)
				cancel() // signal siblings to stop
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		return e
	}
	return nil
}

func syncOneIssue(
	ctx context.Context,
	deps SyncDeps,
	in RunInitialSyncInput,
	wsUUID pgtype.UUID,
	issue gitlabapi.Issue,
	agentMap map[string]string,
	labelIDByName map[string]int64,
) error {
	values := TranslateIssue(issue, &TranslateContext{AgentBySlug: agentMap})

	row, err := deps.Queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(wsUUID, in.ProjectID, issue, values))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("upsert issue: %w", err)
		}
		// Skipped because existing row is newer; fetch for label work below.
		row, err = deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
			WorkspaceID: wsUUID,
			GitlabIid:   pgtype.Int4{Int32: int32(issue.IID), Valid: true},
		})
		if err != nil {
			return fmt.Errorf("re-fetch after skipped upsert: %w", err)
		}
	}

	// Replace label associations.
	labelIDs := make([]int64, 0, len(issue.Labels))
	for _, name := range issue.Labels {
		if id, ok := labelIDByName[name]; ok {
			labelIDs = append(labelIDs, id)
		}
	}
	if err := deps.Queries.ClearIssueLabels(ctx, row.ID); err != nil {
		return fmt.Errorf("clear labels: %w", err)
	}
	if len(labelIDs) > 0 {
		if err := deps.Queries.AddIssueLabels(ctx, db.AddIssueLabelsParams{
			IssueID:     row.ID,
			WorkspaceID: wsUUID,
			LabelIds:    labelIDs,
		}); err != nil {
			return fmt.Errorf("add labels: %w", err)
		}
	}

	// Notes — sync all of them. Agent-prefixed notes get author_type='agent'
	// + a resolved Multica agent UUID. Other notes (human, system) leave
	// Multica author refs NULL and rely on gitlab_author_user_id for display.
	notes, err := deps.Client.ListNotes(ctx, in.Token, in.ProjectID, issue.IID)
	if err != nil {
		return fmt.Errorf("list notes: %w", err)
	}
	for _, n := range notes {
		nv := TranslateNote(n)
		var authorType pgtype.Text
		var authorID pgtype.UUID
		if nv.AuthorType == "agent" {
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
			WorkspaceID:        wsUUID,
			IssueID:            row.ID,
			AuthorType:         authorType,
			AuthorID:           authorID,
			GitlabAuthorUserID: glUser,
			Content:            nv.Body,
			Type:               nv.Type,
			GitlabNoteID:       pgtype.Int8{Int64: n.ID, Valid: true},
			ExternalUpdatedAt:  parseTS(nv.UpdatedAt),
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			// pgx.ErrNoRows here means the cache already has a newer-or-equal
			// row — the clobber guard short-circuited. That's success.
			return fmt.Errorf("upsert note %d: %w", n.ID, err)
		}
	}

	// Awards — sync all. Multica actor refs NULL until Phase 3 wires
	// gitlab user → multica member; gitlab_actor_user_id always populated.
	awards, err := deps.Client.ListAwardEmoji(ctx, in.Token, in.ProjectID, issue.IID)
	if err != nil {
		return fmt.Errorf("list awards: %w", err)
	}
	for _, a := range awards {
		av := TranslateAward(a)
		var glUser pgtype.Int8
		if av.GitlabUserID != 0 {
			glUser = pgtype.Int8{Int64: av.GitlabUserID, Valid: true}
		}
		if _, err := deps.Queries.UpsertIssueReactionFromGitlab(ctx, db.UpsertIssueReactionFromGitlabParams{
			WorkspaceID:       wsUUID,
			IssueID:           row.ID,
			ActorType:         pgtype.Text{},
			ActorID:           pgtype.UUID{},
			GitlabActorUserID: glUser,
			Emoji:             av.Emoji,
			GitlabAwardID:     pgtype.Int8{Int64: a.ID, Valid: true},
			ExternalUpdatedAt: parseTS(av.UpdatedAt),
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("upsert award %d: %w", a.ID, err)
		}
	}

	return nil
}

// buildUpsertIssueParams converts a translated IssueValues + raw GitLab issue
// into the sqlc params struct.
func buildUpsertIssueParams(wsUUID pgtype.UUID, projectID int64, issue gitlabapi.Issue, values IssueValues) db.UpsertIssueFromGitlabParams {
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if values.AssigneeType != "" {
		assigneeType = pgtype.Text{String: values.AssigneeType, Valid: true}
		_ = assigneeID.Scan(values.AssigneeID)
	}
	desc := pgtype.Text{}
	if values.Description != "" {
		desc = pgtype.Text{String: values.Description, Valid: true}
	}
	return db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: int32(issue.IID), Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: projectID, Valid: true},
		GitlabIssueID:     pgtype.Int8{Int64: issue.ID, Valid: issue.ID != 0},
		Title:             values.Title,
		Description:       desc,
		Status:            values.Status,
		Priority:          values.Priority,
		AssigneeType:      assigneeType,
		AssigneeID:        assigneeID,
		CreatorType:       pgtype.Text{}, // NULL — Phase 3 populates
		CreatorID:         pgtype.UUID{}, // NULL — Phase 3 populates
		DueDate:           parseTS(values.DueDate),
		ExternalUpdatedAt: parseTS(values.UpdatedAt),
	}
}

// buildAgentSlugMap loads slug→uuid for every agent in the workspace.
// The agent table has no slug column; we derive slug from name (lowercased,
// spaces → hyphens). Phase 4 may formalize this with a real slug column.
func buildAgentSlugMap(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID) (map[string]string, error) {
	rows, err := q.ListAgents(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		slug := strings.ToLower(strings.ReplaceAll(r.Name, " ", "-"))
		if existing, dup := out[slug]; dup {
			slog.Warn("agent slug collision in workspace; later agent wins",
				"workspace_id", uuidString(wsUUID),
				"slug", slug,
				"existing_agent_id", existing,
				"new_agent_id", uuidString(r.ID))
		}
		out[slug] = uuidString(r.ID)
	}
	return out, nil
}

// parseTS converts a GitLab RFC3339 timestamp into pgtype.Timestamptz.
func parseTS(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// uuidString stringifies a pgtype.UUID. Returns "" if invalid.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	bs, _ := u.MarshalJSON()
	s := string(bs)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}
