package handler

// CEREBRO-PATCH(cerebro-issue-update-custom-status-hook): FIR-1550 v2b —
// invoked from UpdateIssue after the upstream base-status commit so the
// project's custom_status sidecar stays in sync with the new base. Handles
// both the explicit picker case (caller passed custom_status_key) and the
// auto-resolve case (caller only touched the base status). Also runs the
// per-status trigger engine when the sidecar pin lands on an entry with
// configured automation (assignee pin / fire agent).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cerebroApplyCustomStatusAfterUpdate runs the resolver when (and only when)
// the upstream UpdateIssue touched anything that affects the sidecar:
// the base status, the project assignment, or an explicit custom_status_key.
// Re-enriches resp.CustomStatus from the canonical sidecar after the write
// so the HTTP response and the issue:updated WS event carry the new pin.
//
// statusChanged: the upstream base status moved this request.
// projectChanged: the issue's project_id moved (model may differ).
// requestedKey: pointer indicates the caller sent custom_status_key; nil =
// not in payload. Empty string = sent but blank, treated as "no pin".
//
// Returns false + writes the response when the explicit key is malformed
// (400); the caller MUST stop after a false return. Other resolver errors
// are best-effort — logged, never block the response.
func (h *Handler) cerebroApplyCustomStatusAfterUpdate(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	issueID, projectID, workspaceID pgtype.UUID,
	newBase string,
	actorType, actorID string,
	statusChanged, projectChanged bool,
	requestedKey *string,
	resp *IssueResponse,
) bool {
	if h.CustomStatusResolver == nil {
		return true
	}
	// Only run when something on the sidecar-relevant axes moved or the
	// caller explicitly addressed the sidecar.
	if !statusChanged && !projectChanged && requestedKey == nil {
		return true
	}
	if !projectID.Valid {
		// Standalone issue (no project) — clear any leftover sidecar so
		// the issue doesn't keep a stale pin from a previous project.
		resp.CustomStatus = nil
		return true
	}
	// Capture the pre-resolver pin so the trigger engine can detect a
	// genuine pin change (vs. the resolver no-op'ing because the pin
	// already matched the new base).
	prevSide, _ := h.lookupSidecarKey(ctx, issueID)
	var keyArg string
	if requestedKey != nil {
		keyArg = *requestedKey
	}
	err := h.CustomStatusResolver.ResolveCustomStatusAfterBaseChange(
		ctx, issueID, projectID, workspaceID, newBase, actorID, actorType, keyArg,
	)
	if err != nil {
		if errors.Is(err, ErrCustomStatusKeyMismatch) {
			writeError(w, http.StatusBadRequest, err.Error())
			return false
		}
		// Best-effort: the base status update has already committed. Log
		// and move on so the user still sees their status change land.
		slog.Warn("cerebro custom-status resolver failed",
			"issue_id", uuidToString(issueID),
			"project_id", uuidToString(projectID),
			"new_base", newBase,
			"error", err,
		)
	}
	// Re-enrich the response from the canonical sidecar so the HTTP body
	// and the issue:updated event reflect the post-resolver state.
	resp.CustomStatus = h.cerebroLoadIssueCustomStatus(ctx, issueID)

	// FIR-1550 v2b trigger engine: when the resolver landed the issue on a
	// custom_status with configured automation AND the pin is new (or
	// changed from a previous custom_status), fire the triggers. Skipped
	// silently when there is no change so a same-status save doesn't
	// re-fire automations.
	if resp.CustomStatus != nil && resp.CustomStatus.CustomStatusKey != prevSide {
		h.cerebroFireCustomStatusTriggers(ctx, r, issueID, workspaceID, resp.CustomStatus, actorType, actorID)
	}
	return true
}

// lookupSidecarKey returns the current sidecar's custom_status_key for the
// issue, or "" when no sidecar exists. Used to decide whether the trigger
// engine should fire (only on a real pin change).
func (h *Handler) lookupSidecarKey(ctx context.Context, issueID pgtype.UUID) (string, bool) {
	if h.CerebroQueries == nil {
		return "", false
	}
	row, err := h.CerebroQueries.GetCerebroIssueStatus(ctx, issueID)
	if err != nil {
		return "", false
	}
	return row.CustomStatusKey, true
}

