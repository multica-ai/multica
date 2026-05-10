// Phase 7c — Staging deploy linkage, smoke tests, manual verify gate.
//
// The staging half of a release: once the merge train completes and
// all PRs land on main, the project's CI/CD picks up the new commit
// and deploys it to staging. This file covers everything that happens
// from "release.stage = in_staging" through "release.stage = verifying":
//
//   * LinkStagingDeploy is called by the deployment_status webhook
//     handler when it sees a successful staging deploy whose sha
//     matches a release's merged_main_sha. It records the linkage,
//     either kicks off the smoke workflow (if configured) or
//     transitions straight to verifying (if not).
//
//   * RecordSmokeOutcome is called by the check_run webhook handler
//     when it matches a check run id back to a release's smoke_run_id.
//     It records the outcome, transitions stage on success, posts
//     to the channel on failure.
//
//   * RunSmokeTests / MarkSmokeManualPass / MarkVerified / Unverify
//     are user-driven mutations exposed via HTTP endpoints. Each
//     enforces preconditions (correct stage, smoke status, approver
//     eligibility), writes the audit row, and emits a WS event.
//
// All four user-facing methods take a `ChannelOps` for the channel
// post side-effect; we reuse the existing release-channel poster
// the merge train wires up. Smoke trigger errors never fail the
// caller — the trigger is best-effort because the stage transition
// (or the user click) is the durable outcome.

package ship

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Sentinel errors. Mapped to clean HTTP statuses by the handler.
var (
	ErrReleaseNotInStaging   = errors.New("release: not in staging stage")
	ErrReleaseNotInVerifying = errors.New("release: not in verifying stage")
	ErrSmokeNotFinished      = errors.New("release: smoke tests have not completed")
	ErrSmokeNotConfigured    = errors.New("release: smoke workflow is not configured for this workspace")
	ErrApproverRequired      = errors.New("release: caller is not eligible to verify this release")
	// ErrTwoApproverPending is returned when the workspace's per-tier
	// rule is "two" and the caller has signed off as one approver, but
	// the matching counterparty hasn't signed off yet. The release
	// stays in_staging until the second slot is filled. The handler
	// surfaces this as a 202 Accepted with a "waiting for second
	// approver" message rather than a hard failure.
	ErrTwoApproverPending = errors.New("release: awaiting second approver signoff")
)

// Approval rule constants. String enum mirrored on the SQL CHECK
// constraint (see migration 090_ship_hub_approval_config.up.sql).
const (
	ApprovalRuleMember   = "member"   // any workspace member
	ApprovalRuleAdmin    = "admin"    // workspace owner or admin
	ApprovalRuleApprover = "approver" // release.approver_id (or admin)
	ApprovalRuleTwo      = "two"      // both approver_id + second_approver_id
)

// SignoffSlot enum values. Stored on ship_release_signoff.approver_slot.
const (
	SignoffSlotFirst  = "first"
	SignoffSlotSecond = "second"
)

// ApprovalContext bundles the per-call inputs the approval gate needs
// beyond the release row itself. The handler builds it once after
// resolving the workspace + the calling member's role.
type ApprovalContext struct {
	// Rule is the workspace-configured rule for the release's risk
	// tier. Empty string falls back to the legacy hardcoded behavior
	// (see resolveApprovalRule).
	Rule string
	// MemberRole is the caller's role on the workspace ("owner" |
	// "admin" | "member"). Used by the "admin" / "approver" / "two"
	// rules.
	MemberRole string
	// AllowAuthor controls whether the caller may verify if they
	// authored one of the release's PRs. When false and the caller is
	// in the PR-author set, the gate denies. The handler resolves the
	// PR-author set via Queries.ListReleasePullRequests; we pass the
	// resolved boolean down so the service layer doesn't fan out to
	// queries the handler already ran.
	IsAuthor bool
	// CanBeAuthor mirrors workspace.ship_hub_approver_can_be_author.
	// When false, IsAuthor=true denies.
	CanBeAuthor bool
}

