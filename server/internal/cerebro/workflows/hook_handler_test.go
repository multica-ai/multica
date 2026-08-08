package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const hookTestWorkspaceID = "11111111-1111-1111-1111-111111111111"

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

func TestHookAPIRejectsIncompleteTypedActionConfiguration(t *testing.T) {
	repo := NewMemoryHookRepository()
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))
	policy := newTestHookPolicy("", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	policy.Handlers[0].Actions = []HookAction{{Type: "judge.gate", Config: map[string]any{"agent_id": "11111111-1111-1111-1111-111111111111"}}}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, hookRequest(t, http.MethodPost, "/", policy, "member-1", false))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create incomplete judge status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHookAPIPublishRequiresHumanPermissionRunAndBaseline(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
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
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true, HookPermissionManageManaged: true}}

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
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeModel, ID: "claude-opus-4-6"})
	repo.Seed(hookTestWorkspaceID, policy)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
	router := hookTestRouter(NewHookAPI(repo, auth))

	req := hookRequest(t, http.MethodPost, "/"+policy.ID+"/test", HookEvent{Type: HookBeforeTaskComplete}, "member-1", false)
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

func TestHookAPIDisablesAndDeletesEditableHooks(t *testing.T) {
	repo := NewMemoryHookRepository()
	policy := newTestHookPolicy("22222222-2222-2222-2222-222222222222", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: hookTestWorkspaceID})
	repo.Seed(hookTestWorkspaceID, policy)
	auth := &fakeHookAuthorizer{allow: map[HookPermission]bool{HookPermissionWrite: true}}
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
