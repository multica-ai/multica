package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// KagentEnrollmentResponse is the desired reconciliation identity. Credentials,
// runtime configuration, and private agent metadata never cross this boundary.
type KagentEnrollmentResponse struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	WorkspaceID  string `json:"workspace_id"`
	RuntimeID    string `json:"runtime_id"`
	Revision     int64  `json:"revision"`
	Generation   int64  `json:"generation"`
	DesiredState string `json:"desired_state"`
	Phase        string `json:"phase"`
}

// KagentEnrollmentReportRequest only reports readiness; it cannot prove drain
// or authorize remote deletion. Unknown fields are rejected.
type KagentEnrollmentReportRequest struct {
	ExpectedRevision   int64  `json:"expected_revision"`
	ExpectedGeneration int64  `json:"expected_generation"`
	ExpectedPhase      string `json:"expected_phase"`
	Phase              string `json:"phase"`
}

const kagentReadinessBlockedMessage = "Kagent readiness reconciliation blocked"

// Recheck the runtime inside the snapshot used to read enrollment and agent
// rows, so a cached runtime lookup cannot authorize a changed binding.
func (h *Handler) beginKagentLifecycleAccess(w http.ResponseWriter, r *http.Request) (pgx.Tx, *db.Queries, db.AgentRuntime, bool) {
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return nil, nil, db.AgentRuntime{}, false
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
		return nil, nil, db.AgentRuntime{}, false
	}
	if _, err := tx.Exec(r.Context(), "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		tx.Rollback(r.Context())
		writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
		return nil, nil, db.AgentRuntime{}, false
	}
	queries := h.Queries.WithTx(tx)
	runtime, err = queries.GetAgentRuntime(r.Context(), runtime.ID)
	if err != nil {
		tx.Rollback(r.Context())
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "runtime not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load runtime")
		}
		return nil, nil, db.AgentRuntime{}, false
	}
	if !h.requireDaemonRuntimeAuthorized(w, r, uuidToString(runtime.WorkspaceID), runtime.DaemonID, runtime.OwnerID, "runtime not found") {
		tx.Rollback(r.Context())
		return nil, nil, db.AgentRuntime{}, false
	}
	if runtime.Provider != "kagent" {
		tx.Rollback(r.Context())
		writeError(w, http.StatusNotFound, "runtime not found")
		return nil, nil, db.AgentRuntime{}, false
	}
	return tx, queries, runtime, true
}

func kagentEnrollmentResponse(row db.KagentEnrollment, name string) KagentEnrollmentResponse {
	return KagentEnrollmentResponse{AgentID: uuidToString(row.AgentID), Name: name,
		WorkspaceID: uuidToString(row.WorkspaceID), RuntimeID: uuidToString(row.RuntimeID),
		Revision: row.Revision, Generation: row.Generation, DesiredState: row.DesiredState, Phase: row.Phase}
}

func (h *Handler) ListKagentRuntimeAgents(w http.ResponseWriter, r *http.Request) {
	tx, queries, runtime, ok := h.beginKagentLifecycleAccess(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := queries.ListKagentEnrollments(r.Context(), db.ListKagentEnrollmentsParams{WorkspaceID: runtime.WorkspaceID, RuntimeID: runtime.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
		return
	}
	result := make([]KagentEnrollmentResponse, 0, len(rows))
	for _, row := range rows {
		a, err := queries.GetAgent(r.Context(), row.AgentID)
		// Tombstones survive removal of the agent itself and retain their identity.
		if isNotFound(err) && row.DesiredState == "absent" {
			result = append(result, kagentEnrollmentResponse(row, ""))
			continue
		}
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusConflict, "kagent enrollment binding changed")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
			}
			return
		}
		if a.WorkspaceID != runtime.WorkspaceID || (a.RuntimeID != runtime.ID && (row.DesiredState == "present" || a.RuntimeID.Valid)) {
			writeError(w, http.StatusConflict, "kagent enrollment binding changed")
			return
		}
		result = append(result, kagentEnrollmentResponse(row, a.Name))
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ReportKagentRuntimeAgent(w http.ResponseWriter, r *http.Request) {
	tx, queries, runtime, ok := h.beginKagentLifecycleAccess(w, r)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent_id")
	if !ok {
		return
	}
	var req KagentEnrollmentReportRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.ExpectedGeneration <= 0 || req.ExpectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "invalid readiness report")
		return
	}
	allowed := (req.ExpectedPhase == "pending" && (req.Phase == "ready" || req.Phase == "blocked")) ||
		(req.ExpectedPhase == "ready" && req.Phase == "blocked") ||
		(req.ExpectedPhase == "blocked" && req.Phase == "pending")
	if !allowed {
		writeError(w, http.StatusBadRequest, "invalid readiness transition")
		return
	}
	// Resolve enrollment before looking up any agent fields outside its scope.
	enrollment, err := queries.GetKagentEnrollment(r.Context(), db.GetKagentEnrollmentParams{WorkspaceID: runtime.WorkspaceID, RuntimeID: runtime.ID, AgentID: agentID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "enrollment not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
		}
		return
	}
	if enrollment.DesiredState != "present" {
		writeError(w, http.StatusConflict, "readiness report conflicts with enrollment")
		return
	}
	a, err := queries.GetAgent(r.Context(), agentID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusConflict, "kagent enrollment binding changed")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read kagent lifecycle")
		}
		return
	}
	if a.WorkspaceID != runtime.WorkspaceID || a.RuntimeID != runtime.ID {
		writeError(w, http.StatusConflict, "kagent enrollment binding changed")
		return
	}
	message := ""
	if req.Phase == "blocked" {
		message = kagentReadinessBlockedMessage
	}
	row, err := queries.ReportKagentEnrollmentStatus(r.Context(), db.ReportKagentEnrollmentStatusParams{
		WorkspaceID: runtime.WorkspaceID, RuntimeID: runtime.ID, AgentID: agentID,
		ExpectedRevision: req.ExpectedRevision, ExpectedGeneration: req.ExpectedGeneration, ExpectedPhase: req.ExpectedPhase, Phase: req.Phase, LastError: message,
	})
	if err != nil {
		if isNotFound(err) || isKagentSerializationFailure(err) {
			writeError(w, http.StatusConflict, "readiness report conflicts with enrollment")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to report kagent readiness")
		}
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		if isKagentSerializationFailure(err) {
			writeError(w, http.StatusConflict, "readiness report conflicts with enrollment")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to report kagent readiness")
		}
		return
	}
	writeJSON(w, http.StatusOK, kagentEnrollmentResponse(row, a.Name))
}

func isKagentSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
