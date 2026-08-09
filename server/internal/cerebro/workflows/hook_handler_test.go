package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const hookTestWorkspaceID = "11111111-1111-1111-1111-111111111111"

func TestValidateHookForExecutionRequiresReadableContract(t *testing.T) {
	policy := newTestHookPolicy("", HookRequire, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.Name = "Guard completion"
	policy.ContractRule = ""
	policy.ContractSatisfy = ""
	policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": "hook.test"}}}
	if err := validateHookForExecution(&policy, hookTestWorkspaceID); err == nil || !strings.Contains(err.Error(), "contract_rule") {
		t.Fatalf("validation error = %v, want missing contract_rule", err)
	}
}

func TestHookAPIAllowsFreshAgentReadButDeniesWrite(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{}
	router := hookTestRouter(NewHookAPI(repo, auth))

	read := hookRequest(t, http.MethodGet, "/", nil, "agent-1", false)
	readRecorder := httptest.NewRecorder()
	router.ServeHTTP(readRecorder, read)
	if readRecorder.Code != http.StatusOK {
		t.Fatalf("read status = %d, body=%s", readRecorder.Code, readRecorder.Body.String())
	}

	write := hookRequest(t, http.MethodPost, "/", newTestHookPolicy("", HookBlock, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID}), "agent-1", false)
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, write)
	if writeRecorder.Code != http.StatusForbidden {
		t.Fatalf("write status = %d, want 403; body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}
}

func TestHookAPICreateAlwaysStartsInDryRun(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))
	request := hookRequest(t, http.MethodPost, "/", newTestHookPolicy("", HookBlock, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID}), "agent-1", false)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var created HookPolicy
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Mode != HookModeDryRun || created.ID == "" || created.Version != 1 {
		t.Fatalf("created policy = %#v", created)
	}
}

