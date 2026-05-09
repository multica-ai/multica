package ship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// WebhookEvent holds the parsed envelope for one GitHub webhook
// delivery. The handler decodes the headers + body once and passes this
// struct to ProcessWebhook so the per-event-type code doesn't have to
// re-handle HTTP.
type WebhookEvent struct {
	WorkspaceID pgtype.UUID
	DeliveryID  string
	EventType   string
	Body        []byte
}

// WebhookOutcome surfaces what happened so the caller can publish the
// right WS event. Empty PRID / EnvironmentID indicate "nothing to
// publish" — for example a check_run that didn't match any tracked PR.
type WebhookOutcome struct {
	// Kind controls which WS event to fire:
	//   "pr_state_changed" — pull_request:state_changed
	//   "deploy_progress"  — deploy:progress
	//   "deploy_completed" — deploy:completed
	//   "noop"             — nothing to publish
	Kind          string
	PRID          pgtype.UUID
	ProjectID     pgtype.UUID
	State         string
	CIStatus      string
	ReviewDec     string
	EnvironmentID pgtype.UUID
	DeployID      pgtype.UUID
	DeployStatus  string
	SHA           string
}

// ProcessWebhook is the dispatcher. It mutates the DB and returns one
// outcome per event; the caller is responsible for marking the
// delivery row processed and publishing the WS event.
//
// This function is the only place per-event-type knowledge lives —
// keeping all the GitHub-payload-specific shape mapping in one file
// makes future event additions a one-stop change.
func (s *Service) ProcessWebhook(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	switch ev.EventType {
	case "pull_request":
		return s.processPullRequest(ctx, ev)
	case "pull_request_review":
		return s.processPullRequestReview(ctx, ev)
	case "check_run":
		return s.processCheckRun(ctx, ev)
	case "status":
		return s.processStatus(ctx, ev)
	case "deployment":
		return s.processDeployment(ctx, ev)
	case "deployment_status":
		return s.processDeploymentStatus(ctx, ev)
	case "push":
		return s.processPush(ctx, ev)
	default:
		// Quietly ignore — GitHub sends ping, repository, etc. that we
		// have nothing to do with. Returning a "noop" outcome lets the
		// caller still mark the delivery processed.
		slog.Debug("ship webhook: ignoring unhandled event type", "event_type", ev.EventType)
		return WebhookOutcome{Kind: "noop"}, nil
	}
}

// processPullRequest handles pull_request.opened/synchronize/closed/...
// We resolve the project by repo_url, upsert the PR row, then return
// the state for the WS event.
func (s *Service) processPullRequest(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.PullRequestEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode pull_request payload: %w", err)
	}
	repoURL := payload.Repository.HTMLURL
	project, err := s.resolveProject(ctx, ev.WorkspaceID, repoURL)
	if err != nil {
		return WebhookOutcome{}, err
	}
	pr := payload.PullRequest
	pr.Number = payload.Number // PR list endpoints carry .number on the event envelope; mirror it onto the embedded struct so downstream code uses one source.
	if err := s.upsertPR(ctx, ev.WorkspaceID, project.ID, repoURL, pr); err != nil {
		return WebhookOutcome{}, fmt.Errorf("upsert pr: %w", err)
	}
	row, err := s.Q.GetPullRequestByNumber(ctx, db.GetPullRequestByNumberParams{
		WorkspaceID: ev.WorkspaceID,
		RepoUrl:     repoURL,
		PrNumber:    int32(pr.Number),
	})
	if err != nil {
		return WebhookOutcome{}, fmt.Errorf("re-read pr: %w", err)
	}
	return WebhookOutcome{
		Kind:      "pr_state_changed",
		PRID:      row.ID,
		ProjectID: row.ProjectID,
		State:     string(row.State),
		CIStatus:  textValue(row.CiStatus),
		ReviewDec: textValue(row.ReviewDecision),
	}, nil
}

