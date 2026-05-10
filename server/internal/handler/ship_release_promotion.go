// Phase 7d — Production-stage HTTP handlers.
//
// Endpoints:
//   POST /api/releases/{id}/promote                     → verifying → promoting
//   POST /api/releases/{id}/mark_production_deployed    → manual escape hatch
//   POST /api/releases/{id}/rollback                    → record rollback intent
//   POST /api/releases/{id}/mark_done                   → fast-forward 24h gate
//   GET  /api/releases/{id}/health                      → health rollup payload
//
// Authorization model:
//   - promote: workspace member. The verify gate (Phase 7c) already
//     enforced approver requirements; the service layer re-checks
//     canVerifyRelease for high/critical risk so a non-approver who
//     somehow reaches this endpoint can't bypass the rule.
//   - mark_production_deployed: workspace member (mirrors Phase 7c's
//     mark_staging_deployed escape hatch).
//   - rollback: workspace owner/admin OR release.approver_id /
//     second_approver_id. Destructive — the audit footprint is large
//     and we want a single clear actor.
//   - mark_done: workspace member.
//   - health: workspace member.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ----- request shapes -------------------------------------------------------

// PromoteReleaseRequest is the body for POST .../promote. RollbackPlan
// is captured at click time so it lands on the audit row alongside
// the stage transition (currently informational; Phase 5's
// deploy_preflight rollback_plan column is the durable home for it).
type PromoteReleaseRequest struct {
	RollbackPlan string `json:"rollback_plan"`
}

// RollbackReleaseRequest is the body for POST .../rollback. Reason is
// REQUIRED on the wire — the channel post echoes it and the audit log
// requires an actor + reason.
type RollbackReleaseRequest struct {
	Reason string `json:"reason"`
}

// ----- handlers -------------------------------------------------------------

// PromoteRelease — POST /api/releases/{id}/promote.
//
// 202 Accepted on success: the release stage flips to promoting and
// the user's CI/CD takes it from there. The deployment_status webhook
// (or the manual mark_production_deployed escape hatch) will land the
// linkage and advance to in_production.
func (h *Handler) PromoteRelease(w http.ResponseWriter, r *http.Request) {
	rel, ws, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found")
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	var req PromoteReleaseRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(wsID)
	approval := buildApprovalContext(ws, rel, member.Role)
	updated, err := svc.PromoteRelease(r.Context(), rel.ID, requestedBy, approval, deps)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseStageMismatch):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrApproverRequired):
			writeError(w, http.StatusForbidden, err.Error())
		default:
			slog.Warn("ship: promote release failed",
				"release_id", uuidToString(rel.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to promote release: "+err.Error())
		}
		return
	}

	// Record the rollback_plan as an informational audit event. A
	// future Phase 7e tightening could persist this on the deploy
	// preflight row, but the audit trail is the durable home today.
	if strings.TrimSpace(req.RollbackPlan) != "" {
		_, _ = h.Queries.InsertReleaseEvent(r.Context(), db.InsertReleaseEventParams{
			ReleaseID:   rel.ID,
			EventType:   "rollback_plan_recorded",
			ActorUserID: requestedBy,
			Payload:     mustJSON(map[string]any{"rollback_plan": req.RollbackPlan}),
		})
	}

	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
		"stage":      string(updated.Stage),
	})
	writeJSON(w, http.StatusAccepted, releaseToResponse(updated, int(count)))
}

