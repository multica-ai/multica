package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Automatic run queue. A backlog issue in the queue moves to todo on its own
// once every issue it waits on (run_after_issue_ids) has finished. An issue with
// no waits starts at once. The release rule is the ListReleasableQueuedIssues
// query: it holds no state of its own, so the event listener and the periodic
// sweep can both call PromoteQueuedIssues freely.

const (
	maxRunAfterIssues     = 100
	maxQueueBatch         = 200
	queuePromoteBatchSize = 200
)

// runQueueRequest is the body of the single and batch writes and of the preview.
// Queued false takes the issues out of the queue; AfterIssueIDs is then ignored.
type runQueueRequest struct {
	Queued        bool     `json:"queued"`
	AfterIssueIDs []string `json:"after_issue_ids"`
	IssueIDs      []string `json:"issue_ids"`
}

// validateRunAfter parses the waits and checks them against the issues being
// queued: the waits must exist in the workspace, must not include the issues
// themselves, and must not lead back to them (a cycle would never release).
func (h *Handler) validateRunAfter(ctx context.Context, w http.ResponseWriter, workspaceID pgtype.UUID, targets []pgtype.UUID, queued bool, raw []string) ([]pgtype.UUID, bool) {
	if !queued {
		return []pgtype.UUID{}, true
	}
	ids, ok := parseUUIDSliceOrBadRequest(w, raw, "after_issue_ids")
	if !ok {
		return nil, false
	}
	ids = uniqueUUIDs(ids)
	if len(ids) > maxRunAfterIssues {
		writeError(w, http.StatusBadRequest, "after_issue_ids must hold at most 100 issues")
		return nil, false
	}
	if len(ids) == 0 {
		return []pgtype.UUID{}, true
	}
	found, err := h.Queries.ListIssuesByIDsInWorkspace(ctx, db.ListIssuesByIDsInWorkspaceParams{WorkspaceID: workspaceID, Ids: ids})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check run queue waits")
		return nil, false
	}
	if len(found) != len(ids) {
		writeError(w, http.StatusBadRequest, "after_issue_ids must name issues in this workspace")
		return nil, false
	}
	cycle, err := h.runQueueWouldCycle(ctx, workspaceID, targets, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check run queue waits")
		return nil, false
	}
	if cycle {
		writeError(w, http.StatusBadRequest, "an issue cannot wait on itself or on an issue that waits on it")
		return nil, false
	}
	return ids, true
}

// runQueueWouldCycle reports whether any target is reachable from the waits by
// following run_after_issue_ids. Targets are about to get the same waits, so
// their stored edges are not expanded: a path through one of them ends there.
func (h *Handler) runQueueWouldCycle(ctx context.Context, workspaceID pgtype.UUID, targets, waits []pgtype.UUID) (bool, error) {
	targetSet := make(map[string]bool, len(targets))
	for _, t := range targets {
		targetSet[uuidToString(t)] = true
	}
	seen := map[string]bool{}
	frontier := waits
	for len(frontier) > 0 {
		var toFetch []pgtype.UUID
		for _, id := range frontier {
			key := uuidToString(id)
			if targetSet[key] {
				return true, nil
			}
			if !seen[key] {
				seen[key] = true
				toFetch = append(toFetch, id)
			}
		}
		if len(toFetch) == 0 {
			break
		}
		rows, err := h.Queries.ListIssuesByIDsInWorkspace(ctx, db.ListIssuesByIDsInWorkspaceParams{WorkspaceID: workspaceID, Ids: toFetch})
		if err != nil {
			return false, err
		}
		frontier = nil
		for _, row := range rows {
			frontier = append(frontier, row.RunAfterIssueIds...)
		}
	}
	return false, nil
}

func uniqueUUIDs(ids []pgtype.UUID) []pgtype.UUID {
	seen := make(map[string]bool, len(ids))
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		key := uuidToString(id)
		if !seen[key] {
			seen[key] = true
			out = append(out, id)
		}
	}
	return out
}

// SetIssueRunQueue puts an issue in the run queue, waiting on the given issues,
// or takes it out, then promotes whatever the change makes ready.
func (h *Handler) SetIssueRunQueue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req runQueueRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	after, ok := h.validateRunAfter(r.Context(), w, issue.WorkspaceID, []pgtype.UUID{issue.ID}, req.Queued, req.AfterIssueIDs)
	if !ok {
		return
	}
	updated, err := h.Queries.SetIssueRunQueue(r.Context(), db.SetIssueRunQueueParams{
		ID:               issue.ID,
		WorkspaceID:      issue.WorkspaceID,
		RunQueued:        req.Queued,
		RunAfterIssueIds: after,
	})
	if err != nil {
		slog.Warn("set issue run queue failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, http.StatusInternalServerError, "failed to set run queue")
		return
	}

	workspaceID := uuidToString(updated.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	resp := issueToResponse(updated, h.getIssuePrefix(r.Context(), updated.WorkspaceID))
	h.fillStatusCategory(r.Context(), updated.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{"issue": resp})

	if _, err := h.PromoteQueuedIssues(r.Context(), updated.WorkspaceID, false); err != nil {
		slog.Warn("promote queued issues failed", "error", err, "workspace_id", workspaceID)
	}
	// Promotion may have moved this very issue; answer with what is stored now.
	if current, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: updated.ID, WorkspaceID: updated.WorkspaceID}); err == nil {
		resp = issueToResponse(current, h.getIssuePrefix(r.Context(), current.WorkspaceID))
		h.fillStatusCategory(r.Context(), current.WorkspaceID, &resp)
	}
	writeJSON(w, http.StatusOK, resp)
}

