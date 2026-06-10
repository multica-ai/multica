package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// NormalizedPREvent is the provider-agnostic representation of a pull request
// webhook event. Both GitHub and Gitee handlers normalize their payloads into
// this struct before calling ProcessPullRequestEvent.
type NormalizedPREvent struct {
	Provider        string // "github" or "gitee"
	WorkspaceID     pgtype.UUID
	InstallationID  pgtype.Int8 // nullable — Gitee has no installation concept
	RepoOwner       string
	RepoName        string
	Number          int32
	Title           string
	Body            string
	HTMLURL         string
	SourceBranch    string
	AuthorLogin     string
	AuthorAvatarURL string
	State           string // open | draft | merged | closed
	CreatedAt       time.Time
	UpdatedAt       time.Time
	MergedAt        *time.Time
	ClosedAt        *time.Time
	HeadSHA         string
	MergeableState  pgtype.Text
	ClearMergeable  bool
	Additions       int32
	Deletions       int32
	ChangedFiles    int32
}

// ProcessPullRequestEvent upserts the PR row, auto-links to issues via
// identifier extraction, and optionally auto-advances issues to done.
// autoDoneEnabled controls whether the auto-done logic fires on terminal
// events; Gitee should pass false (conservative), GitHub passes true.
func (h *Handler) ProcessPullRequestEvent(ctx context.Context, evt NormalizedPREvent, autoDoneEnabled bool) {
	pr, err := h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:         evt.WorkspaceID,
		InstallationID:      evt.InstallationID,
		RepoOwner:           evt.RepoOwner,
		RepoName:            evt.RepoName,
		PrNumber:            evt.Number,
		Title:               evt.Title,
		State:               evt.State,
		HtmlUrl:             evt.HTMLURL,
		Branch:              ptrToText(strPtrOrNil(evt.SourceBranch)),
		AuthorLogin:         ptrToText(strPtrOrNil(evt.AuthorLogin)),
		AuthorAvatarUrl:     ptrToText(strPtrOrNil(evt.AuthorAvatarURL)),
		MergedAt:            timePtrToTimestamptz(evt.MergedAt),
		ClosedAt:            timePtrToTimestamptz(evt.ClosedAt),
		PrCreatedAt:         pgtype.Timestamptz{Time: evt.CreatedAt, Valid: true},
		PrUpdatedAt:         pgtype.Timestamptz{Time: evt.UpdatedAt, Valid: true},
		HeadSha:             evt.HeadSHA,
		MergeableState:      evt.MergeableState,
		ClearMergeableState: pgtype.Bool{Bool: evt.ClearMergeable, Valid: true},
		Additions:           evt.Additions,
		Deletions:           evt.Deletions,
		ChangedFiles:        evt.ChangedFiles,
		Provider:            evt.Provider,
	})
	if err != nil {
		slog.Warn("pr: upsert failed", "err", err, "provider", evt.Provider)
		return
	}
	if evt.Provider == "github" && evt.State == "merged" && evt.MergedAt != nil && h.Metrics != nil {
		if openSeconds := evt.MergedAt.Sub(evt.CreatedAt).Seconds(); openSeconds > 0 {
			h.Metrics.ObserveGithubPRMergeSeconds(openSeconds)
		}
	}

	workspaceID := uuidToString(evt.WorkspaceID)
	resp := githubPullRequestToResponse(pr)

	// Auto-link: scan title/body/branch for issue identifiers.
	idents := extractIdentifiers(evt.Title, evt.Body, evt.SourceBranch)
	prefix := h.getIssuePrefix(ctx, evt.WorkspaceID)
	linkedIssueIDs := make([]string, 0, len(idents))
	for _, id := range idents {
		issue, ok := h.lookupIssueByIdentifier(ctx, evt.WorkspaceID, prefix, id)
		if !ok {
			continue
		}
		if err := h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:             issue.ID,
			PullRequestID:       pr.ID,
			CloseIntent:         false,
			LinkedByType:        strToText("system"),
			LinkedByID:          pgtype.UUID{},
			PreserveCloseIntent: false,
		}); err != nil {
			slog.Warn("pr: link failed", "err", err, "provider", evt.Provider)
			continue
		}
		linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))

		// Auto-done logic: only if enabled and the event is terminal.
		if autoDoneEnabled && (evt.State == "merged" || evt.State == "closed") &&
			issue.Status != "done" && issue.Status != "cancelled" {
			counts, err := h.Queries.GetIssuePullRequestCloseAggregate(ctx, issue.ID)
			if err != nil {
				slog.Warn("pr: count linked pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
				continue
			}
			if counts.OpenCount == 0 && counts.MergedWithCloseIntentCount > 0 {
				h.advanceIssueToDone(ctx, issue, workspaceID)
			}
		}
	}

	// Broadcast PR change to the workspace.
	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request":     resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

// timePtrToTimestamptz converts a *time.Time to pgtype.Timestamptz.
func timePtrToTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
