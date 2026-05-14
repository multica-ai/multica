// Phase 7b — Merge train orchestration.
//
// A merge train walks a release's PRs in `position` order and merges
// each via the workspace GitHub client. The HTTP layer fires the
// orchestrator and immediately returns 202 Accepted — the goroutine
// runs in the background, posts WS progress events, and either reaches
// stage=in_staging (success) or pauses with merge_paused=TRUE on a
// failure.
//
// Key design points the merge train relies on:
//
//   * The orchestrator goroutine is started under a SERVICE-LEVEL
//     context, NOT the request context that triggered it. The request
//     context dies the moment the HTTP response is written; if we used
//     it, the very first GitHub call would race against context
//     cancellation and the train would die mid-flight. The caller
//     supplies a `parentCtx` (typically the long-lived sweepCtx from
//     cmd/server/main.go) that survives request lifecycle.
//
//   * Idempotency. Two concurrent start_merge requests must not spawn
//     two orchestrators. We guard with a process-local map keyed by
//     release_id + the durable merge_paused / stage flags. Calling
//     StartMerge on an already-running release returns
//     ErrMergeAlreadyRunning so the handler can answer 409 (or, more
//     friendly, 202 with a "no-op" body). Same for Resume —
//     ResumeMerge only acts when merge_paused=TRUE.
//
//   * Per-PR retry. GitHub flakes (5xx, network) are transient; the
//     orchestrator retries each PR up to 3 times with linear backoff
//     before giving up and pausing the train.
//
//   * Linkage on merge. After each successful PR merge we synchronously
//     run the same auto-close-on-merge pathway the webhook handler
//     would: post a comment on the linked issue, optionally close it.
//     This keeps the merge-train UX honest even if the webhook
//     delivery is delayed or lost (which we've observed in practice).
//
// Channel posts and WS events are best-effort — a failed Slack-style
// post never causes the train itself to fail.

package ship

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Sentinel errors specific to the merge train flow. The handler maps
// these to clean status codes; callers should never `==` against the
// underlying errors-package value.
var (
	ErrReleaseStageMismatch = errors.New("release: stage does not allow this transition")
	ErrMergeAlreadyRunning  = errors.New("release: merge train already running")
	ErrMergeNotPaused       = errors.New("release: merge train is not paused")
	ErrTokenMissing         = errors.New("release: workspace github token is not configured")
	ErrPreconditionFailed   = errors.New("release: preconditions not met")
	ErrInvalidMergeMethod   = errors.New("release: invalid merge method")
)

// MergePreconditionError carries a human-readable list of reasons the
// merge train cannot start. Returned by StartMerge when the release is
// in the right stage but one or more PRs aren't eligible.
type MergePreconditionError struct {
	Reasons []string
}

func (e *MergePreconditionError) Error() string {
	if len(e.Reasons) == 0 {
		return "release: preconditions not met"
	}
	return "release: preconditions not met: " + strings.Join(e.Reasons, "; ")
}

func (*MergePreconditionError) Is(target error) bool { return target == ErrPreconditionFailed }

// MergeEventPublisher is the slim slice of the events bus the merge
// orchestrator needs. Defining it as an interface here keeps the
// service decoupled from `internal/events` (which would force a
// dependency cycle through the handler).
//
// The handler wires a real implementation that calls h.publish().
// Tests pass nil to skip WS publication.
type MergeEventPublisher interface {
	PublishMergeEvent(eventType, workspaceID string, payload map[string]any)
}

// mergeOrchestratorRegistry tracks running orchestrators per release
// so a duplicate StartMerge returns ErrMergeAlreadyRunning instead of
// spawning a second goroutine. Process-local: a multi-replica deploy
// would need a Postgres advisory lock, but Phase 7b's targets ship
// from a single API instance and we don't need cross-process
// coordination yet.
type mergeOrchestratorRegistry struct {
	mu      sync.Mutex
	running map[string]struct{} // release_id (uuid string) → present
}

var globalMergeRegistry = &mergeOrchestratorRegistry{running: map[string]struct{}{}}

func (r *mergeOrchestratorRegistry) tryClaim(releaseID pgtype.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := uuidString(releaseID)
	if _, exists := r.running[key]; exists {
		return false
	}
	r.running[key] = struct{}{}
	return true
}

func (r *mergeOrchestratorRegistry) release(releaseID pgtype.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, uuidString(releaseID))
}

