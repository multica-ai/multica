package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

func TestFeishuDocumentsConnectionPinsExactlySixTools(t *testing.T) {
	updatedAt := time.Unix(1_700_000_000, 123)
	installation := lark.Installation{
		ID:        util.MustParseUUID("30000000-0000-4000-8000-000000000001"),
		UpdatedAt: pgtype.Timestamptz{Time: updatedAt, Valid: true},
	}
	connection, err := FeishuDocumentsConnection("https://agents.example.com", "task-id", installation)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{
		"feishu_docs_append",
		"feishu_docs_delete_block",
		"feishu_docs_fetch",
		"feishu_docs_insert_after",
		"feishu_docs_replace_block",
		"feishu_docs_replace_text",
	}
	gotNames := make([]string, 0, len(connection.ApprovedTools))
	for _, tool := range connection.ApprovedTools {
		gotNames = append(gotNames, tool.Name)
		if tool.SchemaDigest != remotemcp.DigestBytes(tool.InputSchema) {
			t.Fatalf("schema for %s is not pinned", tool.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("schema %s: %v", tool.Name, err)
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("schema %s permits additional properties", tool.Name)
		}
		if !strings.Contains(strings.ToLower(tool.Description), "bot") {
			t.Fatalf("description %s does not state bot identity", tool.Name)
		}
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("tools = %v, want %v", gotNames, wantNames)
	}
	wantContribution := "builtin:feishu-documents:" + util.UUIDToString(installation.ID) + ":" + strconv.FormatInt(updatedAt.UnixNano(), 10)
	if connection.ContributionID != wantContribution || connection.ContributionKey != "feishu-documents" {
		t.Fatalf("contribution = %q key=%q", connection.ContributionID, connection.ContributionKey)
	}
	if connection.Endpoint != "https://agents.example.com/api/daemon/tasks/task-id/feishu-documents/"+wantContribution+"/mcp" {
		t.Fatalf("endpoint = %q", connection.Endpoint)
	}
	if connection.CredentialHeader != "Authorization" || !reflect.DeepEqual(connection.EndpointAllowedHosts, []string{"agents.example.com"}) {
		t.Fatalf("credential/hosts = %q/%v", connection.CredentialHeader, connection.EndpointAllowedHosts)
	}
	wantDigest, err := remotemcp.ToolSetDigest(connection.ApprovedTools)
	if err != nil {
		t.Fatal(err)
	}
	if connection.ToolSchemaDigest != wantDigest {
		t.Fatalf("tool set digest = %q, want %q", connection.ToolSchemaDigest, wantDigest)
	}
}

func TestFeishuDocumentsConnectionRequiresHTTPSOrigin(t *testing.T) {
	installation := lark.Installation{
		ID:        util.MustParseUUID("30000000-0000-4000-8000-000000000001"),
		UpdatedAt: pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 123), Valid: true},
	}
	for _, publicURL := range []string{
		"http://agents.example.com",
		"https://user@agents.example.com",
		"https://agents.example.com/base",
		"https://agents.example.com?region=cn",
		"https://agents.example.com#fragment",
	} {
		_, err := FeishuDocumentsConnection(publicURL, "task-id", installation)
		if err == nil {
			t.Fatalf("accepted non-origin public URL %q", publicURL)
		}
	}
}

func TestParseFeishuDocumentsContributionRejectsMalformedValues(t *testing.T) {
	for _, raw := range []string{
		"",
		"plugin:30000000-0000-4000-8000-000000000001:1",
		"builtin:feishu-documents:bad:1",
		"builtin:feishu-documents:30000000-0000-4000-8000-000000000001:0",
		"builtin:feishu-documents:30000000-0000-4000-8000-000000000001:1:extra",
	} {
		if _, _, ok := parseFeishuDocumentsContribution(raw); ok {
			t.Fatalf("accepted malformed contribution %q", raw)
		}
	}
}

