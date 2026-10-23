package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// The transcript endpoint, exercised through the paths that actually decide
// what a plugin may read: the tasks:read grant, the workspace the token's
// installation lives in, and the issue a callback grant narrows to.

const taskReaderPluginManifest = `{
  "manifest_version": 1,
  "key": "com.example.taskreader",
  "name": "Task Reader",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["tasks:read", "net:example.com"],
  "contributes": {
    "hooks": [{
      "key": "archive",
      "name": "Archive",
      "description": "Archive finished runs.",
      "triggers": ["event"],
      "events": ["task.completed"],
      "transport": { "type": "http", "url": "https://example.com/hooks/archive" }
    }]
  }
}`

func installTaskReaderPlugin(t *testing.T) string {
	t.Helper()
	versionID := withLocalPluginSource(t, taskReaderPluginManifest)
	body, _ := json.Marshal(map[string]any{
		"version_id":     versionID,
		"granted_scopes": []string{"tasks:read", "net:example.com"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install task reader plugin: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

// taskCallbackToken mints a callback grant the way a task.completed dispatch
// would: the archive hook, event trigger, and — when issueID is set — the
// issue-scoped narrowing a hook about one issue carries.
func taskCallbackToken(t *testing.T, installationID string, issueID string) string {
	t.Helper()
	installation, err := testHandler.PluginService.InstallationForWorkspace(
		context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	invocation := service.HookInvocation{
		Installation: installation,
		Hook:         plugincontract.Hook{Key: "archive"},
		Trigger:      plugincontract.TriggerEvent,
		Actor:        service.HookActor{Type: "member", ID: parseUUID(testUserID)},
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

func pluginTaskMessagesRequest(token, taskID, query string) *http.Request {
	return callbackRequest(token, http.MethodGet, "/v1/tasks/"+taskID+"/messages"+query, nil,
		map[string]string{"task_id": taskID})
}

func seedTranscript(t *testing.T, label string) (taskID, issueID string) {
	t.Helper()
	taskID = seedBatchTask(t, label)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil || !task.IssueID.Valid {
		t.Fatalf("load seeded task: err=%v issue_valid=%v", err, task.IssueID.Valid)
	}
	issueID = uuidToString(task.IssueID)
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "thinking", "content": "planning the run"},
		map[string]any{"seq": 2, "type": "tool_use", "tool": "fs_read",
			"input": map[string]any{"path": "/etc/hosts"}, "output": "127.0.0.1 localhost"},
		map[string]any{"seq": 3, "type": "text", "content": "done"},
	}))
	return taskID, issueID
}

type taskMessagesBody struct {
	Messages []struct {
		Seq     int    `json:"seq"`
		Type    string `json:"type"`
		IssueID string `json:"issue_id"`
	} `json:"messages"`
}

func decodeTaskMessages(t *testing.T, recorder *httptest.ResponseRecorder) taskMessagesBody {
	t.Helper()
	var body taskMessagesBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode messages: %v (body=%s)", err, recorder.Body.String())
	}
	return body
}

func TestPluginTaskMessagesReturnsTranscriptInSeqOrder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installTaskReaderPlugin(t)

	taskID, issueID := seedTranscript(t, "plugin-transcript")
	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, pluginTaskMessagesRequest(taskCallbackToken(t, installationID, ""), taskID, ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := decodeTaskMessages(t, recorder)
	if len(body.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (thinking ships by default)", len(body.Messages))
	}
	for i, want := range []string{"thinking", "tool_use", "text"} {
		if body.Messages[i].Type != want || body.Messages[i].Seq != i+1 {
			t.Fatalf("message[%d] = seq %d %s, want seq %d %s", i, body.Messages[i].Seq, body.Messages[i].Type, i+1, want)
		}
	}
	if body.Messages[0].IssueID != issueID {
		t.Fatalf("issue_id = %q, want %q", body.Messages[0].IssueID, issueID)
	}
}

func TestPluginTaskMessagesIncludeThinkingFalseDropsThinking(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installTaskReaderPlugin(t)

	taskID, _ := seedTranscript(t, "plugin-transcript-nothinking")
	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, pluginTaskMessagesRequest(taskCallbackToken(t, installationID, ""), taskID, "?include=thinking=false"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := decodeTaskMessages(t, recorder)
	if len(body.Messages) != 2 || body.Messages[0].Type == "thinking" {
		t.Fatalf("messages = %+v, want the two non-thinking entries", body.Messages)
	}
}

func TestPluginTaskMessagesSinceReturnsOnlyNewer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installTaskReaderPlugin(t)

	taskID, _ := seedTranscript(t, "plugin-transcript-since")
	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, pluginTaskMessagesRequest(taskCallbackToken(t, installationID, ""), taskID, "?since=1"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := decodeTaskMessages(t, recorder)
	if len(body.Messages) != 2 || body.Messages[0].Seq != 2 {
		t.Fatalf("messages = %+v, want seq 2..3 only", body.Messages)
	}
}

// A callback grant issued about one issue must not reach another issue's
// tasks, even inside the same workspace — same 404-not-403 rule the issue
// endpoint uses, so the grant's reach is not observable by probing.
func TestPluginTaskMessagesIssueScopedGrant404sOtherIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installTaskReaderPlugin(t)

	_, issueA := seedTranscript(t, "plugin-transcript-scope-a")
	taskB, _ := seedTranscript(t, "plugin-transcript-scope-b")

	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, pluginTaskMessagesRequest(taskCallbackToken(t, installationID, issueA), taskB, ""))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404 for an out-of-scope issue's task", recorder.Code, recorder.Body.String())
	}
}

func TestPluginTaskMessagesUnknownTask404s(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installTaskReaderPlugin(t)

	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, pluginTaskMessagesRequest(taskCallbackToken(t, installationID, ""),
		"00000000-0000-0000-0000-000000000000", ""))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", recorder.Code)
	}
}

// tasks:read is the gate: an installation holding only the hooked-plugin
// fixture's scopes must not read transcripts.
func TestPluginTaskMessagesRequiresTasksReadScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t) // issues:read + comments:write, no tasks:read

	taskID, _ := seedTranscript(t, "plugin-transcript-scope-gate")
	recorder := httptest.NewRecorder()
	testHandler.ListPluginTaskMessages(recorder, pluginTaskMessagesRequest(taskCallbackToken(t, installationID, ""), taskID, ""))
	if recorder.Code == http.StatusOK {
		t.Fatalf("status=200 without tasks:read — scope gate broken (body=%s)", recorder.Body.String())
	}
}