// MergeTrainDeps bundles everything the orchestrator needs that
// doesn't naturally belong on the Service struct (publishers,
// channel ops). Passed in by the handler at call time so the service
// stays free of import-cycle prone deps.
type MergeTrainDeps struct {
	// ParentCtx is the long-lived service context the orchestrator's
	// goroutine derives from. MUST outlive any single HTTP request
	// (e.g. the cmd/server sweepCtx). nil falls back to
	// context.Background which is fine for tests.
	ParentCtx context.Context
	// ChannelOps is reused from Phase 7a — the orchestrator only
	// needs CreateReleaseChannel/ArchiveReleaseChannel (well, only
	// archive on abort), but accepting the same interface keeps the
	// handler wiring identical to CreateRelease.
	ChannelOps ChannelOps
	// Publisher is the WS event publisher. nil = quiet (tests).
	Publisher MergeEventPublisher
	// PostToReleaseChannel is the analog of PostToPRChannel. The
	// merge orchestrator posts progress lines into the release's
	// auto-created channel; nil disables.
	PostToReleaseChannel func(ctx context.Context, channelID pgtype.UUID, content string) error
	// Now is the clock. nil → time.Now. Lets tests pin event ts.
	Now func() time.Time
}

func (d *MergeTrainDeps) now() time.Time {
	if d == nil || d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func (d *MergeTrainDeps) parentCtx() context.Context {
	if d == nil || d.ParentCtx == nil {
		return context.Background()
	}
	return d.ParentCtx
}

// StartMerge transitions a release from assembling → merging and
// kicks off the orchestrator goroutine.
//
// Preconditions:
//   - release.stage == "assembling"
//   - All PRs eligible per Phase 7a CreateRelease checks (open, not
//     draft, mergeable, CI green, APPROVED).
//   - approver_id set if risk_level >= medium; second_approver_id set
//     if risk_level == critical.
//   - Workspace GitHub client configured (s.Github != nil).
//
// Returns nil on success (orchestrator goroutine started); a typed
// error otherwise. The 202 Accepted response confirms "your request
// was accepted"; clients poll /releases/{id}/merge_state or listen
// on WS for progress.
func (s *Service) StartMerge(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	mergeMethod string,
	approvalRule string,
	deps *MergeTrainDeps,
) error {
	if s.Github == nil {
		return ErrTokenMissing
	}

	method, err := normalizeMergeMethod(mergeMethod)
	if err != nil {
		return err
	}

	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageAssembling {
		return fmt.Errorf("%w: stage=%s, want assembling", ErrReleaseStageMismatch, release.Stage)
	}

	// Eligibility re-check. CreateRelease already gated this at create
	// time, but we're at start_merge time now and the world has
	// shifted: a PR's CI may have flipped red since the release was
	// assembled. Catching it here gives the user a precise reason
	// instead of a mid-train pause halfway through.
	prRows, err := s.Q.ListReleasePullRequests(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("list release prs: %w", err)
	}
	reasons := []string{}
	for _, row := range prRows {
		// We need a db.PullRequest to feed releaseEligibilityReason;
		// the row already carries every column we need so build it
		// inline rather than another GetPullRequest round-trip.
		pr := pullRequestFromListRow(row)
		if reason, ok := releaseEligibilityReason(pr); !ok {
			reasons = append(reasons, fmt.Sprintf("PR #%d %s", pr.PrNumber, reason))
		}
	}
	// Approver gate. The workspace's per-risk-tier approval rule
	// decides whether an approver_id (and second_approver_id) must be
	// set before start_merge. Previously this was hardcoded to require
	// approvers for every medium+ release regardless of the workspace's
	// configured rule — which contradicted the verify-path code that
	// DID respect the rule, and made `ship_hub_approval_medium=member`
	// useless. The user could not start a merge train even with the
	// rule set to "any member can verify."
	//
	// Now the gate mirrors the verify-time logic: only `approver` and
	// `two` rules require approver_id; `member` / `admin` / empty rules
	// don't block start_merge on approver presence. `resolveApprovalRule`
	// supplies the legacy defaults when `approvalRule` is "".
	rule := resolveApprovalRule(approvalRule, release.RiskLevel)
	switch rule {
	case ApprovalRuleApprover:
		if !release.ApproverID.Valid {
			reasons = append(reasons, "approver required for risk level "+string(release.RiskLevel))
		}
	case ApprovalRuleTwo:
		if !release.ApproverID.Valid {
			reasons = append(reasons, "approver required for risk level "+string(release.RiskLevel))
		}
		if !release.SecondApproverID.Valid {
			reasons = append(reasons, "second approver required for risk level "+string(release.RiskLevel))
		}
	}
	if len(reasons) > 0 {
		return &MergePreconditionError{Reasons: reasons}
	}

	// Idempotency — if this release already has an orchestrator in
	// flight (which can only mean a near-simultaneous duplicate
	// request, since the stage flip below would otherwise have
	// blocked us), bail.
	if !globalMergeRegistry.tryClaim(releaseID) {
		return ErrMergeAlreadyRunning
	}

	// Stamp the merge_method first; if it fails the stage flip below
	// would still produce a working train but the user would see the
	// default method instead of their choice.
	if _, err := s.Q.SetReleaseMergeMethod(ctx, db.SetReleaseMergeMethodParams{
		ID:          releaseID,
		MergeMethod: method,
	}); err != nil {
		globalMergeRegistry.release(releaseID)
		return fmt.Errorf("set merge method: %w", err)
	}

	// Stage flip: assembling → merging. We stamp merged_at later
	// when the train finishes; setting it here would lie about
	// "release fully merged" being true.
	updated, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:    releaseID,
		Stage: db.ReleaseStageMerging,
	})
	if err != nil {
		globalMergeRegistry.release(releaseID)
		return fmt.Errorf("update stage: %w", err)
	}

	// Audit log + WS event before we hand off to the goroutine —
	// these need ctx (request-scoped is fine because they're cheap
	// and synchronous).
	_, _ = s.insertReleaseEvent(ctx, releaseID, "merge_train_started", requestedBy, map[string]any{
		"merge_method": method,
		"pr_count":     len(prRows),
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseMergeStarted, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"total_prs":  len(prRows),
		})
	}
	postReleaseChannel(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"🚂 Merge train started · %d PR%s to merge in sequence", len(prRows), pluralS(len(prRows)),
	))

	// Spawn the goroutine under the parent (long-lived) context.
	// The request ctx is about to be cancelled when the HTTP
	// response writes — using it here would kill the train on its
	// first iteration.
	go func() {
		defer globalMergeRegistry.release(releaseID)
		s.runMergeTrain(deps.parentCtx(), releaseID, requestedBy, deps)
	}()

	return nil
}