// Smoke status string vocabulary. Free-form on the wire (CLAUDE.md API
// drift contract) — these constants are the values the service writes;
// readers tolerate any string and fall back generically.
const (
	SmokeStatusEmpty             = ""
	SmokeStatusQueued            = "queued"
	SmokeStatusInProgress        = "in_progress"
	SmokeStatusCompletedSuccess  = "completed_success"
	SmokeStatusCompletedFailure  = "completed_failure"
	SmokeStatusSkipped           = "skipped"
	SmokeStatusManualPass        = "manual_pass"
)

// StagingDeps bundles the pieces the staging-stage actions need that
// don't naturally live on the Service struct: a channel poster, a WS
// publisher, and (for the smoke trigger path) a workspace lookup so
// we can read smoke_workflow + repo URL.
//
// Same shape as MergeTrainDeps so the handler wiring stays uniform.
type StagingDeps struct {
	ParentCtx            context.Context
	ChannelOps           ChannelOps
	Publisher            MergeEventPublisher
	PostToReleaseChannel func(ctx context.Context, channelID pgtype.UUID, content string) error
	Now                  func() time.Time
}

func (d *StagingDeps) now() time.Time {
	if d == nil || d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func (d *StagingDeps) parentCtx() context.Context {
	if d == nil || d.ParentCtx == nil {
		return context.Background()
	}
	return d.ParentCtx
}

// LinkStagingDeploy records the deploy_id on the release and either
// triggers the smoke workflow (when configured) or transitions stage
// in_staging → verifying (when no smoke is configured).
//
// Called from the deployment_status webhook handler. Tolerant of
// release stage being either "in_staging" or "merging" — the deploy
// can land before the goroutine flips the stage on a fast deploy
// pipeline. We also tolerate a missing smoke workflow: empty string
// means "no smoke configured for this workspace", which is the
// dev-environment default.
//
// `smokeWorkflow` and `repoURL` come from the workspace + project
// rows the caller already has in hand; passing them in (rather than
// re-reading) avoids an extra DB round-trip on a hot webhook path.
func (s *Service) LinkStagingDeploy(
	ctx context.Context,
	releaseID, deployID pgtype.UUID,
	deploySHA, smokeWorkflow, repoURL string,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	updated, err := s.Q.SetReleaseStagingDeploy(ctx, db.SetReleaseStagingDeployParams{
		ID:              releaseID,
		StagingDeployID: deployID,
		StagedAt:        now,
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set staging deploy: %w", err)
	}
	// If the release is still in stage=merging (deploy beat the
	// merge-train completion goroutine), advance it to in_staging.
	if updated.Stage == db.ReleaseStageMerging {
		flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:       releaseID,
			Stage:    db.ReleaseStageInStaging,
			MergedAt: pgtype.Timestamptz{Time: deps.now(), Valid: true},
			StagedAt: now,
		})
		if err == nil {
			updated = flipped
		}
	}

	shortSHA := deploySHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	// Branch on smoke configuration. We post to the channel + emit a
	// staging_landed event in BOTH branches so the UI gets a
	// consistent signal regardless of the smoke-trigger path.
	hasSmoke := strings.TrimSpace(smokeWorkflow) != ""
	smokeStatus := SmokeStatusSkipped
	if hasSmoke {
		smokeStatus = SmokeStatusQueued
	}

	if !hasSmoke {
		// No smoke workflow → straight to verifying. The user's manual
		// QA gate is the only thing left between staging and prod.
		// We mark smoke_status = "skipped" rather than empty so the UI
		// can render a "smoke skipped (manual QA only)" pill rather
		// than a vacant "no smoke yet" state.
		if _, err := s.Q.SetReleaseSmokeStatus(ctx, db.SetReleaseSmokeStatusParams{
			ID:               releaseID,
			SmokeStatus:      pgtype.Text{String: SmokeStatusSkipped, Valid: true},
			SmokeCompletedAt: pgtype.Timestamptz{Time: deps.now(), Valid: true},
		}); err != nil {
			slog.Warn("ship: mark smoke skipped failed",
				"release_id", uuidString(releaseID), "error", err)
		}
		flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:    releaseID,
			Stage: db.ReleaseStageVerifying,
		})
		if err == nil {
			updated = flipped
		}
	} else {
		// Smoke configured → mark queued. The actual workflow_dispatch
		// happens below; if it errors we leave the row in queued and
		// the user can manually re-run.
		if u, err := s.Q.SetReleaseSmokeStatus(ctx, db.SetReleaseSmokeStatusParams{
			ID:          releaseID,
			SmokeStatus: pgtype.Text{String: SmokeStatusQueued, Valid: true},
		}); err == nil {
			updated = u
		}
	}

	// Audit + WS + channel post BEFORE the dispatch so the UI flips
	// even if the dispatch fails.
	_, _ = s.insertReleaseEvent(ctx, releaseID, "staging_deploy_landed", pgtype.UUID{}, map[string]any{
		"deploy_id":    uuidString(deployID),
		"sha":          deploySHA,
		"smoke_status": smokeStatus,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseStagingLanded, uuidString(updated.WorkspaceID), map[string]any{
			"release_id":   uuidString(releaseID),
			"deploy_id":    uuidString(deployID),
			"sha":          deploySHA,
			"smoke_status": smokeStatus,
		})
		// Also fire the generic stage-update so any rail listeners
		// pick up the in_staging → verifying flip when smoke isn't
		// configured.
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"🟧 Staging deploy landed · sha=%s · smoke=%s",
		shortSHA, smokeStatus,
	))

	// Trigger the smoke workflow if configured. Best-effort: a
	// dispatch failure logs + posts but doesn't unwind the linkage.
	if hasSmoke && s.Github != nil {
		owner, repo, perr := gh.ParseRepoURL(repoURL)
		if perr != nil {
			slog.Warn("ship: smoke trigger parse repo failed",
				"release_id", uuidString(releaseID), "error", perr)
			postReleaseChannelStaging(deps, ctx, updated.ChannelID,
				"⚠️ Smoke trigger failed: could not parse repo URL")
			return updated, nil
		}
		ref := updated.MergedMainSha.String
		if ref == "" {
			ref = deploySHA
		}
		inputs := map[string]string{
			"release_id": uuidString(releaseID),
			"sha":        deploySHA,
		}
		if err := s.Github.DispatchWorkflow(ctx, owner, repo, smokeWorkflow, ref, inputs); err != nil {
			slog.Warn("ship: smoke workflow dispatch failed",
				"release_id", uuidString(releaseID), "error", err)
			postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
				"⚠️ Smoke trigger failed: %s · click 'Run smoke tests' to retry",
				err.Error(),
			))
		}
	}

	return updated, nil
}