var (
	feishuDocumentsTestWorkspaceID    = util.MustParseUUID("10000000-0000-4000-8000-000000000001")
	feishuDocumentsTestTaskID         = util.MustParseUUID("20000000-0000-4000-8000-000000000001")
	feishuDocumentsTestAgentID        = util.MustParseUUID("30000000-0000-4000-8000-000000000001")
	feishuDocumentsTestRuntimeID      = util.MustParseUUID("40000000-0000-4000-8000-000000000001")
	feishuDocumentsTestInstallationID = util.MustParseUUID("50000000-0000-4000-8000-000000000001")
)

const feishuDocumentsTestRevision int64 = 1_700_000_000_000_000_123

func validFeishuDocumentsScopeFixture() feishuDocumentsScope {
	return feishuDocumentsScope{
		Task: db.AgentTaskQueue{
			ID: feishuDocumentsTestTaskID, AgentID: feishuDocumentsTestAgentID,
			RuntimeID: feishuDocumentsTestRuntimeID, Status: "running",
		},
		Runtime: db.AgentRuntime{
			ID: feishuDocumentsTestRuntimeID, WorkspaceID: feishuDocumentsTestWorkspaceID,
			DaemonID: pgtype.Text{String: "daemon-a", Valid: true},
		},
		Agent: db.Agent{
			ID: feishuDocumentsTestAgentID, WorkspaceID: feishuDocumentsTestWorkspaceID,
			RuntimeID: feishuDocumentsTestRuntimeID,
		},
		Installation: lark.Installation{
			ID: feishuDocumentsTestInstallationID, WorkspaceID: feishuDocumentsTestWorkspaceID,
			AgentID: feishuDocumentsTestAgentID, Status: "active",
			UpdatedAt: pgtype.Timestamptz{Time: time.Unix(0, feishuDocumentsTestRevision), Valid: true},
		},
		WorkspaceID: feishuDocumentsTestWorkspaceID,
		DaemonID:    "daemon-a",
	}
}

func TestValidateFeishuDocumentsScopeRejectsCrossIdentityAndStaleCapabilities(t *testing.T) {
	valid := validFeishuDocumentsScopeFixture()
	if !validateFeishuDocumentsScope(valid, feishuDocumentsTestInstallationID, feishuDocumentsTestRevision) {
		t.Fatal("valid task capability was rejected")
	}

	tests := map[string]func(*feishuDocumentsScope){
		"wrong daemon":   func(s *feishuDocumentsScope) { s.DaemonID = "daemon-b" },
		"ended task":     func(s *feishuDocumentsScope) { s.Task.Status = "completed" },
		"cancelled task": func(s *feishuDocumentsScope) { s.Task.Status = "cancelled" },
		"different task runtime": func(s *feishuDocumentsScope) {
			s.Task.RuntimeID = util.MustParseUUID("40000000-0000-4000-8000-000000000002")
		},
		"different runtime daemon": func(s *feishuDocumentsScope) { s.Runtime.DaemonID.String = "daemon-b" },
		"different runtime workspace": func(s *feishuDocumentsScope) {
			s.Runtime.WorkspaceID = util.MustParseUUID("10000000-0000-4000-8000-000000000002")
		},
		"rebound agent": func(s *feishuDocumentsScope) {
			s.Agent.RuntimeID = util.MustParseUUID("40000000-0000-4000-8000-000000000002")
		},
		"archived agent": func(s *feishuDocumentsScope) { s.Agent.ArchivedAt.Valid = true },
		"other agent installation": func(s *feishuDocumentsScope) {
			s.Installation.AgentID = util.MustParseUUID("30000000-0000-4000-8000-000000000002")
		},
		"revoked installation": func(s *feishuDocumentsScope) { s.Installation.Status = "revoked" },
		"changed revision": func(s *feishuDocumentsScope) {
			s.Installation.UpdatedAt.Time = s.Installation.UpdatedAt.Time.Add(time.Nanosecond)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			scope := valid
			mutate(&scope)
			if validateFeishuDocumentsScope(scope, feishuDocumentsTestInstallationID, feishuDocumentsTestRevision) {
				t.Fatal("stale or cross-identity capability was accepted")
			}
		})
	}
}

