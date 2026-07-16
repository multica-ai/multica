package handler

// CEREBRO-PATCH(run-prompt-snapshot): FIR-3212 — net-new fork handler for the
// byte-exact per-run production prompt snapshot.
//
// Ingest (daemon POST): identity is resolved server-side — agent, workspace,
// and issue come from the task row, never from the payload — and the stored
// view is verified self-consistent at the door: the server recomputes
// sha256_redacted over the delivered layer contents and rejects a mismatch, so
// a corrupt or tampered snapshot is never recorded. sha256_original cannot be
// re-verified here by design: the pre-redaction bytes are not transmitted.
// The insert is idempotent (first write wins) and the row is UPDATE-blocked by
// trigger, so recorded history cannot be rewritten.
//
// Read (member GET): scoped by the agent's workspace membership.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// maxPromptSnapshotBytes caps the total stored layer content. Runtime briefs
// plus task prompts run tens of KB; 4 MiB leaves generous headroom while
// keeping a hostile daemon from filling the table.
const maxPromptSnapshotBytes = 4 << 20

type promptSnapshotLayerPayload struct {
	Name            string `json:"name"`
	Delivery        string `json:"delivery"`
	ByteSize        int    `json:"byte_size"`
	SHA256Original  string `json:"sha256_original"`
	SHA256Redacted  string `json:"sha256_redacted"`
	ContentRedacted string `json:"content_redacted"`
}

type promptSnapshotPayload struct {
	AgentContextVersion string                       `json:"agent_context_version"`
	Provider            string                       `json:"provider"`
	Model               string                       `json:"model"`
	RuntimeVersion      string                       `json:"runtime_version"`
	SystemPromptMode    string                       `json:"system_prompt_mode"`
	Layers              []promptSnapshotLayerPayload `json:"layers"`
	SHA256Original      string                       `json:"sha256_original"`
	SHA256Redacted      string                       `json:"sha256_redacted"`
	TotalBytes          int                          `json:"total_bytes"`
	Redacted            bool                         `json:"redacted"`
}

var promptSnapshotDeliveries = map[string]bool{
	"system_prompt": true,
	"workdir_file":  true,
	"user_prompt":   true,
}