// MarkReleaseProductionDeployed is the manual escape-hatch when GitHub
// deployment_status webhooks aren't firing for a workspace's repo.
// Mirrors MarkReleaseStagingDeployed exactly — synthesizes a
// successful production deploy at release.merged_main_sha and runs
// the same linkage flow the webhook path runs.
//
// Auth: workspace member.
//
// Idempotent: if the release already has a production_deploy_id, returns
// 409 Conflict — the user should refresh first.
func (h *Handler) MarkReleaseProductionDeployed(w http.ResponseWriter, r *http.Request) {
	// loadReleaseAndProject returns (release, workspace, project, wsID, ok)
	// — the destructure ordering bit me in Phase 7c (commit 1e424aa7),
	// so being explicit here is worth the comment cost.
	rel, _, project, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, memberOK := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !memberOK {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	// Allowed entry stages: promoting (the user clicked Promote and
	// is now manually confirming the deploy) or verifying (auto-promote
	// pipelines that skip the explicit click).
	if rel.Stage != db.ReleaseStagePromoting && rel.Stage != db.ReleaseStageVerifying {
		writeError(w, http.StatusConflict, "release is not in promoting or verifying")
		return
	}
	if rel.ProductionDeployID.Valid {
		writeError(w, http.StatusConflict, "release already has a linked production deploy")
		return
	}
	if !rel.MergedMainSha.Valid || rel.MergedMainSha.String == "" {
		writeError(w, http.StatusBadRequest,
			"release has no merged_main_sha — manual deploy linkage requires a recorded merge commit")
		return
	}

	// Find or create the production deploy environment for the project.
	envs, err := h.Queries.ListDeployEnvironmentsByProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deploy environments")
		return
	}
	var prodEnv *db.DeployEnvironment
	for i := range envs {
		if envs[i].Kind == db.DeployEnvironmentKindProduction {
			prodEnv = &envs[i]
			break
		}
	}
	if prodEnv == nil {
		created, err := h.Queries.UpsertDeployEnvironment(r.Context(), db.UpsertDeployEnvironmentParams{
			WorkspaceID:  wsID,
			ProjectID:    project.ID,
			Kind:         db.DeployEnvironmentKindProduction,
			Name:         "Production",
			TargetBranch: "main",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create production environment")
			return
		}
		prodEnv = &created
	}

	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	deploy, err := h.Queries.InsertDeploy(r.Context(), db.InsertDeployParams{
		WorkspaceID:   wsID,
		EnvironmentID: prodEnv.ID,
		Ref:           prodEnv.TargetBranch,
		Sha:           rel.MergedMainSha.String,
		Status:        db.DeployStatusSucceeded,
		TriggeredBy:   requestedBy,
		StartedAt:     now,
		CompletedAt:   now,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record deploy")
		return
	}
	_, _ = h.Queries.UpdateDeployEnvironmentCurrent(r.Context(), db.UpdateDeployEnvironmentCurrentParams{
		ID:                prodEnv.ID,
		CurrentSha:        pgtype.Text{String: deploy.Sha, Valid: true},
		CurrentDeployedAt: deploy.TriggeredAt,
	})

	// Reuse the webhook-path linkage so the channel post / WS event /
	// stage transition flow matches the auto-linked path exactly.
	h.linkProductionDeployForRelease(r.Context(), wsID, deploy.ID, deploy.Sha)

	updated, err := h.Queries.GetRelease(r.Context(), rel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload release")
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
		"stage":      string(updated.Stage),
	})
	writeJSON(w, http.StatusOK, releaseToResponse(updated, int(count)))
}

// RollbackRelease — POST /api/releases/{id}/rollback. Owner/admin OR
// approver. Records the user's rollback decision; v1 leaves the
// actual revert PRs to the user (channel post lists each merged PR
// with a deep link).
func (h *Handler) RollbackRelease(w http.ResponseWriter, r *http.Request) {
	rel, _, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	// Auth tier: owner/admin OR approver (canVerifyRelease eligibility
	// covers high/critical risk approvers; for low/medium risk we still
	// want an admin gate so a random member can't roll back). We do
	// the membership check first (single round-trip) and then accept
	// either role-based admission or approver-equality.
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found")
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	isAdmin := member.Role == "owner" || member.Role == "admin"
	isApprover := requestedBy.Valid &&
		((rel.ApproverID.Valid && uuidToString(rel.ApproverID) == uuidToString(requestedBy)) ||
			(rel.SecondApproverID.Valid && uuidToString(rel.SecondApproverID) == uuidToString(requestedBy)))
	if !isAdmin && !isApprover {
		writeError(w, http.StatusForbidden,
			"rollback requires workspace owner/admin or release approver")
		return
	}

	var req RollbackReleaseRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required for rollback")
		return
	}

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(wsID)
	updated, err := svc.MarkReleaseRollback(r.Context(), rel.ID, requestedBy, reason, deps)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseAlreadyRolled):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrReleaseNotInProduction):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrReleaseRollbackNoTarget):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			slog.Warn("ship: rollback release failed",
				"release_id", uuidToString(rel.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to roll back release: "+err.Error())
		}
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
		"stage":      string(updated.Stage),
	})
	writeJSON(w, http.StatusOK, releaseToResponse(updated, int(count)))
}