func TestHookAPIPublishRejectsSemanticallyIncompleteDrafts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*HookPolicy)
	}{
		{name: "missing handler", mutate: func(policy *HookPolicy) {
			policy.Handlers = nil
		}},
		{name: "missing action", mutate: func(policy *HookPolicy) {
			policy.Handlers[0].Actions = nil
		}},
		{name: "unknown action", mutate: func(policy *HookPolicy) {
			policy.Handlers[0].Actions = []HookAction{{Type: "unknown.action"}}
		}},
		{name: "missing action target", mutate: func(policy *HookPolicy) {
			policy.Handlers[0].Actions = []HookAction{{Type: "judge.gate", Config: map[string]any{"agent_id": "11111111-1111-1111-1111-111111111111"}}}
		}},
		{name: "missing name", mutate: func(policy *HookPolicy) { policy.Name = "" }},
		{name: "missing trigger", mutate: func(policy *HookPolicy) { policy.Events = nil }},
		{name: "missing scope", mutate: func(policy *HookPolicy) { policy.Bindings = nil }},
		{name: "missing named scope target", mutate: func(policy *HookPolicy) {
			policy.Bindings = []HookBinding{{Kind: HookScopeProject}}
		}},
		{name: "missing requirement", mutate: func(policy *HookPolicy) {
			policy.Handlers[0].Decision = HookRequire
			policy.Handlers[0].Requirement = ""
		}},
		{name: "missing condition value", mutate: func(policy *HookPolicy) {
			policy.Conditions = []Condition{{Field: "attempt", Op: "gte"}}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := NewMemoryHookRepository()
			policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
			policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": "test"}}}
			testCase.mutate(&policy)
			repo.Seed(hookTestWorkspaceID, policy)
			repo.RecordObservedRun(hookTestWorkspaceID, policy.ID)
			repo.MarkBaselineFresh(hookTestWorkspaceID, policy.ID)
			auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionEnforce: true}}
			router := hookTestRouter(NewHookAPI(repo, auth))

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/"+policy.ID+"/publish", nil, "member-1", false))

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("publish status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHookAPIPublishCanonicalizesLegacyThisWorkspaceBinding(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy(
		"22222222-2222-2222-2222-222222222222",
		HookAllow,
		HookModeDryRun,
		HookBinding{Kind: HookScopeWorkspace},
	)
	policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": "test"}}}
	repo.Seed(hookTestWorkspaceID, policy)
	repo.RecordObservedRun(hookTestWorkspaceID, policy.ID)
	repo.MarkBaselineFresh(hookTestWorkspaceID, policy.ID)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionEnforce: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/"+policy.ID+"/publish", nil, "member-1", false))

	if recorder.Code != http.StatusOK {
		t.Fatalf("publish status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var published HookPolicy
	if err := json.Unmarshal(recorder.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if len(published.Bindings) != 1 || published.Bindings[0].ID != hookTestWorkspaceID {
		t.Fatalf("published workspace bindings = %#v", published.Bindings)
	}
}

func TestHookAPICreateAllowsIncompleteDraftConfiguration(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))
	policy := newTestHookPolicy("", HookRequire, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.Handlers[0].Actions = []HookAction{{
		Type:   "judge.gate",
		Config: map[string]any{"rubric": "Approve complete delivery evidence."},
	}}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/", policy, "member-1", false))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("create incomplete Draft status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHookAPICreateRejectsStructurallyInvalidDrafts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*HookPolicy)
	}{
		{name: "unknown action", mutate: func(policy *HookPolicy) {
			policy.Handlers[0].Actions = []HookAction{{Type: "unknown.action"}}
		}},
		{name: "unknown condition operator", mutate: func(policy *HookPolicy) {
			policy.Conditions = []Condition{{Field: "attempt", Op: "approximately", Value: 2}}
		}},
		{name: "unknown trigger", mutate: func(policy *HookPolicy) {
			policy.Events = []HookEventType{"before.unknown.event"}
		}},
		{name: "unknown fail mode", mutate: func(policy *HookPolicy) {
			policy.FailMode = HookFailMode("explode")
		}},
		{name: "unknown decision", mutate: func(policy *HookPolicy) {
			policy.Handlers[0].Decision = HookDecision("guess")
		}},
		{name: "unknown binding", mutate: func(policy *HookPolicy) {
			policy.Bindings = []HookBinding{{Kind: HookScopeKind("organization"), ID: "outside"}}
		}},
		{name: "duplicate binding", mutate: func(policy *HookPolicy) {
			policy.Bindings = []HookBinding{
				{Kind: HookScopeProject, ID: "project-1"},
				{Kind: HookScopeProject, ID: "project-1"},
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := NewMemoryHookRepository()
			auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
			router := hookTestRouter(NewHookAPI(repo, auth))
			policy := newTestHookPolicy("", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
			testCase.mutate(&policy)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/", policy, "member-1", false))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("create status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHookAPICreateCanonicalizesThisWorkspaceBinding(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))
	policy := newTestHookPolicy("", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/", policy, "member-1", false))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("create This workspace Draft status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var created HookPolicy
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Bindings) != 1 || created.Bindings[0].ID != hookTestWorkspaceID {
		t.Fatalf("workspace binding = %#v, want authenticated workspace %q", created.Bindings, hookTestWorkspaceID)
	}
}

func TestHookAPICreateRejectsAnotherWorkspaceBinding(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))
	policy := newTestHookPolicy("", HookAllow, HookModeDryRun, HookBinding{
		Kind: HookScopeWorkspace,
		ID:   "33333333-3333-3333-3333-333333333333",
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/", policy, "member-1", false))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create cross-workspace Draft status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHookAPIUpdateCanonicalizesThisWorkspaceBinding(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	repo.Seed(hookTestWorkspaceID, policy)
	saved, err := repo.Get(context.Background(), hookTestWorkspaceID, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Bindings = []HookBinding{{Kind: HookScopeWorkspace}}
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPut, "/"+saved.ID, saved, "member-1", false))

	if recorder.Code != http.StatusOK {
		t.Fatalf("update This workspace Draft status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var updated HookPolicy
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Bindings) != 1 || updated.Bindings[0].ID != hookTestWorkspaceID {
		t.Fatalf("workspace binding = %#v, want authenticated workspace %q", updated.Bindings, hookTestWorkspaceID)
	}
}

func TestHookAPITestRejectsIncompleteDraftConfiguration(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookRequire, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.Handlers[0].Actions = []HookAction{{
		Type:   "judge.gate",
		Config: map[string]any{"rubric": "Approve complete delivery evidence."},
	}}
	repo.Seed(hookTestWorkspaceID, policy)
	saved, err := repo.Get(context.Background(), hookTestWorkspaceID, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := repo.CaptureEvent(context.Background(), hookTestWorkspaceID, HookEvent{
		EventID: "incomplete-draft-event", Type: HookBeforeTaskComplete, WorkspaceID: hookTestWorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/"+saved.ID+"/test", map[string]any{
		"event_id": retained.ID,
		"revision": saved.Revision,
	}, "member-1", false))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("test incomplete Draft status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHookAPIRejectsUnknownConditionMode(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))
	policy := newTestHookPolicy("", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.ConditionMode = HookConditionMode("sometimes")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/", policy, "member-1", false))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create invalid condition_mode status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHookAPIPublishRequiresHumanPermissionRunAndBaseline(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": "publish"}}}
	repo.Seed(hookTestWorkspaceID, policy)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionEnforce: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	withoutBaseline := hookRequest(t, http.MethodPost, "/"+policy.ID+"/publish", nil, "member-1", false)
	withoutBaselineRecorder := httptest.NewRecorder()
	router.ServeHTTP(withoutBaselineRecorder, withoutBaseline)
	if withoutBaselineRecorder.Code != http.StatusConflict {
		t.Fatalf("publish without baseline status = %d, want 409", withoutBaselineRecorder.Code)
	}

	repo.RecordObservedRun(hookTestWorkspaceID, policy.ID)
	repo.MarkBaselineFresh(hookTestWorkspaceID, policy.ID)
	withBaseline := hookRequest(t, http.MethodPost, "/"+policy.ID+"/publish", nil, "member-1", false)
	withBaselineRecorder := httptest.NewRecorder()
	router.ServeHTTP(withBaselineRecorder, withBaseline)
	if withBaselineRecorder.Code != http.StatusOK {
		t.Fatalf("publish status = %d, body=%s", withBaselineRecorder.Code, withBaselineRecorder.Body.String())
	}
}

// Pausing a managed policy has to work over HTTP, not just against the
// repository: the owner flag is derived from the member in request context,
// so this is what proves an owner can actually disable one and a plain member
// still cannot.
func TestHookAPIDisableOnManagedPolicyIsOwnerOnly(t *testing.T) {
	const policyID = "33333333-3333-3333-3333-333333333333"
	// Disable authorizes with HookPermissionEnforce; managed lock is then
	// enforced via actor.IsOwner in the repository (not ManageManaged).
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionEnforce: true}}

	for _, testCase := range []struct {
		name  string
		owner bool
		want  int
	}{
		{name: "member", owner: false, want: http.StatusForbidden},
		{name: "owner", owner: true, want: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := NewMemoryHookRepository()
			repo.Seed(hookTestWorkspaceID, newTestHookPolicy(policyID, HookRequire, HookModeManaged, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID}))
			router := hookTestRouter(NewHookAPI(repo, auth))

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, hookRequest(t, http.MethodPost, "/"+policyID+"/disable", nil, "member-1", testCase.owner))
			if rec.Code != testCase.want {
				t.Fatalf("disable status = %d, want %d; body=%s", rec.Code, testCase.want, rec.Body.String())
			}
			if testCase.want != http.StatusOK {
				return
			}
			var disabled HookPolicy
			if err := json.Unmarshal(rec.Body.Bytes(), &disabled); err != nil {
				t.Fatal(err)
			}
			if disabled.Mode != HookModeOff {
				t.Fatalf("mode after disable = %q, want %q", disabled.Mode, HookModeOff)
			}
		})
	}
}

