// Phase 7d — Production promotion + rollback.
//
// This file owns the production half of a release: PromoteRelease
// flips the release into the "promoting" stage and waits for the
// production deploy to land; LinkProductionDeploy is the webhook-side
// counterpart that records the linkage when a deployment_status
// event matches; MarkReleaseRollback records the user's decision to
// roll back.
//
// Why no auto-revert orchestrator in v1: a "create revert PR" REST
// endpoint doesn't exist on GitHub — generating a true revert means
// constructing new tree objects via the Git Trees + Refs API, which
// has a substantial correctness/idempotency surface (cherry-pick
// conflicts, branch protection interactions, signed commits, etc.).
// Phase 7d's value is closing the release loop *now*: a user can
// click Promote, see the deploy, and roll back if needed. The "roll
// back" part is currently user-driven — the channel post lists the
// merged PRs in reverse merge order with deep links so the user can
// click GitHub's per-PR "Revert" button. v2 (Phase 7e or later) adds
// the auto-orchestrator on top of the same data model — the
// per-PR revert_state columns we added in migration 089 are already
// in place.
//
// All four user-facing methods take a `*StagingDeps` (re-used from
// Phase 7c) for the channel post + WS publisher. Phase 7d doesn't
// need a separate deps struct because the production-stage actions
// follow the same write-pattern as the staging ones.

package ship

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Sentinel errors specific to the production-stage transitions. Mapped
// to clean HTTP statuses by the handler.
var (
	ErrReleaseNotInVerifying2  = errors.New("release: not in verifying stage")
	ErrReleaseNotInProduction  = errors.New("release: not in a production stage (verifying / promoting / in_production)")
	ErrReleaseAlreadyPromoted  = errors.New("release: already promoted")
	ErrReleaseAlreadyRolled    = errors.New("release: already rolled back")
	ErrReleaseRollbackNoTarget = errors.New("release: nothing to roll back (no merged PRs)")
)

// PromoteRelease initiates the production deploy. Records the stage
// transition (verifying → promoting) and the promoted_at / promoted_by
// pair. The actual deploy still has to land via the user's CI/CD; we
// record the intent and wait for the deployment_status webhook (or a
// manual MarkReleaseProductionDeployed) to confirm.
//
// Preconditions:
//   - release.stage == "verifying"
//   - All Phase 5 risk-tier rules satisfied (the verify gate already
//     enforced approver_id for medium+ and second_approver_id for
//     critical, so by the time we reach Promote those have been set).
//   - Caller passes the canVerifyRelease eligibility test (re-used
//     from Phase 7c — same approver-equality rule).
//
// Returns ErrReleaseStageMismatch / ErrApproverRequired so the handler
// can surface clean 4xx codes.
func (s *Service) PromoteRelease(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageVerifying {
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s, want verifying", ErrReleaseStageMismatch, release.Stage)
	}
	// Approver eligibility — same rule as MarkVerified. We re-check
	// here because the user who clicks Promote may not be the same
	// user who verified (and high+/critical risk demands the right
	// approver every transition).
	if !canVerifyRelease(release, requestedBy) {
		return db.ShipRelease{}, ErrApproverRequired
	}

	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	updated, err := s.Q.SetReleasePromoted(ctx, db.SetReleasePromotedParams{
		ID:                 releaseID,
		PromotedAt:         now,
		ProductionDeployID: pgtype.UUID{}, // filled by LinkProductionDeploy
		ProductionMainSha:  pgtype.Text{},
		PromotedBy:         requestedBy,
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set release promoted: %w", err)
	}

	// Stage flip: verifying → promoting.
	flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:         releaseID,
		Stage:      db.ReleaseStagePromoting,
		PromotedAt: now,
	})
	if err == nil {
		updated = flipped
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "release_promoted", requestedBy, map[string]any{
		"sha":        textValue(updated.MergedMainSha),
		"by_user_id": uuidString(requestedBy),
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleasePromoted, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"sha":        textValue(updated.MergedMainSha),
			"by_user_id": uuidString(requestedBy),
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"🚀 Promoted to production · sha=%s · awaiting production deploy",
		shortSHA(textValue(updated.MergedMainSha)),
	))
	return updated, nil
}