type fakeFeishuDocumentExecutor struct {
	called    int
	operation lark.DocumentOperation
	input     lark.DocumentToolInput
	result    lark.DocumentToolResult
	toolErr   *lark.DocumentToolError
}

func (f *fakeFeishuDocumentExecutor) Execute(_ context.Context, _ lark.DocumentTaskScope, operation lark.DocumentOperation, input lark.DocumentToolInput) (lark.DocumentToolResult, *lark.DocumentToolError) {
	f.called++
	f.operation = operation
	f.input = input
	return f.result, f.toolErr
}

func feishuDocumentsHandlerRequest(method, body, authPath string) *http.Request {
	request := httptest.NewRequest(method, "/mcp", strings.NewReader(body))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", util.UUIDToString(feishuDocumentsTestTaskID))
	routeContext.URLParams.Add("contributionId", feishuDocumentsContributionPrefix+util.UUIDToString(feishuDocumentsTestInstallationID)+":"+strconv.FormatInt(feishuDocumentsTestRevision, 10))
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	if authPath == middleware.DaemonAuthPathDaemonToken {
		ctx = middleware.WithDaemonContext(ctx, util.UUIDToString(feishuDocumentsTestWorkspaceID), "daemon-a")
	}
	request = request.WithContext(ctx)
	request.Header.Set("Authorization", "Bearer mdt_task_capability")
	return request
}

func newFeishuDocumentsTestHandler(executor feishuDocumentExecutor) *Handler {
	return &Handler{
		LarkDocuments: executor,
		feishuDocumentsScopeLoader: func(context.Context, *http.Request, string, string) (feishuDocumentsScope, error) {
			return validFeishuDocumentsScopeFixture(), nil
		},
	}
}

func decodeFeishuDocumentsRPC(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return response
}

func TestFeishuDocumentsMCPAcceptsInitializeListAndPinnedCall(t *testing.T) {
	executor := &fakeFeishuDocumentExecutor{result: lark.DocumentToolResult{RevisionID: 12, Content: "hello", Verified: true}}
	h := newFeishuDocumentsTestHandler(executor)

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"feishu_docs_fetch","arguments":{"document_url":"https://example.feishu.cn/docx/AbCdEfGh1234"}}}`,
	}
	for _, body := range requests {
		recorder := httptest.NewRecorder()
		h.ServeFeishuDocumentsMCP(recorder, feishuDocumentsHandlerRequest(http.MethodPost, body, middleware.DaemonAuthPathDaemonToken))
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %s: status=%d body=%s", body, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(body, "notifications/initialized") {
			continue
		}
		response := decodeFeishuDocumentsRPC(t, recorder)
		if response["error"] != nil {
			t.Fatalf("request %s: response=%v", body, response)
		}
	}
	if executor.called != 1 || executor.operation != lark.DocumentOperationFetch || executor.input.DocumentURL == "" {
		t.Fatalf("executor called=%d operation=%q input=%+v", executor.called, executor.operation, executor.input)
	}
}

func TestFeishuDocumentsMCPStrictlyRejectsUnknownMethodsToolsAndFields(t *testing.T) {
	executor := &fakeFeishuDocumentExecutor{}
	h := newFeishuDocumentsTestHandler(executor)
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not_allowed","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feishu_docs_fetch","arguments":{"document_url":"https://example.feishu.cn/docx/AbCdEfGh1234","content":"must not pass"}}}`,
		`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","extra":true}`,
	}
	for _, body := range requests {
		recorder := httptest.NewRecorder()
		h.ServeFeishuDocumentsMCP(recorder, feishuDocumentsHandlerRequest(http.MethodPost, body, middleware.DaemonAuthPathDaemonToken))
		response := decodeFeishuDocumentsRPC(t, recorder)
		if response["error"] == nil {
			t.Fatalf("request %s was accepted: %s", body, recorder.Body.String())
		}
	}
	if executor.called != 0 {
		t.Fatalf("executor was called %d times for rejected requests", executor.called)
	}
}

