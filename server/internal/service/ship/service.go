// Package ship implements Ship Hub Phase 1: GitHub PR sync + deploy
// environment bookkeeping. The service deliberately holds no HTTP /
// websocket plumbing — handlers wire those, this package just talks to
// GitHub and the database.
package ship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// GithubClient is the slice of *gh.Client the service needs. Defining the
// interface here (rather than depending on the concrete type) keeps tests
// from needing an httptest server when they only want to assert on
// upsert behavior.
//
// Phase 3 added the write-side methods. The interface is intentionally a
// subset of *gh.Client's surface — adding a new endpoint requires
// updating both the concrete type and this interface, which is the
// price for the test ergonomics.
type GithubClient interface {
	ListPullRequests(ctx context.Context, owner, repo string, opts gh.ListOptions) ([]gh.PullRequest, error)
	MergePullRequest(ctx context.Context, owner, repo string, prNumber int, method, sha string) (*gh.MergeResult, error)
	UpdatePullRequestBranch(ctx context.Context, owner, repo string, prNumber int, expectedSHA string) error
	CreatePullRequestComment(ctx context.Context, owner, repo string, prNumber int, body string) (*gh.Comment, error)
	DismissPullRequestReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error
	ClosePullRequest(ctx context.Context, owner, repo string, prNumber int) error
	DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string) error
}

// Service is the Ship Hub entry point. Construct one per workspace token
// (so the GithubClient can carry workspace-specific auth) — the periodic
// reconciler in cmd/server constructs a Service per iteration.
type Service struct {
	Q      *db.Queries
	Github GithubClient
	// Now lets tests pin time. nil → time.Now.
	Now func() time.Time
}

