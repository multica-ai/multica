package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CompleteCurrentRun accepts only the server-authenticated task token's own
// run. It queues intent; it never marks the DB run complete or kills a process.
func (h *Handler) CompleteCurrentRun(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "task_token" || r.Header.Get("X-Task-ID") != chi.URLParam(r, "taskId") {
		writeError(w, http.StatusForbidden, "completion requires this run's task-scoped token")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	ws, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: id, WorkspaceID: ws})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "load completion task failed")
		return
	}
	if err != nil || !task.IssueID.Valid || uuidToString(task.AgentID) != r.Header.Get("X-Agent-ID") {
		writeError(w, http.StatusForbidden, "task token does not own this issue run")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "begin completion failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	locked, err := q.LockTaskInteraction(r.Context(), id)
	if err != nil {
		writeError(w, 500, "load completion state failed")
		return
	}
	var record interaction.Record
	if locked.Status != "running" || len(locked.Interaction) == 0 || json.Unmarshal(locked.Interaction, &record) != nil || record.Owner == "" {
		writeError(w, 409, "run is not an open interactive run")
		return
	}
	// Retries deduplicate, but a human correction permits a fresh request even
	// when steering kept the same native activity alive.
	boundary := "initial"
	for _, input := range record.Commands {
		if input.Kind == "input" || input.Kind == "interrupt" {
			boundary = input.ID
		}
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", record.Owner, record.State.Activity, boundary)))
	command := agent.InteractionCommand{ID: fmt.Sprintf("complete:%x", digest), Kind: "complete", Activity: record.State.Activity}
	if err := record.Accept(command, uuidToString(task.AgentID), time.Now()); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		writeError(w, 500, "encode completion failed")
		return
	}
	if err = q.SaveTaskInteraction(r.Context(), db.SaveTaskInteractionParams{ID: id, Interaction: data}); err != nil {
		writeError(w, 500, "save completion failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "commit completion failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"requested": true, "activity": command.Activity})
}
