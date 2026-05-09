// Phase 5 Ship Hub — workspace summary + time-machine snapshot.
//
// Two endpoints powering the new ambient-sidebar widget and the
// time-machine scrubber:
//
//   GET /api/workspaces/{id}/ship_hub/summary
//       One JSON object aggregating the segments shown in the
//       sidebar strip:
//         in_staging        — open PRs whose head_sha is on staging
//         awaiting_review   — open PRs in review_decision="" or
//                             "REVIEW_REQUIRED" (degraded data lands
//                             here too, matching the Kanban column)
//         failing           — open PRs with ci_status="failure" OR
//                             mergeable="CONFLICTING" OR risk_level
//                             in ("high", "critical")
//         in_production_24h — successful production deploys in last 24h
//         promotion_pending — deploy_preflight rows with promoted_at
//                             still null
//
//   GET /api/projects/{id}/ship_snapshot?at=<RFC3339>
//       Reconstruct the project's PR + deploy state as of a past
//       timestamp. Best-effort approximation per the spec — see the
//       sqlc query commentary for the exact shape.
//
// Both endpoints respect the ship_hub_enabled gate and the standard
// workspace-membership middleware applied at router-mount time.

package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type shipHubSummaryResponse struct {
	InStaging         int64 `json:"in_staging"`
	AwaitingReview    int64 `json:"awaiting_review"`
	Failing           int64 `json:"failing"`
	InProduction24h   int64 `json:"in_production_24h"`
	PromotionPending  int64 `json:"promotion_pending"`
	OpenPRTotal       int64 `json:"open_pr_total"`
}