// RecordSmokeOutcome handles a check_run.completed webhook that
// matched the release's smoke_run_id. Transitions stage in_staging
// → verifying on success; logs + posts on failure.
//
// `conclusion` is GitHub's check_run.conclusion vocabulary
// ("success" | "failure" | "cancelled" | "timed_out" | "neutral" |
// "skipped" | "action_required"). We coalesce everything except
// "success" / "neutral" / "skipped" to failure.
func (s *Service) RecordSmokeOutcome(
	ctx context.Context,
	releaseID pgtype.UUID,
	conclusion string,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	statusValue := SmokeStatusCompletedFailure
	switch strings.ToLower(strings.TrimSpace(conclusion)) {
	case "success", "neutral", "skipped":
		statusValue = SmokeStatusCompletedSuccess
	}
	updated, err := s.Q.SetReleaseSmokeStatus(ctx, db.SetReleaseSmokeStatusParams{
		ID:               releaseID,
		SmokeStatus:      pgtype.Text{String: statusValue, Valid: true},
		SmokeCompletedAt: now,
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set smoke status: %w", err)
	}

	if statusValue == SmokeStatusCompletedSuccess && updated.Stage == db.ReleaseStageInStaging {
		// Smoke passed → stage flips to verifying so the user can
		// click "Mark verified" and the manual QA gate becomes
		// active. We DON'T set qa_verified_* yet — that's the human
		// click.
		flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:    releaseID,
			Stage: db.ReleaseStageVerifying,
		})
		if err == nil {
			updated = flipped
		}
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "smoke_completed", pgtype.UUID{}, map[string]any{
		"conclusion":   conclusion,
		"smoke_status": statusValue,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseSmokeUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id":   uuidString(releaseID),
			"smoke_status": statusValue,
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	if statusValue == SmokeStatusCompletedSuccess {
		postReleaseChannelStaging(deps, ctx, updated.ChannelID,
			"✅ Smoke tests passed · ready for manual QA")
	} else {
		postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
			"❌ Smoke tests failed (conclusion=%s) · resolve and retry, or use 'Mark smoke pass' to override",
			conclusion,
		))
	}
	return updated, nil
}

