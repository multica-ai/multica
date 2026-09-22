package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/workflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) workflowContext(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return "", "", false
	}
	return workspaceID, userID, true
}

func writeWorkflowError(w http.ResponseWriter, err error) {
	var typed *workflow.Error
	if errors.As(err, &typed) {
		if len(typed.Details) > 0 {
			writeJSON(w, typed.Status, map[string]any{
				"error":   typed.Message,
				"code":    typed.Code,
				"details": typed.Details,
			})
			return
		}
		if typed.Code != "" {
			writeErrorCode(w, typed.Status, typed.Code, typed.Message)
			return
		}
		writeError(w, typed.Status, typed.Message)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "workflow operation failed")
}

type workflowCreateRequest struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	TemplateID     string          `json:"template_id"`
	Graph          *workflow.Graph `json:"graph"`
	IdempotencyKey string          `json:"idempotency_key"`
}

func (h *Handler) ListWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.workflowContext(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": workflow.ListTemplates()})
}

func (h *Handler) GetWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.workflowContext(w, r); !ok {
		return
	}
	template, err := workflow.GetTemplate(chi.URLParam(r, "templateId"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, template)
}

func (h *Handler) ListWorkflowWorkItems(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	items, err := h.Workflow.ListWorkItems(r.Context(), ws, user, r.URL.Query().Get("status"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if items == nil {
		items = []workflow.WorkItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"work_items": items})
}

func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	items, err := h.Workflow.List(r.Context(), ws)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if items == nil {
		items = []workflow.Workflow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": items})
}

func (h *Handler) WorkflowCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.workflowContext(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"graph_schema_versions": []int{1, workflow.CurrentGraphSchemaVersion},
		"engine_versions":       []int{2},
		"node_types":            []string{"start", "agent", "human_task", "human_review", "condition", "parallel", "merge", "end"},
		"limits":                map[string]int{"max_nodes": workflow.MaxWorkflowNodes, "max_edges": workflow.MaxWorkflowEdges, "max_nesting": 3},
	})
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow request")
		return
	}
	graph := req.Graph
	if graph == nil && req.TemplateID != "" {
		template, templateErr := workflow.GetTemplate(req.TemplateID)
		if templateErr != nil {
			writeWorkflowError(w, templateErr)
			return
		}
		graph = &template.Graph
	}
	item, err := h.Workflow.CreateKey(r.Context(), ws, user, req.Name, req.Description, graph, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	item, err := h.Workflow.Get(r.Context(), ws, chi.URLParam(r, "id"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowEditRequest struct {
	Name             *string         `json:"name"`
	Description      *string         `json:"description"`
	Graph            *workflow.Graph `json:"graph"`
	ExpectedRevision int64           `json:"expected_revision"`
	IdempotencyKey   string          `json:"idempotency_key"`
}

func (h *Handler) EditWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow edit")
		return
	}
	item, err := h.Workflow.EditKey(r.Context(), ws, user, chi.URLParam(r, "id"), workflow.Edit{Name: req.Name, Description: req.Description, Graph: req.Graph, ExpectedRevision: req.ExpectedRevision}, "", req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowHistoryRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func (h *Handler) HistoryWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	direction := chi.URLParam(r, "direction")
	if direction != "undo" && direction != "redo" {
		writeError(w, http.StatusNotFound, "workflow history action not found")
		return
	}
	var req workflowHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow history request")
		return
	}
	item, err := h.Workflow.EditKey(r.Context(), ws, user, chi.URLParam(r, "id"), workflow.Edit{ExpectedRevision: req.ExpectedRevision}, direction, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowRunActionRequest
	if err := decodeWorkflowActionRequest(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow deletion request")
		return
	}
	if err := h.Workflow.DeleteKey(r.Context(), ws, user, chi.URLParam(r, "id"), req.IdempotencyKey); err != nil {
		writeWorkflowError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type workflowValidateRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Target           string `json:"target"`
}

func (h *Handler) ValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow validation request")
		return
	}
	result, err := h.Workflow.ValidateGraph(r.Context(), ws, user, chi.URLParam(r, "id"), req.ExpectedRevision)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) PreviewWorkflowUpgrade(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	result, err := h.Workflow.PreviewUpgrade(r.Context(), ws, chi.URLParam(r, "id"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type workflowUpgradeRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func (h *Handler) UpgradeWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowUpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow upgrade request")
		return
	}
	item, err := h.Workflow.UpgradeDraftKey(r.Context(), ws, user, chi.URLParam(r, "id"), req.ExpectedRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowPublishRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Notes            string `json:"notes"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func (h *Handler) PublishWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow publish request")
		return
	}
	release, err := h.Workflow.Publish(r.Context(), ws, user, chi.URLParam(r, "id"), req.ExpectedRevision, req.Notes, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, release)
}

func (h *Handler) ListWorkflowReleases(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	releases, err := h.Workflow.ListReleases(r.Context(), ws, chi.URLParam(r, "id"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if releases == nil {
		releases = []workflow.Release{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

func (h *Handler) GetWorkflowRelease(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	release, graph, plan, err := h.Workflow.GetRelease(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "releaseId"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"release": release, "graph": graph, "compiled_plan": plan})
}

type workflowReleaseActionRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func (h *Handler) ActivateWorkflowRelease(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowReleaseActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid release activation request")
		return
	}
	item, err := h.Workflow.ActivateReleaseKey(r.Context(), ws, user, chi.URLParam(r, "id"), chi.URLParam(r, "releaseId"), req.ExpectedRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) CopyWorkflowReleaseToDraft(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowReleaseActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid release copy request")
		return
	}
	item, err := h.Workflow.CopyReleaseToDraftKey(r.Context(), ws, user, chi.URLParam(r, "id"), chi.URLParam(r, "releaseId"), req.ExpectedRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowArchiveRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Archived         bool   `json:"archived"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func (h *Handler) ArchiveWorkflow(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow archive request")
		return
	}
	item, err := h.Workflow.ArchiveKey(r.Context(), ws, user, chi.URLParam(r, "id"), req.ExpectedRevision, req.Archived, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	runs, err := h.Workflow.ListRuns(r.Context(), ws, chi.URLParam(r, "id"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if runs == nil {
		runs = []workflow.Run{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *Handler) GetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	run, err := h.Workflow.GetRun(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) GetWorkflowRunNode(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	detail, err := h.Workflow.GetRunNode(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId"), chi.URLParam(r, "nodeId"))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) ListWorkflowRunEvents(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	afterSequence := int64(0)
	if raw := r.URL.Query().Get("after_sequence"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "after_sequence must be a non-negative integer")
			return
		}
		afterSequence = parsed
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	events, next, err := h.Workflow.ListRunEvents(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId"), afterSequence, limit)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if events == nil {
		events = []workflow.WorkflowEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "next_after_sequence": next})
}

func (h *Handler) GetWorkflowWorkItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	wid, runID, itemID := chi.URLParam(r, "id"), chi.URLParam(r, "runId"), chi.URLParam(r, "itemId")
	if _, err := h.Workflow.GetRun(r.Context(), ws, wid, runID); err != nil {
		writeWorkflowError(w, err)
		return
	}
	item, err := h.Workflow.GetWorkItem(r.Context(), ws, runID, itemID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowWorkItemSubmissionRequest struct {
	ExpectedStateRevision int64          `json:"expected_state_revision"`
	ExpectedItemVersion   int64          `json:"expected_item_version"`
	Action                string         `json:"action"`
	IdempotencyKey        string         `json:"idempotency_key"`
	Values                map[string]any `json:"values"`
	Feedback              string         `json:"feedback"`
}

func (h *Handler) SubmitWorkflowWorkItem(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	wid, runID, itemID := chi.URLParam(r, "id"), chi.URLParam(r, "runId"), chi.URLParam(r, "itemId")
	if _, err := h.Workflow.GetRun(r.Context(), ws, wid, runID); err != nil {
		writeWorkflowError(w, err)
		return
	}
	var req workflowWorkItemSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid work item submission")
		return
	}
	run, err := h.Workflow.SubmitWorkItem(r.Context(), ws, user, runID, itemID, workflow.WorkItemSubmission{
		ExpectedStateRevision: req.ExpectedStateRevision,
		ExpectedItemVersion:   req.ExpectedItemVersion,
		Action:                req.Action,
		IdempotencyKey:        req.IdempotencyKey,
		Values:                req.Values,
		Feedback:              req.Feedback,
	})
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type workflowWorkItemTransferRequest struct {
	ExpectedStateRevision int64  `json:"expected_state_revision"`
	ExpectedItemVersion   int64  `json:"expected_item_version"`
	MemberID              string `json:"member_id"`
	Reason                string `json:"reason"`
	IdempotencyKey        string `json:"idempotency_key"`
}

func (h *Handler) TransferWorkflowWorkItem(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowWorkItemTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid work item transfer request")
		return
	}
	if _, err := h.Workflow.GetRun(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId")); err != nil {
		writeWorkflowError(w, err)
		return
	}
	item, err := h.Workflow.TransferWorkItemKey(r.Context(), ws, user, chi.URLParam(r, "runId"), chi.URLParam(r, "itemId"), req.MemberID, req.Reason, req.ExpectedStateRevision, req.ExpectedItemVersion, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type workflowWorkItemExtendRequest struct {
	ExpectedStateRevision int64  `json:"expected_state_revision"`
	ExpectedItemVersion   int64  `json:"expected_item_version"`
	DueAt                 string `json:"due_at"`
	Reason                string `json:"reason"`
	IdempotencyKey        string `json:"idempotency_key"`
}

func (h *Handler) ExtendWorkflowWorkItem(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowWorkItemExtendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid work item extension request")
		return
	}
	dueAt, err := time.Parse(time.RFC3339, req.DueAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "due_at must be an RFC3339 timestamp")
		return
	}
	if _, err := h.Workflow.GetRun(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId")); err != nil {
		writeWorkflowError(w, err)
		return
	}
	item, err := h.Workflow.ExtendWorkItemKey(r.Context(), ws, user, chi.URLParam(r, "runId"), chi.URLParam(r, "itemId"), dueAt, req.Reason, req.ExpectedStateRevision, req.ExpectedItemVersion, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type startWorkflowRunRequest struct {
	Input            string         `json:"input"`
	InputValues      map[string]any `json:"input_values"`
	ExpectedRevision int64          `json:"expected_revision"`
	IdempotencyKey   string         `json:"idempotency_key"`
	ReleaseID        string         `json:"release_id"`
}

func (h *Handler) StartWorkflowRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req startWorkflowRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow run request")
		return
	}
	if strings.TrimSpace(req.Input) == "" && req.InputValues != nil {
		encoded, encodeErr := json.Marshal(req.InputValues)
		if encodeErr != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow input values")
			return
		}
		req.Input = string(encoded)
	}
	var run workflow.Run
	var err error
	if req.ReleaseID != "" {
		run, err = h.Workflow.StartRunFromRelease(r.Context(), ws, user, chi.URLParam(r, "id"), req.ReleaseID, req.Input, req.IdempotencyKey)
	} else {
		run, err = h.Workflow.StartRun(r.Context(), ws, user, chi.URLParam(r, "id"), req.Input, req.IdempotencyKey, req.ExpectedRevision)
	}
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

type workflowTestRunRequest struct {
	Mode               string                                `json:"mode"`
	InputValues        map[string]any                        `json:"input_values"`
	DraftRevision      int64                                 `json:"draft_revision"`
	IdempotencyKey     string                                `json:"idempotency_key"`
	SimulationFixtures map[string]workflow.SimulationFixture `json:"simulation_fixtures"`
}

func (h *Handler) StartWorkflowTestRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowTestRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow test run request")
		return
	}
	run, err := h.Workflow.StartTestRun(r.Context(), ws, user, chi.URLParam(r, "id"), workflow.TestRunRequest{
		Mode: req.Mode, Revision: req.DraftRevision, Input: req.InputValues, Key: req.IdempotencyKey, Fixtures: req.SimulationFixtures,
	})
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (h *Handler) CancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowRunActionRequest
	if err := decodeWorkflowActionRequest(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid run cancellation request")
		return
	}
	run, err := h.Workflow.CancelWithRevisionKey(r.Context(), ws, user, chi.URLParam(r, "id"), chi.URLParam(r, "runId"), req.ExpectedStateRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type workflowRunOwnerRequest struct {
	ExpectedStateRevision int64  `json:"expected_state_revision"`
	OwnerID               string `json:"owner_id"`
	Reason                string `json:"reason"`
	IdempotencyKey        string `json:"idempotency_key"`
}

func (h *Handler) UpdateWorkflowRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowRunOwnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid run update request")
		return
	}
	if _, err := h.Workflow.GetRun(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId")); err != nil {
		writeWorkflowError(w, err)
		return
	}
	run, err := h.Workflow.UpdateRunOwnerKey(r.Context(), ws, user, chi.URLParam(r, "runId"), req.OwnerID, req.Reason, req.ExpectedStateRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) TerminateWorkflowRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowRunActionRequest
	if err := decodeWorkflowActionRequest(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid run termination request")
		return
	}
	run, err := h.Workflow.TerminateWithRevisionKey(r.Context(), ws, user, chi.URLParam(r, "id"), chi.URLParam(r, "runId"), req.Reason, req.ExpectedStateRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) RetryWorkflowNode(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowRunActionRequest
	if err := decodeWorkflowActionRequest(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid node retry request")
		return
	}
	run, err := h.Workflow.RetryWithRevisionKey(r.Context(), ws, user, chi.URLParam(r, "id"), chi.URLParam(r, "runId"), chi.URLParam(r, "nodeId"), req.ExpectedStateRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type workflowTakeoverRequest struct {
	ExpectedStateRevision int64  `json:"expected_state_revision"`
	MemberID              string `json:"member_id"`
	Reason                string `json:"reason"`
	IdempotencyKey        string `json:"idempotency_key"`
}

func (h *Handler) TakeoverWorkflowNode(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowTakeoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid node takeover request")
		return
	}
	if _, err := h.Workflow.GetRun(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId")); err != nil {
		writeWorkflowError(w, err)
		return
	}
	run, err := h.Workflow.TakeoverNodeKey(r.Context(), ws, user, chi.URLParam(r, "runId"), chi.URLParam(r, "nodeId"), req.MemberID, req.Reason, req.ExpectedStateRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type workflowResolveRequest struct {
	ExpectedStateRevision int64          `json:"expected_state_revision"`
	Resolution            string         `json:"resolution"`
	Values                map[string]any `json:"values"`
	Evidence              string         `json:"evidence"`
	IdempotencyKey        string         `json:"idempotency_key"`
}

func (h *Handler) ResolveWorkflowNode(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	var req workflowResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid node resolution request")
		return
	}
	if _, err := h.Workflow.GetRun(r.Context(), ws, chi.URLParam(r, "id"), chi.URLParam(r, "runId")); err != nil {
		writeWorkflowError(w, err)
		return
	}
	run, err := h.Workflow.ResolveNodeKey(r.Context(), ws, user, chi.URLParam(r, "runId"), chi.URLParam(r, "nodeId"), workflow.NodeResolution{Resolution: req.Resolution, Values: req.Values, Evidence: req.Evidence}, req.ExpectedStateRevision, req.IdempotencyKey)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type workflowRunActionRequest struct {
	ExpectedStateRevision int64  `json:"expected_state_revision"`
	Reason                string `json:"reason"`
	IdempotencyKey        string `json:"idempotency_key"`
}

func decodeWorkflowActionRequest(r *http.Request, request *workflowRunActionRequest) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(request)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

type workflowChatSessionRequest struct {
	AgentID string `json:"agent_id"`
}

func (h *Handler) WorkflowChatSession(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	wid := chi.URLParam(r, "id")
	wfID, ok := parseUUIDOrBadRequest(w, wid, "workflow id")
	if !ok {
		return
	}
	if _, err := h.Workflow.Get(r.Context(), ws, wid); err != nil {
		writeWorkflowError(w, err)
		return
	}
	userID, ok := parseUUIDOrBadRequest(w, user, "user id")
	if !ok {
		return
	}
	var req workflowChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow chat request")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	wsID, ok := parseUUIDOrBadRequest(w, ws, "workspace id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: wsID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}
	if !h.canInvokeAgent(r.Context(), agent, "member", user, user, ws) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	var sessionID string
	err = h.DB.QueryRow(r.Context(), `SELECT session_id::text FROM workflow_chat WHERE workflow_id=$1 AND user_id=$2 AND agent_id=$3`, wfID, userID, agentID).Scan(&sessionID)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{"session_id": sessionID})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load workflow chat")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start workflow chat")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	session, err := qtx.CreateChatSession(r.Context(), db.CreateChatSessionParams{ID: dbid.NewV7(), WorkspaceID: wsID, AgentID: agentID, CreatorID: userID, Title: "Workflow builder"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow chat")
		return
	}
	if _, err = qtx.MarkChatSessionExplicitlyCreated(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow chat")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO workflow_chat(workflow_id,user_id,agent_id,session_id) VALUES($1,$2,$3,$4) ON CONFLICT(workflow_id,user_id,agent_id) DO UPDATE SET session_id=EXCLUDED.session_id`, wfID, userID, agentID, session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link workflow chat")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow chat")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"session_id": util.UUIDToString(session.ID)})
}

type workflowChatMessageRequest struct {
	SessionID        string `json:"session_id"`
	Content          string `json:"content"`
	SelectedNodeID   string `json:"selected_node_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func workflowChatRequestHash(workflowID, workspaceID, userID string, req workflowChatMessageRequest) string {
	payload, _ := json.Marshal(struct {
		WorkflowID       string `json:"workflow_id"`
		WorkspaceID      string `json:"workspace_id"`
		UserID           string `json:"user_id"`
		SessionID        string `json:"session_id"`
		Content          string `json:"content"`
		SelectedNodeID   string `json:"selected_node_id"`
		ExpectedRevision int64  `json:"expected_revision"`
	}{workflowID, workspaceID, userID, req.SessionID, req.Content, req.SelectedNodeID, req.ExpectedRevision})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (h *Handler) SendWorkflowChatMessage(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.workflowContext(w, r)
	if !ok {
		return
	}
	wid := chi.URLParam(r, "id")
	wfID, ok := parseUUIDOrBadRequest(w, wid, "workflow id")
	if !ok {
		return
	}
	userID, ok := parseUUIDOrBadRequest(w, user, "user id")
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, ws, "workspace id")
	if !ok {
		return
	}
	var req workflowChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 200 {
		writeError(w, http.StatusBadRequest, "idempotency_key is required")
		return
	}
	if req.ExpectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision is required")
		return
	}
	session, ok := h.gatePublicChatSessionForUser(w, r, user, ws, req.SessionID)
	if !ok {
		return
	}
	var linkedAgentID pgtype.UUID
	err := h.DB.QueryRow(r.Context(), `SELECT agent_id FROM workflow_chat WHERE workflow_id=$1 AND user_id=$2 AND session_id=$3 AND agent_id=$4`, wfID, userID, session.ID, session.AgentID).Scan(&linkedAgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow chat session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify workflow chat session")
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !h.canInvokeAgent(r.Context(), agent, "member", user, user, ws) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	wf, err := h.Workflow.Get(r.Context(), ws, wid)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	available, err := h.Queries.ListAgents(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow agents")
		return
	}
	callableAgents := make([]map[string]string, 0, len(available))
	for _, candidate := range available {
		if h.canInvokeAgent(r.Context(), candidate, "member", user, user, ws) {
			callableAgents = append(callableAgents, map[string]string{"id": util.UUIDToString(candidate.ID), "name": candidate.Name})
		}
	}
	contextBlock, _ := json.Marshal(map[string]any{
		"workflow_id":      wid,
		"revision":         wf.Revision,
		"graph":            wf.Graph,
		"selected_node_id": req.SelectedNodeID,
		"available_agents": callableAgents,
	})
	prompt := "You are the Multica workflow builder. Help edit the workflow only. Preserve fields the user did not ask to change. Use only agent ids from available_agents. When the user confirms a change, append exactly one <workflow_edit> JSON block containing {\\\"expected_revision\\\":number,\\\"name\\\":string?,\\\"description\\\":string?,\\\"graph\\\":{\\\"nodes\\\":[{\\\"id\\\":string,\\\"type\\\":\\\"start\\\"|\\\"agent\\\"|\\\"end\\\",\\\"label\\\":string,\\\"position\\\":{\\\"x\\\":number,\\\"y\\\":number},\\\"agent_id\\\":string?,\\\"instructions\\\":string?,\\\"input_refs\\\":string[]}],\\\"edges\\\":[{\\\"id\\\":string,\\\"source\\\":string,\\\"target\\\":string}]}}. Never claim a change was applied unless you include the block. Current workflow context: " + string(contextBlock) + "\nUser request:\n" + req.Content
	prompt += " The graph supports v2 node types human_task, human_review, condition, parallel, and merge, plus flow, rework, and compensation edges; preserve unknown fields and never invent member or agent IDs."
	sent, err := h.TaskService.SendDirectChatMessageWithIdempotency(r.Context(), session, agent, userID, prompt, nil, "member", userID, service.DirectChatIdempotency{
		WorkflowID: wfID, WorkspaceID: workspaceID, UserID: userID, Key: req.IdempotencyKey,
		RequestHash: workflowChatRequestHash(wid, ws, user, req), BaseRevision: req.ExpectedRevision,
	})
	if err != nil {
		if errors.Is(err, service.ErrDirectChatIdempotencyConflict) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, service.ErrDirectChatRevisionConflict) {
			writeError(w, http.StatusConflict, "save the latest workflow before chatting")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to send workflow chat")
		return
	}
	if sent.Replay != nil {
		writeJSON(w, http.StatusCreated, SendChatMessageResponse{MessageID: sent.Replay.MessageID, TaskID: sent.Replay.TaskID, SupportsQueue: true, Queued: sent.Replay.Queued, CreatedAt: sent.Replay.CreatedAt})
		return
	}
	h.publishChat(protocol.EventChatMessage, ws, "member", user, util.UUIDToString(session.ID), protocol.ChatMessagePayload{ChatSessionID: util.UUIDToString(session.ID), MessageID: util.UUIDToString(sent.Message.ID), Role: "user", Content: req.Content, TaskID: util.UUIDToString(sent.Task.ID), CreatedAt: timestampToString(sent.Message.CreatedAt)})
	writeJSON(w, http.StatusCreated, SendChatMessageResponse{MessageID: util.UUIDToString(sent.Message.ID), TaskID: util.UUIDToString(sent.Task.ID), SupportsQueue: true, Queued: sent.Queued, CreatedAt: timestampToString(sent.Message.CreatedAt)})
}

// Keep the workflow package's UUID boundary local to this handler. Invalid
// path IDs are rejected by the service before a query can mutate state.