// LinkProductionDeploy is the webhook-side counterpart to PromoteRelease.
// When a successful production deploy lands whose sha matches the
// release's merged_main_sha (or production_main_sha when set), we
// record production_deploy_id / production_main_sha and advance to
// in_production.
//
// Tolerant of stage being either "verifying" or "promoting" — the
// deploy can land before the user clicks Promote (a fast pipeline that
// auto-promotes from staging), and we don't want to drop the linkage
// in that case. When stage is verifying, we treat the deploy as a
// promote-and-link in a single step.
func (s *Service) LinkProductionDeploy(
	ctx context.Context,
	releaseID, deployID pgtype.UUID,
	deploySHA string,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}

	// Stamp production_deploy_id + production_main_sha. promoted_at is
	// COALESCE'd so a delayed webhook doesn't overwrite the click-time
	// timestamp (and an auto-promote-from-staging path stamps it now).
	updated, err := s.Q.SetReleasePromoted(ctx, db.SetReleasePromotedParams{
		ID:                 releaseID,
		PromotedAt:         now,
		ProductionDeployID: deployID,
		ProductionMainSha:  pgtype.Text{String: deploySHA, Valid: deploySHA != ""},
		PromotedBy:         pgtype.UUID{},
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set production deploy: %w", err)
	}

	// Stage flip to in_production. We allow the transition from either
	// promoting or verifying — the second case happens when the user's
	// pipeline auto-promotes (no explicit click) and the webhook fires
	// while the release is still in verifying.
	if updated.Stage == db.ReleaseStagePromoting || updated.Stage == db.ReleaseStageVerifying {
		flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:         releaseID,
			Stage:      db.ReleaseStageInProduction,
			PromotedAt: now,
		})
		if err == nil {
			updated = flipped
		}
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "production_deploy_landed", pgtype.UUID{}, map[string]any{
		"deploy_id": uuidString(deployID),
		"sha":       deploySHA,
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseInProduction, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"deploy_id":  uuidString(deployID),
			"sha":        deploySHA,
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, updated.ChannelID, fmt.Sprintf(
		"🟢 Production deploy landed · sha=%s · monitoring health for 24h",
		shortSHA(deploySHA),
	))
	return updated, nil
}

