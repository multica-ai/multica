package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// loadInteractiveTask applies the same issue/private-agent access boundary as
// cancellation. Standalone chats are deliberately not part of this feature.
func (h *Handler) loadInteractiveTask(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, string, bool) {
	user, ok := requireUserID(w, r)
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	workspace := ctxWorkspaceID(r.Context())
	ws, ok := parseUUIDOrBadRequest(w, workspace, "workspace id")
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: id, WorkspaceID: ws})
	if err != nil && !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to load issue run")
		return task, "", false
	}
	if err != nil || !task.IssueID.Valid {
		writeError(w, http.StatusNotFound, "issue run not found")
		return task, "", false
	}
	a, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: ws})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "agent not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load agent")
		}
		return task, "", false
	}
	actorType, actorID := h.resolveActor(r, user, workspace)
	if !h.canAccessPrivateAgent(r.Context(), a, actorType, actorID, workspace) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return task, "", false
	}
	if r.Method != http.MethodGet && !h.canInvokeAgent(r.Context(), a, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspace) {
		writeError(w, http.StatusForbidden, "you do not have permission to instruct this agent")
		return task, "", false
	}
	return task, user, true
}

func (h *Handler) GetTaskInteraction(w http.ResponseWriter, r *http.Request) {
	task, _, ok := h.loadInteractiveTask(w, r)
	if !ok {
		return
	}
	var record *interaction.Record
	if len(task.Interaction) > 0 && json.Unmarshal(task.Interaction, &record) != nil {
		writeError(w, 500, "invalid interaction state")
		return
	}
	if record != nil && task.Status != "running" {
		record.State.State = agent.InteractionFinished
		for i := range record.Commands {
			if record.Commands[i].Receipt == nil && record.Commands[i].Error == "" {
				record.Commands[i].Error = "run ended before delivery was confirmed"
			}
		}
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) SendTaskInteraction(w http.ResponseWriter, r *http.Request) {
	task, user, ok := h.loadInteractiveTask(w, r)
	if !ok {
		return
	}
	var input struct {
		Owner string `json:"owner"`
		agent.InteractionCommand
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70*1024)
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		writeError(w, 400, "invalid interaction command")
		return
	}
	if input.Kind == "complete" {
		writeError(w, http.StatusBadRequest, "agent completion requires the task-scoped completion endpoint")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "begin interaction transaction failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	locked, err := q.LockTaskInteraction(r.Context(), task.ID)
	if err != nil {
		writeError(w, 500, "load interaction failed")
		return
	}
	var record interaction.Record
	if len(locked.Interaction) == 0 || json.Unmarshal(locked.Interaction, &record) != nil || record.Owner != input.Owner {
		writeError(w, 409, "run is no longer interactive; refresh its state")
		return
	}
	if locked.Status != "running" {
		for _, prior := range record.Commands {
			if prior.InteractionCommand == input.InteractionCommand && prior.ActorID == user {
				writeJSON(w, http.StatusAccepted, record)
				return
			}
		}
		writeError(w, 409, "run has finished; refresh its state")
		return
	}
	if err := record.Accept(input.InteractionCommand, user, time.Now()); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		writeError(w, 500, "encode interaction failed")
		return
	}
	if err := q.SaveTaskInteraction(r.Context(), db.SaveTaskInteractionParams{ID: task.ID, Interaction: data}); err != nil {
		writeError(w, 500, "save interaction failed")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "commit interaction failed")
		return
	}
	writeJSON(w, http.StatusAccepted, record)
}

