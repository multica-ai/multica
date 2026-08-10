package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type HookAuthorizer interface {
	Can(context.Context, string, HookPermissionActor, HookPermission) bool
}

type HookAPI struct {
	repository                HookRepository
	authorizer                HookAuthorizer
	activeRules               *ActiveRuleService
	activeRuleContextResolver ActiveRuleContextResolver
}

func (h *HookAPI) WithActiveRuleContextResolver(resolver ActiveRuleContextResolver) *HookAPI {
	h.activeRuleContextResolver = resolver
	return h
}

func NewHookAPI(repository HookRepository, authorizer HookAuthorizer) *HookAPI {
	return &HookAPI{repository: repository, authorizer: authorizer, activeRules: NewActiveRuleService(repository)}
}

func (h *HookAPI) WithActiveRuleService(service *ActiveRuleService) *HookAPI {
	h.activeRules = service
	return h
}

// Routes returns the complete workflow-hook HTTP surface. Keeping route
// ownership here means the shared router only mounts one workflow feature.
func (h *HookAPI) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/active-rules", h.ListActiveRules)
	r.Get("/runs", h.RecentRuns)
	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Delete("/{id}/draft", h.DiscardDraft)
	r.Post("/{id}/disable", h.Disable)
	r.Post("/{id}/test", h.Test)
	r.Get("/{id}/events", h.Events)
	r.Post("/{id}/publish", h.Publish)
	r.Get("/{id}/effective", h.Effective)
	r.Get("/{id}/runs", h.Runs)
	return r
}

func (h *HookAPI) ListActiveRules(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	issueID := strings.TrimSpace(r.URL.Query().Get("issue_id"))
	if agentID == "" || issueID == "" {
		writeError(w, http.StatusBadRequest, "agent_id and issue_id are required")
		return
	}
	scope := ActiveRuleContext{WorkspaceID: workspaceID, AgentID: agentID, IssueID: issueID}
	if h.activeRuleContextResolver != nil {
		var err error
		scope, err = h.activeRuleContextResolver.Resolve(r.Context(), workspaceID, agentID, issueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "agent_id and issue_id must identify records in this workspace")
			return
		}
	}
	rules, err := h.activeRules.List(r.Context(), scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list active hook rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (h *HookAPI) Events(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	events, err := h.repository.CompatibleEvents(r.Context(), workspaceID, id, limit)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *HookAPI) DiscardDraft(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionWrite)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	policy, err := h.repository.DiscardDraft(r.Context(), workspaceID, actor, id)
	if err != nil {
		hookLifecycleAudit(hookAuditDiscardDraft, workspaceID, id, actor, nil, err)
		h.writeRepositoryError(w, err)
		return
	}
	hookLifecycleAudit(hookAuditDiscardDraft, workspaceID, id, actor, &policy, nil)
	writeJSON(w, http.StatusOK, policy)
}

func (h *HookAPI) Disable(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionEnforce)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	policy, err := h.repository.Disable(r.Context(), workspaceID, actor, id)
	if err != nil {
		hookLifecycleAudit(hookAuditDisable, workspaceID, id, actor, nil, err)
		h.writeRepositoryError(w, err)
		return
	}
	hookLifecycleAudit(hookAuditDisable, workspaceID, id, actor, &policy, nil)
	writeJSON(w, http.StatusOK, policy)
}