// processPullRequestReview persists the review and recomputes the PR's
// review_decision rollup. The decision rule: any CHANGES_REQUESTED
// dominates; otherwise APPROVED if at least one approval and no
// non-approval among the latest distinct-reviewer reviews; otherwise
// REVIEW_REQUIRED.
func (s *Service) processPullRequestReview(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.PullRequestReviewEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode review payload: %w", err)
	}
	repoURL := payload.Repository.HTMLURL
	prRow, err := s.Q.GetPullRequestByNumber(ctx, db.GetPullRequestByNumberParams{
		WorkspaceID: ev.WorkspaceID,
		RepoUrl:     repoURL,
		PrNumber:    int32(payload.PullRequest.Number),
	})
	if err != nil {
		// PR not yet in our cache — defer; the next reconciler tick will
		// pick it up. We still want the delivery row marked processed,
		// so swallow the error.
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("ship webhook: review for unknown PR, skipping",
				"repo", repoURL, "pr_number", payload.PullRequest.Number)
			return WebhookOutcome{Kind: "noop"}, nil
		}
		return WebhookOutcome{}, fmt.Errorf("get pr: %w", err)
	}
	state := strings.ToUpper(strings.TrimSpace(payload.Review.State))
	if _, err := s.Q.UpsertPullRequestReview(ctx, db.UpsertPullRequestReviewParams{
		WorkspaceID:       ev.WorkspaceID,
		PullRequestID:     prRow.ID,
		ReviewerLogin:     payload.Review.User.Login,
		ReviewerAvatarUrl: textOrEmpty(payload.Review.User.AvatarURL),
		State:             state,
		Body:              textOrEmpty(payload.Review.Body),
		SubmittedAt:       pgTime(payload.Review.SubmittedAt),
	}); err != nil {
		return WebhookOutcome{}, fmt.Errorf("upsert review: %w", err)
	}
	decision, err := s.recomputeReviewDecision(ctx, prRow.ID)
	if err != nil {
		return WebhookOutcome{}, err
	}
	updated, err := s.Q.UpdatePullRequestReviewDecision(ctx, db.UpdatePullRequestReviewDecisionParams{
		ID:             prRow.ID,
		ReviewDecision: pgtype.Text{String: decision, Valid: true},
	})
	if err != nil {
		return WebhookOutcome{}, fmt.Errorf("update review decision: %w", err)
	}
	return WebhookOutcome{
		Kind:      "pr_state_changed",
		PRID:      updated.ID,
		ProjectID: updated.ProjectID,
		State:     string(updated.State),
		CIStatus:  textValue(updated.CiStatus),
		ReviewDec: decision,
	}, nil
}

// recomputeReviewDecision folds the latest review per distinct
// reviewer. ListReviewsForPR is ordered submitted_at DESC so the first
// time we see a reviewer wins.
func (s *Service) recomputeReviewDecision(ctx context.Context, prID pgtype.UUID) (string, error) {
	rows, err := s.Q.ListReviewsForPR(ctx, prID)
	if err != nil {
		return "", fmt.Errorf("list reviews: %w", err)
	}
	seen := map[string]bool{}
	hasApproval := false
	for _, r := range rows {
		if seen[r.ReviewerLogin] {
			continue
		}
		seen[r.ReviewerLogin] = true
		switch strings.ToUpper(r.State) {
		case "CHANGES_REQUESTED":
			return "CHANGES_REQUESTED", nil
		case "APPROVED":
			hasApproval = true
		}
	}
	if hasApproval {
		return "APPROVED", nil
	}
	return "REVIEW_REQUIRED", nil
}

// processCheckRun maps a check_run.completed event onto the
// pull_request_check rows for any tracked PR sharing the head_sha.
func (s *Service) processCheckRun(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.CheckRunEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode check_run payload: %w", err)
	}
	if payload.Action != "completed" && payload.Action != "rerequested" && payload.Action != "created" {
		return WebhookOutcome{Kind: "noop"}, nil
	}
	repoURL := payload.Repository.HTMLURL
	for _, attached := range payload.CheckRun.PullRequests {
		prRow, err := s.Q.GetPullRequestByNumber(ctx, db.GetPullRequestByNumberParams{
			WorkspaceID: ev.WorkspaceID,
			RepoUrl:     repoURL,
			PrNumber:    int32(attached.Number),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return WebhookOutcome{}, fmt.Errorf("get pr for check_run: %w", err)
		}
		if _, err := s.Q.UpsertPullRequestCheck(ctx, db.UpsertPullRequestCheckParams{
			WorkspaceID:   ev.WorkspaceID,
			PullRequestID: prRow.ID,
			HeadSha:       payload.CheckRun.HeadSHA,
			Name:          payload.CheckRun.Name,
			Conclusion:    pgtype.Text{String: payload.CheckRun.Conclusion, Valid: true},
			Status:        payload.CheckRun.Status,
			DetailsUrl:    pgtype.Text{String: payload.CheckRun.DetailsURL, Valid: payload.CheckRun.DetailsURL != ""},
			StartedAt:     pgTime(payload.CheckRun.StartedAt),
			CompletedAt:   pgTime(payload.CheckRun.CompletedAt),
		}); err != nil {
			return WebhookOutcome{}, fmt.Errorf("upsert check: %w", err)
		}
		ciStatus, err := s.recomputeCIStatus(ctx, prRow.ID, prRow.HeadSha)
		if err != nil {
			return WebhookOutcome{}, err
		}
		updated, err := s.Q.UpdatePullRequestCIStatus(ctx, db.UpdatePullRequestCIStatusParams{
			ID:       prRow.ID,
			CiStatus: pgtype.Text{String: ciStatus, Valid: true},
		})
		if err != nil {
			return WebhookOutcome{}, fmt.Errorf("update ci_status: %w", err)
		}
		// We only emit one outcome — even if multiple PRs share the head
		// sha (rare), one signal is enough for the frontend to refresh.
		return WebhookOutcome{
			Kind:      "pr_state_changed",
			PRID:      updated.ID,
			ProjectID: updated.ProjectID,
			State:     string(updated.State),
			CIStatus:  ciStatus,
			ReviewDec: textValue(updated.ReviewDecision),
		}, nil
	}
	return WebhookOutcome{Kind: "noop"}, nil
}

