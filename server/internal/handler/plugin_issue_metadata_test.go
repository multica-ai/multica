package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// createPluginMetadataTestIssue creates an issue in the shared test workspace and
// deletes it when the test ends, so list tests elsewhere in the package do not
// page over it.
func createPluginMetadataTestIssue(t *testing.T, title, status, priority string) string {
	t.Helper()
	issueID := createTestIssue(t, title, status, priority)
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	return issueID
}

func pluginMetadataParams(issueRef, key string) map[string]string {
	return map[string]string{"issue_ref": issueRef, "key": key}
}

func decodePluginMetadata(t *testing.T, recorder *httptest.ResponseRecorder) publicapiv1.IssueMetadataResponse {
	t.Helper()
	var body publicapiv1.IssueMetadataResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metadata response: %v body=%s", err, recorder.Body.String())
	}
	return body
}

func decodePluginProblem(t *testing.T, recorder *httptest.ResponseRecorder) publicapiv1.Problem {
	t.Helper()
	var problem publicapiv1.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v body=%s", err, recorder.Body.String())
	}
	return problem
}

// metadataEvents records issue:metadata_changed broadcasts for one issue. The
// bus is synchronous, so what a request published is visible on return.
func metadataEvents(t *testing.T, issueID string) func() []events.Event {
	t.Helper()
	var mu sync.Mutex
	var seen []events.Event
	testHandler.Bus.Subscribe(protocol.EventIssueMetadataChanged, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["issue_id"] != issueID {
			return
		}
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
	})
	return func() []events.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]events.Event(nil), seen...)
	}
}

func TestPluginIssueMetadataSetAndDeleteAsInstallation(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	token, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(installationID))
	if err != nil {
		t.Fatalf("issue install token: %v", err)
	}
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata install token", "todo", "none")
	published := metadataEvents(t, issueID)

	set := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(set, pluginInstallTokenRequest(http.MethodPut, "/v1/issues/"+issueID+"/metadata/jira.key", token,
		map[string]any{"value": "OPS-42"}, pluginMetadataParams(issueID, "jira.key")))
	if set.Code != http.StatusOK {
		t.Fatalf("set status=%d body=%s", set.Code, set.Body.String())
	}
	setBody := decodePluginMetadata(t, set)
	if setBody.Metadata["jira.key"] != "OPS-42" {
		t.Fatalf("metadata after set = %v", setBody.Metadata)
	}
	if want := fmt.Sprintf(`W/"%d"`, setBody.IssueRevision); set.Header().Get("ETag") != want {
		t.Fatalf("ETag=%q, want %q", set.Header().Get("ETag"), want)
	}

	// An event hook has nobody behind it, so the write is the installation's —
	// the same rule plugin comments follow.
	events := published()
	if len(events) != 1 {
		t.Fatalf("metadata_changed broadcasts after set = %d, want 1", len(events))
	}
	if events[0].ActorType != "plugin" || events[0].ActorID != installationID {
		t.Fatalf("set attributed to %s/%s, want plugin/%s", events[0].ActorType, events[0].ActorID, installationID)
	}
	if _, via := events[0].Payload.(map[string]any)["via_plugin_id"]; via {
		t.Fatalf("installation write carries via_plugin_id: %+v", events[0].Payload)
	}

	// The value is what the ordinary issue read now returns, to agents and UI.
	get := httptest.NewRecorder()
	testHandler.GetPluginIssue(get, pluginInstallTokenRequest(http.MethodGet, "/v1/issues/"+issueID, token, nil,
		map[string]string{"issue_ref": issueID}))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"jira.key":"OPS-42"`) {
		t.Fatalf("issue read after set status=%d body=%s", get.Code, get.Body.String())
	}

	// Writing the same value again changes nothing: same revision, no broadcast,
	// so a sync loop that re-asserts a link does not wake subscribed agents.
	again := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(again, pluginInstallTokenRequest(http.MethodPut, "/v1/issues/"+issueID+"/metadata/jira.key", token,
		map[string]any{"value": "OPS-42"}, pluginMetadataParams(issueID, "jira.key")))
	if again.Code != http.StatusOK {
		t.Fatalf("no-op set status=%d body=%s", again.Code, again.Body.String())
	}
	if got := decodePluginMetadata(t, again).IssueRevision; got != setBody.IssueRevision {
		t.Fatalf("no-op set moved revision %d -> %d", setBody.IssueRevision, got)
	}
	if len(published()) != 1 {
		t.Fatalf("no-op set broadcast; total broadcasts = %d", len(published()))
	}

	del := httptest.NewRecorder()
	testHandler.DeletePluginIssueMetadata(del, pluginInstallTokenRequest(http.MethodDelete, "/v1/issues/"+issueID+"/metadata/jira.key", token,
		nil, pluginMetadataParams(issueID, "jira.key")))
	if del.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
	delBody := decodePluginMetadata(t, del)
	if _, present := delBody.Metadata["jira.key"]; present || delBody.IssueRevision <= setBody.IssueRevision {
		t.Fatalf("after delete metadata=%v revision=%d (set revision %d)", delBody.Metadata, delBody.IssueRevision, setBody.IssueRevision)
	}
	if len(published()) != 2 {
		t.Fatalf("broadcasts after delete = %d, want 2", len(published()))
	}

	// Deleting an absent key is a retry-safe no-op.
	delAgain := httptest.NewRecorder()
	testHandler.DeletePluginIssueMetadata(delAgain, pluginInstallTokenRequest(http.MethodDelete, "/v1/issues/"+issueID+"/metadata/jira.key", token,
		nil, pluginMetadataParams(issueID, "jira.key")))
	if delAgain.Code != http.StatusOK || decodePluginMetadata(t, delAgain).IssueRevision != delBody.IssueRevision {
		t.Fatalf("repeat delete status=%d body=%s", delAgain.Code, delAgain.Body.String())
	}
	if len(published()) != 2 {
		t.Fatalf("repeat delete broadcast; total = %d", len(published()))
	}
}

func TestPluginIssueMetadataFromASurfaceIsTheUsers(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata surface", "todo", "none")
	published := metadataEvents(t, issueID)

	// Identifier spelling resolves to the same issue as the uuid.
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	workspace, err := testHandler.Queries.GetWorkspace(context.Background(), issue.WorkspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	identifier := fmt.Sprintf("%s-%d", workspace.IssuePrefix, issue.Number)

	recorder := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(recorder, pluginActionRequest(http.MethodPut, "/metadata", installationID,
		map[string]any{"value": 7}, pluginMetadataParams(identifier, "jira.sync_count")))
	if recorder.Code != http.StatusOK {
		t.Fatalf("surface set status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// Integers survive as integers, not 7.0.
	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT metadata->>'jira.sync_count' FROM issue WHERE id = $1`, issueID).Scan(&stored); err != nil {
		t.Fatalf("read stored metadata: %v", err)
	}
	if stored != "7" {
		t.Fatalf("stored value = %q, want 7", stored)
	}
	events := published()
	if len(events) != 1 || events[0].ActorType != "member" || events[0].ActorID != testUserID {
		t.Fatalf("surface write attribution = %+v, want member/%s", events, testUserID)
	}
	// The write is the member's, but stays traceable to the plugin that made it.
	if via := events[0].Payload.(map[string]any)["via_plugin_id"]; via != installationID {
		t.Fatalf("surface write via_plugin_id = %v, want %s", via, installationID)
	}
}