// SyncResult is the per-call return shape — the handler echoes it back so
// the UI can show "Synced 12 PRs in 2.1s".
type SyncResult struct {
	Repo     string `json:"repo"`
	Upserted int    `json:"upserted"`
	Errors   int    `json:"errors"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// SyncProject pulls fresh PR data for one project's attached github_repo
// resources and upserts into the pull_request table. Idempotent: every
// sync produces the same final state when GitHub returns the same payload.
//
// We sync open PRs and the most recently updated closed PRs in two calls,
// then merge — this gives the Kanban a complete "in-flight + recently
// shipped" view without scanning every closed PR ever.
func (s *Service) SyncProject(ctx context.Context, workspaceID, projectID pgtype.UUID) (SyncResult, error) {
	if s.Github == nil {
		return SyncResult{}, errors.New("ship: github client not configured")
	}
	resources, err := s.Q.ListProjectResources(ctx, projectID)
	if err != nil {
		return SyncResult{}, fmt.Errorf("ship: list project resources: %w", err)
	}

	result := SyncResult{}
	for _, res := range resources {
		if res.ResourceType != "github_repo" {
			continue
		}
		repoURL, err := repoURLFromResource(res.ResourceRef)
		if err != nil {
			slog.Warn("ship: skipping github_repo resource with bad ref",
				"resource_id", res.ID, "error", err)
			result.Errors++
			continue
		}
		owner, repo, err := gh.ParseRepoURL(repoURL)
		if err != nil {
			slog.Warn("ship: skipping unparseable repo url",
				"resource_id", res.ID, "url", repoURL, "error", err)
			result.Errors++
			continue
		}
		result.Repo = owner + "/" + repo

		// Open + recently-closed in two calls. We don't paginate beyond
		// page 1 because the per_page=50 default already covers the
		// "active churn" window we care about — projects with >50 open
		// PRs are pathological for a Kanban anyway.
		open, err := s.Github.ListPullRequests(ctx, owner, repo, gh.ListOptions{State: "open"})
		if err != nil {
			slog.Warn("ship: list open PRs failed", "repo", result.Repo, "error", err)
			result.Errors++
			// Continue to closed list — partial success is better than
			// nothing for a manual sync trigger.
		}
		closed, err := s.Github.ListPullRequests(ctx, owner, repo, gh.ListOptions{State: "closed", PerPage: 25})
		if err != nil {
			slog.Warn("ship: list closed PRs failed", "repo", result.Repo, "error", err)
			result.Errors++
		}

		for _, pr := range append(open, closed...) {
			if err := s.upsertPR(ctx, workspaceID, projectID, repoURL, pr); err != nil {
				slog.Warn("ship: upsert PR failed",
					"repo", result.Repo, "pr", pr.Number, "error", err)
				result.Errors++
				continue
			}
			result.Upserted++
		}
	}
	return result, nil
}

// SyncWorkspace iterates every project with at least one github_repo
// resource and calls SyncProject. Used by the periodic reconciler. Errors
// from individual projects are logged and skipped — one broken repo must
// not stop the rest of the workspace from updating.
func (s *Service) SyncWorkspace(ctx context.Context, workspaceID pgtype.UUID) error {
	// We list projects in the workspace and let SyncProject filter the
	// resources. Could fetch only github_repo project_resources directly
	// instead, but iterating projects keeps the code simple and the
	// workspace-level reconciler runs every 5 minutes — the extra rows
	// are noise compared to the GitHub round-trip.
	projects, err := s.Q.ListProjects(ctx, db.ListProjectsParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: false,
	})
	if err != nil {
		return fmt.Errorf("ship: list workspace projects: %w", err)
	}
	for _, p := range projects {
		if _, err := s.SyncProject(ctx, workspaceID, p.ID); err != nil {
			slog.Warn("ship: sync project failed",
				"workspace_id", uuidString(workspaceID),
				"project_id", uuidString(p.ID),
				"error", err)
			// Keep going — defensive per-project isolation.
			continue
		}
	}
	return nil
}

// upsertPR maps a gh.PullRequest into UpsertPullRequestParams and writes it.
func (s *Service) upsertPR(
	ctx context.Context,
	workspaceID, projectID pgtype.UUID,
	repoURL string,
	pr gh.PullRequest,
) error {
	state := mapPRState(pr)

	labelsJSON, err := json.Marshal(pr.Labels)
	if err != nil {
		// Should never happen (Labels is plain JSON), but if it did the
		// constraint would reject NULL — fall back to an empty array.
		labelsJSON = []byte(`[]`)
	}

	params := db.UpsertPullRequestParams{
		WorkspaceID:     workspaceID,
		ProjectID:       projectID,
		RepoUrl:         repoURL,
		PrNumber:        int32(pr.Number),
		Title:           pr.Title,
		State:           state,
		IsDraft:         pr.Draft,
		AuthorLogin:     pr.User.Login,
		AuthorAvatarUrl: textOrEmpty(pr.User.AvatarURL),
		BaseRef:         pr.Base.Ref,
		HeadRef:         pr.Head.Ref,
		HeadSha:         pr.Head.SHA,
		HtmlUrl:         pr.HTMLURL,
		Body:            textOrEmpty(pr.Body),
		// CI status is a separate API call; Phase 1 leaves it blank to
		// keep the rate-limit budget under control. The frontend renders
		// "unknown" for empty strings.
		CiStatus:       pgtype.Text{String: "", Valid: true},
		ReviewDecision: pgtype.Text{String: "", Valid: true},
		Mergeable:      mapMergeable(pr.Mergeable),
		Additions:      int32(pr.Additions),
		Deletions:      int32(pr.Deletions),
		ChangedFiles:   int32(pr.ChangedFiles),
		Labels:         labelsJSON,
		PrCreatedAt:    pgTime(pr.CreatedAt),
		PrUpdatedAt:    pgTime(pr.UpdatedAt),
		PrMergedAt:     pgTimePtr(pr.MergedAt),
		PrClosedAt:     pgTimePtr(pr.ClosedAt),
	}
	_, err = s.Q.UpsertPullRequest(ctx, params)
	return err
}

// mapPRState collapses GitHub's two states ("open"/"closed") plus the
// merged_at timestamp into our three-way enum. GitHub's "merged" is just
// "closed AND merged_at IS NOT NULL"; we promote it so the Kanban can
// show a "merged" column without re-deriving it on read.
func mapPRState(pr gh.PullRequest) db.PullRequestState {
	if pr.MergedAt != nil {
		return db.PullRequestStateMerged
	}
	if pr.State == "closed" {
		return db.PullRequestStateClosed
	}
	return db.PullRequestStateOpen
}

// mapMergeable converts GitHub's Boolean (with the *bool null hole) to the
// MERGEABLE/CONFLICTING/UNKNOWN enum we store. nil means GitHub hasn't
// computed it yet — common immediately after an opened-PR webhook.
func mapMergeable(m *bool) pgtype.Text {
	if m == nil {
		return pgtype.Text{String: "UNKNOWN", Valid: true}
	}
	if *m {
		return pgtype.Text{String: "MERGEABLE", Valid: true}
	}
	return pgtype.Text{String: "CONFLICTING", Valid: true}
}

// textOrEmpty returns a Valid=true pgtype.Text even for empty strings.
// Most of these columns are NOT NULL DEFAULT ” on the DB side, so we
// always want to write the actual string.
func textOrEmpty(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func pgTimePtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// repoURLFromResource pulls the `url` field out of a github_repo
// resource_ref blob. The validator in handler/project_resource.go already
// guarantees the field exists, but this re-derives defensively.
func repoURLFromResource(ref []byte) (string, error) {
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(ref, &payload); err != nil {
		return "", fmt.Errorf("invalid github_repo ref: %w", err)
	}
	if payload.URL == "" {
		return "", errors.New("github_repo ref missing url")
	}
	return payload.URL, nil
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