// PromoteQueuedIssues moves every queued backlog issue whose waits are over to
// todo, through the same workflow handoff and run start as a person dragging it
// there. anyScope reads all workspaces; otherwise it reads one workspace. It
// returns how many issues moved.
func (h *Handler) PromoteQueuedIssues(ctx context.Context, workspaceID pgtype.UUID, anyScope bool) (int, error) {
	ready, err := h.Queries.ListReleasableQueuedIssues(ctx, db.ListReleasableQueuedIssuesParams{
		AnyScope:    anyScope,
		WorkspaceID: workspaceID,
		PageLimit:   queuePromoteBatchSize,
	})
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, issue := range ready {
		if h.promoteQueuedIssue(ctx, issue) {
			moved++
		}
	}
	return moved, nil
}

// promoteQueuedIssue moves one backlog issue to todo. It reports false when the
// issue changed under it or its project workflow refuses todo; the next pass
// looks at it again.
func (h *Handler) promoteQueuedIssue(ctx context.Context, prev db.Issue) bool {
	const target = "todo"
	params := db.UpdateIssueParams{
		ID:               prev.ID,
		AssigneeType:     prev.AssigneeType,
		AssigneeID:       prev.AssigneeID,
		StartDate:        prev.StartDate,
		DueDate:          prev.DueDate,
		ParentIssueID:    prev.ParentIssueID,
		ProjectID:        prev.ProjectID,
		Stage:            prev.Stage,
		Status:           pgtype.Text{String: target, Valid: true},
		ExpectedRevision: pgtype.Int8{Int64: prev.Revision, Valid: true},
	}
	handoff, err := h.applyIssueWorkflow(ctx, prev, &params, true, false, false, nil)
	if err != nil {
		slog.Warn("queue promote: workflow refused", "error", err, "issue_id", uuidToString(prev.ID))
		return false
	}

	writeCtx := withWakeupHandedTo(ctx, h.handedToAgent(ctx, prev, params, handoff != nil))
	issue, err := h.updateIssueWithStatusGuard(writeCtx, prev.WorkspaceID, target, params)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("queue promote: write failed", "error", err, "issue_id", uuidToString(prev.ID))
		}
		return false
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	resp := issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.fillStatusCategory(ctx, issue.WorkspaceID, &resp)
	assigneeChanged := handoff != nil &&
		(prev.AssigneeType.String != issue.AssigneeType.String || uuidToString(prev.AssigneeID) != uuidToString(issue.AssigneeID))
	trigger, willRun := h.IssueService.WillEnqueueRun(ctx, service.IssueTriggerInput{
		Issue:              issue,
		PrevStatus:         prev.Status,
		AssigneeChanged:    assigneeChanged || handoff != nil,
		StatusChanged:      true,
		EnteredHandoffStep: handoff != nil,
	}, service.IssueTriggerProbe{})

	payload := map[string]any{
		"issue":              resp,
		"assignee_changed":   assigneeChanged,
		"status_changed":     true,
		"prev_assignee_type": textToPtr(prev.AssigneeType),
		"prev_assignee_id":   uuidToPtr(prev.AssigneeID),
		"prev_status":        prev.Status,
		"prev_priority":      prev.Priority,
		"creator_type":       prev.CreatorType,
		"creator_id":         uuidToString(prev.CreatorID),
		"source":             "issue_queue",
	}
	if handoff != nil {
		payload["workflow_handoff"] = workflowHandoffPayload(issue, handoff, h.workflowHandoffBrief(ctx, issue, handoff), willRun)
	}
	h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", payload)
	if willRun {
		h.dispatchIssueRun(ctx, issue, trigger, "system", "", "")
	}
	if issue.ParentIssueID.Valid {
		h.processChildEvents(ctx, issue.ParentIssueID)
	}
	return true
}