// ResumeMerge clears merge_paused on a paused release and restarts the
// orchestrator. Optional skip list marks specific PRs as "skipped" so
// the train moves past them without retrying — useful when a PR is
// hopelessly conflicting and the user wants to abandon it without
// aborting the whole release.
func (s *Service) ResumeMerge(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	skipPRIDs []pgtype.UUID,
	deps *MergeTrainDeps,
) error {
	if s.Github == nil {
		return ErrTokenMissing
	}
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageMerging {
		return fmt.Errorf("%w: stage=%s, want merging", ErrReleaseStageMismatch, release.Stage)
	}
	if !release.MergePaused {
		return ErrMergeNotPaused
	}

	// Mark explicit skips. Each row's merge_state goes failed/queued
	// → skipped; the orchestrator's main loop ignores skipped rows.
	for _, prID := range skipPRIDs {
		if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
			ReleaseID:     releaseID,
			PullRequestID: prID,
			MergeState:    db.PrMergeStateSkipped,
			MergeError:    pgtype.Text{String: "skipped on resume", Valid: true},
		}); err != nil {
			slog.Warn("ship: skip pr on resume failed",
				"release_id", uuidString(releaseID), "pr_id", uuidString(prID), "error", err)
		}
	}

	// Failed PRs that AREN'T in the skip list need to flip back to
	// queued so the orchestrator picks them up again. A failed row
	// otherwise stays terminal and the train would no-op.
	prRows, err := s.Q.ListReleasePRsForMerge(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("list release prs: %w", err)
	}
	skipSet := map[string]struct{}{}
	for _, id := range skipPRIDs {
		skipSet[uuidString(id)] = struct{}{}
	}
	for _, row := range prRows {
		if row.MergeState != db.PrMergeStateFailed {
			continue
		}
		if _, skipped := skipSet[uuidString(row.PullRequestID)]; skipped {
			continue
		}
		if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
			ReleaseID:     releaseID,
			PullRequestID: row.PullRequestID,
			MergeState:    db.PrMergeStateQueued,
			MergeError:    pgtype.Text{String: "", Valid: true}, // clear prior error
		}); err != nil {
			slog.Warn("ship: requeue failed pr on resume failed",
				"release_id", uuidString(releaseID), "pr_id", uuidString(row.PullRequestID), "error", err)
		}
	}

	if !globalMergeRegistry.tryClaim(releaseID) {
		return ErrMergeAlreadyRunning
	}

	updated, err := s.Q.SetReleaseMergePaused(ctx, db.SetReleaseMergePausedParams{
		ID:          releaseID,
		MergePaused: false,
	})
	if err != nil {
		globalMergeRegistry.release(releaseID)
		return fmt.Errorf("clear merge paused: %w", err)
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "merge_train_resumed", requestedBy, map[string]any{
		"skipped_pr_ids": uuidStringsFromSlice(skipPRIDs),
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannel(deps, ctx, updated.ChannelID, "▶ Resuming merge train")

	go func() {
		defer globalMergeRegistry.release(releaseID)
		s.runMergeTrain(deps.parentCtx(), releaseID, requestedBy, deps)
	}()

	return nil
}

// AbortMergeTrain cancels a running or paused merging release. PRs
// that already merged stay merged — un-merging is the user's
// responsibility (Phase 7d will offer rollback). The release row is
// flipped to stage=cancelled and the channel post records the
// decision.
func (s *Service) AbortMergeTrain(
	ctx context.Context,
	releaseID pgtype.UUID,
	reason string,
	cancelledBy pgtype.UUID,
	channelOps ChannelOps,
	issueOps IssueOps,
	deps *MergeTrainDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageMerging {
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s, want merging", ErrReleaseStageMismatch, release.Stage)
	}

	now := s.now()
	updated, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:             releaseID,
		Stage:          db.ReleaseStageCancelled,
		DoneAt:         pgtype.Timestamptz{Time: now, Valid: true},
		RollbackReason: pgtype.Text{String: reason, Valid: reason != ""},
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("update stage: %w", err)
	}
	// Clear the paused flag too — abort supersedes pause.
	if _, err := s.Q.SetReleaseMergePaused(ctx, db.SetReleaseMergePausedParams{
		ID:          releaseID,
		MergePaused: false,
	}); err != nil {
		// Best-effort: the row is now in stage=cancelled, the flag is
		// vestigial. Log and continue.
		slog.Warn("ship: clear merge paused on abort failed",
			"release_id", uuidString(releaseID), "error", err)
	}

	if err := s.Q.DeactivateReleasePullRequests(ctx, releaseID); err != nil {
		return updated, fmt.Errorf("deactivate prs: %w", err)
	}

	if release.ChannelID.Valid && channelOps != nil {
		if err := channelOps.ArchiveReleaseChannel(ctx, release.ChannelID); err != nil {
			_, _ = s.insertReleaseEvent(ctx, releaseID, "warning", cancelledBy, map[string]any{
				"reason": "channel archive failed: " + err.Error(),
			})
		}
	}
	if release.IssueID.Valid && issueOps != nil {
		if err := issueOps.CloseReleaseIssue(ctx, release.IssueID, "cancelled"); err != nil {
			_, _ = s.insertReleaseEvent(ctx, releaseID, "warning", cancelledBy, map[string]any{
				"reason": "issue close failed: " + err.Error(),
			})
		}
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "merge_train_aborted", cancelledBy, map[string]any{
		"reason": reason,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseMergeAborted, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"reason":     reason,
		})
	}
	postReleaseChannel(deps, ctx, release.ChannelID, fmt.Sprintf("🛑 Merge train cancelled: %s", reason))

	return updated, nil
}