func TestPluginIssueMetadataHonoursIfMatch(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata if-match", "todo", "none")
	published := metadataEvents(t, issueID)
	put := func(ifMatch string, body map[string]any) *httptest.ResponseRecorder {
		request := pluginActionRequest(http.MethodPut, "/metadata", installationID, body, pluginMetadataParams(issueID, "jira.key"))
		if ifMatch != "" {
			request.Header.Set("If-Match", ifMatch)
		}
		recorder := httptest.NewRecorder()
		testHandler.SetPluginIssueMetadata(recorder, request)
		return recorder
	}

	first := put("", map[string]any{"value": "OPS-1"})
	if first.Code != http.StatusOK {
		t.Fatalf("unconditional set status=%d body=%s", first.Code, first.Body.String())
	}
	revision := decodePluginMetadata(t, first).IssueRevision

	// Current revision, as the ETag spells it: applies.
	current := put(first.Header().Get("ETag"), map[string]any{"value": "OPS-2"})
	if current.Code != http.StatusOK || decodePluginMetadata(t, current).IssueRevision != revision+1 {
		t.Fatalf("conditional set at the current revision status=%d body=%s", current.Code, current.Body.String())
	}

	// The revision the caller read before that write is now stale.
	stale := put(fmt.Sprintf(`W/"%d"`, revision), map[string]any{"value": "OPS-3"})
	if stale.Code != http.StatusConflict || decodePluginProblem(t, stale).Code != "revision_conflict" {
		t.Fatalf("stale set status=%d body=%s, want 409 revision_conflict", stale.Code, stale.Body.String())
	}
	// Stale stays stale even when the value it asks for is already stored.
	staleNoop := put("", map[string]any{"value": "OPS-2", "expected_revision": revision})
	if staleNoop.Code != http.StatusConflict {
		t.Fatalf("stale no-op set status=%d body=%s, want 409", staleNoop.Code, staleNoop.Body.String())
	}
	staleDelete := httptest.NewRecorder()
	deleteRequest := pluginActionRequest(http.MethodDelete, "/metadata", installationID, nil, pluginMetadataParams(issueID, "jira.key"))
	deleteRequest.Header.Set("If-Match", fmt.Sprintf(`W/"%d"`, revision))
	testHandler.DeletePluginIssueMetadata(staleDelete, deleteRequest)
	if staleDelete.Code != http.StatusConflict {
		t.Fatalf("stale delete status=%d body=%s, want 409", staleDelete.Code, staleDelete.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT metadata->>'jira.key' FROM issue WHERE id = $1`, issueID).Scan(&stored); err != nil {
		t.Fatalf("read stored metadata: %v", err)
	}
	if stored != "OPS-2" {
		t.Fatalf("stored value after refused writes = %q, want OPS-2", stored)
	}
	if got := len(published()); got != 2 {
		t.Fatalf("metadata_changed broadcasts = %d, want 2 (refused writes publish nothing)", got)
	}

	mismatch := put(fmt.Sprintf(`W/"%d"`, revision+1), map[string]any{"value": "OPS-4", "expected_revision": revision})
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("If-Match and expected_revision disagree: status=%d, want 400", mismatch.Code)
	}
}

// The If-Match check is enforced by the write itself, not only by the
// pre-check on the loaded issue: of two writers holding the same revision,
// exactly one wins.
func TestPluginIssueMetadataIfMatchIsAtomic(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata if-match race", "todo", "none")
	var revision int64
	if err := testPool.QueryRow(context.Background(), `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision); err != nil {
		t.Fatalf("read revision: %v", err)
	}

	const writers = 8
	codes := make(chan int, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recorder := httptest.NewRecorder()
			testHandler.SetPluginIssueMetadata(recorder, pluginActionRequest(http.MethodPut, "/metadata", installationID,
				map[string]any{"value": fmt.Sprintf("OPS-%d", i), "expected_revision": revision}, pluginMetadataParams(issueID, "jira.key")))
			codes <- recorder.Code
		}(i)
	}
	wg.Wait()
	close(codes)
	won := 0
	for code := range codes {
		switch code {
		case http.StatusOK:
			won++
		case http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if won != 1 {
		t.Fatalf("writers that won at revision %d = %d, want exactly 1", revision, won)
	}
}

func TestPluginIssueMetadataRequiresIssuesWrite(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata scope", "todo", "none")

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"set":    testHandler.SetPluginIssueMetadata,
		"delete": testHandler.DeletePluginIssueMetadata,
	} {
		recorder := httptest.NewRecorder()
		call(recorder, pluginActionRequest(http.MethodPut, "/metadata", installationID,
			map[string]any{"value": "x"}, pluginMetadataParams(issueID, "jira.key")))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s without issues:write status=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
		if problem := decodePluginProblem(t, recorder); !strings.Contains(problem.Detail, "issues:write") {
			t.Fatalf("%s refusal does not name the scope: %+v", name, problem)
		}
	}
}

func TestPluginIssueMetadataRejectsInvalidInput(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata validation", "todo", "none")

	cases := []struct {
		name string
		key  string
		body any
	}{
		{"key starts with a digit", "1jira", map[string]any{"value": "x"}},
		{"key contains a space", "jira key", map[string]any{"value": "x"}},
		{"key too long", strings.Repeat("k", 65), map[string]any{"value": "x"}},
		{"null value", "jira.key", map[string]any{"value": nil}},
		{"object value", "jira.key", map[string]any{"value": map[string]any{"a": 1}}},
		{"array value", "jira.key", map[string]any{"value": []string{"a"}}},
		{"missing value", "jira.key", map[string]any{}},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		testHandler.SetPluginIssueMetadata(recorder, pluginActionRequest(http.MethodPut, "/metadata", installationID,
			tc.body, pluginMetadataParams(issueID, tc.key)))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s, want 400", tc.name, recorder.Code, recorder.Body.String())
			continue
		}
		if problem := decodePluginProblem(t, recorder); problem.Code != "invalid_request" {
			t.Errorf("%s: problem code=%q", tc.name, problem.Code)
		}
	}

	recorder := httptest.NewRecorder()
	testHandler.DeletePluginIssueMetadata(recorder, pluginActionRequest(http.MethodDelete, "/metadata", installationID,
		nil, pluginMetadataParams(issueID, "-bad")))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("delete with invalid key status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginIssueMetadataEnforcesKeyAndSizeLimits(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata limits", "todo", "none")

	bag := make(map[string]any, maxIssueMetadataKeys)
	for i := 0; i < maxIssueMetadataKeys; i++ {
		bag[fmt.Sprintf("k%02d", i)] = i
	}
	full, _ := json.Marshal(bag)
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET metadata = $2 WHERE id = $1`, issueID, full); err != nil {
		t.Fatalf("seed full metadata: %v", err)
	}

	over := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(over, pluginActionRequest(http.MethodPut, "/metadata", installationID,
		map[string]any{"value": "x"}, pluginMetadataParams(issueID, "jira.key")))
	if over.Code != http.StatusBadRequest || !strings.Contains(decodePluginProblem(t, over).Detail, "50 keys") {
		t.Fatalf("51st key status=%d body=%s", over.Code, over.Body.String())
	}
	// Overwriting an existing key at the cap is still allowed.
	overwrite := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(overwrite, pluginActionRequest(http.MethodPut, "/metadata", installationID,
		map[string]any{"value": "updated"}, pluginMetadataParams(issueID, "k00")))
	if overwrite.Code != http.StatusOK {
		t.Fatalf("overwrite at cap status=%d body=%s", overwrite.Code, overwrite.Body.String())
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET metadata = '{}'::jsonb WHERE id = $1`, issueID); err != nil {
		t.Fatalf("reset metadata: %v", err)
	}
	tooBig := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(tooBig, pluginActionRequest(http.MethodPut, "/metadata", installationID,
		map[string]any{"value": strings.Repeat("x", 9000)}, pluginMetadataParams(issueID, "jira.description")))
	if tooBig.Code != http.StatusBadRequest || !strings.Contains(decodePluginProblem(t, tooBig).Detail, "8KB") {
		t.Fatalf("oversized value status=%d body=%s", tooBig.Code, tooBig.Body.String())
	}
}

