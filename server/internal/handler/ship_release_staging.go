// Phase 7c — Staging-stage HTTP handlers.
//
// Endpoints:
//   POST /api/releases/{id}/run_smoke_tests   → manually re-trigger smoke
//   POST /api/releases/{id}/mark_smoke_pass   → owner/admin override
//   POST /api/releases/{id}/mark_verified     → human QA gate
//   POST /api/releases/{id}/unverify          → reverse mark_verified
//
// Authorization model:
//   - run_smoke_tests, mark_verified, unverify — workspace member.
//   - mark_smoke_pass — workspace owner/admin (it's a destructive
//     override that bypasses CI signal; we keep the audit clear).
//
// Approver risk gate: the service layer (MarkVerified) enforces the
// approver-equality rule for high/critical risk; the handler only
// proves workspace membership.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// stagingPublisher is the events-bus adapter for the staging deps.
// Mirrors busMergePublisher's shape so the wiring stays uniform.
type stagingPublisher struct{ bus *events.Bus }

func (p *stagingPublisher) PublishMergeEvent(eventType, workspaceID string, payload map[string]any) {
	if p == nil || p.bus == nil {
		return
	}
	p.bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

// stagingDepsFor builds the StagingDeps for a request. Same wiring as
// merge train: long-lived parent context + workspace orchestrator
// attribution for channel posts.
func (h *Handler) stagingDepsFor(workspaceID pgtype.UUID) *ship.StagingDeps {
	parentCtx := h.ServiceCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return &ship.StagingDeps{
		ParentCtx:            parentCtx,
		ChannelOps:           &releaseChannelOps{h: h},
		Publisher:            &stagingPublisher{bus: h.Bus},
		PostToReleaseChannel: h.makeReleaseChannelPoster(workspaceID),
	}
}

// loadReleaseAndProject resolves the {id} URL param to a release row +
// the workspace + the project (so we can supply repo URL to the smoke
// trigger path). Returns ok=false with the error response already
// written.
func (h *Handler) loadReleaseAndProject(w http.ResponseWriter, r *http.Request) (
	db.ShipRelease,
	db.Workspace,
	db.Project,
	pgtype.UUID,
	bool,
) {
	rel, wsID, ok := h.loadRelease(w, r)
	if !ok {
		return db.ShipRelease{}, db.Workspace{}, db.Project{}, pgtype.UUID{}, false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return db.ShipRelease{}, db.Workspace{}, db.Project{}, pgtype.UUID{}, false
	}
	project, err := h.Queries.GetProject(r.Context(), rel.ProjectID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.ShipRelease{}, db.Workspace{}, db.Project{}, pgtype.UUID{}, false
	}
	return rel, ws, project, wsID, true
}

// repoURLForRelease finds the first github_repo resource on the
// project. Releases are project-scoped and projects almost always
// have one repo; multi-repo releases aren't a Phase 7c surface.
//
// The resource_ref column is a JSON blob with `{ url: "..." }`; we
// peel it out the same way SyncProject does.
func (h *Handler) repoURLForRelease(ctx context.Context, projectID pgtype.UUID) (string, error) {
	resources, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, res := range resources {
		if res.ResourceType != "github_repo" {
			continue
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(res.ResourceRef, &payload); err != nil {
			continue
		}
		if payload.URL != "" {
			return payload.URL, nil
		}
	}
	return "", errors.New("no github_repo resource on project")
}

// ----- request shapes -------------------------------------------------------

// MarkSmokePassRequest is the body for POST .../mark_smoke_pass.
type MarkSmokePassRequest struct {
	Note string `json:"note"`
}

// MarkVerifiedRequest is the body for POST .../mark_verified.
type MarkVerifiedRequest struct {
	Note string `json:"note"`
}

// UnverifyRequest is the body for POST .../unverify.
type UnverifyRequest struct {
	Reason string `json:"reason"`
}

// ----- handlers -------------------------------------------------------------

// RunSmokeTestsForRelease — POST /api/releases/{id}/run_smoke_tests.
func (h *Handler) RunSmokeTestsForRelease(w http.ResponseWriter, r *http.Request) {
	rel, ws, project, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	smokeWorkflow := ""
	if ws.ShipHubSmokeWorkflow.Valid {
		smokeWorkflow = ws.ShipHubSmokeWorkflow.String
	}
	repoURL, repoErr := h.repoURLForRelease(r.Context(), project.ID)
	if repoErr != nil {
		writeError(w, http.StatusBadRequest, repoErr.Error())
		return
	}

	svc, ok := h.shipServiceFromWorkspace(w, r, ws, true)
	if !ok {
		return
	}
	deps := h.stagingDepsFor(wsID)
	updated, err := svc.RunSmokeTests(r.Context(), rel.ID, requestedBy, ship.RunSmokeTestsParams{
		WorkspaceID:   wsID,
		SmokeWorkflow: smokeWorkflow,
		RepoURL:       repoURL,
	}, deps)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrTokenMissing):
			writeError(w, http.StatusBadRequest, "GitHub token not configured")
		case errors.Is(err, ship.ErrSmokeNotConfigured):
			writeError(w, http.StatusBadRequest, "smoke workflow not configured for this workspace")
		case errors.Is(err, ship.ErrReleaseNotInStaging):
			writeError(w, http.StatusConflict, err.Error())
		default:
			slog.Warn("ship: run smoke tests failed",
				"release_id", uuidToString(rel.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to trigger smoke tests: "+err.Error())
		}
		return
	}

	// 202 Accepted — the workflow is now queued; the smoke_status
	// will flip via webhook in a few seconds.
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	writeJSON(w, http.StatusAccepted, releaseToResponse(updated, int(count)))
}

// MarkSmokePass — POST /api/releases/{id}/mark_smoke_pass. Owner/admin only.
func (h *Handler) MarkSmokePass(w http.ResponseWriter, r *http.Request) {
	rel, _, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(wsID), "workspace not found", "owner", "admin"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	var req MarkSmokePassRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(wsID)
	updated, err := svc.MarkSmokeManualPass(r.Context(), rel.ID, requestedBy, strings.TrimSpace(req.Note), deps)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseNotInStaging):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to mark smoke pass: "+err.Error())
		}
		return
	}
	count, _ := h.Queries.CountActiveReleasePullRequests(r.Context(), updated.ID)
	h.publish(protocol.EventReleaseSmokeUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"release_id":   uuidToString(updated.ID),
		"smoke_status": ship.SmokeStatusManualPass,
	})
	writeJSON(w, http.StatusOK, releaseToResponse(updated, int(count)))
}