// GetShipHubSummary returns the aggregate counts for the sidebar
// widget. Three SQL calls, no per-PR scan in Go — the counts are
// computed via GROUP BY on the indexed columns.
func (h *Handler) GetShipHubSummary(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}

	// Failing/risk pivot: counts grouped by risk_level for open PRs.
	// We sum high+critical for the "failing" segment (per the spec's
	// "🔴 1 failing" exemplar; ci_status checks happen below as well).
	riskRows, err := h.Queries.CountWorkspacePullRequestsByRisk(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load risk counts")
		return
	}
	var openTotal int64
	for _, row := range riskRows {
		openTotal += row.PrCount
	}

	// CI-status / awaiting / staging pivots. We pull the workspace's
	// open PRs once and bucket in memory because the per-PR fields
	// don't have indexed counts and the workspace's open PR count is
	// bounded (the spec target is teams of 2-10 with ≤ 100 open PRs).
	prs, err := h.Queries.ListPullRequestsByWorkspace(r.Context(), db.ListPullRequestsByWorkspaceParams{
		WorkspaceID: wsID,
		State:       db.NullPullRequestState{PullRequestState: db.PullRequestStateOpen, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load open PRs")
		return
	}

	// Build a project_id -> staging.current_sha map so we can attribute
	// PRs to staging without a per-PR join.
	envByProject := map[string]string{}
	if envs, err := h.Queries.ListDeployEnvironmentsByWorkspace(r.Context(), wsID); err == nil {
		for _, e := range envs {
			if e.Kind == db.DeployEnvironmentKindStaging && e.CurrentSha.Valid {
				envByProject[uuidToString(e.ProjectID)] = e.CurrentSha.String
			}
		}
	}

	var inStaging, awaiting, failing int64
	failingSet := map[string]struct{}{}
	for _, pr := range prs {
		// Awaiting-review bucket — empty review_decision falls in here
		// so degraded data still surfaces a clear "yellow segment"
		// count (matching the Kanban's in_review column).
		switch textValueUpper(pr.ReviewDecision) {
		case "", "REVIEW_REQUIRED":
			awaiting++
		}
		// In-staging predicate mirrors the Kanban derivation: the PR's
		// head_sha matches the project's staging current_sha.
		if pr.ProjectID.Valid {
			if sha, ok := envByProject[uuidToString(pr.ProjectID)]; ok && sha == pr.HeadSha {
				inStaging++
			}
		}
		// Failing — union of (risk high/critical) AND
		// (ci_status=failure OR mergeable=CONFLICTING). Set-based
		// dedup so a high-risk PR with failing CI counts once.
		isHighRisk := pr.RiskLevel == db.RiskLevelHigh || pr.RiskLevel == db.RiskLevelCritical
		isStuck := textValueLower(pr.CiStatus) == "failure" || textValueUpper(pr.Mergeable) == "CONFLICTING"
		if isHighRisk || isStuck {
			failingSet[uuidToString(pr.ID)] = struct{}{}
		}
	}
	failing = int64(len(failingSet))

	// Production-deploys-in-24h count.
	prodCount, err := h.Queries.CountWorkspaceDeploysIn24h(r.Context(), wsID)
	if err != nil {
		prodCount = 0
	}

	// Promotion-pending count — preflight rows whose promoted_at is
	// still null. We do this in-memory because there's no indexed
	// "WHERE promoted_at IS NULL" partial today and the cardinality is
	// tiny (one row per env+sha undergoing review).
	var promotionPending int64
	if envs, err := h.Queries.ListDeployEnvironmentsByWorkspace(r.Context(), wsID); err == nil {
		for _, e := range envs {
			if e.Kind != db.DeployEnvironmentKindProduction {
				continue
			}
			if !e.CurrentSha.Valid {
				continue
			}
			if pre, err := h.Queries.GetDeployPreflightByEnvAndSHA(r.Context(), db.GetDeployPreflightByEnvAndSHAParams{
				EnvironmentID: e.ID,
				TargetSha:     e.CurrentSha.String,
			}); err == nil {
				if !pre.PromotedAt.Valid {
					promotionPending++
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, shipHubSummaryResponse{
		InStaging:        inStaging,
		AwaitingReview:   awaiting,
		Failing:          failing,
		InProduction24h:  prodCount,
		PromotionPending: promotionPending,
		OpenPRTotal:      openTotal,
	})
}

// shipSnapshotResponse mirrors the project-level Kanban shape but with
// every collection re-derived as of `at`. Matches the live endpoints'
// response shapes so the frontend can reuse the same render
// components — the `at` param is the only difference.
type shipSnapshotResponse struct {
	At           string                      `json:"at"`
	PullRequests []pullRequestResponse       `json:"pull_requests"`
	Environments []deployEnvironmentResponse `json:"environments"`
	// "current" SHA per environment AS OF `at`. Computed from the
	// deploy timeline rather than the env row's current_sha column —
	// the env row reflects today's state, not the past's.
	EnvironmentSHAsAtTime map[string]string `json:"environment_shas_at_time"`
}

// GetProjectShipSnapshot reconstructs the ship state for a project
// at a past timestamp. Best-effort approximation:
//   - PRs where pr_created_at <= at AND (pr_closed_at IS NULL OR > at)
//   - For each env, the most recent succeeded deploy with
//     triggered_at <= at — its sha becomes "what was running then".
func (h *Handler) GetProjectShipSnapshot(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	atRaw := strings.TrimSpace(r.URL.Query().Get("at"))
	if atRaw == "" {
		writeError(w, http.StatusBadRequest, "at parameter is required (RFC3339)")
		return
	}
	at, err := time.Parse(time.RFC3339, atRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "at must be RFC3339")
		return
	}
	// Refuse times in the future — the snapshot is meaningless and a
	// typo could waste a slow scan.
	if at.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "at must not be in the future")
		return
	}
	// Refuse times older than 30 days — matches the UI's slider
	// range. Older snapshots are a different surface (audit, not
	// time-machine).
	if at.Before(time.Now().AddDate(0, 0, -30)) {
		writeError(w, http.StatusBadRequest, "at must not be older than 30 days")
		return
	}

	prs, err := h.Queries.ListPullRequestsCreatedAt(r.Context(), db.ListPullRequestsCreatedAtParams{
		ProjectID: project.ID,
		At:        pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load PRs at time")
		return
	}
	envs, err := h.Queries.ListDeployEnvironmentsByProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deploy environments")
		return
	}
	envSHAs := map[string]string{}
	for _, e := range envs {
		// Walk this env's deploys triggered_at <= at, newest first; the
		// first succeeded row is the active SHA at that time.
		rows, err := h.Queries.ListDeploysByEnvironmentBefore(r.Context(), db.ListDeploysByEnvironmentBeforeParams{
			EnvironmentID: e.ID,
			At:            pgtype.Timestamptz{Time: at, Valid: true},
		})
		if err != nil {
			continue
		}
		for _, d := range rows {
			if d.Status == db.DeployStatusSucceeded {
				envSHAs[uuidToString(e.ID)] = d.Sha
				break
			}
		}
	}

	prResp := make([]pullRequestResponse, 0, len(prs))
	for _, pr := range prs {
		prResp = append(prResp, pullRequestToResponse(pr))
	}
	envResp := make([]deployEnvironmentResponse, 0, len(envs))
	for _, e := range envs {
		envResp = append(envResp, deployEnvironmentToResponse(e))
	}

	writeJSON(w, http.StatusOK, shipSnapshotResponse{
		At:                    at.UTC().Format(time.RFC3339),
		PullRequests:          prResp,
		Environments:          envResp,
		EnvironmentSHAsAtTime: envSHAs,
	})
}

// deal with the chi import being unused in some build modes.
var _ = chi.URLParam

// textValueLower / textValueUpper read a pgtype.Text and normalise.
// Local helpers — duplicated from the service package because the
// handler doesn't import it for these tiny utilities.
func textValueLower(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return strings.ToLower(t.String)
}

func textValueUpper(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return strings.ToUpper(t.String)
}
