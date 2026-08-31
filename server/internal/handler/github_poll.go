package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const githubPRPollPageSize = 100

// GitHubPRPollConfig controls the optional in-process outbound poller.
type GitHubPRPollConfig struct {
	Interval        time.Duration
	InitialLookback time.Duration
	Overlap         time.Duration
}

// RunGitHubPRPoller polls once immediately, then at the configured interval.
// It is started only when GITHUB_PR_POLLING_ENABLED is explicitly true.
func (h *Handler) RunGitHubPRPoller(ctx context.Context, cfg GitHubPRPollConfig) {
	if cfg.Interval <= 0 || cfg.InitialLookback <= 0 || cfg.Overlap < 0 {
		slog.Error("github PR poller: invalid configuration",
			"interval", cfg.Interval, "initial_lookback", cfg.InitialLookback, "overlap", cfg.Overlap)
		return
	}
	if h.githubAPIGet == nil {
		slog.Error("github PR poller: enabled but GitHub App API credentials are unavailable")
		return
	}
	slog.Info("github PR poller: starting",
		"interval", cfg.Interval, "initial_lookback", cfg.InitialLookback, "overlap", cfg.Overlap)

	run := func() {
		if err := h.pollGitHubPullRequestsOnce(ctx, cfg); err != nil && ctx.Err() == nil {
			slog.Error("github PR poller: poll failed; cursor unchanged for failed repositories", "err", err)
		}
	}
	run()
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

type githubPRPollTarget struct {
	workspaceID   pgtype.UUID
	owner         string
	repo          string
	installations []int64
}

func (h *Handler) pollGitHubPullRequestsOnce(ctx context.Context, cfg GitHubPRPollConfig) error {
	rows, err := h.Queries.ListGitHubPRPollingTargets(ctx)
	if err != nil {
		return fmt.Errorf("list polling targets: %w", err)
	}
	targets := make([]githubPRPollTarget, 0, len(rows))
	byKey := make(map[string]int, len(rows))
	for _, row := range rows {
		owner, repo, ok := parseGitHubRepositoryURL(row.RepoUrl)
		if !ok {
			continue
		}
		key := uuidToString(row.WorkspaceID) + "/" + strings.ToLower(owner) + "/" + strings.ToLower(repo)
		if i, exists := byKey[key]; exists {
			targets[i].installations = appendUniqueInt64(targets[i].installations, row.InstallationID)
			continue
		}
		byKey[key] = len(targets)
		targets = append(targets, githubPRPollTarget{
			workspaceID:   row.WorkspaceID,
			owner:         owner,
			repo:          repo,
			installations: []int64{row.InstallationID},
		})
	}

	var pollErrs []error
	for _, target := range targets {
		if err := h.pollGitHubRepository(ctx, cfg, target); err != nil {
			pollErrs = append(pollErrs, fmt.Errorf("workspace %s repository %s/%s: %w",
				uuidToString(target.workspaceID), target.owner, target.repo, err))
		}
	}
	return errors.Join(pollErrs...)
}

func (h *Handler) pollGitHubRepository(ctx context.Context, cfg GitHubPRPollConfig, target githubPRPollTarget) error {
	startedAt := time.Now().UTC()
	cursorOwner := strings.ToLower(target.owner)
	cursorRepo := strings.ToLower(target.repo)
	cursor, err := h.Queries.GetGitHubPRPollCursor(ctx, db.GetGitHubPRPollCursorParams{
		WorkspaceID: target.workspaceID,
		RepoOwner:   cursorOwner,
		RepoName:    cursorRepo,
	})
	cutoff := startedAt.Add(-cfg.InitialLookback)
	if err == nil {
		cutoff = cursor.Time.Add(-cfg.Overlap)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load cursor: %w", err)
	}

	var (
		pullRequests   []ghPullRequestPayload
		installationID int64
		fetchErrs      []error
	)
	for _, candidate := range target.installations {
		pullRequests, err = h.fetchUpdatedGitHubPullRequests(ctx, candidate, target.owner, target.repo, cutoff)
		if err == nil {
			installationID = candidate
			break
		}
		fetchErrs = append(fetchErrs, fmt.Errorf("installation %d: %w", candidate, err))
	}
	if installationID == 0 {
		return fmt.Errorf("fetch pull requests: %w", errors.Join(fetchErrs...))
	}

	closePolicyBindings, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		return fmt.Errorf("load installation bindings: %w", err)
	}
	if len(closePolicyBindings) == 0 {
		return errors.New("selected GitHub installation has no workspace bindings")
	}

	binding := db.GithubInstallation{WorkspaceID: target.workspaceID, InstallationID: installationID}
	for i := range pullRequests {
		if err := h.processGitHubPullRequest(ctx, &pullRequests[i], []db.GithubInstallation{binding}, closePolicyBindings); err != nil {
			return fmt.Errorf("process pull request #%d: %w", pullRequests[i].PullRequest.Number, err)
		}
	}
	if err := h.Queries.UpsertGitHubPRPollCursor(ctx, db.UpsertGitHubPRPollCursorParams{
		WorkspaceID:     target.workspaceID,
		RepoOwner:       cursorOwner,
		RepoName:        cursorRepo,
		CursorUpdatedAt: pgtype.Timestamptz{Time: startedAt, Valid: true},
	}); err != nil {
		return fmt.Errorf("save cursor: %w", err)
	}
	slog.Info("github PR poller: repository poll complete",
		"workspace_id", uuidToString(target.workspaceID),
		"repository", target.owner+"/"+target.repo,
		"pull_requests", len(pullRequests))
	return nil
}

