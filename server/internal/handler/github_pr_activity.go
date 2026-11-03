package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	githubPRActivityMerged = "pull_request_merged"
	githubPRActivityClosed = "pull_request_closed"
)

type ghPRActor struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// Both GitHub and Multica actions arrive through the same signed webhook.
// Event time, rather than delivery id, identifies each close: redelivery is a
// no-op, but reopening and closing the PR again is a new timeline event.
func (h *Handler) recordGitHubPRWebhookActivity(ctx context.Context, pr db.GithubPullRequest, p *ghPullRequestPayload) {
	if p.Action != "closed" {
		return
	}
	action := githubPRActivityClosed
	at := parseGHTime(p.PullRequest.ClosedAt)
	actor := p.Sender
	if p.PullRequest.Merged {
		action = githubPRActivityMerged
		at = parseGHTime(p.PullRequest.MergedAt)
		if p.PullRequest.MergedBy.ID != 0 || p.PullRequest.MergedBy.Login != "" {
			actor = p.PullRequest.MergedBy
		}
	}
	if !at.Valid {
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, pr.WorkspaceID)
	if err != nil || !githubFeaturesEnabled(ws) {
		return
	}
	issueIDs, err := h.Queries.ListIssueIDsForPullRequest(ctx, pr.ID)
	if err != nil {
		slog.Warn("github: list PR activity issues failed", "err", err)
		return
	}
	if len(issueIDs) == 0 {
		return
	}

	actorType := "system"
	var actorID pgtype.UUID
	var actorName, actorAvatarURL string
	if actor.ID != 0 {
		members, err := h.Queries.FindGitHubActivityMembers(ctx, db.FindGitHubActivityMembersParams{
			WorkspaceID: pr.WorkspaceID, GithubUserID: actor.ID,
		})
		if err != nil {
			slog.Warn("github: resolve PR activity actor failed", "err", err)
		} else if len(members) == 1 {
			member := members[0]
			actorType, actorID, actorName = "member", member.ID, member.Name
			if member.AvatarUrl.Valid {
				actorAvatarURL = h.resolveAvatarURL(member.AvatarUrl.String)
			}
		}
	}
	details, _ := json.Marshal(map[string]any{
		"pull_request_id": uuidToString(pr.ID),
		"pr_number":       pr.PrNumber, "pr_title": p.PullRequest.Title, "pr_url": p.PullRequest.HTMLURL,
		"repo_owner": pr.RepoOwner, "repo_name": pr.RepoName,
		"source": "github", "github_user_id": actor.ID, "github_login": actor.Login,
	})
	for _, issueID := range issueIDs {
		key := fmt.Sprintf("github-pr-activity/v1/%s/%s/%s", uuidToString(issueID), action, at.Time.UTC().Format(time.RFC3339Nano))
		id := uuid.NewSHA1(uuid.UUID(pr.ID.Bytes), []byte(key))
		activity, err := h.Queries.CreateGitHubPRActivity(ctx, db.CreateGitHubPRActivityParams{
			ID: pgtype.UUID{Bytes: id, Valid: true}, WorkspaceID: pr.WorkspaceID, IssueID: issueID, PullRequestID: pr.ID,
			ActorType: strToText(actorType), ActorID: actorID, Action: action, Details: details, CreatedAt: at,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			slog.Warn("github: record PR activity failed", "action", action, "err", err)
			continue
		}
		entry := activityToEntry(activity)
		if actorType == "member" {
			entry.ActorName, entry.ActorAvatarURL = actorName, actorAvatarURL
		}
		h.publish(protocol.EventActivityCreated, uuidToString(pr.WorkspaceID), actorType, uuidToString(actorID), map[string]any{
			"issue_id": uuidToString(issueID), "entry": entry,
		})
	}
}