// processStatus is the legacy combined-status sibling of check_run.
// Some older repos still publish via this API; we treat each context as
// its own row in pull_request_check (synthetic "name" = context).
func (s *Service) processStatus(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.StatusEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode status payload: %w", err)
	}
	repoURL := payload.Repository.HTMLURL
	// Find any open PR whose head_sha matches. Status events don't
	// carry the PR number directly, so we scan the project's PR rows.
	project, err := s.resolveProject(ctx, ev.WorkspaceID, repoURL)
	if err != nil {
		return WebhookOutcome{Kind: "noop"}, nil // unknown repo, swallow
	}
	prs, err := s.Q.ListPullRequestsByProject(ctx, db.ListPullRequestsByProjectParams{
		ProjectID: project.ID,
		State:     db.NullPullRequestState{PullRequestState: db.PullRequestStateOpen, Valid: true},
	})
	if err != nil {
		return WebhookOutcome{}, fmt.Errorf("list project PRs: %w", err)
	}
	conclusion := mapStatusToConclusion(payload.State)
	for _, pr := range prs {
		if pr.HeadSha != payload.SHA {
			continue
		}
		if _, err := s.Q.UpsertPullRequestCheck(ctx, db.UpsertPullRequestCheckParams{
			WorkspaceID:   ev.WorkspaceID,
			PullRequestID: pr.ID,
			HeadSha:       payload.SHA,
			Name:          payload.Context,
			Conclusion:    pgtype.Text{String: conclusion, Valid: true},
			Status:        "completed",
			DetailsUrl:    pgtype.Text{String: payload.TargetURL, Valid: payload.TargetURL != ""},
			CompletedAt:   pgTime(payload.UpdatedAt),
		}); err != nil {
			return WebhookOutcome{}, fmt.Errorf("upsert status check: %w", err)
		}
		ciStatus, err := s.recomputeCIStatus(ctx, pr.ID, payload.SHA)
		if err != nil {
			return WebhookOutcome{}, err
		}
		updated, err := s.Q.UpdatePullRequestCIStatus(ctx, db.UpdatePullRequestCIStatusParams{
			ID:       pr.ID,
			CiStatus: pgtype.Text{String: ciStatus, Valid: true},
		})
		if err != nil {
			return WebhookOutcome{}, fmt.Errorf("update ci_status: %w", err)
		}
		return WebhookOutcome{
			Kind:      "pr_state_changed",
			PRID:      updated.ID,
			ProjectID: updated.ProjectID,
			State:     string(updated.State),
			CIStatus:  ciStatus,
			ReviewDec: textValue(updated.ReviewDecision),
		}, nil
	}
	return WebhookOutcome{Kind: "noop"}, nil
}

// mapStatusToConclusion translates a legacy status state to our
// pull_request_check.conclusion vocabulary so the rollup logic can
// stay in one place.
func mapStatusToConclusion(state string) string {
	switch strings.ToLower(state) {
	case "success":
		return "success"
	case "failure", "error":
		return "failure"
	case "pending":
		return ""
	default:
		return ""
	}
}