// RunSmokeTestsParams carries the workspace state needed to dispatch
// the smoke workflow. The handler builds it once per call so the
// service never has to re-read the workspace.
type RunSmokeTestsParams struct {
	WorkspaceID   pgtype.UUID
	SmokeWorkflow string
	RepoURL       string
}

// RunSmokeTests manually triggers the smoke workflow against the
// release's merged_main_sha. Used both as a retry path after a
// failure and as the initial trigger when the deploy webhook didn't
// fire (manual deploys outside our pipeline).
//
// Updates smoke_status="queued" + clears smoke_run_id (we don't have
// the new id until check_run.created arrives). Returns the updated
// release row.
func (s *Service) RunSmokeTests(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	p RunSmokeTestsParams,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	if s.Github == nil {
		return db.ShipRelease{}, ErrTokenMissing
	}
	if strings.TrimSpace(p.SmokeWorkflow) == "" {
		return db.ShipRelease{}, ErrSmokeNotConfigured
	}
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageInStaging && release.Stage != db.ReleaseStageVerifying {
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s", ErrReleaseNotInStaging, release.Stage)
	}

	owner, repo, perr := gh.ParseRepoURL(p.RepoURL)
	if perr != nil {
		return db.ShipRelease{}, fmt.Errorf("parse repo: %w", perr)
	}
	ref := release.MergedMainSha.String
	if ref == "" {
		// Defensive — every release that reached in_staging should
		// have a merged_main_sha, but a workspace migrating off an
		// older Phase 7b build might be missing one. Fall back to
		// the project's default branch via the deploy environment;
		// for this code path we just bail with a sentinel error.
		return db.ShipRelease{}, fmt.Errorf("release has no merged_main_sha")
	}
	inputs := map[string]string{
		"release_id": uuidString(releaseID),
		"sha":        ref,
	}
	if err := s.Github.DispatchWorkflow(ctx, owner, repo, p.SmokeWorkflow, ref, inputs); err != nil {
		return db.ShipRelease{}, fmt.Errorf("dispatch smoke workflow: %w", err)
	}

	// We don't get a workflow_run id back from workflow_dispatch —
	// that arrives via the check_run.created webhook. Stamp the
	// status as queued so the UI flips immediately; the run_id +
	// run_url will land via the webhook a few seconds later.
	updated, err := s.Q.SetReleaseSmokeRun(ctx, db.SetReleaseSmokeRunParams{
		ID:          releaseID,
		SmokeRunID:  pgtype.Text{},
		SmokeRunUrl: pgtype.Text{},
		SmokeStatus: pgtype.Text{String: SmokeStatusQueued, Valid: true},
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set smoke run: %w", err)
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "smoke_run_triggered", requestedBy, map[string]any{
		"workflow": p.SmokeWorkflow,
		"ref":      ref,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseSmokeUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id":   uuidString(releaseID),
			"smoke_status": SmokeStatusQueued,
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID,
		"🧪 Smoke tests triggered manually")
	return updated, nil
}

// MarkSmokeManualPass overrides the smoke gate. Used both for
// releases without a smoke workflow (where the user wants to advance
// past in_staging without fully blocking on automation) and for
// failures the user has triaged out-of-band. Owner/admin only —
// gated by the handler.
func (s *Service) MarkSmokeManualPass(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	note string,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageInStaging && release.Stage != db.ReleaseStageVerifying {
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s", ErrReleaseNotInStaging, release.Stage)
	}
	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	updated, err := s.Q.SetReleaseSmokeStatus(ctx, db.SetReleaseSmokeStatusParams{
		ID:               releaseID,
		SmokeStatus:      pgtype.Text{String: SmokeStatusManualPass, Valid: true},
		SmokeCompletedAt: now,
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set smoke status: %w", err)
	}

	// Manual pass advances stage to verifying when the release is
	// still in_staging. If it's already in verifying (smoke passed
	// then user re-marked-pass for some reason), this is a no-op.
	if updated.Stage == db.ReleaseStageInStaging {
		if flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:    releaseID,
			Stage: db.ReleaseStageVerifying,
		}); err == nil {
			updated = flipped
		}
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "smoke_manual_pass", requestedBy, map[string]any{
		"note": note,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseSmokeUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id":   uuidString(releaseID),
			"smoke_status": SmokeStatusManualPass,
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"🛡 Smoke manually passed by operator · note: %s",
		fallbackNote(note),
	))
	return updated, nil
}

// MarkVerified is the human-QA gate. Transitions stage in_staging →
// verifying (if smoke gated up directly) or stamps qa_verified_*
// when already in verifying.
//
// Approver eligibility (per the workspace's configured rules):
//   - "member"   — any workspace member
//   - "admin"    — workspace owner or admin
//   - "approver" — must equal release.approver_id (or workspace admin)
//   - "two"      — must equal one of release.approver_id /
//                  release.second_approver_id; both slots must sign
//                  off before the release advances to verifying.
//                  Workspace admins satisfy either slot but two
//                  distinct admins are still required.
//
// Defaults (when ApprovalContext.Rule is "" — e.g. older workspace
// row pre-migration): low/medium → "member", high → "approver",
// critical → "two", which preserves the legacy hardcoded behavior.
//
// Handler is responsible for confirming workspace membership before
// calling; this method only enforces the rule-driven eligibility.
//
// For the "two" rule:
//   - The caller's signoff is recorded on ship_release_signoff. If
//     both slots already have a signoff, the stage flips to verifying
//     (or stays in verifying with qa_verified_* stamped to the
//     latest signer).
//   - If only the caller's slot has a signoff, returns
//     ErrTwoApproverPending so the handler can surface a 202.
func (s *Service) MarkVerified(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	note string,
	approval ApprovalContext,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	// Allow either in_staging (skip-smoke path) or verifying
	// (re-verify after unverify) as the entry stage.
	if release.Stage != db.ReleaseStageInStaging && release.Stage != db.ReleaseStageVerifying {
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s", ErrReleaseNotInStaging, release.Stage)
	}
	rule := resolveApprovalRule(approval.Rule, release.RiskLevel)
	slot, eligible := approverEligibility(rule, release, requestedBy, approval)
	if !eligible {
		return db.ShipRelease{}, ErrApproverRequired
	}
	// Smoke gate — high/critical risk releases additionally need
	// smoke to be passing or manually-passed before verification.
	// Low/medium is more relaxed: a missing smoke is OK.
	if release.RiskLevel == db.RiskLevelHigh || release.RiskLevel == db.RiskLevelCritical {
		if !smokePassedOrSkipped(release.SmokeStatus) {
			return db.ShipRelease{}, ErrSmokeNotFinished
		}
	}

	// "two" rule: record the slot signoff; if both slots have a row,
	// proceed with the verify; otherwise return ErrTwoApproverPending.
	if rule == ApprovalRuleTwo {
		if _, err := s.Q.UpsertReleaseSignoff(ctx, db.UpsertReleaseSignoffParams{
			ReleaseID:    releaseID,
			ApproverSlot: slot,
			SignedBy:     requestedBy,
			Note:         pgtype.Text{String: note, Valid: note != ""},
		}); err != nil {
			return db.ShipRelease{}, fmt.Errorf("record signoff: %w", err)
		}
		signoffs, err := s.Q.ListReleaseSignoffs(ctx, releaseID)
		if err != nil {
			return db.ShipRelease{}, fmt.Errorf("list signoffs: %w", err)
		}
		_, _ = s.insertReleaseEvent(ctx, releaseID, "release_signoff", requestedBy, map[string]any{
			"slot": slot,
			"note": note,
		})
		if !bothSlotsSigned(signoffs) {
			// Single-slot signoff. Emit a WS event so the UI can flip
			// the banner ("awaiting second approver"); leave stage
			// untouched.
			if deps != nil && deps.Publisher != nil {
				deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(release.WorkspaceID), map[string]any{
					"release_id": uuidString(releaseID),
					"stage":      string(release.Stage),
				})
			}
			postReleaseChannelStaging(deps, ctx, release.ChannelID, fmt.Sprintf(
				"🟨 Release awaiting second approver · slot=%s signed",
				slot,
			))
			return release, ErrTwoApproverPending
		}
		// Both slots signed — fall through to the standard verify
		// stamp. The qa_verified_by ends up as the latest signer
		// (either slot); the audit trail keeps both signoffs.
	}

	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	updated, err := s.Q.SetReleaseQAVerified(ctx, db.SetReleaseQAVerifiedParams{
		ID:           releaseID,
		QaVerifiedAt: now,
		QaVerifiedBy: requestedBy,
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set qa verified: %w", err)
	}

	if updated.Stage == db.ReleaseStageInStaging {
		flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:    releaseID,
			Stage: db.ReleaseStageVerifying,
		})
		if err == nil {
			updated = flipped
		}
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "release_verified", requestedBy, map[string]any{
		"note": note,
		"rule": rule,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseVerified, uuidString(updated.WorkspaceID), map[string]any{
			"release_id":  uuidString(releaseID),
			"verified_by": uuidString(requestedBy),
			"verified_at": deps.now().Format(time.RFC3339),
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"✅ Release verified · note: %s",
		fallbackNote(note),
	))
	return updated, nil
}