// runMergeTrain is the orchestrator goroutine body. It walks queued
// PRs in position order, merges each via the workspace GitHub
// client, updates state. On any non-recoverable failure: sets
// merge_paused=true, posts to channel, exits. On all-merged:
// transitions stage to in_staging.
func (s *Service) runMergeTrain(
	ctx context.Context,
	releaseID, actor pgtype.UUID,
	deps *MergeTrainDeps,
) {
	releaseRow, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		slog.Warn("ship: merge train get release failed",
			"release_id", uuidString(releaseID), "error", err)
		return
	}
	workspaceID := releaseRow.WorkspaceID
	mergeMethod := releaseRow.MergeMethod

	// Total includes already-merged + queued + failed (which the
	// resume path may flip back to queued); skipped/excluded PRs
	// are counted but the progress numerator excludes them.
	total, _, mergedNow := s.mergeTrainCounts(ctx, releaseID)

	for {
		if ctx.Err() != nil {
			slog.Info("ship: merge train context cancelled",
				"release_id", uuidString(releaseID), "error", ctx.Err())
			return
		}

		nextRow, err := s.Q.NextQueuedReleasePR(ctx, releaseID)
		if err != nil {
			// pgx.ErrNoRows means "no more queued PRs" — happy path
			// terminator. We can't import pgx here without a service
			// dependency cycle; check for the error message instead.
			if isNoRowsError(err) {
				break
			}
			slog.Warn("ship: merge train next pr lookup failed",
				"release_id", uuidString(releaseID), "error", err)
			s.pauseMergeTrain(ctx, releaseID, pgtype.UUID{}, "internal: next pr lookup failed", actor, deps)
			return
		}

		// Mark this PR `merging` so the UI's pill flips to "merging…"
		// before we issue the GitHub call. Failure to set the state
		// here is non-fatal — the merge call still goes ahead and the
		// final state-write below will record what actually happened.
		if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
			ReleaseID:     releaseID,
			PullRequestID: nextRow.PullRequestID,
			MergeState:    db.PrMergeStateMerging,
		}); err != nil {
			slog.Warn("ship: mark pr merging failed",
				"pr_id", uuidString(nextRow.PullRequestID), "error", err)
		}
		s.publishMergeProgress(deps, workspaceID, releaseID, nextRow.PullRequestID, db.PrMergeStateMerging, mergedNow, total)

		pr, err := s.Q.GetPullRequest(ctx, nextRow.PullRequestID)
		if err != nil {
			s.failPRAndPause(ctx, releaseID, nextRow.PullRequestID, "load pr: "+err.Error(), actor, deps, workspaceID, mergedNow, total)
			return
		}
		owner, repo, err := gh.ParseRepoURL(pr.RepoUrl)
		if err != nil {
			s.failPRAndPause(ctx, releaseID, nextRow.PullRequestID, "parse repo: "+err.Error(), actor, deps, workspaceID, mergedNow, total)
			return
		}

		// Per-PR retry. Three attempts with linear backoff (1s, 2s,
		// 4s) covers the transient GitHub 5xx / network blip class.
		// Authoritative-failure errors (422 not mergeable, 401
		// unauthorized, 403 forbidden, 404 not found) skip the retry
		// and pause/skip immediately.
		var mergeResult *gh.MergeResult
		var mergeErr error
		for attempt := 1; attempt <= 3; attempt++ {
			mergeResult, mergeErr = s.Github.MergePullRequest(ctx, owner, repo, int(pr.PrNumber), mergeMethod, "")
			if mergeErr == nil {
				break
			}
			// Authoritative failures — don't retry.
			if errors.Is(mergeErr, gh.ErrUnprocessable) ||
				errors.Is(mergeErr, gh.ErrUnauthorized) ||
				errors.Is(mergeErr, gh.ErrForbidden) ||
				errors.Is(mergeErr, gh.ErrNotFound) ||
				errors.Is(mergeErr, gh.ErrConflict) {
				break
			}
			if ctx.Err() != nil {
				return
			}
			// Linear-ish backoff. Bounded so we don't sit on a single
			// PR for tens of seconds before paging the user.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		if mergeErr != nil {
			// PR closed mid-train — skip it rather than failing the
			// whole train. Common when a teammate manually intervenes
			// while we're queueing.
			if errors.Is(mergeErr, gh.ErrNotFound) {
				if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
					ReleaseID:     releaseID,
					PullRequestID: nextRow.PullRequestID,
					MergeState:    db.PrMergeStateSkipped,
					MergeError:    pgtype.Text{String: "PR not found (closed?)", Valid: true},
				}); err != nil {
					slog.Warn("ship: mark pr skipped failed", "error", err)
				}
				s.publishMergeProgress(deps, workspaceID, releaseID, nextRow.PullRequestID, db.PrMergeStateSkipped, mergedNow, total)
				continue
			}
			s.failPRAndPause(ctx, releaseID, nextRow.PullRequestID,
				summarizeMergeError(mergeErr),
				actor, deps, workspaceID, mergedNow, total)
			return
		}

		// Success. Persist sha + ts on the membership row, mark the
		// PR row itself merged (optimistic — the webhook will
		// confirm when it lands), then run linkage hooks.
		now := s.now()
		if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
			ReleaseID:     releaseID,
			PullRequestID: nextRow.PullRequestID,
			MergeState:    db.PrMergeStateMerged,
			MergedSha:     pgtype.Text{String: mergeResult.SHA, Valid: mergeResult.SHA != ""},
			MergedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			slog.Warn("ship: persist pr merged state failed",
				"pr_id", uuidString(nextRow.PullRequestID), "error", err)
		}
		if _, err := s.Q.MarkPullRequestMerged(ctx, db.MarkPullRequestMergedParams{
			ID:       nextRow.PullRequestID,
			MergedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			slog.Warn("ship: mark pull request merged failed",
				"pr_id", uuidString(nextRow.PullRequestID), "error", err)
		}
		// Run the same auto-close-on-merge path the webhook would.
		// We do it inline rather than waiting because (a) the
		// webhook delivery may be delayed and (b) the user expects
		// the linked issue to close as the train progresses, not
		// minutes later.
		if err := s.handleMerge(ctx, workspaceID, pr); err != nil {
			slog.Warn("ship: post-merge linkage failed",
				"pr_id", uuidString(nextRow.PullRequestID), "error", err)
		}

		mergedNow++
		s.publishMergeProgress(deps, workspaceID, releaseID, nextRow.PullRequestID, db.PrMergeStateMerged, mergedNow, total)
		shortSha := mergeResult.SHA
		if len(shortSha) > 7 {
			shortSha = shortSha[:7]
		}
		postReleaseChannel(deps, ctx, releaseRow.ChannelID, fmt.Sprintf(
			"✅ Merged #%d (%s) · sha=%s · %d/%d",
			pr.PrNumber, pr.Title, shortSha, mergedNow, total,
		))
	}

	// All queued PRs consumed. Did anything actually merge? If every
	// row ended up skipped (degenerate but possible after a Resume
	// with skip-all), we still complete the release rather than
	// pausing — there's nothing left to do.
	if err := s.completeMergeTrain(ctx, releaseID, actor, deps); err != nil {
		slog.Warn("ship: complete merge train failed",
			"release_id", uuidString(releaseID), "error", err)
	}
}