func (h *Handler) decodeRunQueueBatch(w http.ResponseWriter, r *http.Request) (runQueueRequest, []pgtype.UUID, pgtype.UUID, bool) {
	var req runQueueRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return req, nil, pgtype.UUID{}, false
	}
	if len(req.IssueIDs) == 0 || len(req.IssueIDs) > maxQueueBatch {
		writeError(w, http.StatusBadRequest, "issue_ids must hold 1 to 200 ids")
		return req, nil, pgtype.UUID{}, false
	}
	ids, ok := parseUUIDSliceOrBadRequest(w, req.IssueIDs, "issue_ids")
	if !ok {
		return req, nil, pgtype.UUID{}, false
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return req, nil, pgtype.UUID{}, false
	}
	return req, uniqueUUIDs(ids), workspaceID, true
}

// BatchSetRunQueue queues or takes out several issues at once, all waiting on
// the same issues, then promotes whatever the change makes ready.
func (h *Handler) BatchSetRunQueue(w http.ResponseWriter, r *http.Request) {
	req, ids, workspaceID, ok := h.decodeRunQueueBatch(w, r)
	if !ok {
		return
	}
	after, ok := h.validateRunAfter(r.Context(), w, workspaceID, ids, req.Queued, req.AfterIssueIDs)
	if !ok {
		return
	}
	updated, err := h.Queries.SetIssuesRunQueue(r.Context(), db.SetIssuesRunQueueParams{
		WorkspaceID:      workspaceID,
		Ids:              ids,
		RunQueued:        req.Queued,
		RunAfterIssueIds: after,
	})
	if err != nil {
		slog.Warn("batch set run queue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set run queue")
		return
	}
	wsString := uuidToString(workspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsString)
	prefix := h.getIssuePrefix(r.Context(), workspaceID)
	for _, issue := range updated {
		resp := issueToResponse(issue, prefix)
		h.fillStatusCategory(r.Context(), workspaceID, &resp)
		h.publish(protocol.EventIssueUpdated, wsString, actorType, actorID, map[string]any{"issue": resp})
	}
	if _, err := h.PromoteQueuedIssues(r.Context(), workspaceID, false); err != nil {
		slog.Warn("promote queued issues failed", "error", err, "workspace_id", wsString)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(updated)})
}

type runQueuePreviewResponse struct {
	// ReleaseNow counts the backlog issues among the request that would move to
	// todo at once: none of their waits is unfinished.
	ReleaseNow int `json:"release_now"`
	// RunCount counts those that would also start an agent run.
	RunCount int `json:"run_count"`
}

// PreviewRunQueue answers "if I queue these issues behind these, how many start now".
func (h *Handler) PreviewRunQueue(w http.ResponseWriter, r *http.Request) {
	req, ids, workspaceID, ok := h.decodeRunQueueBatch(w, r)
	if !ok {
		return
	}
	out := runQueuePreviewResponse{}
	if !req.Queued {
		writeJSON(w, http.StatusOK, out)
		return
	}
	after, ok := h.validateRunAfter(r.Context(), w, workspaceID, ids, true, req.AfterIssueIDs)
	if !ok {
		return
	}
	if len(after) > 0 {
		unfinished, err := h.Queries.CountUnfinishedIssues(r.Context(), db.CountUnfinishedIssuesParams{WorkspaceID: workspaceID, Ids: after})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to preview run queue")
			return
		}
		if unfinished > 0 {
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	issues, err := h.Queries.ListIssuesByIDsInWorkspace(r.Context(), db.ListIssuesByIDsInWorkspaceParams{WorkspaceID: workspaceID, Ids: ids})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preview run queue")
		return
	}
	for _, issue := range issues {
		if issue.Status != "backlog" || issue.TriageState.Valid {
			continue
		}
		out.ReleaseNow++
		next := issue
		next.Status = "todo"
		if _, willRun := h.IssueService.WillEnqueueRun(r.Context(), service.IssueTriggerInput{
			Issue: next, PrevStatus: issue.Status, StatusChanged: true,
		}, service.IssueTriggerProbe{}); willRun {
			out.RunCount++
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type runQueueWaitResponse struct {
	IssueID         string   `json:"issue_id"`
	WaitingOnIssues []string `json:"waiting_on_issue_ids"`
}

// ListRunQueueWaits gives, for every queued backlog issue that still waits on
// unfinished work, the issues it waits on.
func (h *Handler) ListRunQueueWaits(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListRunQueueWaits(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run queue waits")
		return
	}
	index := map[string]int{}
	out := []runQueueWaitResponse{}
	for _, row := range rows {
		key := uuidToString(row.IssueID)
		i, seen := index[key]
		if !seen {
			i = len(out)
			index[key] = i
			out = append(out, runQueueWaitResponse{IssueID: key, WaitingOnIssues: []string{}})
		}
		out[i].WaitingOnIssues = append(out[i].WaitingOnIssues, uuidToString(row.WaitingOnID))
	}
	writeJSON(w, http.StatusOK, out)
}