func TestHookAPITestCreatesFreshBaselineWithoutSideEffects(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace})
	policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": "test"}}}
	repo.Seed(hookTestWorkspaceID, policy)
	saved, err := repo.Get(context.Background(), hookTestWorkspaceID, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := repo.CaptureEvent(context.Background(), hookTestWorkspaceID, HookEvent{EventID: "observed-model-event", Type: HookBeforeTaskComplete, WorkspaceID: hookTestWorkspaceID, Model: "claude-opus-4-6"})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	req := hookRequest(t, http.MethodPost, "/"+policy.ID+"/test", map[string]any{"event_id": retained.ID, "revision": saved.Revision}, "member-1", false)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		SideEffects bool       `json:"side_effects"`
		BaselineAt  *time.Time `json:"baseline_at"`
		Result      HookResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SideEffects || response.BaselineAt == nil || len(response.Result.Matches) != 1 {
		t.Fatalf("test response = %#v", response)
	}
	stored, err := repo.Get(context.Background(), hookTestWorkspaceID, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.CanPublish {
		t.Fatalf("tested policy should be publishable: %#v", stored)
	}
}

func TestHookAPIListsOnlyCompatibleRedactedRetainedEvents(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	repo.Seed(hookTestWorkspaceID, policy)
	compatible, err := repo.CaptureEvent(context.Background(), hookTestWorkspaceID, HookEvent{
		EventID: "compatible", Type: HookBeforeTaskComplete, WorkspaceID: hookTestWorkspaceID,
		Context: map[string]any{"issue": map[string]any{"status": "in_review"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CaptureEvent(context.Background(), hookTestWorkspaceID, HookEvent{EventID: "other", Type: HookOnTaskFailure, WorkspaceID: hookTestWorkspaceID}); err != nil {
		t.Fatal(err)
	}
	auth := &fakeHookAuthorizer{}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodGet, "/"+policy.ID+"/events", nil, "member-1", false))
	if recorder.Code != http.StatusOK {
		t.Fatalf("events status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Events) != 1 || response.Events[0]["id"] != compatible.ID {
		t.Fatalf("compatible events = %#v", response.Events)
	}
	if response.Events[0]["replay_event"] != nil || response.Events[0]["event_hash"] != nil {
		t.Fatalf("protected event data leaked: %#v", response.Events[0])
	}
}

func TestHookAPITestQualifiesOnlyTheExactSavedDraftRevision(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.Handlers[0].Actions = []HookAction{{Type: "task.cancel", Config: map[string]any{"task_id": "{{task.id}}", "reason": "would cancel"}}}
	repo.Seed(hookTestWorkspaceID, policy)
	saved, err := repo.Get(ctx, hookTestWorkspaceID, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := repo.CaptureEvent(ctx, hookTestWorkspaceID, HookEvent{EventID: "observed-1", Type: HookBeforeTaskComplete, WorkspaceID: hookTestWorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true, HookPermissionEnforce: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	testRecorder := httptest.NewRecorder()
	router.ServeHTTP(testRecorder, hookRequest(t, http.MethodPost, "/"+saved.ID+"/test", map[string]any{
		"event_id": retained.ID,
		"revision": saved.Revision,
	}, "member-1", false))
	if testRecorder.Code != http.StatusOK {
		t.Fatalf("test status = %d, body=%s", testRecorder.Code, testRecorder.Body.String())
	}
	var response struct {
		SideEffects    bool       `json:"side_effects"`
		TestedRevision int        `json:"tested_revision"`
		Result         HookResult `json:"result"`
	}
	if err := json.Unmarshal(testRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SideEffects || response.TestedRevision != saved.Revision {
		t.Fatalf("test response = %#v", response)
	}
	if len(response.Result.ActionResults) != 1 || response.Result.ActionResults[0].Status != HookActionWouldRun {
		t.Fatalf("planning-only action result = %#v", response.Result.ActionResults)
	}
	tested, err := repo.Get(ctx, hookTestWorkspaceID, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !tested.CanPublish {
		t.Fatalf("tested exact revision should be publishable: %#v", tested)
	}

	newer, err := repo.Update(ctx, hookTestWorkspaceID, HookPermissionActor{Type: "member", ID: "member-1"}, saved.ID, HookPolicy{
		Name: saved.Name, Events: saved.Events, Bindings: saved.Bindings, ConditionMode: saved.ConditionMode,
		ContractRule: saved.ContractRule, ContractSatisfy: saved.ContractSatisfy,
		Handlers: saved.Handlers, FailMode: saved.FailMode, Revision: saved.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if newer.CanPublish {
		t.Fatalf("new revision reused old evidence: %#v", newer)
	}
	publishRecorder := httptest.NewRecorder()
	router.ServeHTTP(publishRecorder, hookRequest(t, http.MethodPost, "/"+newer.ID+"/publish", nil, "member-1", false))
	if publishRecorder.Code != http.StatusConflict {
		t.Fatalf("publish untested revision status = %d, want 409; body=%s", publishRecorder.Code, publishRecorder.Body.String())
	}
}

func TestHookAPIMalformedIDAndWorkspaceIsolationFailSafely(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{}
	router := hookTestRouter(NewHookAPI(repo, auth))

	bad := hookRequest(t, http.MethodGet, "/not-a-uuid", nil, "agent-1", false)
	badRecorder := httptest.NewRecorder()
	router.ServeHTTP(badRecorder, bad)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed id status = %d", badRecorder.Code)
	}

	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: "other"})
	repo.Seed("33333333-3333-3333-3333-333333333333", policy)
	missing := hookRequest(t, http.MethodGet, "/"+policy.ID, nil, "agent-1", false)
	missingRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace status = %d", missingRecorder.Code)
	}
}

func TestHookAPIListsActiveRulesForAnAgentAndIssue(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := activeRulePolicy("active-rule", "Completion rule", HookBinding{Kind: HookScopeModel, ID: "gpt-5"})
	repo.Seed(hookTestWorkspaceID, policy)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionRead: true}}
	resolver := activeRuleContextResolverFunc(func(_ context.Context, workspaceID, agentID, issueID string) (ActiveRuleContext, error) {
		return ActiveRuleContext{WorkspaceID: workspaceID, AgentID: agentID, IssueID: issueID, Model: "gpt-5", ProjectID: "project-1"}, nil
	})
	router := hookTestRouter(NewHookAPI(repo, auth).WithActiveRuleService(NewActiveRuleService(repo)).WithActiveRuleContextResolver(resolver))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodGet, "/active-rules?agent_id=agent-1&issue_id=issue-1", nil, "member-1", false))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Rules []ActiveHookRule `json:"rules"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Rules) != 1 || response.Rules[0].Name != policy.Name {
		t.Fatalf("rules = %#v, want active policy", response.Rules)
	}
}

type activeRuleContextResolverFunc func(context.Context, string, string, string) (ActiveRuleContext, error)

func (f activeRuleContextResolverFunc) Resolve(ctx context.Context, workspaceID, agentID, issueID string) (ActiveRuleContext, error) {
	return f(ctx, workspaceID, agentID, issueID)
}

func TestHookAPIDisablesAndDeletesEditableHooks(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	repo.Seed(hookTestWorkspaceID, policy)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true, HookPermissionEnforce: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	disable := httptest.NewRecorder()
	router.ServeHTTP(disable, hookRequest(t, http.MethodPost, "/"+policy.ID+"/disable", nil, "member-1", false))
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body=%s", disable.Code, disable.Body.String())
	}
	stored, err := repo.Get(context.Background(), hookTestWorkspaceID, policy.ID)
	if err != nil || stored.Mode != HookModeOff {
		t.Fatalf("disabled policy = %#v, err=%v", stored, err)
	}

	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, hookRequest(t, http.MethodDelete, "/"+policy.ID, nil, "member-1", false))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", deleted.Code, deleted.Body.String())
	}
	if _, err := repo.Get(context.Background(), hookTestWorkspaceID, policy.ID); !errors.Is(err, ErrHookPolicyNotFound) {
		t.Fatalf("deleted policy error = %v, want not found", err)
	}
}

func TestHookAPIDisableRequiresEnforcePermission(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	repo.Seed(hookTestWorkspaceID, policy)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/"+policy.ID+"/disable", nil, "member-1", false))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("disable status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHookAPIDiscardDraftLeavesLivePolicyUnchanged(t *testing.T) {
	repo := NewMemoryHookRepository()
	live := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	live.FamilyID = "33333333-3333-3333-3333-333333333333"
	live.Version = 4
	repo.Seed(hookTestWorkspaceID, live)
	draft, err := repo.Update(context.Background(), hookTestWorkspaceID, HookPermissionActor{Type: "member", ID: "44444444-4444-4444-4444-444444444444"}, live.ID, live)
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodDelete, "/"+draft.ID+"/draft", nil, "member-1", false))
	if recorder.Code != http.StatusOK {
		t.Fatalf("discard status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var remaining HookPolicy
	if err := json.Unmarshal(recorder.Body.Bytes(), &remaining); err != nil {
		t.Fatal(err)
	}
	if remaining.ID != live.ID || remaining.Lifecycle.State != HookLifecycleLive {
		t.Fatalf("remaining policy = %#v", remaining)
	}
}

func TestHookAPIEffectiveReturnsLiveWhileDraftExists(t *testing.T) {
	repo := NewMemoryHookRepository()
	live := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	live.FamilyID = "33333333-3333-3333-3333-333333333333"
	live.Version = 4
	repo.Seed(hookTestWorkspaceID, live)
	draft, err := repo.Update(context.Background(), hookTestWorkspaceID, HookPermissionActor{Type: "member", ID: "44444444-4444-4444-4444-444444444444"}, live.ID, live)
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeHookAuthorizer{}
	router := hookTestRouter(NewHookAPI(repo, auth))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodGet, "/"+draft.FamilyID+"/effective", nil, "member-1", false))
	if recorder.Code != http.StatusOK {
		t.Fatalf("effective status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Policy HookPolicy `json:"policy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Policy.ID != live.ID || response.Policy.Mode != HookModeEnforce {
		t.Fatalf("effective policy = %#v, want Live %s", response.Policy, live.ID)
	}
}

type fakeHookAuthorizer struct{ allow map[HookPermission]bool }

func (f *fakeHookAuthorizer) Can(_ context.Context, _ string, actor HookPermissionActor, permission HookPermission) bool {
	if permission == HookPermissionRead {
		return actor.Type == "agent" || actor.Type == "member"
	}
	return f.allow[permission]
}

func hookTestRouter(api *HookAPI) http.Handler {
	router := chi.NewRouter()
	router.Mount("/", api.Routes())
	return router
}

func hookRequest(t *testing.T, method, path string, body any, actorID string, owner bool) *http.Request {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	member := db.Member{UserID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Role: "member"}
	if owner {
		member.Role = "owner"
	}
	req = req.WithContext(middleware.SetMemberContext(req.Context(), hookTestWorkspaceID, member))
	if len(actorID) > 6 && actorID[:6] == "agent-" {
		req.Header.Set("X-Agent-ID", actorID)
		req.Header.Set("X-User-ID", "member-1")
	} else {
		req.Header.Set("X-User-ID", actorID)
	}
	return req
}