// Unverify reverses MarkVerified. Returns the release to in_staging
// and clears qa_verified_*. Useful when QA spots an issue post-
// verification but pre-promote.
//
// Also clears any ship_release_signoff rows so a re-verify under the
// "two" rule starts fresh — both approvers must sign off again.
func (s *Service) Unverify(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	reason string,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageVerifying {
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s", ErrReleaseNotInVerifying, release.Stage)
	}
	updated, err := s.Q.SetReleaseQAUnverified(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("clear qa verified: %w", err)
	}
	// Best-effort signoff wipe — failure here is informational; the
	// stage rollback is the durable outcome.
	if err := s.Q.DeleteReleaseSignoffs(ctx, releaseID); err != nil {
		slog.Debug("ship: clear release signoffs failed",
			"release_id", uuidString(releaseID), "error", err)
	}
	flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:    releaseID,
		Stage: db.ReleaseStageInStaging,
	})
	if err == nil {
		updated = flipped
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "release_unverified", requestedBy, map[string]any{
		"reason": reason,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUnverified, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"reason":     reason,
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"⚠️ Release un-verified · reason: %s",
		fallbackNote(reason),
	))
	return updated, nil
}

// resolveApprovalRule maps an empty / unrecognized rule string back
// to the legacy hardcoded default for the release's risk tier. This
// keeps existing workspaces (pre-migration) on the original behavior
// without a backfill: low/medium → "member", high → "approver",
// critical → "two".
func resolveApprovalRule(rule string, risk db.RiskLevel) string {
	switch rule {
	case ApprovalRuleMember, ApprovalRuleAdmin, ApprovalRuleApprover, ApprovalRuleTwo:
		return rule
	}
	switch risk {
	case db.RiskLevelHigh:
		return ApprovalRuleApprover
	case db.RiskLevelCritical:
		return ApprovalRuleTwo
	}
	return ApprovalRuleMember
}