// MarkReleaseVerified — POST /api/releases/{id}/mark_verified.
func (h *Handler) MarkReleaseVerified(w http.ResponseWriter, r *http.Request) {
	rel, _, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	var req MarkVerifiedRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(wsID)
	updated, err := svc.MarkVerified(r.Context(), rel.ID, requestedBy, strings.TrimSpace(req.Note), deps)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseNotInStaging):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ship.ErrApproverRequired):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, ship.ErrSmokeNotFinished):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to verify release: "+err.Error())
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

// UnverifyRelease — POST /api/releases/{id}/unverify. Reverses
// mark_verified. Workspace member is the floor; the service-layer
// gate also checks the approver-equality rule for owner/admin or
// original-approver semantics. We do NOT enforce owner/admin at the
// handler level because workspace membership + audit-trail
// requirement keep this honest in practice.
func (h *Handler) UnverifyRelease(w http.ResponseWriter, r *http.Request) {
	rel, _, _, wsID, ok := h.loadReleaseAndProject(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	requestedBy, _ := h.parseUserUUIDOrZero(userID)

	var req UnverifyRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required for unverify")
		return
	}

	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(wsID)
	updated, err := svc.Unverify(r.Context(), rel.ID, requestedBy, reason, deps)
	if err != nil {
		switch {
		case errors.Is(err, ship.ErrReleaseNotInVerifying):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to unverify release: "+err.Error())
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

// ----- webhook integration --------------------------------------------------

// linkStagingDeployForRelease is called from the deployment_status
// webhook handler after a successful staging deploy lands. It looks
// up a release whose merged_main_sha matches the deploy's sha; on
// hit, it triggers the staging linkage flow.
//
// Best-effort: every error path logs + returns; a missed linkage is
// recoverable by the user clicking "Run smoke tests" manually.
func (h *Handler) linkStagingDeployForRelease(
	ctx context.Context,
	workspaceID pgtype.UUID,
	deployID pgtype.UUID,
	deploySHA, repoURL string,
) {
	if !workspaceID.Valid || !deployID.Valid || deploySHA == "" {
		return
	}
	release, err := h.Queries.FindReleaseByMergedMainSHA(ctx, db.FindReleaseByMergedMainSHAParams{
		WorkspaceID:   workspaceID,
		MergedMainSha: pgtype.Text{String: deploySHA, Valid: true},
	})
	if err != nil {
		// pgx.ErrNoRows is the common case — most deploys aren't
		// from a release. Quiet on miss; warn on real errors.
		if !isNotFound(err) {
			slog.Warn("ship: find release by sha failed",
				"sha", deploySHA, "error", err)
		}
		return
	}

	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("ship: get workspace for release linkage failed",
			"workspace_id", uuidToString(workspaceID), "error", err)
		return
	}
	smokeWorkflow := ""
	if ws.ShipHubSmokeWorkflow.Valid {
		smokeWorkflow = ws.ShipHubSmokeWorkflow.String
	}

	// Build a service that has the workspace's GitHub client (only
	// needed when smoke is configured). Mirrors the dispatcher path.
	token := readShipHubGitHubToken(ws.Settings)
	if token == "" {
		if encToken, ok := h.readEncryptedToken(ctx, workspaceID); ok {
			token = encToken
		}
	}
	svc := h.shipServiceFromToken(token)
	deps := h.stagingDepsFor(workspaceID)
	if _, err := svc.LinkStagingDeploy(ctx, release.ID, deployID, deploySHA, smokeWorkflow, repoURL, deps); err != nil {
		slog.Warn("ship: link staging deploy failed",
			"release_id", uuidToString(release.ID), "error", err)
	}
}

// recordSmokeOutcomeForRelease maps a check_run.completed event to a
// release whose smoke_run_id matches. Best-effort: a stray check_run
// for some unrelated workflow is the common case (it returns no rows
// from FindReleaseBySmokeRunID and we drop it silently).
func (h *Handler) recordSmokeOutcomeForRelease(
	ctx context.Context,
	workspaceID pgtype.UUID,
	smokeRunID, conclusion string,
) {
	if !workspaceID.Valid || smokeRunID == "" {
		return
	}
	release, err := h.Queries.FindReleaseBySmokeRunID(ctx, db.FindReleaseBySmokeRunIDParams{
		WorkspaceID: workspaceID,
		SmokeRunID:  pgtype.Text{String: smokeRunID, Valid: true},
	})
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("ship: find release by smoke run id failed",
				"run_id", smokeRunID, "error", err)
		}
		return
	}
	svc := &ship.Service{Q: h.Queries}
	deps := h.stagingDepsFor(workspaceID)
	if _, err := svc.RecordSmokeOutcome(ctx, release.ID, conclusion, deps); err != nil {
		slog.Warn("ship: record smoke outcome failed",
			"release_id", uuidToString(release.ID), "error", err)
	}
}

// shipServiceFromToken constructs a service with the workspace's
// GitHub token from a webhook goroutine context (where the http
// request is already gone). Mirrors shipServiceFromWorkspace's wiring
// without the response-writer dependency.
func (h *Handler) shipServiceFromToken(token string) *ship.Service {
	return &ship.Service{
		Q:      h.Queries,
		Github: gh.NewClient(token),
	}
}
