package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// A plugin reaches no further than the identity behind the call, and that
// includes private projects (MUL-8472): an object in one the identity cannot
// see answers exactly like an id that does not exist.

var pluginPrivateProjectScopes = []string{
	plugincontract.ScopeIssuesRead, plugincontract.ScopeIssuesWrite,
	plugincontract.ScopeCommentsRead, plugincontract.ScopeCommentsWrite,
	plugincontract.ScopeTasksRead,
}

type pluginProblemAnswer struct {
	status int
	code   string
	detail string
}

func pluginAnswer(t *testing.T, recorder *httptest.ResponseRecorder) pluginProblemAnswer {
	t.Helper()
	answer := pluginProblemAnswer{status: recorder.Code}
	if recorder.Code >= 400 {
		var problem publicapiv1.Problem
		if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decode problem: %v (body %s)", err, recorder.Body.String())
		}
		answer.code, answer.detail = problem.Code, problem.Detail
	}
	return answer
}

func privateProjectTask(t *testing.T, issueID string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`, testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	return dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "completed"})
}

// pluginActionCalls runs every Action API read and write that names an issue
// or a run, through one request builder. Each returns the answer for one id.
func pluginActionCalls(build func(method, path string, body any, params map[string]string) *http.Request) map[string]func(id string) *httptest.ResponseRecorder {
	run := func(handler http.HandlerFunc, request *http.Request) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler(recorder, request)
		return recorder
	}
	return map[string]func(string) *httptest.ResponseRecorder{
		"GET issue": func(id string) *httptest.ResponseRecorder {
			return run(testHandler.GetPluginIssue, build(http.MethodGet, "/v1/issues/"+id, nil, map[string]string{"issue_ref": id}))
		},
		"PATCH issue": func(id string) *httptest.ResponseRecorder {
			return run(testHandler.PatchPluginIssue, build(http.MethodPatch, "/v1/issues/"+id,
				map[string]any{"description": "patched through a plugin"}, map[string]string{"issue_ref": id}))
		},
		"GET comments": func(id string) *httptest.ResponseRecorder {
			return run(testHandler.ListPluginComments, build(http.MethodGet, "/v1/issues/"+id+"/comments", nil, map[string]string{"issue_ref": id}))
		},
		"POST comment": func(id string) *httptest.ResponseRecorder {
			return run(testHandler.CreatePluginComment, build(http.MethodPost, "/v1/issues/"+id+"/comments",
				map[string]any{"content": "posted through a plugin"}, map[string]string{"issue_ref": id}))
		},
		"GET context": func(id string) *httptest.ResponseRecorder {
			return run(testHandler.GetPluginContext, build(http.MethodGet, "/v1/context?issue_id="+id, nil, nil))
		},
	}
}

func listPluginTaskMessages(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, request)
	return recorder
}

// The surface bridge acts as the signed-in user, so a workspace member outside
// the project must not reach its issues through a plugin either.
func TestPluginBridgeHidesPrivateProjects(t *testing.T) {
	f := newPrivateProjectFixture(t)
	installationID := installPluginForAction(t, pluginPrivateProjectScopes)
	secretTask := privateProjectTask(t, f.secretIssue)

	asCaller := func(userID string) func(string, string, any, map[string]string) *http.Request {
		return func(method, path string, body any, params map[string]string) *http.Request {
			return asUser(pluginActionRequest(method, path, installationID, body, params), userID)
		}
	}
	missingIssue := uuid.NewString()
	for name, call := range pluginActionCalls(asCaller(f.outsider)) {
		hidden, missing := pluginAnswer(t, call(f.secretIssue)), pluginAnswer(t, call(missingIssue))
		if hidden != missing || hidden.status != http.StatusNotFound {
			t.Errorf("%s: outsider got %+v for the private issue, %+v for a missing one", name, hidden, missing)
		}
	}
	for name, call := range pluginActionCalls(asCaller(f.outsider)) {
		if got := call(f.publicIssue).Code; got >= 400 {
			t.Errorf("%s: outsider on an issue outside any project = %d, want success", name, got)
		}
	}
	for name, call := range pluginActionCalls(asCaller(f.insider)) {
		if got := call(f.secretIssue).Code; got >= 400 {
			t.Errorf("%s: project member = %d, want success", name, got)
		}
	}

	messages := func(userID, taskID string) pluginProblemAnswer {
		return pluginAnswer(t, listPluginTaskMessages(asCaller(userID)(http.MethodGet, "/v1/tasks/"+taskID+"/messages", nil,
			map[string]string{"task_id": taskID})))
	}
	if hidden, missing := messages(f.outsider, secretTask), messages(f.outsider, uuid.NewString()); hidden != missing || hidden.status != http.StatusNotFound {
		t.Errorf("task messages: outsider got %+v for the private run, %+v for a missing one", hidden, missing)
	}
	if got := messages(f.insider, secretTask); got.status != http.StatusOK {
		t.Errorf("task messages: project member got %+v, want 200", got)
	}
}

// A hook invoked from the issue actions menu sends the plugin the issue id;
// naming a private issue the user cannot see must stop before the call.
func TestPluginHookInvocationHidesPrivateProjects(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	f := newPrivateProjectFixture(t)
	installationID := installHookPlugin(t)

	invoke := func(issueID string) pluginProblemAnswer {
		request := asUser(pluginActionRequest(http.MethodPost, "/hooks/summarize", installationID,
			map[string]any{"trigger": plugincontract.TriggerManual, "issue_id": issueID}, map[string]string{"key": "summarize"}), f.outsider)
		recorder := httptest.NewRecorder()
		testHandler.InvokePluginHook(recorder, request)
		return pluginAnswer(t, recorder)
	}
	if hidden, missing := invoke(f.secretIssue), invoke(uuid.NewString()); hidden != missing || hidden.status != http.StatusNotFound {
		t.Errorf("outsider hook on the private issue got %+v, on a missing one %+v", hidden, missing)
	}
}

// A token call has no session. A callback grant that stands for a person is
// bounded by that person's project membership as of today; a call with nobody
// behind it — an install token, an event callback — sees no private project.
func TestPluginTokensHidePrivateProjects(t *testing.T) {
	f := newPrivateProjectFixture(t)
	installationID := installPluginForAction(t, pluginPrivateProjectScopes)
	withCallbackTokens(t)
	secretTask := privateProjectTask(t, f.secretIssue)
	installToken, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(installationID))
	if err != nil {
		t.Fatalf("issue install token: %v", err)
	}
	installation, err := testHandler.PluginService.InstallationForWorkspace(context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	callbackFor := func(actor service.HookActor, trigger, issueID string) string {
		invocation := service.HookInvocation{
			Installation: installation,
			Hook:         plugincontract.Hook{Key: "summarize"},
			Trigger:      trigger,
			Actor:        actor,
		}
		if issueID != "" {
			invocation.IssueID = parseUUID(issueID)
		}
		token, err := testHandler.PluginService.Callbacks.Issue(context.Background(), invocation)
		if err != nil {
			t.Fatalf("issue callback token: %v", err)
		}
		return token
	}
	asToken := func(token string) func(string, string, any, map[string]string) *http.Request {
		return func(method, path string, body any, params map[string]string) *http.Request {
			return callbackRequest(token, method, path, body, params)
		}
	}
	member := func(userID string) service.HookActor {
		return service.HookActor{Type: "member", ID: parseUUID(userID)}
	}
	pluginActor := service.HookActor{Type: "plugin", ID: parseUUID(installationID)}

	insiderToken := callbackFor(member(f.insider), plugincontract.TriggerManual, "")
	for _, tc := range []struct {
		name    string
		token   string
		visible bool
	}{
		{"install token", installToken, false},
		{"callback for a project member", insiderToken, true},
		{"callback for a workspace member outside the project", callbackFor(member(f.outsider), plugincontract.TriggerManual, ""), false},
		{"callback for an admin outside the project", callbackFor(member(f.outsideAdmin), plugincontract.TriggerManual, ""), false},
		{"event callback about the private issue", callbackFor(pluginActor, plugincontract.TriggerEvent, f.secretIssue), false},
	} {
		for name, call := range pluginActionCalls(asToken(tc.token)) {
			got := pluginAnswer(t, call(f.secretIssue))
			if tc.visible && got.status >= 400 {
				t.Errorf("%s, %s: got %+v, want success", tc.name, name, got)
			}
			if !tc.visible && (got.status != http.StatusNotFound || got.code != "not_found") {
				t.Errorf("%s, %s: got %+v, want 404 not_found", tc.name, name, got)
			}
		}
		got := pluginAnswer(t, listPluginTaskMessages(callbackRequest(tc.token, http.MethodGet, "/v1/tasks/"+secretTask+"/messages", nil,
			map[string]string{"task_id": secretTask})))
		if tc.visible != (got.status == http.StatusOK) {
			t.Errorf("%s, task messages: got %+v, visible=%v", tc.name, got, tc.visible)
		}
	}

	// Issues outside any project stay reachable without a person behind the call.
	if got := pluginAnswer(t, pluginActionCalls(asToken(installToken))["GET issue"](f.publicIssue)); got.status != http.StatusOK {
		t.Errorf("install token on an issue outside any project: got %+v, want 200", got)
	}

	// Removing the person from the project cuts off a grant already in flight.
	removeProjectMemberRow(t, f.projectID, f.insider)
	if got := pluginAnswer(t, pluginActionCalls(asToken(insiderToken))["GET issue"](f.secretIssue)); got.status != http.StatusNotFound {
		t.Errorf("callback after the member left the project: got %+v, want 404", got)
	}
}