// validatePromptSnapshotPayload enforces shape and self-consistency: the
// combined redacted hash must equal SHA-256 over each layer's stored content
// in order, each terminated by 0x00 — the same serialization the daemon and
// the migration document.
func validatePromptSnapshotPayload(p promptSnapshotPayload) error {
	if p.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if len(p.Layers) == 0 {
		return fmt.Errorf("at least one layer is required")
	}
	if p.SHA256Original == "" || p.SHA256Redacted == "" {
		return fmt.Errorf("sha256_original and sha256_redacted are required")
	}
	total := 0
	h := sha256.New()
	for i, l := range p.Layers {
		if l.Name == "" {
			return fmt.Errorf("layer %d: name is required", i)
		}
		if !promptSnapshotDeliveries[l.Delivery] {
			return fmt.Errorf("layer %d: unknown delivery %q", i, l.Delivery)
		}
		total += len(l.ContentRedacted)
		h.Write([]byte(l.ContentRedacted))
		h.Write([]byte{0})
	}
	if total > maxPromptSnapshotBytes {
		return fmt.Errorf("snapshot exceeds %d bytes", maxPromptSnapshotBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != p.SHA256Redacted {
		return fmt.Errorf("sha256_redacted does not match the delivered layer contents")
	}
	return nil
}

// ReportTaskPromptSnapshot ingests the daemon-reported snapshot for a task.
// POST /api/daemon/tasks/{taskId}/prompt-snapshot
func (h *Handler) ReportTaskPromptSnapshot(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "taskId")
	if !ok {
		return
	}
	var payload promptSnapshotPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPromptSnapshotBytes+(1<<20))).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snapshot payload")
		return
	}
	if err := validatePromptSnapshotPayload(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Identity comes from the task row, not the payload.
	var agentID, workspaceID, issueID pgtype.UUID
	err := h.DB.QueryRow(r.Context(), `
		SELECT atq.agent_id, a.workspace_id, atq.issue_id
		FROM agent_task_queue atq
		JOIN agent a ON a.id = atq.agent_id
		WHERE atq.id = $1`, taskID).Scan(&agentID, &workspaceID, &issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// Best-effort FK to the version history row; the reported semver string is
	// stored regardless, so evidence survives even if history is pruned.
	var versionID pgtype.UUID
	if payload.AgentContextVersion != "" && h.CerebroQueries != nil {
		if id, err := h.CerebroQueries.ResolveAgentContextVersionID(r.Context(), cerebrodb.ResolveAgentContextVersionIDParams{
			AgentID: agentID,
			Version: payload.AgentContextVersion,
		}); err == nil {
			versionID = id
		}
	}

	layersJSON, err := json.Marshal(payload.Layers)
	if err != nil {
		writeError(w, http.StatusBadRequest, "layers not serializable")
		return
	}
	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot store unavailable")
		return
	}
	if _, err := h.CerebroQueries.CreateRunPromptSnapshot(r.Context(), cerebrodb.CreateRunPromptSnapshotParams{
		WorkspaceID:           workspaceID,
		TaskID:                taskID,
		AgentID:               agentID,
		IssueID:               issueID,
		AgentContextVersion:   payload.AgentContextVersion,
		AgentContextVersionID: versionID,
		Provider:              payload.Provider,
		Model:                 payload.Model,
		RuntimeVersion:        payload.RuntimeVersion,
		SystemPromptMode:      payload.SystemPromptMode,
		Layers:                layersJSON,
		Sha256Original:        payload.SHA256Original,
		Sha256Redacted:        payload.SHA256Redacted,
		TotalBytes:            int32(payload.TotalBytes),
		Redacted:              payload.Redacted,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record prompt snapshot")
		return
	}
	// Replays fall through here with 0 rows written — same 204: the evidence
	// already exists and must not change.
	w.WriteHeader(http.StatusNoContent)
}

// promptSnapshotResponseBody is the member-facing shape of one snapshot. The
// JSONB layers column is []byte in the generated row; returned directly Go
// would base64-encode it, so it is re-typed to json.RawMessage here.
type promptSnapshotResponseBody struct {
	TaskID                string          `json:"task_id"`
	IssueID               *string         `json:"issue_id"`
	AgentContextVersion   string          `json:"agent_context_version"`
	AgentContextVersionID *string         `json:"agent_context_version_id"`
	Provider              string          `json:"provider"`
	Model                 string          `json:"model"`
	RuntimeVersion        string          `json:"runtime_version"`
	SystemPromptMode      string          `json:"system_prompt_mode"`
	Layers                json.RawMessage `json:"layers"`
	SHA256Original        string          `json:"sha256_original"`
	SHA256Redacted        string          `json:"sha256_redacted"`
	TotalBytes            int32           `json:"total_bytes"`
	Redacted              bool            `json:"redacted"`
	CreatedAt             *string         `json:"created_at"`
}

func promptSnapshotResponse(row cerebrodb.CerebroRunPromptSnapshot) promptSnapshotResponseBody {
	body := promptSnapshotResponseBody{
		AgentContextVersion: row.AgentContextVersion,
		Provider:            row.Provider,
		Model:               row.Model,
		RuntimeVersion:      row.RuntimeVersion,
		SystemPromptMode:    row.SystemPromptMode,
		Layers:              json.RawMessage(`[]`),
		SHA256Original:      row.Sha256Original,
		SHA256Redacted:      row.Sha256Redacted,
		TotalBytes:          row.TotalBytes,
		Redacted:            row.Redacted,
	}
	if len(row.Layers) > 0 && json.Valid(row.Layers) {
		body.Layers = json.RawMessage(row.Layers)
	}
	if row.TaskID.Valid {
		body.TaskID = uuidToString(row.TaskID)
	}
	if row.IssueID.Valid {
		id := uuidToString(row.IssueID)
		body.IssueID = &id
	}
	if row.AgentContextVersionID.Valid {
		id := uuidToString(row.AgentContextVersionID)
		body.AgentContextVersionID = &id
	}
	if row.CreatedAt.Valid {
		ts := row.CreatedAt.Time.UTC().Format(time.RFC3339)
		body.CreatedAt = &ts
	}
	return body
}

// GetAgentPromptSnapshot returns one run's full snapshot.
// GET /api/agents/{id}/prompt-snapshots/{taskId}
func (h *Handler) GetAgentPromptSnapshot(w http.ResponseWriter, r *http.Request) {
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "taskId")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(agent.WorkspaceID)); !ok {
		return
	}
	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot store unavailable")
		return
	}
	snap, err := h.CerebroQueries.GetRunPromptSnapshotByTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "prompt snapshot not found")
		return
	}
	if snap.AgentID != agentID {
		writeError(w, http.StatusNotFound, "prompt snapshot not found")
		return
	}
	writeJSON(w, http.StatusOK, promptSnapshotResponse(snap))
}

// ListAgentPromptSnapshots returns recent snapshot refs (no prompt bodies).
// GET /api/agents/{id}/prompt-snapshots
func (h *Handler) ListAgentPromptSnapshots(w http.ResponseWriter, r *http.Request) {
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(agent.WorkspaceID)); !ok {
		return
	}
	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot store unavailable")
		return
	}
	rows, err := h.CerebroQueries.ListRunPromptSnapshotsByAgent(r.Context(), cerebrodb.ListRunPromptSnapshotsByAgentParams{
		AgentID: agentID,
		Limit:   100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list prompt snapshots")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": rows})
}
