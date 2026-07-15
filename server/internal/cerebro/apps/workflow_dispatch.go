package apps

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	hatchet "github.com/hatchet-dev/hatchet/sdks/go"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
)

const miniAppWorkflowTaskName = "cerebro-mini-app-workflow"

type workflowDispatcher interface {
	Dispatch(context.Context, string) error
}

type hatchetWorkflowDispatcher struct{}

func (hatchetWorkflowDispatcher) Dispatch(ctx context.Context, runID string) error {
	client, err := hatchet.NewClient()
	if err != nil {
		return err
	}
	_, err = client.RunNoWait(ctx, miniAppWorkflowTaskName, map[string]string{"run_id": runID})
	return err
}

// SubmitWorkflowRun is the Hatchet worker's only backend contract. The worker
// has no database credential; it presents a narrow service key and the backend
// remains the sole owner of workflow state and Registry credentials.
func SubmitWorkflowRun(ctx context.Context, client *http.Client, endpoint, key, runID string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/"+runID+"/execute", bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("workflow backend returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Status == "" {
		return "", errors.New("workflow backend returned an invalid response")
	}
	return result.Status, nil
}

func SubmitWorkflowTrigger(ctx context.Context, client *http.Client, endpoint, key, trigger string) (int, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/"+trigger, bytes.NewReader(nil))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return 0, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("workflow trigger backend returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Queued int `json:"queued"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, errors.New("workflow trigger backend returned an invalid response")
	}
	return result.Queued, nil
}

// ExecuteWorkflowRun is intentionally mounted outside user authentication. It
// accepts only the Hatchet worker key and derives all identity from the durable
// run envelope, never from request headers.
func (h *Handler) ExecuteWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if h == nil || !h.enabled {
		http.NotFound(w, r)
		return
	}
	if h.workerIngestKey == "" || !constantTimeBearer(r, h.workerIngestKey) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="cerebro-app-workflows"`)
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow run id")
		return
	}
	var status string
	var definition, envelope, triggerPayload json.RawMessage
	err = h.pool.QueryRow(r.Context(), `
		SELECT r.status,d.definition,r.identity_envelope,r.trigger_payload
		FROM cerebro_app_workflow_run r
		JOIN cerebro_app_workflow_def d ON d.id=r.workflow_id
		WHERE r.id=$1`, runID).Scan(&status, &definition, &envelope, &triggerPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow run")
		return
	}
	if status != "queued" {
		writeJSON(w, http.StatusOK, map[string]any{"id": runID, "status": status})
		return
	}
	identity, err := tokens.ParseWorkflowIdentityEnvelope(envelope)
	if err != nil {
		writeError(w, http.StatusConflict, "workflow identity envelope is invalid")
		return
	}
	var trigger any
	if err := json.Unmarshal(triggerPayload, &trigger); err != nil {
		writeError(w, http.StatusConflict, "workflow trigger payload is invalid")
		return
	}
	startedAt := time.Now()
	result, err := h.pool.Exec(r.Context(), `UPDATE cerebro_app_workflow_run SET status='running',started_at=$2 WHERE id=$1 AND status='queued'`, runID, startedAt)
	if err != nil || result.RowsAffected() != 1 {
		writeError(w, http.StatusConflict, "workflow run was already claimed")
		return
	}
	runResult, runErr := h.runWorkflow(r.Context(), runID, definition, trigger, identity)
	stepLog, _ := json.Marshal(runResult.Steps)
	finishedStatus, publicError := runResult.Status, ""
	if runErr != nil {
		finishedStatus, publicError = "failed", "workflow execution failed"
		slog.Error("mini-app workflow failed", "run_id", runID, "error", runErr)
	}
	_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_workflow_run SET status=$2,step_log=$3,error=$4,finished_at=now() WHERE id=$1`, runID, finishedStatus, stepLog, publicError)
	writeJSON(w, http.StatusOK, map[string]any{"id": runID, "status": finishedStatus})
}

func constantTimeBearer(r *http.Request, expected string) bool {
	const prefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	if len(authorization) <= len(prefix) || !strings.EqualFold(authorization[:len(prefix)], prefix) {
		return false
	}
	provided := strings.TrimSpace(authorization[len(prefix):])
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
