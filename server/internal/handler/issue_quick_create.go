package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// QuickCreateIssueRequest is the body for POST /api/issues/quick-create. The
// user picks an actor (agent or squad) in the modal and types one line of
// natural language; the server validates the actor's reachability up front,
// queues a quick-create task, and returns 202 immediately. The agent
// translates the prompt into a `multica issue create` invocation in the
// background; success and failure both surface as inbox notifications to
// the requester.
//
// Exactly one of AgentID / SquadID is required. When SquadID is set, the
// task is enqueued against the squad's leader agent and the leader receives
// the same Operating Protocol briefing it would for an issue assigned to
// the squad, so it can choose to delegate to a squad member as usual.
//
// ProjectID is optional and lets the modal target a specific project so
// the agent's `multica issue create` invocation passes `--project <uuid>`
// instead of letting it default. The frontend remembers the user's last
// pick per workspace, so frequent users skip retyping "in project X".
//
// ParentIssueID is optional and is set by the "Add sub issue" entry point
// when the modal is opened from an existing issue. The agent passes it
// through as `--parent <uuid>` so the new issue is filed as a sub-issue,
// keeping the sub-issue intent of the entry point regardless of whether
// the user submits via manual or agent mode.
type QuickCreateIssueRequest struct {
	AgentID       string   `json:"agent_id,omitempty"`
	SquadID       string   `json:"squad_id,omitempty"`
	Prompt        string   `json:"prompt"`
	ProjectID     string   `json:"project_id,omitempty"`
	ParentIssueID string   `json:"parent_issue_id,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
}

// QuickCreateIssueResponse echoes the queued task id so the frontend can
// correlate the eventual inbox item, even though completion is fully async.
type QuickCreateIssueResponse struct {
	TaskID string `json:"task_id"`
}

func (h *Handler) QuickCreateIssue(w http.ResponseWriter, r *http.Request) {
	var req QuickCreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	hasAgent := strings.TrimSpace(req.AgentID) != ""
	hasSquad := strings.TrimSpace(req.SquadID) != ""
	if hasAgent == hasSquad {
		writeError(w, http.StatusBadRequest, "exactly one of agent_id or squad_id is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	requesterID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	requesterUUID, ok := parseUUIDOrBadRequest(w, requesterID, "requester_id")
	if !ok {
		return
	}

	// Resolve the actor to the agent that will actually run the task. For
	// agent picks that's the agent itself; for squad picks it's the squad's
	// leader agent. The leader receives a squad-leader briefing on dispatch
	// (see daemon.go), matching the behavior of an issue assigned to the
	// squad — picking a squad here is functionally "ask the squad leader to
	// create this issue, on behalf of the squad".
	var agentUUID pgtype.UUID
	var squadUUID pgtype.UUID
	if hasSquad {
		var ok bool
		squadUUID, ok = parseUUIDOrBadRequest(w, req.SquadID, "squad_id")
		if !ok {
			return
		}
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
			ID:          squadUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "squad not found")
			return
		}
		if squad.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "squad is archived")
			return
		}
		agentUUID = squad.LeaderID
	} else {
		var ok bool
		agentUUID, ok = parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !ok {
			return
		}
	}

	// Reuse the same workspace-membership / archived / private-agent
	// ownership rules as `validateAssigneePair` so a user can't POST a
	// private agent_id they shouldn't be able to dispatch (the frontend
	// filters them out, but the handler is the trust boundary). Squad
	// picks reach this with the resolved leader agent; the same rules
	// apply — a private leader behind a squad the user can't reach
	// should still be rejected.
	if status, msg := h.validateAssigneePair(
		r.Context(), r, workspaceID,
		pgtype.Text{String: "agent", Valid: true},
		agentUUID,
	); status != 0 {
		writeError(w, status, msg)
		return
	}

	// Re-load the agent for the runtime liveness check below. Safe by
	// construction: validateAssigneePair just confirmed it exists in this
	// workspace and the caller has visibility.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !agent.RuntimeID.Valid {
		writeAgentUnavailable(w, "agent has no runtime")
		return
	}
	if !h.isRuntimeOnline(r.Context(), agent.RuntimeID) {
		writeAgentUnavailable(w, "agent's runtime is offline")
		return
	}

	// Daemon CLI version gate. The agent-side prompt + create-flow rely on
	// behaviors introduced in MinQuickCreateCLIVersion (URL attachment
	// handling, quick-create attachment binding, no-retry on partial failure).
	// Older daemons either double-create issues on partial CLI failures, drop
	// attachment bindings, or mishandle pasted screenshot URLs; fail closed
	// before enqueuing rather than surface the breakage as an inbox failure
	// twenty seconds later. Dev-built
	// daemons (git-describe shape) are exempted inside CheckMinCLIVersion
	// so `make daemon` works without weakening staging or production.
	if status, payload := h.checkQuickCreateDaemonVersion(r.Context(), agent.RuntimeID); status != 0 {
		writeJSON(w, status, payload)
		return
	}

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	// Optional project_id — validate it belongs to the same workspace before
	// pinning the task to it. The handler is the trust boundary; the frontend
	// already only shows projects from the active workspace, but we re-check
	// here so a forged request can't smuggle a foreign project ID through.
	var projectUUID pgtype.UUID
	if strings.TrimSpace(req.ProjectID) != "" {
		pid, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID:          pid,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found")
			return
		}
		projectUUID = pid
	}

	// Optional parent_issue_id — validate same-workspace membership just like
	// the regular CreateIssue path. Frontend seeds this from the "Add sub
	// issue" entry, but the handler re-checks so a forged request can't
	// smuggle a foreign parent UUID through.
	var parentIssueUUID pgtype.UUID
	if strings.TrimSpace(req.ParentIssueID) != "" {
		pid, ok := parseUUIDOrBadRequest(w, req.ParentIssueID, "parent_issue_id")
		if !ok {
			return
		}
		parent, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          pid,
			WorkspaceID: wsUUID,
		})
		if err != nil || !parent.ID.Valid {
			writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
			return
		}
		parentIssueUUID = pid
	}

	task, err := h.TaskService.EnqueueQuickCreateTask(r.Context(), wsUUID, requesterUUID, agentUUID, squadUUID, prompt, projectUUID, parentIssueUUID, attachmentIDs)
	if err != nil {
		slog.Warn("quick-create enqueue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to enqueue quick-create task")
		return
	}

	writeJSON(w, http.StatusAccepted, QuickCreateIssueResponse{TaskID: uuidToString(task.ID)})
}

// writeAgentUnavailable returns 422 with a stable error code so the modal
// can show a "switch agent" hint without parsing the human-readable reason.
func writeAgentUnavailable(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	json.NewEncoder(w).Encode(map[string]any{
		"code":   "agent_unavailable",
		"reason": reason,
	})
}

// isRuntimeOnline returns true when the given runtime is currently
// reachable (status == "online"). Quick-create rejects submissions whose
// agent's runtime is offline so the user gets immediate feedback in the
// modal instead of an inbox failure twenty seconds later.
func (h *Handler) isRuntimeOnline(ctx context.Context, runtimeID pgtype.UUID) bool {
	rt, err := h.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		return false
	}
	return rt.Status == "online"
}

// checkQuickCreateDaemonVersion enforces MinQuickCreateCLIVersion against the
// CLI version the daemon reported at registration time (stored on the runtime
// row's metadata.cli_version). Returns (0, nil) when the version is
// acceptable, otherwise (status, payload) ready to hand to writeJSON.
//
// Failure shape is stable so the modal can branch on the `code` field and
// surface a "needs upgrade" hint that points at the specific runtime:
//
//	422 {
//	  "code": "daemon_version_unsupported",
//	  "current_version": "0.2.18" | "",
//	  "min_version":     "0.2.21",
//	  "runtime_id":      "<uuid>"
//	}
func (h *Handler) checkQuickCreateDaemonVersion(ctx context.Context, runtimeID pgtype.UUID) (int, map[string]any) {
	rt, err := h.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		// Runtime row vanished between the online check and here — treat
		// as unavailable rather than wedging the request on a 500.
		return http.StatusUnprocessableEntity, map[string]any{
			"code":   "agent_unavailable",
			"reason": "agent's runtime is no longer registered",
		}
	}
	current := readRuntimeCLIVersion(rt.Metadata)
	switch err := agent.CheckMinCLIVersion(current); {
	case err == nil:
		return 0, nil
	case errors.Is(err, agent.ErrCLIVersionMissing), errors.Is(err, agent.ErrCLIVersionTooOld):
		return http.StatusUnprocessableEntity, map[string]any{
			"code":            "daemon_version_unsupported",
			"current_version": current,
			"min_version":     agent.MinQuickCreateCLIVersion,
			"runtime_id":      uuidToString(runtimeID),
		}
	default:
		// Defensive fall-through: unknown error from the version check is
		// also fail-closed, since the gate exists precisely because we
		// can't trust older daemons with this flow.
		return http.StatusUnprocessableEntity, map[string]any{
			"code":            "daemon_version_unsupported",
			"current_version": current,
			"min_version":     agent.MinQuickCreateCLIVersion,
			"runtime_id":      uuidToString(runtimeID),
		}
	}
}

// readRuntimeCLIVersion pulls metadata.cli_version off a runtime row. The
// metadata column is JSONB on the wire; the daemon stores the multica CLI
// version under that key during registration (see DaemonRegister).
func readRuntimeCLIVersion(metadata []byte) string {
	if len(metadata) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(metadata, &m); err != nil {
		return ""
	}
	if v, ok := m["cli_version"].(string); ok {
		return v
	}
	return ""
}
