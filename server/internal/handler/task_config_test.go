package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestValidateTaskConfigResourceRef(t *testing.T) {
	valid := json.RawMessage(`{"provider":"aws_secrets_manager","provider_ref":"secret-ref","version":"v1","path":"deploy/terraform/backend.hcl","mode":384,"repo":"repo","target":"main","account":"acct","region":"ap-southeast-2"}`)
	got, err := validateAndNormalizeResourceRef("task_config", valid)
	if err != nil {
		t.Fatalf("valid task_config: %v", err)
	}
	if strings.Contains(string(got), "unique-backend-sentinel") {
		t.Fatal("normalized resource contains secret sentinel")
	}
	for _, tc := range []struct {
		name string
		ref  string
	}{
		{"absolute path", `{"provider":"aws_secrets_manager","provider_ref":"r","version":"v1","path":"/tmp/backend.hcl","mode":384}`},
		{"unsafe path", `{"provider":"aws_secrets_manager","provider_ref":"r","version":"v1","path":"../backend.hcl","mode":384}`},
		{"wrong mode", `{"provider":"aws_secrets_manager","provider_ref":"r","version":"v1","path":"backend.hcl","mode":420}`},
		{"missing version", `{"provider":"aws_secrets_manager","provider_ref":"r","path":"backend.hcl","mode":384}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateAndNormalizeResourceRef("task_config", json.RawMessage(tc.ref)); err == nil {
				t.Fatal("accepted invalid task_config")
			}
		})
	}
}

func TestTaskConfigClaimSerializationDoesNotContainProviderBytes(t *testing.T) {
	ref := taskConfigRef{
		Provider:    "aws_secrets_manager",
		ProviderRef: "secret-ref",
		Version:     "v1",
		Path:        "deploy/terraform/backend.hcl",
		Mode:        0o600,
	}
	payload, err := json.Marshal(ProjectResourceData{ResourceType: "task_config", ResourceRef: mustMarshal(ref)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "unique-backend-sentinel") {
		t.Fatal("claim payload contains provider bytes")
	}
}

func TestTaskConfigResourceViewsStripUnknownSecretFields(t *testing.T) {
	ref := json.RawMessage(`{"provider":"aws_secrets_manager","provider_ref":"ref","version":"v1","path":"deploy/terraform/backend.hcl","mode":384,"repo":"repo","target":"main","account":"acct","region":"ap-southeast-2","value":"unique-backend-sentinel"}`)
	response := projectResourceToResponse(db.ProjectResource{ResourceType: "task_config", ResourceRef: ref})
	if strings.Contains(string(response.ResourceRef), "unique-backend-sentinel") {
		t.Fatal("API resource response contains an unknown secret field")
	}

	claim, _ := projectResourcesForClaim([]db.ProjectResource{{ResourceType: "task_config", ResourceRef: ref}})
	if len(claim) != 1 || strings.Contains(string(claim[0].ResourceRef), "unique-backend-sentinel") {
		t.Fatal("claim resource contains an unknown secret field")
	}
}

type fakeSecretsManager struct {
	value *secretsmanager.GetSecretValueOutput
	err   error
}

func (f fakeSecretsManager) GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return f.value, f.err
}

func TestSecretsManagerTaskConfigProviderReturnsBytesWithoutEchoingFailures(t *testing.T) {
	request := TaskConfigResolveRequest{WorkspaceID: "workspace", TaskID: "task", RuntimeID: "runtime", AgentID: "agent", Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1", Selectors: TaskConfigSelectors{Repo: "repo", Target: "main", Account: "acct", Region: "ap-southeast-2"}}
	provider := NewSecretsManagerTaskConfigProvider(fakeSecretsManager{value: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("unique-backend-sentinel")}})
	got, err := provider.Resolve(context.Background(), request)
	if err != nil || string(got) != "unique-backend-sentinel" {
		t.Fatalf("Resolve() = %q, %v", got, err)
	}
	failing := NewSecretsManagerTaskConfigProvider(fakeSecretsManager{err: errors.New("sentinel provider error")})
	_, err = failing.Resolve(context.Background(), request)
	if err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("provider error = %v, expected stable redacted error", err)
	}
	for name, bad := range map[string]TaskConfigResolveRequest{
		"missing workspace": func() TaskConfigResolveRequest { r := request; r.WorkspaceID = ""; return r }(),
		"missing task":      func() TaskConfigResolveRequest { r := request; r.TaskID = ""; return r }(),
		"missing runtime":   func() TaskConfigResolveRequest { r := request; r.RuntimeID = ""; return r }(),
		"missing agent":     func() TaskConfigResolveRequest { r := request; r.AgentID = ""; return r }(),
		"missing selector":  func() TaskConfigResolveRequest { r := request; r.Selectors.Region = ""; return r }(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.Resolve(context.Background(), bad); err == nil {
				t.Fatal("provider accepted an unbound task identity")
			}
		})
	}
}

func TestTaskConfigProviderRefRequiresConfiguredAllowlist(t *testing.T) {
	const allowed = "arn:aws:secretsmanager:ap-southeast-2:123456789012:secret:multica/task-config/"
	for _, tc := range []struct {
		name   string
		ref    string
		prefix []string
		want   bool
	}{
		{name: "approved prefix", ref: allowed + "project/backend", prefix: []string{allowed}, want: true},
		{name: "arbitrary secret", ref: "arn:aws:secretsmanager:ap-southeast-2:123456789012:secret:unrelated", prefix: []string{allowed}},
		{name: "empty allowlist", ref: allowed + "project/backend", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskConfigProviderRefAllowed(tc.ref, tc.prefix); got != tc.want {
				t.Fatalf("taskConfigProviderRefAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskConfigResolveActorRequiresDaemonIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, daemon, runtime string
		want                  bool
	}{
		{name: "matching daemon", daemon: "daemon-1", runtime: "daemon-1", want: true},
		{name: "member auth has no daemon", daemon: "", runtime: "daemon-1"},
		{name: "wrong daemon", daemon: "daemon-2", runtime: "daemon-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskConfigResolveActorAllowed(tc.daemon, tc.runtime); got != tc.want {
				t.Fatalf("taskConfigResolveActorAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskConfigResolvePayloadRequiresCompleteBinding(t *testing.T) {
	binding := taskConfigRef{Provider: "aws_secrets_manager", ProviderRef: "approved/ref", Version: "v1", Path: "deploy/terraform/backend.hcl", Mode: 0o600, Repo: "repo", Target: "main", Account: "acct", Region: "ap-southeast-2"}
	payload := taskConfigResolvePayload{Provider: binding.Provider, ProviderRef: binding.ProviderRef, Version: binding.Version, Path: binding.Path, Mode: binding.Mode, Selectors: TaskConfigSelectors{Repo: binding.Repo, Target: binding.Target, Account: binding.Account, Region: binding.Region}}
	if !taskConfigResolvePayloadMatches(payload, binding, payload.Selectors) {
		t.Fatal("matching task_config payload was rejected")
	}
	payload.Selectors.Region = "wrong-region"
	if taskConfigResolvePayloadMatches(payload, binding, TaskConfigSelectors{Repo: binding.Repo, Target: binding.Target, Account: binding.Account, Region: binding.Region}) {
		t.Fatal("selector mismatch was accepted")
	}
}

func TestTaskConfigResolveTaskGateBindsWorkspaceRuntimeAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, taskWorkspace, runtimeWorkspace, taskRuntime, runtime, status string
		want                                                                bool
	}{
		{name: "preparing matching task", taskWorkspace: "ws", runtimeWorkspace: "ws", taskRuntime: "rt", runtime: "rt", status: "dispatched", want: true},
		{name: "wrong workspace", taskWorkspace: "other", runtimeWorkspace: "ws", taskRuntime: "rt", runtime: "rt", status: "dispatched"},
		{name: "wrong runtime", taskWorkspace: "ws", runtimeWorkspace: "ws", taskRuntime: "other", runtime: "rt", status: "dispatched"},
		{name: "completed task", taskWorkspace: "ws", runtimeWorkspace: "ws", taskRuntime: "rt", runtime: "rt", status: "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := taskConfigResolveTaskIdentityAllowed(tc.taskWorkspace, tc.runtimeWorkspace, tc.taskRuntime, tc.runtime) && taskConfigResolveStatusAllowed(tc.status)
			if got != tc.want {
				t.Fatalf("task gate = %v, want %v", got, tc.want)
			}
		})
	}
}

type recordingTaskConfigProvider struct {
	content []byte
	err     error
	calls   int
}

func (p *recordingTaskConfigProvider) Resolve(context.Context, TaskConfigResolveRequest) ([]byte, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return append([]byte(nil), p.content...), nil
}

// TestResolveTaskConfigEndpointUsesDatabaseAndDaemonBinding exercises the
// actual HTTP handler against rows in the test database. The helper tests
// above intentionally remain useful when PostgreSQL is unavailable; this test
// covers the DB-backed authorization and binding checks that must protect the
// only endpoint that returns provider bytes.
func TestResolveTaskConfigEndpointUsesDatabaseAndDaemonBinding(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	const (
		d         = "task-config-endpoint-daemon"
		secret    = "unique-task-config-endpoint-sentinel"
		refPrefix = "approved/task-config/"
	)
	fixture := testutil.New(testPool, testWorkspaceID, testUserID)
	projectID := fixture.Project(t, "task config endpoint project")
	issueID := fixture.Issue(t, "task config endpoint issue", testutil.Cols{"project_id": projectID})
	runtimeID := fixture.Runtime(t, "task config endpoint runtime", testutil.Cols{"daemon_id": d})
	otherRuntimeID := fixture.Runtime(t, "task config endpoint other runtime", testutil.Cols{"daemon_id": "task-config-endpoint-other-daemon"})
	agentID := fixture.Agent(t, "task config endpoint agent", runtimeID)
	taskID := fixture.Task(t, agentID, testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
		"status":     "dispatched",
	})
	ref := taskConfigRef{
		Provider:    "aws_secrets_manager",
		ProviderRef: refPrefix + uuid.NewString(),
		Version:     "version-1",
		Path:        "deploy/terraform/backend.hcl",
		Mode:        0o600,
		Repo:        "github.com/example/infrastructure",
		Target:      "main",
		Account:     "123456789012",
		Region:      "ap-southeast-2",
	}
	refJSON, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal task_config ref: %v", err)
	}
	resourceID := fixture.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "task_config",
		"resource_ref":  refJSON,
		"created_by":    testUserID,
	})

	provider := &recordingTaskConfigProvider{content: []byte(secret)}
	h := *testHandler
	h.TaskConfigProvider = provider
	h.cfg.TaskConfigProviderRefPrefixes = []string{refPrefix}

	selectors := TaskConfigSelectors{Repo: ref.Repo, Target: ref.Target, Account: ref.Account, Region: ref.Region}
	requestBody, err := json.Marshal(taskConfigResolvePayload{
		Provider:    ref.Provider,
		ProviderRef: ref.ProviderRef,
		Version:     ref.Version,
		Path:        ref.Path,
		Mode:        ref.Mode,
		Selectors:   selectors,
	})
	if err != nil {
		t.Fatalf("marshal resolve request: %v", err)
	}
	call := func(routeRuntimeID, daemon string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/"+routeRuntimeID+"/tasks/"+taskID+"/configs/"+resourceID+"/resolve", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withURLParams(req, "runtimeId", routeRuntimeID, "taskId", taskID, "resourceId", resourceID)
		req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, daemon))
		w := httptest.NewRecorder()
		h.ResolveTaskConfig(w, req)
		return w
	}

	positive := call(runtimeID, d, requestBody)
	if positive.Code != http.StatusOK || positive.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("positive resolve = %d %q, want octet-stream 200", positive.Code, positive.Body.String())
	}
	if positive.Body.String() != secret {
		t.Fatalf("positive resolve body = %q, want provider bytes", positive.Body.String())
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls after positive resolve = %d, want 1", provider.calls)
	}

	if got := call(runtimeID, "", requestBody); got.Code != http.StatusNotFound {
		t.Fatalf("member-auth resolve status = %d, want 404", got.Code)
	}
	if got := call(runtimeID, "different-daemon", requestBody); got.Code != http.StatusNotFound {
		t.Fatalf("wrong-daemon resolve status = %d, want 404", got.Code)
	}
	wrongSelectors := append([]byte(nil), requestBody...)
	var payload taskConfigResolvePayload
	if err := json.Unmarshal(wrongSelectors, &payload); err != nil {
		t.Fatalf("decode request copy: %v", err)
	}
	payload.Selectors.Region = "us-east-1"
	wrongSelectors, err = json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal mismatch request: %v", err)
	}
	if got := call(runtimeID, d, wrongSelectors); got.Code != http.StatusForbidden {
		t.Fatalf("selector-mismatch resolve status = %d, want 403", got.Code)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls after rejected requests = %d, want 1", provider.calls)
	}
	if got := call(otherRuntimeID, "task-config-endpoint-other-daemon", requestBody); got.Code != http.StatusNotFound {
		t.Fatalf("cross-runtime resolve status = %d, want 404", got.Code)
	}
	if got := call(runtimeID, d, []byte("{}")); got.Code != http.StatusForbidden && got.Code != http.StatusBadRequest {
		t.Fatalf("malformed binding payload status = %d, want request rejection", got.Code)
	}

	provider.err = errors.New("upstream unavailable")
	if got := call(runtimeID, d, requestBody); got.Code != http.StatusBadGateway || strings.Contains(got.Body.String(), secret) {
		t.Fatalf("provider-error resolve = %d %q, want redacted 502", got.Code, got.Body.String())
	}
	provider.err = nil
	h.TaskConfigProvider = nil
	if got := call(runtimeID, d, requestBody); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("provider-unavailable resolve status = %d, want 503", got.Code)
	}
	h.TaskConfigProvider = provider
	fixture.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, taskID)
	if got := call(runtimeID, d, requestBody); got.Code != http.StatusConflict {
		t.Fatalf("completed-task resolve status = %d, want 409", got.Code)
	}

}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
