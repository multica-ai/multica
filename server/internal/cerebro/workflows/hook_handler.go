package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type HookAuthorizer interface {
	Can(context.Context, string, HookPermissionActor, HookPermission) bool
}

type HookAPI struct {
	repository HookRepository
	authorizer HookAuthorizer
}

func NewHookAPI(repository HookRepository, authorizer HookAuthorizer) *HookAPI {
	return &HookAPI{repository: repository, authorizer: authorizer}
}

// Routes returns the complete workflow-hook HTTP surface. Keeping route
// ownership here means the shared router only mounts one workflow feature.
func (h *HookAPI) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Post("/{id}/test", h.Test)
	r.Post("/{id}/publish", h.Publish)
	r.Get("/{id}/effective", h.Effective)
	r.Get("/{id}/runs", h.Runs)
	return r
}

func (h *HookAPI) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	_ = actor
	policies, err := h.repository.List(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow hooks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hooks": policies})
}

func (h *HookAPI) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	policy, err := h.repository.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *HookAPI) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionWrite)
	if !ok {
		return
	}
	var policy HookPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow hook payload")
		return
	}
	created, err := h.repository.Create(r.Context(), workspaceID, actor, policy)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *HookAPI) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionWrite)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	current, err := h.repository.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	if current.Mode == HookModeManaged && !h.authorizer.Can(r.Context(), workspaceID, actor, HookPermissionManageManaged) {
		writeError(w, http.StatusForbidden, "managed workflow hooks are owner-only")
		return
	}
	var policy HookPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow hook payload")
		return
	}
	updated, err := h.repository.Update(r.Context(), workspaceID, actor, id, policy)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *HookAPI) Publish(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionEnforce)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	published, err := h.repository.Publish(r.Context(), workspaceID, id, actor.ID)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, published)
}

func (h *HookAPI) Test(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionWrite)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	policy, err := h.repository.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	var event HookEvent
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&event)
	}
	if event.EventID == "" {
		event.EventID = "test-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	if event.Type == "" && len(policy.Events) > 0 {
		event.Type = policy.Events[0]
	}
	event.WorkspaceID = workspaceID
	seedHookTestBinding(&event, policy.Bindings)
	testPolicy := policy
	testPolicy.Mode = HookModeDryRun
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{testPolicy})).Evaluate(r.Context(), event)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	run := HookRunRecord{PolicyID: policy.ID, PolicyVersion: policy.Version, Event: event, Result: result, CreatedAt: time.Now().UTC()}
	if len(result.Matches) > 0 {
		run.SourceScope = result.Matches[0].SourceScope
	}
	if err := h.repository.RecordRun(r.Context(), workspaceID, run); err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	baselineAt, err := h.repository.RefreshBaseline(r.Context(), workspaceID, policy.ID)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"side_effects": false, "result": result, "baseline_at": baselineAt})
}

func seedHookTestBinding(event *HookEvent, bindings []HookBinding) {
	if event == nil || len(bindings) == 0 {
		return
	}
	binding := bindings[0]
	switch binding.Kind {
	case HookScopeWorkspace:
		event.WorkspaceID = binding.ID
	case HookScopeProject:
		event.ProjectID = binding.ID
	case HookScopeWorkflow:
		event.WorkflowID = binding.ID
	case HookScopeAgent:
		event.AgentID = binding.ID
	case HookScopeModel:
		event.Model = binding.ID
	case HookScopeIssue:
		event.IssueID = binding.ID
	case HookScopeSession:
		event.SessionID = binding.ID
	}
}

func (h *HookAPI) Effective(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	policy, err := h.repository.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": policy, "bindings": policy.Bindings})
}

func (h *HookAPI) Runs(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	runs, err := h.repository.Runs(r.Context(), workspaceID, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *HookAPI) authorize(w http.ResponseWriter, r *http.Request, permission HookPermission) (string, HookPermissionActor, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return "", HookPermissionActor{}, false
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	if _, err := util.ParseUUID(workspaceID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return "", HookPermissionActor{}, false
	}
	actor := hookPermissionActor(r)
	if h.authorizer == nil || !h.authorizer.Can(r.Context(), workspaceID, actor, permission) {
		writeError(w, http.StatusForbidden, "workflow hook permission denied")
		return "", HookPermissionActor{}, false
	}
	return workspaceID, actor, true
}

func hookPermissionActor(r *http.Request) HookPermissionActor {
	actor := HookPermissionActor{Type: actorType(r), ID: actorID(r, r.Header.Get("X-User-ID")), OwnerUserID: r.Header.Get("X-User-ID")}
	if member, ok := middleware.MemberFromContext(r.Context()); ok {
		actor.IsOwner = member.Role == "owner"
	}
	return actor
}

func hookIDOr400(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "id")
	if _, err := util.ParseUUID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow hook id")
		return "", false
	}
	return id, true
}

func (h *HookAPI) writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrHookPolicyNotFound):
		writeError(w, http.StatusNotFound, "workflow hook not found")
	case errors.Is(err, ErrHookPublishPrerequisite):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrManagedHookLocked):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "workflow hook operation failed")
	}
}
