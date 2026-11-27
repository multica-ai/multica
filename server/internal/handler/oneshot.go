package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const oneShotSystemKeyPrefix = "oneshot:"

type OneShotRunResponse struct {
	Task  AgentTaskResponse       `json:"task"`
	Input service.OneShotRunInput `json:"input"`
}

func (h *Handler) CreateOneShotTask(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "one-shot runs require a member credential")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, wsID)
	if !ok {
		return
	}
	if member.Role == "guest" {
		writeError(w, http.StatusForbidden, "guests cannot start one-shot runs")
		return
	}
	var input service.OneShotRunInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one JSON object")
		return
	}
	ctx, err := service.NewOneShotContext(wsID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctx.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	rtUUID, ok := parseUUIDOrBadRequest(w, ctx.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to submit run")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), wsUUID); err != nil {
		writeError(w, 500, "failed to lock workspace")
		return
	}
	ws, err := qtx.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, 500, "failed to load workspace")
		return
	}
	runtime, err := qtx.LockAgentRuntimeForAccessChange(r.Context(), rtUUID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to load runtime")
		return
	}
	if err != nil || runtime.WorkspaceID != wsUUID {
		writeError(w, 404, "runtime not found")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, 403, "you do not have access to this runtime")
		return
	}
	if runtime.Status != "online" {
		writeError(w, 409, "runtime must be online to submit a run")
		return
	}
	if runtime.Provider == "qoder_cloud" {
		if err := (qoderOverrides{skills: len(ctx.Skills), model: ctx.ModelID, thinkingLevel: ctx.Effort}).validate(); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if !agent.IsKnownThinkingValue(runtime.Provider, ctx.Effort) {
		writeError(w, 400, thinkingLevelRejection(runtime.Provider, ctx.Effort))
		return
	}
	if ctx.Effort != "" {
		switch h.acpThinkingDecision(r.Context(), runtime.Provider, runtime.ID, ctx.ModelID, ctx.Effort) {
		case acpEffortAbsent:
			writeError(w, 400, thinkingCapabilityRejection(runtime.Provider))
			return
		case acpEffortUnknown:
			writeError(w, 400, thinkingCapabilityUnknownRejection(runtime.Provider))
			return
		}
	}
	if agent.ThinkingLevelRejectedWithoutModel(runtime.Provider) && ctx.Effort != "" && strings.TrimSpace(ctx.ModelID) == "" {
		writeError(w, 400, thinkingNeedsExplicitModelRejection(runtime.Provider))
		return
	}
	// Validate all skills before creating the carrier. The bindings and task
	// commit together, so no partially configured job becomes claimable.
	skillIDs := make([]pgtype.UUID, 0, len(ctx.Skills))
	for _, id := range ctx.Skills {
		uuid, ok := parseUUIDOrBadRequest(w, id, "skill_id")
		if !ok {
			return
		}
		if _, err := qtx.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{ID: uuid, WorkspaceID: wsUUID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, 404, "skill not found")
			} else {
				writeError(w, 500, "failed to load skill")
			}
			return
		}
		skillIDs = append(skillIDs, uuid)
	}
	taskID := dbid.NewV7()
	carrier, err := qtx.CreateOneShotAgent(r.Context(), db.CreateOneShotAgentParams{
		WorkspaceID: wsUUID, RuntimeID: runtime.ID, RuntimeMode: runtime.RuntimeMode,
		Name:    "One-shot " + uuidToString(taskID),
		OwnerID: member.UserID, Instructions: strings.Join(ctx.SystemPrompt, "\n\n"),
		Model:         pgtype.Text{String: ctx.ModelID, Valid: ctx.ModelID != ""},
		ThinkingLevel: pgtype.Text{String: ctx.Effort, Valid: ctx.Effort != ""},
		SystemKey:     pgtype.Text{String: oneShotSystemKeyPrefix + uuidToString(taskID), Valid: true},
	})
	if err != nil {
		writeError(w, 500, "failed to prepare run")
		return
	}
	for _, id := range skillIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{AgentID: carrier.ID, SkillID: id}); err != nil {
			writeError(w, 500, "failed to assign run skills")
			return
		}
	}
	encoded, err := json.Marshal(ctx)
	if err != nil {
		writeError(w, 400, "invalid run input")
		return
	}
	attr := attribution.DirectHumanRun(member.UserID, attribution.EvidenceOneShotRun, taskID)
	task, err := qtx.CreateOneShotTask(r.Context(), db.CreateOneShotTaskParams{
		ID: taskID, AgentID: carrier.ID, RuntimeID: runtime.ID, Context: encoded,
		OriginatorUserID: attr.UserID, AccountableUserID: attr.AccountableUserID,
		OriginatorSource:    pgtype.Text{String: attr.Source.String(), Valid: true},
		TriggerEvidenceKind: pgtype.Text{String: string(attr.EvidenceKind), Valid: true}, TriggerEvidenceRefID: taskID,
	})
	if err != nil {
		writeError(w, 500, "failed to queue run")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit run")
		return
	}
	h.TaskService.RecordHumanDelegation(r.Context(), attr)
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted", "task_id": uuidToString(taskID),
		"logs_uri":   "/" + ws.Slug + "/runs/" + uuidToString(taskID),
		"status_uri": "/api/tasks/oneshot/" + uuidToString(taskID),
	})
}

func (h *Handler) GetOneShotRun(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r)
	if !ok {
		return
	}
	ctx, ok := service.ParseOneShotContext(task)
	if !ok {
		writeError(w, 404, "run not found")
		return
	}
	resp := taskToResponse(task, ctx.WorkspaceID)
	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.Attribution})
	writeJSON(w, 200, OneShotRunResponse{Task: resp, Input: ctx.OneShotRunInput})
}