// MarkReleaseRollback records the user's decision to roll back. Sets
// rolled_back_by + rolled_back_completed_at, posts the rollback
// instructions to the channel (linking each merged PR to its GitHub
// page so the user can click "Revert" on each), and transitions stage
// to rolled_back.
//
// v1 is user-driven: the actual revert PRs are created manually via
// GitHub's per-PR Revert button. v2 (Phase 7e+) will replace the body
// of this method with an orchestrator that creates and merges revert
// PRs automatically — the data model already supports it.
//
// Preconditions:
//   - release.stage in ("verifying", "promoting", "in_production")
//   - At least one merged PR to roll back (otherwise the rollback is
//     a no-op and should be a Cancel instead).
//   - Caller is workspace owner/admin OR the release's approver/second
//     approver (handler-level check; this method assumes the gate
//     already passed).
func (s *Service) MarkReleaseRollback(
	ctx context.Context,
	releaseID, requestedBy pgtype.UUID,
	reason string,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	switch release.Stage {
	case db.ReleaseStageVerifying, db.ReleaseStagePromoting, db.ReleaseStageInProduction:
		// allowed
	case db.ReleaseStageRolledBack:
		return db.ShipRelease{}, ErrReleaseAlreadyRolled
	default:
		return db.ShipRelease{}, fmt.Errorf("%w: stage=%s", ErrReleaseNotInProduction, release.Stage)
	}

	mergedPRs, err := s.Q.ListReleasePRsByMergeOrderDesc(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("list merged prs: %w", err)
	}
	if len(mergedPRs) == 0 {
		return db.ShipRelease{}, ErrReleaseRollbackNoTarget
	}

	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	cleanReason := strings.TrimSpace(reason)
	updated, err := s.Q.SetReleaseRolledBack(ctx, db.SetReleaseRolledBackParams{
		ID:                    releaseID,
		RolledBackCompletedAt: now,
		RolledBackBy:          requestedBy,
		RollbackReason:        pgtype.Text{String: cleanReason, Valid: cleanReason != ""},
	})
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("set rolled back: %w", err)
	}

	// Mark each still-merged PR as 'pending' revert so the UI can
	// surface a per-PR "revert needed" affordance. Best-effort — the
	// state is informational; failures don't block the rollback.
	for _, row := range mergedPRs {
		_, perErr := s.Q.UpdatePRRevertState(ctx, db.UpdatePRRevertStateParams{
			ReleaseID:      releaseID,
			PullRequestID:  row.PullRequestID,
			RevertState:    db.NullPrRevertState{PrRevertState: db.PrRevertStatePending, Valid: true},
			RevertPrNumber: pgtype.Int4{},
			RevertPrUrl:    pgtype.Text{},
			RevertError:    pgtype.Text{},
		})
		if perErr != nil {
			// Just log via insertReleaseEvent so the user has a trail
			// of which PRs failed to mark.
			_, _ = s.insertReleaseEvent(ctx, releaseID, "warning", requestedBy, map[string]any{
				"reason":  "mark pr revert pending failed: " + perErr.Error(),
				"pr_id":   uuidString(row.PullRequestID),
			})
		}
	}

	// Stage flip → rolled_back. Caller pairs this with the audit event
	// + WS publication below.
	flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:             releaseID,
		Stage:          db.ReleaseStageRolledBack,
		RollbackReason: pgtype.Text{String: cleanReason, Valid: cleanReason != ""},
	})
	if err == nil {
		updated = flipped
	}

	_, _ = s.insertReleaseEvent(ctx, releaseID, "release_rolled_back", requestedBy, map[string]any{
		"reason":     cleanReason,
		"pr_count":   len(mergedPRs),
		"by_user_id": uuidString(requestedBy),
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseRollbackInitiated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"reason":     cleanReason,
			"by_user_id": uuidString(requestedBy),
		})
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(updated.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(updated.Stage),
		})
	}

	// Channel post: the rollback instructions. Lists each merged PR
	// in reverse merge order with a deep link to the GitHub PR so the
	// user can click "Revert" on each. v2 will replace this with an
	// auto-orchestrator that creates the revert PRs itself.
	if updated.ChannelID.Valid {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("🛑 Rollback initiated · %d PR%s to revert\n",
			len(mergedPRs), pluralS(len(mergedPRs))))
		if cleanReason != "" {
			b.WriteString(fmt.Sprintf("Reason: %s\n", cleanReason))
		}
		b.WriteString("\nClick GitHub's “Revert” button on each PR (newest first) to create the revert PRs:\n")
		for _, row := range mergedPRs {
			b.WriteString(fmt.Sprintf("• #%d %s — %s\n", row.PrNumber, row.Title, row.HtmlUrl))
		}
		postReleaseChannelStaging(deps, ctx, updated.ChannelID, b.String())
	}

	return updated, nil
}

// MarkReleaseDone is the explicit "fast-forward" path: when the
// 24h health-monitoring window has elapsed without a rollback, the
// finalizer goroutine OR a user click lands here to flip the release
// to its terminal "done" stage. Re-callable on rolled_back releases
// is a no-op — we only act on in_production.
func (s *Service) MarkReleaseDone(
	ctx context.Context,
	releaseID pgtype.UUID,
	deps *StagingDeps,
) (db.ShipRelease, error) {
	release, err := s.Q.GetRelease(ctx, releaseID)
	if err != nil {
		return db.ShipRelease{}, fmt.Errorf("get release: %w", err)
	}
	if release.Stage != db.ReleaseStageInProduction {
		return release, nil // idempotent no-op
	}

	now := pgtype.Timestamptz{Time: deps.now(), Valid: true}
	flipped, err := s.Q.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
		ID:     releaseID,
		Stage:  db.ReleaseStageDone,
		DoneAt: now,
	})
	if err != nil {
		return release, fmt.Errorf("update stage to done: %w", err)
	}
	_, _ = s.insertReleaseEvent(ctx, releaseID, "release_done", pgtype.UUID{}, map[string]any{
		"done_at": deps.now().Format(time.RFC3339),
	})
	if deps != nil && deps.Publisher != nil {
		deps.Publisher.PublishMergeEvent(protocol.EventReleaseUpdated, uuidString(flipped.WorkspaceID), map[string]any{
			"release_id": uuidString(releaseID),
			"stage":      string(flipped.Stage),
		})
	}
	postReleaseChannelStaging(deps, ctx, flipped.ChannelID,
		"✅ Release closed · 24h post-deploy window elapsed without rollback")
	return flipped, nil
}

// shortSHA truncates a SHA to its 7-char abbreviation. Returns the
// input as-is when it's already short or empty.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