// approverEligibility returns (slot, ok) for the given rule + caller.
// `slot` is only meaningful when rule=="two" and ok==true: it tells
// the caller which signoff row to upsert ("first" or "second").
//
// The four rule values:
//
//   - "member"   — any workspace member (handler-level membership
//                  check is the floor; this returns true unconditionally
//                  modulo IsAuthor).
//   - "admin"    — caller's MemberRole must be "owner" or "admin".
//   - "approver" — caller is release.approver_id, OR caller is a
//                  workspace admin. Admin override matches the legacy
//                  Phase 5 behavior so a workspace owner can always
//                  unblock a release whose original approver is OOO.
//   - "two"      — caller maps to one of the two approver slots.
//                  An admin can satisfy either slot but not both
//                  (uniqueness comes from the (release_id, slot)
//                  PK on ship_release_signoff combined with the
//                  signed_by column being a separate user per slot).
//                  Returns slot="first" when the caller is the
//                  primary approver, "second" otherwise. The caller
//                  is responsible for tracking that BOTH slots have
//                  signoff rows before advancing.
//
// Treats a zero requestedBy as "no caller identified" — fail closed.
// Workspace-membership is enforced upstream by the handler.
func approverEligibility(
	rule string,
	release db.ShipRelease,
	requestedBy pgtype.UUID,
	approval ApprovalContext,
) (string, bool) {
	if !requestedBy.Valid {
		return "", false
	}
	// PR-author separation-of-duties. When the workspace has flipped
	// off "approver can be author" AND the caller is in the release's
	// PR-author set, deny regardless of rule.
	if approval.IsAuthor && !approval.CanBeAuthor {
		return "", false
	}

	isAdmin := approval.MemberRole == "owner" || approval.MemberRole == "admin"
	isPrimary := release.ApproverID.Valid &&
		uuidString(release.ApproverID) == uuidString(requestedBy)
	isSecondary := release.SecondApproverID.Valid &&
		uuidString(release.SecondApproverID) == uuidString(requestedBy)

	switch rule {
	case ApprovalRuleMember:
		return "", true
	case ApprovalRuleAdmin:
		return "", isAdmin
	case ApprovalRuleApprover:
		return "", isPrimary || isAdmin
	case ApprovalRuleTwo:
		// Pick the slot the caller fills. Primary approver match
		// wins; otherwise secondary; otherwise an admin maps to
		// whichever slot is still empty (the caller logic in
		// MarkVerified treats both as fungible after both rows exist).
		if isPrimary {
			return SignoffSlotFirst, true
		}
		if isSecondary {
			return SignoffSlotSecond, true
		}
		if isAdmin {
			// Default admin → "first" slot. The MarkVerified caller
			// handles the both-rows-needed check downstream; an admin
			// signing off in slot "first" still requires a second
			// person (admin or approver) to sign off in "second"
			// because the (release_id, slot) PK rejects a duplicate
			// row from the same caller.
			return SignoffSlotFirst, true
		}
		return "", false
	}
	// Unknown rule — fail open for member-equivalent. The handler
	// still gates on workspace membership.
	return "", true
}