// pauseMergeTrain flips merge_paused=TRUE, records an audit event,
// publishes the WS pause signal, and posts to the channel. The
// orchestrator returns immediately after.
func (s *Service) pauseMergeTrain(
	ctx context.Context,
	releaseID, pausedPRID pgtype.UUID,
	errMsg string,
	actor pgtype.UUID,
	deps *MergeTrainDeps,
) {
	updated, err := s.Q.SetReleaseMergePaused(ctx, db.SetReleaseMergePausedParams{
		ID:          releaseID,
		MergePaused: true,
	})
	if err != nil {
		slog.Warn("ship: set merge paused failed",
			"release_id", uuidString(releaseID), "error", err)
		return
	}
	_, _ = s.insertReleaseEvent(ctx, releaseID, "merge_train_paused", actor, map[string]any{
		"paused_pr_id": uuidString(pausedPRID),
		"error":        errMsg,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseMergePaused, uuidString(updated.WorkspaceID), map[string]any{
			"release_id":   uuidString(releaseID),
			"paused_pr_id": uuidString(pausedPRID),
			"error":        errMsg,
		})
	}
	postReleaseChannel(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"❌ Merge failed: %s\n⏸ Train paused · resolve and resume from the release page",
		errMsg,
	))
}

// failPRAndPause is pauseMergeTrain plus a per-PR state flip to
// merge_state=failed with the error message recorded.
func (s *Service) failPRAndPause(
	ctx context.Context,
	releaseID, prID pgtype.UUID,
	errMsg string,
	actor pgtype.UUID,
	deps *MergeTrainDeps,
	workspaceID pgtype.UUID,
	mergedNow, total int,
) {
	if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
		ReleaseID:     releaseID,
		PullRequestID: prID,
		MergeState:    db.PrMergeStateFailed,
		MergeError:    pgtype.Text{String: errMsg, Valid: true},
	}); err != nil {
		slog.Warn("ship: mark pr failed failed",
			"pr_id", uuidString(prID), "error", err)
	}
	s.publishMergeProgress(deps, workspaceID, releaseID, prID, db.PrMergeStateFailed, mergedNow, total)
	s.pauseMergeTrain(ctx, releaseID, prID, errMsg, actor, deps)
}

