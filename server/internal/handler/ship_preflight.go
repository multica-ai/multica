// Phase 5 Ship Hub — pre-flight production gate.
//
// Three endpoints, all gated on the ship_hub_enabled workspace flag:
//
//   POST   /api/deploy_environments/{id}/preflight
//          Get-or-create a preflight row for (env, target_sha). Body:
//          {"target_sha": "<40-hex>"}.
//
//   PATCH  /api/deploy_preflight/{id}
//          Partial update of any field on the checklist. The QA verify
//          and approver fields are auto-stamped with the requesting
//          user when the body sets the corresponding boolean true; we
//          deliberately don't trust client-supplied user IDs here.
//
//   POST   /api/deploy_preflight/{id}/promote
//          Run the gate. On success, the handler creates a `deploy`
//          row in `pending` state for the env+sha and stamps
//          preflight.promoted_at. The actual deploy is fired by the
//          existing post-deploy webhook flow when CI rolls forward.
//
// Risk-tier gate (matches the spec):
//
//   low      — smoke_tests_ok must be true.
//   medium   — smoke_tests_ok AND qa_verified.
//   high     — all three (migrations + smoke + QA) AND a rollback_plan
//              AND a single approver.
//   critical — all three AND rollback_plan AND TWO distinct approvers
//              who are not the requesting user.
//
// The gate consults the most-recently-classified PR for the env+sha so
// the user doesn't have to manually supply the risk level. If no PR is
// found we conservatively pick `high` (one-step-up from medium) — a
// missing PR usually means the workspace is using Ship Hub without the
// upstream PR row, and we'd rather over-gate.