// bothSlotsSigned returns true when the slice of signoffs covers
// both "first" and "second" slots with DISTINCT signers. We require
// distinct signers so a single admin can't satisfy both slots by
// signing twice (the (release_id, slot) PK already prevents a same-
// slot duplicate; this guards against admin1→first + admin1→second
// not satisfying separation-of-duties).
func bothSlotsSigned(signoffs []db.ShipReleaseSignoff) bool {
	var first, second db.ShipReleaseSignoff
	var hasFirst, hasSecond bool
	for _, s := range signoffs {
		switch s.ApproverSlot {
		case SignoffSlotFirst:
			first = s
			hasFirst = true
		case SignoffSlotSecond:
			second = s
			hasSecond = true
		}
	}
	if !hasFirst || !hasSecond {
		return false
	}
	return uuidString(first.SignedBy) != uuidString(second.SignedBy)
}

// smokePassedOrSkipped is the high/critical-risk smoke gate.
// Considers manual_pass and skipped-with-no-workflow as acceptable.
func smokePassedOrSkipped(status pgtype.Text) bool {
	if !status.Valid {
		return false
	}
	switch status.String {
	case SmokeStatusCompletedSuccess, SmokeStatusManualPass, SmokeStatusSkipped:
		return true
	}
	return false
}

// postReleaseChannelStaging is a thin alias for postReleaseChannel
// scoped to the staging deps shape. Best-effort: never returns
// errors, never panics.
func postReleaseChannelStaging(
	deps *StagingDeps,
	ctx context.Context,
	channelID pgtype.UUID,
	content string,
) {
	if deps == nil || deps.PostToReleaseChannel == nil || !channelID.Valid {
		return
	}
	if err := deps.PostToReleaseChannel(ctx, channelID, content); err != nil {
		slog.Debug("ship: release staging channel post failed",
			"channel_id", uuidString(channelID), "error", err)
	}
}

// fallbackNote returns the input or "(no note)" when empty. Keeps
// channel posts readable when the user submits without a note.
func fallbackNote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no note)"
	}
	return s
}