func TestFeishuDocumentsMCPRejectsNonDaemonAuthAndCapabilityFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		authPath string
		loader   feishuDocumentsScopeLoader
	}{
		{name: "human token", authPath: middleware.DaemonAuthPathPAT},
		{name: "stale capability", authPath: middleware.DaemonAuthPathDaemonToken, loader: func(context.Context, *http.Request, string, string) (feishuDocumentsScope, error) {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newFeishuDocumentsTestHandler(&fakeFeishuDocumentExecutor{})
			if tc.loader != nil {
				h.feishuDocumentsScopeLoader = tc.loader
			}
			recorder := httptest.NewRecorder()
			h.ServeFeishuDocumentsMCP(recorder, feishuDocumentsHandlerRequest(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, tc.authPath))
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "capability_denied") {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestResolveRemoteMCPCredentialReturnsOnlyExistingTaskToken(t *testing.T) {
	h := newFeishuDocumentsTestHandler(&fakeFeishuDocumentExecutor{})
	recorder := httptest.NewRecorder()
	h.ResolveRemoteMCPCredential(recorder, feishuDocumentsHandlerRequest(http.MethodGet, "", middleware.DaemonAuthPathDaemonToken))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		CredentialHeader string `json:"credential_header"`
		Credential       string `json:"credential"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CredentialHeader != "Authorization" || response.Credential != "Bearer mdt_task_capability" {
		t.Fatalf("response=%+v", response)
	}
	for _, secret := range []string{"pikachu-app-secret", "pikachu-app-id"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("credential response leaked %q", secret)
		}
	}
}

type mutableFeishuDocumentStore struct {
	installations map[string]lark.Installation
}

func (s *mutableFeishuDocumentStore) GetActiveLarkInstallationForAgent(_ context.Context, workspaceID, agentID pgtype.UUID) (lark.Installation, error) {
	installation, ok := s.installations[util.UUIDToString(agentID)]
	if !ok || installation.WorkspaceID != workspaceID || installation.Status != "active" {
		return lark.Installation{}, pgx.ErrNoRows
	}
	return installation, nil
}

type mappedFeishuDocumentCredentials struct {
	secrets map[string]string
}

func (c mappedFeishuDocumentCredentials) DecryptAppSecret(installation lark.Installation) (string, error) {
	secret, ok := c.secrets[util.UUIDToString(installation.ID)]
	if !ok {
		return "", io.EOF
	}
	return secret, nil
}

type recordingFeishuDocumentClient struct {
	credentials []lark.InstallationCredentials
}

func (c *recordingFeishuDocumentClient) FetchDocument(_ context.Context, credentials lark.InstallationCredentials, _ lark.DocumentFetchParams) (lark.DocumentSnapshot, error) {
	c.credentials = append(c.credentials, credentials)
	return lark.DocumentSnapshot{RevisionID: int64(len(c.credentials)), Content: "bounded content"}, nil
}

func (c *recordingFeishuDocumentClient) UpdateDocument(_ context.Context, credentials lark.InstallationCredentials, _ lark.DocumentUpdateParams) (lark.DocumentUpdateResult, error) {
	c.credentials = append(c.credentials, credentials)
	return lark.DocumentUpdateResult{RevisionID: int64(len(c.credentials))}, nil
}

type feishuDocumentSecurityFixture struct {
	handler       *Handler
	store         *mutableFeishuDocumentStore
	client        *recordingFeishuDocumentClient
	tasks         map[string]db.AgentTaskQueue
	agents        map[string]db.Agent
	workspaceID   pgtype.UUID
	runtimeID     pgtype.UUID
	pikachuTaskID pgtype.UUID
	maimaiTaskID  pgtype.UUID
}

func newFeishuDocumentSecurityFixture() *feishuDocumentSecurityFixture {
	workspaceID := util.MustParseUUID("10000000-0000-4000-8000-000000000101")
	runtimeID := util.MustParseUUID("40000000-0000-4000-8000-000000000101")
	pikachuID := util.MustParseUUID("30000000-0000-4000-8000-000000000101")
	maimaiID := util.MustParseUUID("30000000-0000-4000-8000-000000000102")
	pikachuTaskID := util.MustParseUUID("20000000-0000-4000-8000-000000000101")
	maimaiTaskID := util.MustParseUUID("20000000-0000-4000-8000-000000000102")
	pikachuInstallation := lark.Installation{
		ID: util.MustParseUUID("50000000-0000-4000-8000-000000000101"), WorkspaceID: workspaceID, AgentID: pikachuID,
		AppID: "cli_pikachu", Status: "active", Region: string(lark.RegionFeishu),
		UpdatedAt: pgtype.Timestamptz{Time: time.Unix(0, 1_700_000_000_000_000_101), Valid: true},
	}
	maimaiInstallation := lark.Installation{
		ID: util.MustParseUUID("50000000-0000-4000-8000-000000000102"), WorkspaceID: workspaceID, AgentID: maimaiID,
		AppID: "cli_maimai", Status: "active", Region: string(lark.RegionFeishu),
		UpdatedAt: pgtype.Timestamptz{Time: time.Unix(0, 1_700_000_000_000_000_102), Valid: true},
	}
	store := &mutableFeishuDocumentStore{installations: map[string]lark.Installation{
		util.UUIDToString(pikachuID): pikachuInstallation,
		util.UUIDToString(maimaiID):  maimaiInstallation,
	}}
	client := &recordingFeishuDocumentClient{}
	service := lark.NewDocumentService(store, mappedFeishuDocumentCredentials{secrets: map[string]string{
		util.UUIDToString(pikachuInstallation.ID): "pikachu-secret",
		util.UUIDToString(maimaiInstallation.ID):  "maimai-secret",
	}}, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fixture := &feishuDocumentSecurityFixture{
		store: store, client: client, workspaceID: workspaceID, runtimeID: runtimeID,
		pikachuTaskID: pikachuTaskID, maimaiTaskID: maimaiTaskID,
		tasks: map[string]db.AgentTaskQueue{
			util.UUIDToString(pikachuTaskID): {ID: pikachuTaskID, AgentID: pikachuID, RuntimeID: runtimeID, Status: "running"},
			util.UUIDToString(maimaiTaskID):  {ID: maimaiTaskID, AgentID: maimaiID, RuntimeID: runtimeID, Status: "running"},
		},
		agents: map[string]db.Agent{
			util.UUIDToString(pikachuID): {ID: pikachuID, WorkspaceID: workspaceID, RuntimeID: runtimeID},
			util.UUIDToString(maimaiID):  {ID: maimaiID, WorkspaceID: workspaceID, RuntimeID: runtimeID},
		},
	}
	fixture.handler = &Handler{LarkDocuments: service}
	fixture.handler.feishuDocumentsScopeLoader = func(_ context.Context, _ *http.Request, taskID, _ string) (feishuDocumentsScope, error) {
		task, ok := fixture.tasks[taskID]
		if !ok {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}
		agent := fixture.agents[util.UUIDToString(task.AgentID)]
		installation, err := fixture.store.GetActiveLarkInstallationForAgent(context.Background(), fixture.workspaceID, task.AgentID)
		if err != nil {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}
		return feishuDocumentsScope{
			Task: task,
			Runtime: db.AgentRuntime{
				ID: fixture.runtimeID, WorkspaceID: fixture.workspaceID,
				DaemonID: pgtype.Text{String: "daemon-shared", Valid: true},
			},
			Agent: agent, Installation: installation,
			WorkspaceID: fixture.workspaceID, DaemonID: "daemon-shared",
		}, nil
	}
	return fixture
}

func (f *feishuDocumentSecurityFixture) contribution(t *testing.T, agentID pgtype.UUID) string {
	t.Helper()
	installation := f.store.installations[util.UUIDToString(agentID)]
	connection, err := FeishuDocumentsConnection("https://agents.example.com", "task", installation)
	if err != nil {
		t.Fatal(err)
	}
	return connection.ContributionID
}

func (f *feishuDocumentSecurityFixture) callFetch(taskID pgtype.UUID, contribution string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feishu_docs_fetch","arguments":{"document_url":"https://example.feishu.cn/docx/AbCdEfGh1234"}}}`))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", util.UUIDToString(taskID))
	routeContext.URLParams.Add("contributionId", contribution)
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = middleware.WithDaemonContext(ctx, util.UUIDToString(f.workspaceID), "daemon-shared")
	request = request.WithContext(ctx)
	request.Header.Set("Authorization", "Bearer mdt_security_fixture")
	recorder := httptest.NewRecorder()
	f.handler.ServeFeishuDocumentsMCP(recorder, request)
	return recorder
}

func TestFeishuDocumentsEndToEndKeepsBotIdentityAndRevocationLive(t *testing.T) {
	fixture := newFeishuDocumentSecurityFixture()
	pikachuAgentID := fixture.tasks[util.UUIDToString(fixture.pikachuTaskID)].AgentID
	maimaiAgentID := fixture.tasks[util.UUIDToString(fixture.maimaiTaskID)].AgentID
	pikachuContribution := fixture.contribution(t, pikachuAgentID)
	maimaiContribution := fixture.contribution(t, maimaiAgentID)

	for _, call := range []struct {
		task         pgtype.UUID
		contribution string
	}{
		{fixture.pikachuTaskID, pikachuContribution},
		{fixture.maimaiTaskID, maimaiContribution},
	} {
		response := fixture.callFetch(call.task, call.contribution)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("valid call failed or leaked a secret: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if len(fixture.client.credentials) != 2 ||
		fixture.client.credentials[0].AppID != "cli_pikachu" || fixture.client.credentials[0].AppSecret != "pikachu-secret" ||
		fixture.client.credentials[1].AppID != "cli_maimai" || fixture.client.credentials[1].AppSecret != "maimai-secret" {
		t.Fatalf("document credentials crossed Agent identity: %+v", fixture.client.credentials)
	}

	swapped := fixture.callFetch(fixture.pikachuTaskID, maimaiContribution)
	if swapped.Code != http.StatusForbidden || !strings.Contains(swapped.Body.String(), "capability_denied") {
		t.Fatalf("swapped contribution was not refused: status=%d body=%s", swapped.Code, swapped.Body.String())
	}
	pikachuInstallation := fixture.store.installations[util.UUIDToString(pikachuAgentID)]
	pikachuInstallation.UpdatedAt.Time = pikachuInstallation.UpdatedAt.Time.Add(time.Nanosecond)
	fixture.store.installations[util.UUIDToString(pikachuAgentID)] = pikachuInstallation
	stale := fixture.callFetch(fixture.pikachuTaskID, pikachuContribution)
	if stale.Code != http.StatusForbidden || !strings.Contains(stale.Body.String(), "capability_denied") {
		t.Fatalf("changed installation revision was not live-revoked: status=%d body=%s", stale.Code, stale.Body.String())
	}
	maimaiTask := fixture.tasks[util.UUIDToString(fixture.maimaiTaskID)]
	maimaiTask.Status = "completed"
	fixture.tasks[util.UUIDToString(fixture.maimaiTaskID)] = maimaiTask
	ended := fixture.callFetch(fixture.maimaiTaskID, maimaiContribution)
	if ended.Code != http.StatusForbidden || !strings.Contains(ended.Body.String(), "capability_denied") {
		t.Fatalf("completed task retained document capability: status=%d body=%s", ended.Code, ended.Body.String())
	}
}