// completeMergeTrain transitions stage merging → in_staging when every
// queued PR has been consumed (merged or skipped). Phase 7c picks up
// staging deploy from there.
//
// Pipeline topology drives stage selection: `staged` projects go
// merging → in_staging (the historical default), `direct_to_prod`
// projects skip to `promoting` with synthetic staged_at +
// qa_verified_at stamps so the timeline view doesn't show a gap and
// PromoteRelease's `qa_verified_at IS NOT NULL` precondition holds.
//
// Pipeline kind is an explicit project column (`project.pipeline_kind`,
// migration 095). Pre-095 the flow inferred the answer by querying
// for the existence of a `kind='staging'` env row — that worked but
// was indirect, and any phantom staging env produced by an
// over-eager poller created stuck releases (PR #47 incident). Now
// the project itself declares its topology and the env table goes
// back to being purely about "where code physically lands."
func (s *Service) completeMergeTrain(
	ctx context.Context,
	releaseID pgtype.UUID,
	actor pgtype.UUID,
	deps *MergeTrainDeps,
) error {
	now := s.now()

	// Fetch release → project so we can read the pipeline column.
	// Every code path into completeMergeTrain has the release id but
	// not always the project id, so we do the extra round-trip here
	// rather than thread it through every caller.
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("fetch release for pipeline lookup: %w", err)
	}
	project, err := s.Q.GetProject(ctx, release.ProjectID)
	if err != nil {
		// Defensive: if we can't read the project (deleted? race?),
		// default to the safer `staged` flow. Better to be stuck in
		// staging waiting for a manual unblock than to skip QA gates
		// on a project that actually has them.
		slog.Warn("ship: project lookup for pipeline_kind failed; defaulting to staged",
			"release_id", uuidString(releaseID),
			"project_id", uuidString(release.ProjectID),
			"error", err)
		project = db.Project{PipelineKind: db.ProjectPipelineKindStaged}
	}

	directToProd := project.PipelineKind == db.ProjectPipelineKindDirectToProd

	nextStage := db.ReleaseStageInStaging
	if directToProd {
		nextStage = db.ReleaseStagePromoting
	}

	updateParams := db.UpdateReleaseStageParams{
		ID:       releaseID,
		Stage:    nextStage,
		MergedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}
	if directToProd {
		// Backfill staged_at so the timeline reads "merged → staged →
		// promoting" rather than "merged → ???". COALESCE in the SQL
		// preserves this synthetic value if a real deploy later writes
		// a different timestamp.
		updateParams.StagedAt = pgtype.Timestamptz{Time: now, Valid: true}
	}
	updated, err := s.Q.UpdateReleaseStage(ctx, updateParams)
	if err != nil {
		return fmt.Errorf("update stage to %s: %w", nextStage, err)
	}

	// Direct-to-prod projects also need qa_verified_at stamped because
	// PromoteRelease (and auto-promote) treats a missing verification
	// as the in_staging → verifying gate. We use the same `now` so the
	// timeline is coherent.
	if directToProd {
		if _, err := s.Q.SetReleaseQAVerified(ctx, db.SetReleaseQAVerifiedParams{
			ID:           releaseID,
			QaVerifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
			QaVerifiedBy: actor,
		}); err != nil {
			slog.Warn("ship: stamp synthetic qa_verified failed",
				"release_id", uuidString(releaseID), "error", err)
		}
	}
	// Phase 7c — stamp the merged_main_sha. The LAST merged PR's
	// merge sha is the commit the project's CI/CD will deploy to
	// staging; the deployment_status webhook handler matches deploys
	// against this column to link them back to the release. We do
	// this BEFORE emitting the completed event so a fast-arriving
	// deploy_status webhook (which is what triggers the next stage
	// transition) sees the column populated.
	if last, err := s.Q.GetLastMergedReleasePR(ctx, releaseID); err == nil && last.MergedSha.Valid {
		if u2, err := s.Q.SetReleaseMergedMainSHA(ctx, db.SetReleaseMergedMainSHAParams{
			ID:            releaseID,
			MergedMainSha: pgtype.Text{String: last.MergedSha.String, Valid: true},
		}); err == nil {
			updated = u2
		} else {
			slog.Warn("ship: set merged_main_sha failed",
				"release_id", uuidString(releaseID), "error", err)
		}
	}
	_, _ = s.insertReleaseEvent(ctx, releaseID, "merge_train_completed", actor, map[string]any{
		"merged_at":       now.Format(time.RFC3339),
		"merged_main_sha": textValue(updated.MergedMainSha),
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseMergeCompleted, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
		})
		// Also fire the generic "release:updated" so the rail picks up
		// the new stage immediately.
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	// Count actually-merged PRs for the channel post.
	total, mergedTotal, _ := s.mergeTrainCounts(ctx, releaseID)
	postReleaseChannel(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"🟢 All %d/%d PR%s merged · staging deploy will pick this up shortly",
		mergedTotal, total, pluralS(mergedTotal),
	))
	return nil
}