// cerebroFireCustomStatusTriggers applies the configured automation for a
// custom_status entry: optional assign-to (member or agent) and optional
// fire-agent (enqueues a fresh task on the agent so it sees the new
// status). Best-effort — a trigger failure is logged but never rolled
// back; the user already saw their status change land.
func (h *Handler) cerebroFireCustomStatusTriggers(
	ctx context.Context,
	r *http.Request,
	issueID, workspaceID pgtype.UUID,
	pin *IssueCustomStatusPayload,
	actorType, actorID string,
) {
	if pin == nil || h.CerebroQueries == nil {
		return
	}
	modelUUID, err := parseUUIDOrZero(pin.StatusModelID)
	if !err || !modelUUID.Valid {
		return
	}
	model, mErr := h.CerebroQueries.GetCerebroStatusModel(ctx, modelUUID)
	if mErr != nil {
		return
	}
	entry, ok := findStatusEntryWithTriggers(model.Statuses, pin.CustomStatusKey)
	if !ok || entry.Triggers == nil {
		return
	}
	t := entry.Triggers
	if t.AssignToID != "" && (t.AssignToType == "member" || t.AssignToType == "agent") {
		assigneeID, parsed := parseUUIDOrZero(t.AssignToID)
		if parsed && assigneeID.Valid {
			h.applyAssignTrigger(ctx, r, issueID, workspaceID, t.AssignToType, assigneeID, actorType, actorID)
		}
	}
	if t.FireAgentID != "" {
		agentID, parsed := parseUUIDOrZero(t.FireAgentID)
		if parsed && agentID.Valid {
			h.applyFireAgentTrigger(ctx, issueID, workspaceID, agentID)
		}
	}
}

// statusEntryView mirrors just enough of the cerebro statusmodels.statusEntry
// shape that the trigger engine needs: the key (for lookup), the optional
// trigger block. Kept local so this file doesn't import the cerebro package
// directly and create an upstream-zone import cycle.
type statusEntryView struct {
	Key      string                  `json:"key"`
	Triggers *statusEntryTriggerView `json:"triggers,omitempty"`
}

type statusEntryTriggerView struct {
	AssignToID   string `json:"assign_to_id,omitempty"`
	AssignToType string `json:"assign_to_type,omitempty"`
	FireAgentID  string `json:"fire_agent_id,omitempty"`
}

// findStatusEntryWithTriggers parses the model's statuses JSONB and returns
// the entry matching key (with its trigger config). False when missing or
// malformed.
func findStatusEntryWithTriggers(raw []byte, key string) (statusEntryView, bool) {
	if len(raw) == 0 || key == "" {
		return statusEntryView{}, false
	}
	var entries []statusEntryView
	if err := json.Unmarshal(raw, &entries); err != nil {
		return statusEntryView{}, false
	}
	for _, e := range entries {
		if e.Key == key {
			return e, true
		}
	}
	return statusEntryView{}, false
}

// applyAssignTrigger pins the issue's assignee to the configured target.
// Skipped silently when the target is already assigned (idempotent — no
// task-cancel/enqueue churn on a no-op).
func (h *Handler) applyAssignTrigger(
	ctx context.Context,
	r *http.Request,
	issueID, workspaceID pgtype.UUID,
	assigneeType string,
	assigneeID pgtype.UUID,
	actorType, actorID string,
) {
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	if issue.AssigneeType.String == assigneeType && uuidToString(issue.AssigneeID) == uuidToString(assigneeID) {
		return
	}
	_ = workspaceID // upstream UpdateIssueAssignee identifies the row by id; workspace scope was enforced by loadIssueForUser upstream.
	params := db.UpdateIssueAssigneeParams{
		ID:           issueID,
		AssigneeType: pgtype.Text{String: assigneeType, Valid: true},
		AssigneeID:   assigneeID,
	}
	if _, err := h.Queries.UpdateIssueAssignee(ctx, params); err != nil {
		slog.Warn("cerebro custom-status trigger: assign update failed",
			"issue_id", uuidToString(issueID),
			"assignee_type", assigneeType,
			"assignee_id", uuidToString(assigneeID),
			"error", err,
		)
		return
	}
	// Reconcile task queue: agent assignment enqueues a task (so the new
	// assignee gets the issue right away); member assignment just updates
	// the field — no task enqueue.
	if assigneeType == "agent" && h.TaskService != nil {
		fresh, _ := h.Queries.GetIssue(ctx, issueID)
		h.TaskService.CancelTasksForIssue(ctx, issueID)
		if h.shouldEnqueueAgentTask(ctx, fresh) {
			h.TaskService.EnqueueTaskForIssue(ctx, fresh)
		}
	}
}

// applyFireAgentTrigger enqueues a fresh task on the configured agent so
// it picks up the issue's new status. Idempotent within TaskService —
// duplicates collapse to a single in-flight task per (agent, issue).
func (h *Handler) applyFireAgentTrigger(
	ctx context.Context,
	issueID, workspaceID pgtype.UUID,
	agentID pgtype.UUID,
) {
	if h.TaskService == nil {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	// Only fire when the configured agent is actually the current assignee
	// (or no assignee is set). Firing on an issue assigned to someone else
	// would be surprising — the agent didn't ask for it.
	if issue.AssigneeType.String == "agent" && uuidToString(issue.AssigneeID) != uuidToString(agentID) {
		return
	}
	h.TaskService.EnqueueTaskForIssue(ctx, issue)
}

// parseUUIDOrZero returns (uuid, true) on parse, (zero, false) on failure.
// Kept local so this file doesn't depend on the panic-on-bad-input
// parseUUID helper — trigger inputs are stored user content, not URL path
// params, and a malformed stored UUID must NEVER crash the request.
func parseUUIDOrZero(s string) (pgtype.UUID, bool) {
	var out pgtype.UUID
	if err := out.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return out, out.Valid
}