func (h *Handler) fetchUpdatedGitHubPullRequests(ctx context.Context, installationID int64, owner, repo string, cutoff time.Time) ([]ghPullRequestPayload, error) {
	pullRequests := make([]ghPullRequestPayload, 0)
	for page := 1; ; page++ {
		var listed []struct {
			Number    int32  `json:"number"`
			UpdatedAt string `json:"updated_at"`
		}
		listPath := fmt.Sprintf("/repos/%s/%s/pulls?state=all&sort=updated&direction=desc&per_page=%d&page=%d",
			url.PathEscape(owner), url.PathEscape(repo), githubPRPollPageSize, page)
		if err := h.githubAPIGet(ctx, installationID, listPath, &listed); err != nil {
			return nil, fmt.Errorf("list page %d: %w", page, err)
		}
		stop := false
		for _, item := range listed {
			updatedAt, err := time.Parse(time.RFC3339, item.UpdatedAt)
			if err != nil {
				return nil, fmt.Errorf("pull request #%d has invalid updated_at", item.Number)
			}
			if updatedAt.Before(cutoff) {
				stop = true
				break
			}
			var pr ghPullRequest
			detailPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), item.Number)
			if err := h.githubAPIGet(ctx, installationID, detailPath, &pr); err != nil {
				return nil, fmt.Errorf("fetch pull request #%d: %w", item.Number, err)
			}
			if err := validatePolledGitHubPullRequest(pr, item.Number); err != nil {
				return nil, err
			}
			pr.Merged = pr.Merged || pr.MergedAt != ""
			payload := ghPullRequestPayload{Action: "poll", PullRequest: pr}
			if pr.State == "closed" || pr.Merged {
				payload.Action = "closed"
			}
			payload.Installation.ID = installationID
			payload.Repository.Owner.Login = owner
			payload.Repository.Name = repo
			pullRequests = append(pullRequests, payload)
		}
		if stop || len(listed) < githubPRPollPageSize {
			break
		}
	}

	// Backfills must mirror every current open sibling before terminal PRs can
	// evaluate the existing close-intent aggregate.
	sort.SliceStable(pullRequests, func(i, j int) bool {
		iTerminal := polledPRTerminal(pullRequests[i].PullRequest)
		jTerminal := polledPRTerminal(pullRequests[j].PullRequest)
		return !iTerminal && jTerminal
	})
	return pullRequests, nil
}

func validatePolledGitHubPullRequest(pr ghPullRequest, wantNumber int32) error {
	if pr.Number != wantNumber || pr.Number == 0 {
		return fmt.Errorf("github pull request number mismatch: got %d, want %d", pr.Number, wantNumber)
	}
	if pr.HTMLURL == "" || pr.Title == "" || pr.Head.SHA == "" {
		return fmt.Errorf("github pull request #%d is missing required fields", pr.Number)
	}
	if _, err := time.Parse(time.RFC3339, pr.CreatedAt); err != nil {
		return fmt.Errorf("github pull request #%d has invalid created_at", pr.Number)
	}
	if _, err := time.Parse(time.RFC3339, pr.UpdatedAt); err != nil {
		return fmt.Errorf("github pull request #%d has invalid updated_at", pr.Number)
	}
	return nil
}

func polledPRTerminal(pr ghPullRequest) bool {
	return pr.State == "closed" || pr.Merged || pr.MergedAt != ""
}

func appendUniqueInt64(values []int64, value int64) []int64 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func parseGitHubRepositoryURL(raw string) (owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if strings.HasPrefix(strings.ToLower(raw), "git@github.com:") {
		return splitGitHubRepositoryPath(raw[len("git@github.com:"):])
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", false
	}
	return splitGitHubRepositoryPath(parsed.Path)
}

func splitGitHubRepositoryPath(path string) (owner, repo string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner = strings.TrimSpace(parts[0])
	repo = strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git")
	if owner == "" || repo == "" || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", "", false
	}
	return owner, repo, true
}