// ReconcileStalledMergeTrain repairs merge-train membership rows after
// missed webhooks or a server restart kills the goroutine that was
// walking the train. If GitHub state already says every unresolved PR is
// merged, it syncs the release membership rows and completes the train.
func (s *Service) ReconcileStalledMergeTrain(
	ctx context.Context,
	releaseID pgtype.UUID,
	actor pgtype.UUID,
	deps *MergeTrainDeps,
) error {
	rows, err := s.Q.ListReleasePullRequests(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("list release prs: %w", err)
	}
	now := s.now()
	for _, row := range rows {
		if row.State != db.PullRequestStateMerged {
			continue
		}
		if row.MembershipMergeState == db.PrMergeStateMerged ||
			row.MembershipMergeState == db.PrMergeStateSkipped {
			continue
		}
		if _, err := s.Q.SetReleasePRMergeState(ctx, db.SetReleasePRMergeStateParams{
			ReleaseID:     releaseID,
			PullRequestID: row.ID,
			MergeState:    db.PrMergeStateMerged,
			MergedSha:     pgtype.Text{String: row.HeadSha, Valid: row.HeadSha != ""},
			MergedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			slog.Warn("ship: stalled train reconcile sync failed",
				"pr_id", uuidString(row.ID), "error", err)
		}
	}

	rows, err = s.Q.ListReleasePullRequests(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("re-read release prs: %w", err)
	}
	for _, row := range rows {
		switch row.MembershipMergeState {
		case db.PrMergeStateQueued, db.PrMergeStateMerging, db.PrMergeStateFailed:
			return nil
		}
	}
	return s.completeMergeTrain(ctx, releaseID, actor, deps)
}

// mergeTrainCounts returns (total_membership_rows, merged_count,
// merged_count_excluding_skipped). Used both for progress payloads and
// for the completion summary.
func (s *Service) mergeTrainCounts(ctx context.Context, releaseID pgtype.UUID) (int, int, int) {
	total := 0
	merged := 0
	rows, err := s.Q.CountReleasePRsByMergeState(ctx, releaseID)
	if err != nil {
		return 0, 0, 0
	}
	for _, row := range rows {
		total += int(row.Count)
		if row.MergeState == db.PrMergeStateMerged {
			merged += int(row.Count)
		}
	}
	return total, merged, merged
}

func (s *Service) publishMergeProgress(
	deps *MergeTrainDeps,
	workspaceID, releaseID, prID pgtype.UUID,
	state db.PrMergeState,
	mergedNow, total int,
) {
	if deps == nil || deps.Publisher == nil {
		return
	}
	deps.Publisher.PublishMergeEvent(protocol.EventReleaseMergeProgress, uuidString(workspaceID), map[string]any{
		"release_id":   uuidString(releaseID),
		"pr_id":        uuidString(prID),
		"merge_state":  string(state),
		"merged_count": mergedNow,
		"total":        total,
	})
}

// ---------- helpers ----------

// normalizeMergeMethod accepts the empty string (default to "merge")
// and the three GitHub-supported verbs.
func normalizeMergeMethod(m string) (string, error) {
	switch m {
	case "":
		return "merge", nil
	case "merge", "squash", "rebase":
		return m, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidMergeMethod, m)
	}
}