// recomputeCIStatus folds every check on the PR's current head_sha.
// failure dominates; otherwise all-success is success; otherwise
// pending. Matches GitHub's own "checks summary" UI.
func (s *Service) recomputeCIStatus(ctx context.Context, prID pgtype.UUID, headSha string) (string, error) {
	rows, err := s.Q.ListChecksForPRHead(ctx, db.ListChecksForPRHeadParams{
		PullRequestID: prID,
		HeadSha:       headSha,
	})
	if err != nil {
		return "", fmt.Errorf("list checks: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	allSuccess := true
	for _, r := range rows {
		concl := strings.ToLower(textValue(r.Conclusion))
		switch concl {
		case "failure", "cancelled", "timed_out":
			return "failure", nil
		case "success", "neutral", "skipped":
			// ok
		default:
			allSuccess = false
		}
	}
	if allSuccess {
		return "success", nil
	}
	return "pending", nil
}

// processDeployment maps the create-side of a deployment event into a
// pending deploy row. Status transitions arrive via deployment_status.
func (s *Service) processDeployment(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.DeploymentEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode deployment payload: %w", err)
	}
	repoURL := payload.Repository.HTMLURL
	env, err := s.Q.GetDeployEnvironmentByRepoAndName(ctx, db.GetDeployEnvironmentByRepoAndNameParams{
		WorkspaceID: ev.WorkspaceID,
		RepoUrl:     repoURL,
		EnvName:     payload.Deployment.Environment,
	})
	if err != nil {
		// No matching env — Phase 1 stores envs by NAME (e.g. "staging",
		// "production"); GitHub's enum is identical in normal usage so a
		// miss is "user hasn't set this env up yet". Skip silently.
		if errors.Is(err, pgx.ErrNoRows) {
			return WebhookOutcome{Kind: "noop"}, nil
		}
		return WebhookOutcome{}, fmt.Errorf("find env for deployment: %w", err)
	}
	deploy, err := s.Q.InsertDeploy(ctx, db.InsertDeployParams{
		WorkspaceID:   ev.WorkspaceID,
		EnvironmentID: env.ID,
		Ref:           payload.Deployment.Ref,
		Sha:           payload.Deployment.SHA,
		Status:        db.DeployStatusPending,
	})
	if err != nil {
		return WebhookOutcome{}, fmt.Errorf("insert deploy: %w", err)
	}
	return WebhookOutcome{
		Kind:          "deploy_progress",
		EnvironmentID: env.ID,
		DeployID:      deploy.ID,
		DeployStatus:  string(deploy.Status),
		SHA:           deploy.Sha,
	}, nil
}

// processDeploymentStatus updates the deploy row in place. On success
// we also bump deploy_environment.current_sha so the "what's running"
// answer is one column read.
func (s *Service) processDeploymentStatus(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.DeploymentStatusEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode deployment_status payload: %w", err)
	}
	repoURL := payload.Repository.HTMLURL
	env, err := s.Q.GetDeployEnvironmentByRepoAndName(ctx, db.GetDeployEnvironmentByRepoAndNameParams{
		WorkspaceID: ev.WorkspaceID,
		RepoUrl:     repoURL,
		EnvName:     payload.Deployment.Environment,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WebhookOutcome{Kind: "noop"}, nil
		}
		return WebhookOutcome{}, fmt.Errorf("find env for deployment_status: %w", err)
	}
	deploy, err := s.Q.GetDeployByEnvAndSHA(ctx, db.GetDeployByEnvAndSHAParams{
		EnvironmentID: env.ID,
		Sha:           payload.Deployment.SHA,
	})
	if err != nil {
		// No matching deploy — happens when the deployment_status arrives
		// before the deployment.created event (race) or for SHAs we never
		// saw. Insert a synthetic row so the timeline isn't dropped.
		if errors.Is(err, pgx.ErrNoRows) {
			deploy, err = s.Q.InsertDeploy(ctx, db.InsertDeployParams{
				WorkspaceID:   ev.WorkspaceID,
				EnvironmentID: env.ID,
				Ref:           payload.Deployment.Ref,
				Sha:           payload.Deployment.SHA,
				Status:        db.DeployStatusPending,
			})
			if err != nil {
				return WebhookOutcome{}, fmt.Errorf("synth deploy row: %w", err)
			}
		} else {
			return WebhookOutcome{}, fmt.Errorf("get deploy: %w", err)
		}
	}
	newStatus := mapDeploymentStatusState(payload.DeploymentStatus.State)
	now := pgTime(s.now())
	startedAt := pgtype.Timestamptz{}
	completedAt := pgtype.Timestamptz{}
	switch newStatus {
	case db.DeployStatusInProgress:
		startedAt = now
	case db.DeployStatusSucceeded, db.DeployStatusFailed, db.DeployStatusRolledBack:
		startedAt = now
		completedAt = now
	}
	updated, err := s.Q.UpdateDeployStatus(ctx, db.UpdateDeployStatusParams{
		ID:           deploy.ID,
		Status:       newStatus,
		StartedAt:    startedAt,
		CompletedAt:  completedAt,
		LogUrl:       pgtype.Text{String: payload.DeploymentStatus.LogURL, Valid: payload.DeploymentStatus.LogURL != ""},
		ErrorMessage: pgtype.Text{},
	})
	if err != nil {
		return WebhookOutcome{}, fmt.Errorf("update deploy status: %w", err)
	}
	if newStatus == db.DeployStatusSucceeded {
		_, _ = s.Q.UpdateDeployEnvironmentCurrent(ctx, db.UpdateDeployEnvironmentCurrentParams{
			ID:                env.ID,
			CurrentSha:        pgtype.Text{String: updated.Sha, Valid: true},
			CurrentDeployedAt: now,
		})
	}
	kind := "deploy_progress"
	if newStatus == db.DeployStatusSucceeded || newStatus == db.DeployStatusFailed || newStatus == db.DeployStatusRolledBack {
		kind = "deploy_completed"
	}
	return WebhookOutcome{
		Kind:          kind,
		EnvironmentID: env.ID,
		DeployID:      updated.ID,
		DeployStatus:  string(updated.Status),
		SHA:           updated.Sha,
	}, nil
}