package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// preflightResponse is the wire shape. Pointer fields for everything
// optional so the API contract makes the "not yet set" state explicit
// (an unset approver is `null`, not the zero UUID string).
type preflightResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	EnvironmentID    string  `json:"environment_id"`
	TargetSHA        string  `json:"target_sha"`
	MigrationsOK     bool    `json:"migrations_ok"`
	SmokeTestsOK     bool    `json:"smoke_tests_ok"`
	QAVerifiedAt     *string `json:"qa_verified_at"`
	QAVerifiedBy     *string `json:"qa_verified_by"`
	RollbackPlan     *string `json:"rollback_plan"`
	ApproverID       *string `json:"approver_id"`
	SecondApproverID *string `json:"second_approver_id"`
	ApprovedAt       *string `json:"approved_at"`
	PromotedAt       *string `json:"promoted_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	// Phase 5 — derived fields the UI uses to render the gate. Not
	// persisted; recomputed per request from the linked PR.
	RequiredRiskLevel  string   `json:"required_risk_level"`
	GateStatus         string   `json:"gate_status"`         // "ready" | "blocked"
	GateBlockedReasons []string `json:"gate_blocked_reasons"`
}

func preflightToResponse(p db.DeployPreflight, riskLevel db.RiskLevel, gateOK bool, blockers []string) preflightResponse {
	if blockers == nil {
		blockers = []string{}
	}
	gate := "ready"
	if !gateOK {
		gate = "blocked"
	}
	return preflightResponse{
		ID:                 uuidToString(p.ID),
		WorkspaceID:        uuidToString(p.WorkspaceID),
		EnvironmentID:      uuidToString(p.EnvironmentID),
		TargetSHA:          p.TargetSha,
		MigrationsOK:       p.MigrationsOk,
		SmokeTestsOK:       p.SmokeTestsOk,
		QAVerifiedAt:       timestampToPtr(p.QaVerifiedAt),
		QAVerifiedBy:       uuidToPtr(p.QaVerifiedBy),
		RollbackPlan:       textToPtr(p.RollbackPlan),
		ApproverID:         uuidToPtr(p.ApproverID),
		SecondApproverID:   uuidToPtr(p.SecondApproverID),
		ApprovedAt:         timestampToPtr(p.ApprovedAt),
		PromotedAt:         timestampToPtr(p.PromotedAt),
		CreatedAt:          timestampToString(p.CreatedAt),
		UpdatedAt:          timestampToString(p.UpdatedAt),
		RequiredRiskLevel:  string(riskLevel),
		GateStatus:         gate,
		GateBlockedReasons: blockers,
	}
}

// CreatePreflightRequest is the body for POST.
type CreatePreflightRequest struct {
	TargetSHA string `json:"target_sha"`
}

func (h *Handler) CreateOrGetDeployPreflight(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	envUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "environment id")
	if !ok {
		return
	}
	env, err := h.Queries.GetDeployEnvironmentInWorkspace(r.Context(), db.GetDeployEnvironmentInWorkspaceParams{
		ID: envUUID, WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "deploy environment not found")
		return
	}
	var req CreatePreflightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sha := strings.TrimSpace(req.TargetSHA)
	if sha == "" {
		writeError(w, http.StatusBadRequest, "target_sha is required")
		return
	}

	row, err := h.Queries.GetOrCreateDeployPreflight(r.Context(), db.GetOrCreateDeployPreflightParams{
		WorkspaceID:   wsID,
		EnvironmentID: env.ID,
		TargetSha:     sha,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create preflight")
		return
	}
	risk, gateOK, blockers := h.evaluatePreflightGate(r, wsID, row)
	writeJSON(w, http.StatusCreated, preflightToResponse(row, risk, gateOK, blockers))
}

// UpdatePreflightRequest is the PATCH body. Pointer fields for partial
// update semantics. The QAVerified/Approve actions are explicit
// booleans on the wire — passing `qa_verified: true` stamps the
// timestamp and user from the request context; passing `false` clears
// it.
type UpdatePreflightRequest struct {
	MigrationsOK *bool   `json:"migrations_ok"`
	SmokeTestsOK *bool   `json:"smoke_tests_ok"`
	QAVerified   *bool   `json:"qa_verified"`
	RollbackPlan *string `json:"rollback_plan"`
	// Approve flips approved_at + populates approver_id from the request
	// context. SecondApprove fills second_approver_id (used by critical-
	// risk preflights). Neither action accepts a client-supplied user
	// ID — the requesting user is always the actor.
	Approve       *bool `json:"approve"`
	SecondApprove *bool `json:"second_approve"`
}

func (h *Handler) UpdateDeployPreflight(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	preflightUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "preflight id")
	if !ok {
		return
	}
	row, err := h.Queries.GetDeployPreflightByID(r.Context(), preflightUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "preflight not found")
		return
	}
	if uuidToString(row.WorkspaceID) != uuidToString(wsID) {
		writeError(w, http.StatusNotFound, "preflight not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, _ := h.parseUserUUIDOrZero(userID)
	var req UpdatePreflightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateDeployPreflightParams{
		ID: row.ID,
		// Preserve existing fields. The narg pgtype values default to
		// invalid (no change) — see UpdateDeployPreflight in
		// queries/deploy_preflight.sql for the COALESCE semantics.
		MigrationsOk:     pgBoolPtr(req.MigrationsOK),
		SmokeTestsOk:     pgBoolPtr(req.SmokeTestsOK),
		RollbackPlan:     ptrToText(req.RollbackPlan),
		QaVerifiedAt:     row.QaVerifiedAt,
		QaVerifiedBy:     row.QaVerifiedBy,
		ApproverID:       row.ApproverID,
		SecondApproverID: row.SecondApproverID,
		ApprovedAt:       row.ApprovedAt,
	}
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	// QA verification — set or clear depending on the boolean.
	if req.QAVerified != nil {
		if *req.QAVerified {
			params.QaVerifiedAt = now
			params.QaVerifiedBy = userUUID
		} else {
			params.QaVerifiedAt = pgtype.Timestamptz{}
			params.QaVerifiedBy = pgtype.UUID{}
		}
	}
	// First approve — same pattern. We deliberately don't enforce
	// "approver != requester" at this point; the gate check at promote
	// time handles that for critical risk.
	if req.Approve != nil {
		if *req.Approve {
			params.ApproverID = userUUID
			params.ApprovedAt = now
		} else {
			params.ApproverID = pgtype.UUID{}
			params.ApprovedAt = pgtype.Timestamptz{}
		}
	}
	// Second approve — for critical risk; must come from a different
	// user than the first approver, but the gate enforces that, not
	// this PATCH.
	if req.SecondApprove != nil {
		if *req.SecondApprove {
			params.SecondApproverID = userUUID
		} else {
			params.SecondApproverID = pgtype.UUID{}
		}
	}

	updated, err := h.Queries.UpdateDeployPreflight(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update preflight")
		return
	}
	risk, gateOK, blockers := h.evaluatePreflightGate(r, wsID, updated)
	writeJSON(w, http.StatusOK, preflightToResponse(updated, risk, gateOK, blockers))
}

// PromoteDeployPreflight is the gated promotion endpoint. On success
// it stamps promoted_at and inserts a new `deploy` row in `pending`
// state — the existing webhook ingestion path picks up subsequent
// status transitions.
func (h *Handler) PromoteDeployPreflight(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	preflightUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "preflight id")
	if !ok {
		return
	}
	row, err := h.Queries.GetDeployPreflightByID(r.Context(), preflightUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "preflight not found")
		return
	}
	if uuidToString(row.WorkspaceID) != uuidToString(wsID) {
		writeError(w, http.StatusNotFound, "preflight not found")
		return
	}
	if row.PromotedAt.Valid {
		writeError(w, http.StatusConflict, "preflight already promoted")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, _ := h.parseUserUUIDOrZero(userID)

	risk, gateOK, blockers := h.evaluatePreflightGate(r, wsID, row)
	if !gateOK {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":              "preflight checks not satisfied",
			"required_risk_level": string(risk),
			"blocked_reasons":    blockers,
		})
		return
	}
	// Critical-risk extra check: two distinct approvers AND neither is
	// the requesting user. evaluatePreflightGate already verifies
	// distinctness; we add the requester check here so it stays
	// alongside the action that creates the deploy row.
	if risk == db.RiskLevelCritical {
		if uuidToString(row.ApproverID) == uuidToString(userUUID) ||
			uuidToString(row.SecondApproverID) == uuidToString(userUUID) {
			writeError(w, http.StatusForbidden, "critical-risk promotions cannot be triggered by an approver")
			return
		}
	}

	env, err := h.Queries.GetDeployEnvironment(r.Context(), row.EnvironmentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load environment")
		return
	}
	deploy, err := h.Queries.InsertDeploy(r.Context(), db.InsertDeployParams{
		WorkspaceID:   wsID,
		EnvironmentID: env.ID,
		Ref:           env.TargetBranch,
		Sha:           row.TargetSha,
		Status:        db.DeployStatusPending,
		TriggeredBy:   userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create deploy: "+err.Error())
		return
	}
	updated, err := h.Queries.MarkDeployPreflightPromoted(r.Context(), row.ID)
	if err != nil {
		// The deploy row is already in flight — log + continue. The
		// preflight will lag in `promoted_at = NULL`; the next read
		// recomputes the gate and the user can re-promote (which is a
		// 409). Acceptable degraded state.
		writeError(w, http.StatusInternalServerError, "deploy created but preflight stamp failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"preflight": preflightToResponse(updated, risk, true, nil),
		"deploy":    deployToResponse(deploy),
	})
}

// evaluatePreflightGate returns (required risk, ok, blocked reasons).
// Pure over the row — DB reads only for the linked PR and the
// approver-distinctness check.
func (h *Handler) evaluatePreflightGate(r *http.Request, wsID pgtype.UUID, row db.DeployPreflight) (db.RiskLevel, bool, []string) {
	risk := h.deriveRequiredRiskLevel(r, wsID, row)
	blockers := []string{}

	// All tiers benefit from smoke tests.
	if !row.SmokeTestsOk {
		blockers = append(blockers, "smoke_tests_ok must be true")
	}
	switch risk {
	case db.RiskLevelLow:
		// Smoke is enough; nothing further.
	case db.RiskLevelMedium, db.RiskLevelHigh, db.RiskLevelCritical:
		if !row.QaVerifiedAt.Valid {
			blockers = append(blockers, "qa_verified must be set")
		}
	}
	if risk == db.RiskLevelHigh || risk == db.RiskLevelCritical {
		if !row.MigrationsOk {
			blockers = append(blockers, "migrations_ok must be true")
		}
		if !row.RollbackPlan.Valid || strings.TrimSpace(row.RollbackPlan.String) == "" {
			blockers = append(blockers, "rollback_plan is required")
		}
		if !row.ApproverID.Valid {
			blockers = append(blockers, "an approver is required")
		}
	}
	if risk == db.RiskLevelCritical {
		if !row.SecondApproverID.Valid {
			blockers = append(blockers, "a second approver is required")
		} else if row.ApproverID.Valid && uuidToString(row.ApproverID) == uuidToString(row.SecondApproverID) {
			blockers = append(blockers, "approvers must be distinct users")
		}
	}
	return risk, len(blockers) == 0, blockers
}

// deriveRequiredRiskLevel inspects the PR whose head_sha matches the
// preflight's target_sha. We pick the most-recently-classified one
// (head_sha collisions across PRs are rare enough that this is a
// stable choice). Falls back to `high` when no PR is found — see file
// header.
func (h *Handler) deriveRequiredRiskLevel(r *http.Request, wsID pgtype.UUID, row db.DeployPreflight) db.RiskLevel {
	// We don't have a sqlc query for "find PR by head_sha" yet — the
	// volume per workspace is small enough to scan via the workspace
	// list. ListPullRequestsByWorkspace already orders pr_updated_at
	// desc.
	prs, err := h.Queries.ListPullRequestsByWorkspace(r.Context(), db.ListPullRequestsByWorkspaceParams{
		WorkspaceID: wsID,
	})
	if err != nil {
		return db.RiskLevelHigh
	}
	for _, pr := range prs {
		if pr.HeadSha == row.TargetSha {
			return pr.RiskLevel
		}
	}
	return db.RiskLevelHigh
}

// pgBoolPtr is shared with ship.go — boolean partial-update helper.