func (h *HookAPI) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID, actor, ok := h.authorize(w, r, HookPermissionWrite)
	if !ok {
		return
	}
	id, ok := hookIDOr400(w, r)
	if !ok {
		return
	}
	if err := h.repository.Delete(r.Context(), workspaceID, actor, id); err != nil {
		hookLifecycleAudit(hookAuditDelete, workspaceID, id, actor, nil, err)
		h.writeRepositoryError(w, err)
		return
	}
	hookLifecycleAudit(hookAuditDelete, workspaceID, id, actor, nil, nil)
	w.WriteHeader(http.StatusNoContent)
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
	policy.CompatibleEvents, err = h.repository.CompatibleEvents(r.Context(), workspaceID, id, 50)
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
	canonicalizeWorkspaceBindings(&policy, workspaceID)
	if err := validateHookDraft(policy, workspaceID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.repository.Create(r.Context(), workspaceID, actor, policy)
	if err != nil {
		hookLifecycleAudit(hookAuditCreate, workspaceID, "", actor, nil, err)
		h.writeRepositoryError(w, err)
		return
	}
	hookLifecycleAudit(hookAuditCreate, workspaceID, created.ID, actor, &created, nil)
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
	canonicalizeWorkspaceBindings(&policy, workspaceID)
	if err := validateHookDraft(policy, workspaceID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.repository.Update(r.Context(), workspaceID, actor, id, policy)
	if err != nil {
		hookLifecycleAudit(hookAuditUpdate, workspaceID, id, actor, nil, err)
		h.writeRepositoryError(w, err)
		return
	}
	hookLifecycleAudit(hookAuditUpdate, workspaceID, id, actor, &updated, nil)
	writeJSON(w, http.StatusOK, updated)
}

func validateHookDraft(policy HookPolicy, workspaceID string) error {
	if policy.ConditionMode != HookConditionAll && policy.ConditionMode != HookConditionAny {
		return fmt.Errorf("condition_mode must be %q or %q", HookConditionAll, HookConditionAny)
	}
	if policy.FailMode != HookFailWarn && policy.FailMode != HookFailClosed && policy.FailMode != HookFailMode("open") {
		return fmt.Errorf("unknown workflow hook fail_mode %q", policy.FailMode)
	}
	for _, eventType := range policy.Events {
		if !isSupportedHookEventType(eventType) {
			return fmt.Errorf("unknown workflow hook trigger %q", eventType)
		}
	}
	bindings := make(map[string]struct{}, len(policy.Bindings))
	for _, binding := range policy.Bindings {
		switch binding.Kind {
		case HookScopeWorkspace, HookScopeProject, HookScopeWorkflow, HookScopeAgent, HookScopeModel, HookScopeIssue, HookScopeSession:
		default:
			return fmt.Errorf("unknown workflow hook binding kind %q", binding.Kind)
		}
		if binding.Kind == HookScopeWorkspace && binding.ID != workspaceID {
			return fmt.Errorf("workspace binding must match the authenticated workspace")
		}
		key := string(binding.Kind) + "\x00" + binding.ID
		if _, exists := bindings[key]; exists {
			return fmt.Errorf("duplicate workflow hook binding for %q", binding.Kind)
		}
		bindings[key] = struct{}{}
	}
	for _, condition := range policy.Conditions {
		if !isHookConditionOperator(condition.Op) {
			return fmt.Errorf("unknown workflow hook condition operator %q", condition.Op)
		}
	}
	for _, handler := range policy.Handlers {
		switch handler.Decision {
		case HookAllow, HookBlock, HookModify, HookRequire:
		default:
			return fmt.Errorf("unknown workflow hook decision %q", handler.Decision)
		}
		for _, action := range handler.Actions {
			if !isVersionOneHookActionType(action.Type) {
				return fmt.Errorf("unknown workflow hook action %q", action.Type)
			}
		}
	}
	return nil
}

func canonicalizeWorkspaceBindings(policy *HookPolicy, workspaceID string) {
	for index := range policy.Bindings {
		if policy.Bindings[index].Kind == HookScopeWorkspace && strings.TrimSpace(policy.Bindings[index].ID) == "" {
			policy.Bindings[index].ID = workspaceID
		}
	}
}

func validateHookForExecution(policy *HookPolicy, workspaceID string) error {
	if policy.ConditionMode == "" {
		policy.ConditionMode = HookConditionAll
	}
	canonicalizeWorkspaceBindings(policy, workspaceID)
	if err := validateHookDraft(*policy, workspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(policy.Name) == "" {
		return fmt.Errorf("workflow hook name is required")
	}
	if strings.TrimSpace(policy.ContractRule) == "" {
		return fmt.Errorf("workflow hook contract_rule is required")
	}
	if strings.TrimSpace(policy.ContractSatisfy) == "" {
		return fmt.Errorf("workflow hook contract_satisfy is required")
	}
	if len(policy.Events) == 0 {
		return fmt.Errorf("at least one workflow hook trigger is required")
	}
	if len(policy.Bindings) == 0 {
		return fmt.Errorf("at least one workflow hook scope is required")
	}
	for _, binding := range policy.Bindings {
		if strings.TrimSpace(binding.ID) == "" {
			return fmt.Errorf("workflow hook %s scope target is required", binding.Kind)
		}
	}
	for _, condition := range policy.Conditions {
		if strings.TrimSpace(condition.Field) == "" {
			return fmt.Errorf("workflow hook condition field is required")
		}
		switch condition.Op {
		case "exists", "not_exists", "is_null", "is_not_null":
		case "in", "not_in":
			if len(condition.Values) == 0 {
				return fmt.Errorf("workflow hook condition values are required for %q", condition.Op)
			}
		default:
			if condition.Value == nil || strings.TrimSpace(fmt.Sprint(condition.Value)) == "" {
				return fmt.Errorf("workflow hook condition value is required for %q", condition.Op)
			}
		}
	}
	if len(policy.Handlers) == 0 {
		return fmt.Errorf("at least one workflow hook handler is required")
	}
	for _, handler := range policy.Handlers {
		if (handler.Decision == HookBlock || handler.Decision == HookRequire) && strings.TrimSpace(handler.Requirement) == "" {
			return fmt.Errorf("workflow hook requirement is required for %q decisions", handler.Decision)
		}
		// A hook must do something, but stating what the agent must do IS
		// something: a block/require decision carries its requirement to the
		// agent. Demanding an action on top of that made "instruct instead of
		// stop" impossible to save (FIR-4797).
		if len(handler.Actions) == 0 && handler.Decision != HookBlock && handler.Decision != HookRequire {
			return fmt.Errorf("a workflow hook that only guides needs at least one action, or a decision that states what the agent must do")
		}
	}
	return validateTypedHookActions(*policy)
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
	policy, err := h.repository.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	if err := validateHookForExecution(&policy, workspaceID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	published, err := h.repository.Publish(r.Context(), workspaceID, id, actor.ID)
	if err != nil {
		hookLifecycleAudit(hookAuditPublish, workspaceID, id, actor, nil, err)
		h.writeRepositoryError(w, err)
		return
	}
	hookLifecycleAudit(hookAuditPublish, workspaceID, id, actor, &published, nil)
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
	var request struct {
		EventID  string `json:"event_id"`
		Revision int    `json:"revision"`
	}
	// Revision 0 is a real revision: Drafts saved before the Live/Draft split
	// carry it. Rejecting it below 1 made those Drafts impossible to test at
	// any number, with "revision is stale" as the only clue (FIR-4797).
	if r.Body == nil || json.NewDecoder(r.Body).Decode(&request) != nil || request.EventID == "" || request.Revision < 0 {
		writeError(w, http.StatusBadRequest, "event_id and revision are required")
		return
	}
	if policy.Revision != request.Revision {
		h.writeRepositoryError(w, fmt.Errorf("%w: this Draft is at revision %d, not %d — reload the hook and try again", ErrHookDraftRevisionStale, policy.Revision, request.Revision))
		return
	}
	if err := validateHookForExecution(&policy, workspaceID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	event, err := h.repository.ReplayEvent(r.Context(), workspaceID, request.EventID)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	if !eventListed(policy.Events, event.Type) {
		writeError(w, http.StatusUnprocessableEntity, "retained event is not compatible with this Draft")
		return
	}
	event.WorkspaceID = workspaceID
	testPolicy := policy
	testPolicy.Mode = HookModeDryRun
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{testPolicy})).Evaluate(r.Context(), event)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	run := HookRunRecord{PolicyID: policy.ID, PolicyVersion: policy.Version, Event: event, Result: result, FailMode: policy.FailMode, CreatedAt: time.Now().UTC()}
	if len(result.Matches) > 0 {
		run.SourceScope = result.Matches[0].SourceScope
	}
	baselineAt, err := h.repository.RecordTestEvidence(r.Context(), workspaceID, request.EventID, run)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"side_effects": false, "result": result, "baseline_at": baselineAt,
		"tested_revision": policy.Revision, "retained_event_id": request.EventID,
	})
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
	policy, err := h.repository.GetEffective(r.Context(), workspaceID, id)
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

// RecentRuns answers "what have the hooks actually done lately?" across every
// hook in the workspace, instead of forcing a person to open all 44 of them.
func (h *HookAPI) RecentRuns(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.authorize(w, r, HookPermissionRead)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := h.repository.RecentRuns(r.Context(), workspaceID, limit)
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
	case errors.Is(err, ErrHookDraftRevisionStale):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, ErrHookEventNotFound):
		writeError(w, http.StatusNotFound, "retained workflow event not found")
	case errors.Is(err, ErrManagedHookLocked):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "workflow hook operation failed")
	}
}