// A callback grant issued about one issue reaches only that issue, for
// metadata writes as for everything else.
func TestPluginIssueMetadataCallbackGrantIsIssueScoped(t *testing.T) {
	withCallbackTokens(t)
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	scopedIssue := createPluginMetadataTestIssue(t, "Plugin metadata scoped issue", "todo", "none")
	otherIssue := createPluginMetadataTestIssue(t, "Plugin metadata other issue", "todo", "none")
	token := taskCallbackToken(t, installationID, scopedIssue)

	other := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(other, callbackRequest(token, http.MethodPut, "/v1/issues/"+otherIssue+"/metadata/jira.key",
		map[string]any{"value": "OPS-1"}, pluginMetadataParams(otherIssue, "jira.key")))
	if other.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope set status=%d body=%s, want 404", other.Code, other.Body.String())
	}
	otherDelete := httptest.NewRecorder()
	testHandler.DeletePluginIssueMetadata(otherDelete, callbackRequest(token, http.MethodDelete, "/v1/issues/"+otherIssue+"/metadata/jira.key",
		nil, pluginMetadataParams(otherIssue, "jira.key")))
	if otherDelete.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope delete status=%d body=%s, want 404", otherDelete.Code, otherDelete.Body.String())
	}

	scoped := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(scoped, callbackRequest(token, http.MethodPut, "/v1/issues/"+scopedIssue+"/metadata/jira.key",
		map[string]any{"value": "OPS-1"}, pluginMetadataParams(scopedIssue, "jira.key")))
	if scoped.Code != http.StatusOK {
		t.Fatalf("in-scope set status=%d body=%s", scoped.Code, scoped.Body.String())
	}
}