// mapDeploymentStatusState maps GitHub's deployment_status.state strings
// to our deploy_status enum. "queued" collapses into "pending" because
// our enum doesn't distinguish them — the UI shows both as "waiting".
func mapDeploymentStatusState(s string) db.DeployStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success":
		return db.DeployStatusSucceeded
	case "failure", "error":
		return db.DeployStatusFailed
	case "in_progress":
		return db.DeployStatusInProgress
	case "queued", "pending":
		return db.DeployStatusPending
	case "inactive":
		return db.DeployStatusRolledBack
	default:
		return db.DeployStatusPending
	}
}

// processPush triggers a SyncProject for the repo so any branch protection
// merges (which don't fire pull_request webhooks) catch up immediately.
// We only act on pushes to the default branch — feature-branch pushes
// already land via the pull_request.synchronize signal.
func (s *Service) processPush(ctx context.Context, ev WebhookEvent) (WebhookOutcome, error) {
	var payload gh.PushEvent
	if err := json.Unmarshal(ev.Body, &payload); err != nil {
		return WebhookOutcome{}, fmt.Errorf("decode push payload: %w", err)
	}
	if !strings.HasSuffix(payload.Ref, "/"+payload.Repository.DefaultBranch) {
		return WebhookOutcome{Kind: "noop"}, nil
	}
	repoURL := payload.Repository.HTMLURL
	project, err := s.resolveProject(ctx, ev.WorkspaceID, repoURL)
	if err != nil {
		return WebhookOutcome{Kind: "noop"}, nil
	}
	if _, err := s.SyncProject(ctx, ev.WorkspaceID, project.ID); err != nil {
		// Non-fatal; the next reconciler tick will retry.
		slog.Warn("ship webhook: post-push sync failed",
			"workspace_id", uuidString(ev.WorkspaceID),
			"project_id", uuidString(project.ID),
			"error", err)
	}
	return WebhookOutcome{Kind: "noop"}, nil
}

// resolveProject looks up a project in the workspace by its github_repo
// resource URL.
func (s *Service) resolveProject(ctx context.Context, workspaceID pgtype.UUID, repoURL string) (db.Project, error) {
	if repoURL == "" {
		return db.Project{}, errors.New("ship webhook: repo url is empty")
	}
	project, err := s.Q.FindProjectByRepoURL(ctx, db.FindProjectByRepoURLParams{
		WorkspaceID: workspaceID,
		RepoUrl:     repoURL,
	})
	if err != nil {
		return db.Project{}, fmt.Errorf("find project by repo url: %w", err)
	}
	return project, nil
}

// textValue returns the string side of a pgtype.Text, or "" when null.
func textValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}