// summarizeMergeError shapes the GitHub error into a short channel-
// post-friendly string. Truncates over-long bodies (GitHub
// occasionally returns multi-paragraph 422 reasons).
func summarizeMergeError(err error) string {
	switch {
	case errors.Is(err, gh.ErrUnprocessable):
		return "PR is not mergeable (conflict / branch protection)"
	case errors.Is(err, gh.ErrUnauthorized):
		return "GitHub token unauthorized — check workspace settings"
	case errors.Is(err, gh.ErrForbidden):
		return "GitHub denied the merge (permissions / SSO)"
	case errors.Is(err, gh.ErrConflict):
		return "head SHA changed mid-merge"
	}
	msg := err.Error()
	if len(msg) > 240 {
		msg = msg[:240] + "…"
	}
	return msg
}

// postReleaseChannel is the best-effort wrapper around the channel
// poster. Never returns an error — channel-side failures must not
// cascade into the merge train flow.
func postReleaseChannel(
	deps *MergeTrainDeps,
	ctx context.Context,
	channelID pgtype.UUID,
	content string,
) {
	if deps == nil || deps.PostToReleaseChannel == nil || !channelID.Valid {
		return
	}
	if err := deps.PostToReleaseChannel(ctx, channelID, content); err != nil {
		slog.Debug("ship: release channel post failed",
			"channel_id", uuidString(channelID), "error", err)
	}
}

// pullRequestFromListRow projects a ListReleasePullRequestsRow back
// onto a db.PullRequest. Used so eligibility checks can reuse
// releaseEligibilityReason without a second GetPullRequest round-trip.
func pullRequestFromListRow(row db.ListReleasePullRequestsRow) db.PullRequest {
	return db.PullRequest{
		ID:                     row.ID,
		WorkspaceID:            row.WorkspaceID,
		ProjectID:              row.ProjectID,
		RepoUrl:                row.RepoUrl,
		PrNumber:               row.PrNumber,
		Title:                  row.Title,
		State:                  row.State,
		IsDraft:                row.IsDraft,
		AuthorLogin:            row.AuthorLogin,
		AuthorAvatarUrl:        row.AuthorAvatarUrl,
		BaseRef:                row.BaseRef,
		HeadRef:                row.HeadRef,
		HeadSha:                row.HeadSha,
		HtmlUrl:                row.HtmlUrl,
		Body:                   row.Body,
		CiStatus:               row.CiStatus,
		ReviewDecision:         row.ReviewDecision,
		Mergeable:              row.Mergeable,
		Additions:              row.Additions,
		Deletions:              row.Deletions,
		ChangedFiles:           row.ChangedFiles,
		Labels:                 row.Labels,
		PrCreatedAt:            row.PrCreatedAt,
		PrUpdatedAt:            row.PrUpdatedAt,
		PrMergedAt:             row.PrMergedAt,
		PrClosedAt:             row.PrClosedAt,
		FetchedAt:              row.FetchedAt,
		OriginatingIssueID:     row.OriginatingIssueID,
		OriginatingAgentTaskID: row.OriginatingAgentTaskID,
		AutoCloseIssueOnMerge:  row.AutoCloseIssueOnMerge,
		ConversationChannelID:  row.ConversationChannelID,
		StackParentPrID:        row.StackParentPrID,
		Source:                 row.Source,
		RiskLevel:              row.RiskLevel,
		RiskReasons:            row.RiskReasons,
		RiskClassifiedAt:       row.RiskClassifiedAt,
	}
}

func uuidStringsFromSlice(ids []pgtype.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, uuidString(id))
	}
	return out
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// isNoRowsError checks whether err is the pgx "no rows in result set"
// case without importing pgx (which would force the service package
// to add a transitive dep we don't otherwise need).
func isNoRowsError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rows in result set")
}