func TestPluginIssueMetadataCannotReachAnotherWorkspace(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	foreignIssue := createIssueInForeignWorkspace(t)

	recorder := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(recorder, pluginActionRequest(http.MethodPut, "/metadata", installationID,
		map[string]any{"value": "OPS-1"}, pluginMetadataParams(foreignIssue, "jira.key")))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace set status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var metadata []byte
	if err := testPool.QueryRow(context.Background(), `SELECT metadata FROM issue WHERE id = $1`, foreignIssue).Scan(&metadata); err != nil {
		t.Fatalf("read foreign metadata: %v", err)
	}
	if strings.Contains(string(metadata), "jira.key") {
		t.Fatalf("foreign issue metadata was written: %s", metadata)
	}

	deleted := httptest.NewRecorder()
	testHandler.DeletePluginIssueMetadata(deleted, pluginActionRequest(http.MethodDelete, "/metadata", installationID,
		nil, pluginMetadataParams(foreignIssue, "jira.key")))
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestPluginIssueMetadataStopsWhenTheInstallationIsDisabled(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createPluginMetadataTestIssue(t, "Plugin metadata disabled", "todo", "none")

	disable := httptest.NewRecorder()
	testHandler.DisablePlugin(disable, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil,
		map[string]string{"id": testWorkspaceID, "installationId": installationID}))
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	recorder := httptest.NewRecorder()
	testHandler.SetPluginIssueMetadata(recorder, pluginActionRequest(http.MethodPut, "/metadata", installationID,
		map[string]any{"value": "OPS-1"}, pluginMetadataParams(issueID, "jira.key")))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("disabled installation set status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