// SyncTaskInteraction is both a durable inbox and a finish barrier. A dropped
// response can be retried with the same owner/ack; no WS frame is authoritative.
func (h *Handler) SyncTaskInteraction(w http.ResponseWriter, r *http.Request) {
	task, ok := h.requireDaemonTaskAccess(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if task.RuntimeID != runtime.ID || !task.IssueID.Valid {
		writeError(w, 403, "runtime does not own this issue run")
		return
	}
	if runtime.Provider != "pi" && runtime.Provider != "codex" {
		writeError(w, http.StatusConflict, "runtime does not support interactive runs")
		return
	}
	var request interaction.Sync
	r.Body = http.MaxBytesReader(w, r.Body, 140*1024)
	if json.NewDecoder(r.Body).Decode(&request) != nil || request.Owner == "" || len(request.Owner) > 128 {
		writeError(w, 400, "invalid interaction sync")
		return
	}
	if !request.State.Valid() {
		writeError(w, http.StatusBadRequest, "invalid interaction state")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "begin interaction sync failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	locked, err := q.LockTaskInteraction(r.Context(), task.ID)
	if err != nil {
		writeError(w, 500, "load interaction failed")
		return
	}
	if locked.Status != "running" {
		writeError(w, 409, "run is terminal or not started")
		return
	}
	var record interaction.Record
	if len(locked.Interaction) > 0 && json.Unmarshal(locked.Interaction, &record) != nil {
		writeError(w, 500, "invalid persisted interaction")
		return
	}
	if record.Owner == "" {
		if !request.Register || request.Deadline.IsZero() || !request.Deadline.After(time.Now()) {
			writeError(w, 409, "interactive run was not registered")
			return
		}
		// The daemon chooses the tighter execution bound. Never accept a
		// deadline beyond the task credential's maximum lifetime.
		deadline := request.Deadline
		credentialDeadline, err := q.TaskInteractionCredentialDeadline(r.Context(), task.ID)
		if err != nil {
			writeError(w, 500, "load task credential deadline failed")
			return
		}
		limit := credentialDeadline.Time.Add(-time.Minute)
		if !limit.After(time.Now()) {
			writeError(w, 409, "task credential expired or missing")
			return
		}
		if deadline.After(limit) {
			deadline = limit
		}
		record = interaction.Record{Owner: request.Owner, Deadline: deadline, Commands: []interaction.Input{}}
		record.OpeningInput = task.HandoffNote.String
	}
	if record.Owner != request.Owner {
		writeError(w, 409, "interactive owner changed")
		return
	}
	if ack := request.Acknowledgement; ack != nil {
		for i := range record.Commands {
			if record.Commands[i].ID == ack.ID && record.Commands[i].Receipt == nil && record.Commands[i].Error == "" {
				if ack.Receipt == nil && ack.Error == "" {
					writeError(w, 400, "empty acknowledgement")
					return
				}
				record.Commands[i].Receipt, record.Commands[i].Error = ack.Receipt, ack.Error
			}
		}
	}
	record.State, record.UpdatedAt = request.State, time.Now()
	if request.Finish && !record.Pending() {
		record.Finishing = true
	}
	data, err := json.Marshal(record)
	if err != nil {
		writeError(w, 500, "encode interaction failed")
		return
	}
	if q.SaveTaskInteraction(r.Context(), db.SaveTaskInteractionParams{ID: task.ID, Interaction: data}) != nil {
		writeError(w, 500, "save interaction failed")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "commit interaction sync failed")
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) FollowupTaskInteraction(w http.ResponseWriter, r *http.Request) {
	source, actor, ok := h.loadInteractiveTask(w, r)
	if !ok {
		return
	}
	var input struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70*1024)
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.ID == "" || len(input.ID) > 128 || strings.TrimSpace(input.Text) == "" || len(input.Text) > 65536 {
		writeError(w, 400, "invalid follow-up input")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "begin follow-up failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	locked, err := q.LockTaskInteraction(r.Context(), source.ID)
	if err != nil {
		writeError(w, 500, "load source run failed")
		return
	}
	if locked.Status != "completed" && locked.Status != "cancelled" && locked.Status != "failed" {
		writeError(w, 409, "run is still active; send to its live conversation")
		return
	}
	var record interaction.Record
	if len(locked.Interaction) == 0 || json.Unmarshal(locked.Interaction, &record) != nil {
		writeError(w, 409, "source was not an interactive run")
		return
	}
	for _, followup := range record.Followups {
		if followup.ID == input.ID {
			if followup.Text != input.Text || followup.ActorID != actor {
				writeError(w, 409, "follow-up id already used")
				return
			}
			writeJSON(w, 200, map[string]string{"run_id": followup.RunID})
			return
		}
	}
	if len(record.Followups) >= 128 {
		writeError(w, 409, "source run follow-up limit reached")
		return
	}
	issue, err := q.LockIssueForInteractiveFollowup(r.Context(), db.LockIssueForInteractiveFollowupParams{ID: source.IssueID, WorkspaceID: parseUUID(ctxWorkspaceID(r.Context()))})
	if err != nil {
		writeError(w, 404, "issue not found")
		return
	}
	if issue.AssigneeType.String != "agent" || issue.AssigneeID != source.AgentID {
		writeError(w, 409, "issue assignee changed; start work with the current assignee")
		return
	}
	active, err := q.HasActiveTaskForIssue(r.Context(), source.IssueID)
	if err != nil {
		writeError(w, 500, "load active issue runs failed")
		return
	}
	if active {
		writeError(w, 409, "issue already has an active run; open that conversation")
		return
	}
	// Existing queue uniqueness and claim serialization remain authoritative.
	// Pin source.ID so resumption never picks a different concurrent history.
	task, err := h.TaskService.CreateInteractiveFollowup(r.Context(), q, issue, source, input.Text, parseUUID(actor))
	if err != nil {
		writeError(w, 409, "cannot enqueue follow-up: "+err.Error())
		return
	}
	record.Followups = append(record.Followups, interaction.Followup{ID: input.ID, ActorID: actor, Text: input.Text, RunID: uuidToString(task.ID)})
	data, err := json.Marshal(record)
	if err != nil {
		writeError(w, 500, "encode follow-up failed")
		return
	}
	if q.SaveTaskInteraction(r.Context(), db.SaveTaskInteractionParams{ID: source.ID, Interaction: data}) != nil {
		writeError(w, 500, "save follow-up failed")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "commit follow-up failed")
		return
	}
	h.TaskService.PublishInteractiveFollowup(r.Context(), task)
	writeJSON(w, 201, map[string]string{"run_id": uuidToString(task.ID)})
}