// MarkReleaseDone — POST /api/releases/{id}/mark_done. Fast-forward
// the periodic finalizer when the user is confident the 24h watch
// can be closed early. Idempotent: a release that's already done
// returns 200 with the existing state.
func (h *Handler) MarkReleaseDone(w http.ResponseWriter, r *http.Request) {
	rel, _, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(wsID)
	updated, err := svc.MarkReleaseDone(r.Context(), rel.ID, deps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark done: "+err.Error())
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id": uuidToString(updated.ID),
		"stage":      string(updated.Stage),
	})
	writeJSON(w, http.StatusOK, releaseToResponse(updated, int(count)))
}

// ----- health rollup --------------------------------------------------------

// releaseHealthResponse is the wire shape for GET .../health. All
// fields default to zero/empty when no rollup has been written yet
// (pre-promote releases) — the UI renders an empty state.
type releaseHealthResponse struct {
	ReleaseID                string   `json:"release_id"`
	OverallStatus            string   `json:"overall_status"`
	SnapshotAt               string   `json:"snapshot_at"`
	ErrorRateDelta           *float64 `json:"error_rate_delta"`
	P99LatencyDeltaMs        *float64 `json:"p99_latency_delta_ms"`
	InboxIssuesSincePromote  int32    `json:"inbox_issues_since_promote"`
	AgentFailureRateDelta    *float64 `json:"agent_failure_rate_delta"`
}

// GetReleaseHealth — GET /api/releases/{id}/health. Returns the
// release_health row when present, or an empty payload (overall_status
// = "ok", deltas null, count 0) when the monitor hasn't written one
// yet. We intentionally return 200 with empties rather than 404 so
// the panel can render a "monitoring will start once this release
// reaches production" empty state without an error path.
func (h *Handler) GetReleaseHealth(w http.ResponseWriter, r *http.Request) {
	rel, _, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}

	row, err := h.Queries.GetReleaseHealth(r.Context(), rel.ID)
	if err != nil {
		// pgx.ErrNoRows is the common case; return the empty shape so
		// the UI can render an "awaiting first snapshot" panel.
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, releaseHealthResponse{
				ReleaseID:     uuidToString(rel.ID),
				OverallStatus: "ok",
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load health: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, releaseHealthResponse{
		ReleaseID:               uuidToString(row.ReleaseID),
		OverallStatus:           row.OverallStatus,
		SnapshotAt:              timestampToString(row.SnapshotAt),
		ErrorRateDelta:          float8ToPtr(row.ErrorRateDelta),
		P99LatencyDeltaMs:       float8ToPtr(row.P99LatencyDeltaMs),
		InboxIssuesSincePromote: row.InboxIssuesSincePromote,
		AgentFailureRateDelta:   float8ToPtr(row.AgentFailureRateDelta),
	})
}

// ----- webhook integration --------------------------------------------------

// linkProductionDeployForRelease is the production-side counterpart
// to linkStagingDeployForRelease. Called from the deployment_status
// webhook handler after a successful production deploy lands. Looks
// up a release whose merged_main_sha (or already-set production_main_sha)
// matches the deploy's sha; on hit, advances the release.
//
// Best-effort — every error path logs and returns. A missed linkage
// is recoverable via the manual mark_production_deployed endpoint.
func (h *Handler) linkProductionDeployForRelease(
	ctx context.Context,
	workspaceID pgtype.UUID,
	deployID pgtype.UUID,
	deploySHA string,
) {
	if !workspaceID.Valid || !deployID.Valid || deploySHA == "" {
		return
	}
	release, err := h.Queries.FindReleaseByProductionMainSHA(ctx, db.FindReleaseByProductionMainSHAParams{
		WorkspaceID:       workspaceID,
		ProductionMainSha: pgtype.Text{String: deploySHA, Valid: true},
	})
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("ship: find release by prod sha failed",
				"sha", deploySHA, "error", err)
		}
		return
	}

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(workspaceID)
	if _, err := svc.LinkProductionDeploy(ctx, release.ID, deployID, deploySHA, deps); err != nil {
		slog.Warn("ship: link production deploy failed",
			"release_id", uuidToString(release.ID), "error", err)
	}
}

// ----- helpers --------------------------------------------------------------

// float8ToPtr returns a pointer to the float64 when valid, nil
// otherwise. The wire shape uses a JSON null for "no signal" rather
// than a fake zero so the UI can render "—".
func float8ToPtr(f pgtype.Float8) *float64 {
	if !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}

// mustJSON marshals a payload to JSON, returning nil on error. Used
// for the optional rollback_plan audit-event payload — the audit row
// has a NULL JSONB payload column when marshaling fails, which the
// UI tolerates.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
