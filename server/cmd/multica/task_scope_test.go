package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/taskauth"
)

func bindTaskScopeTestAuthority(cmd *cobra.Command) {
	valueOr := func(name, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
		return fallback
	}
	withTaskAuthority(cmd, taskauth.Authority{
		ManagedBy:   taskauth.ManagedBy,
		Version:     taskauth.Version,
		ServerURL:   strings.TrimRight(valueOr("MULTICA_SERVER_URL", "https://api.example.test"), "/"),
		WorkspaceID: valueOr("MULTICA_WORKSPACE_ID", taskScopedCLIWorkspaceID),
		Token:       valueOr("MULTICA_TOKEN", "mat_contract_test"),
		TaskID:      valueOr("MULTICA_TASK_ID", taskScopedCLITaskID),
		AgentID:     valueOr("MULTICA_AGENT_ID", taskScopedCLIAgentID),
	})
}

func newTaskScopeTestCommand(path []string, ran *bool) (*cobra.Command, error) {
	root := &cobra.Command{
		Use:               "multica",
		SilenceErrors:     true,
		SilenceUsage:      true,
		PersistentPreRunE: enforceTaskScopedCLI,
	}
	root.PersistentFlags().String("server-url", "", "")
	root.PersistentFlags().String("workspace-id", "", "")
	root.PersistentFlags().String("profile", "", "")

	parent := root
	for i, name := range path {
		command := &cobra.Command{Use: name}
		if i == len(path)-1 {
			command.RunE = func(*cobra.Command, []string) error {
				*ran = true
				return nil
			}
		}
		parent.AddCommand(command)
		parent = command
	}
	if len(path) == 0 {
		root.RunE = func(*cobra.Command, []string) error {
			*ran = true
			return nil
		}
	}
	return root, nil
}

func executeTaskScopeTestCommand(t *testing.T, path []string, extraArgs ...string) (bool, error) {
	t.Helper()
	ran := false
	root, err := newTaskScopeTestCommand(path, &ran)
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	root.SetArgs(append(extraArgs, path...))
	if managedTaskSignalPresent() {
		bindTaskScopeTestAuthority(root)
	}
	err = root.Execute()
	return ran, err
}

func clearTaskScopeContext(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MULTICA_AGENT_ID",
		"MULTICA_TASK_ID",
		"MULTICA_TOKEN",
		"MULTICA_DAEMON_PORT",
	} {
		t.Setenv(key, "")
	}
}

func TestTaskScopedCLIAllowsOnlyBoundedIssueCommands(t *testing.T) {
	clearTaskScopeContext(t)
	t.Setenv("MULTICA_TOKEN", "mat_task_token")

	allowed := [][]string{
		{"issue", "get"},
		{"issue", "comment", "list"},
		{"issue", "comment", "add"},
		{"issue", "status"},
		{"issue", "run-messages"},
		{"repo", "checkout"},
	}
	for _, path := range allowed {
		path := path
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			ran, err := executeTaskScopeTestCommand(t, path)
			if err != nil {
				t.Fatalf("allowed command %q returned error: %v", strings.Join(path, " "), err)
			}
			if !ran {
				t.Fatalf("allowed command %q did not execute", strings.Join(path, " "))
			}
		})
	}

	blocked := [][]string{
		{"issue", "list"},
		{"issue", "create"},
		{"issue", "update"},
		{"issue", "assign"},
		{"issue", "rerun"},
		{"issue", "runs"},
		{"issue", "comment", "delete"},
		{"workspace", "update"},
		{"project", "update"},
		{"repo", "list"},
		{"repo", "add"},
		{"repo", "remove"},
		{"config", "set"},
		{"auth", "status"},
		{"login"},
		{"setup"},
		{"runtime", "list"},
		{"daemon", "start"},
		{"autopilot", "restore"},
		{"agent", "update"},
		{"skill", "create"},
		{"squad", "create"},
	}
	for _, path := range blocked {
		path := path
		t.Run("blocked_"+strings.Join(path, "_"), func(t *testing.T) {
			ran, err := executeTaskScopeTestCommand(t, path)
			if err == nil {
				t.Fatalf("blocked command %q returned no error", strings.Join(path, " "))
			}
			if ran {
				t.Fatalf("blocked command %q reached RunE", strings.Join(path, " "))
			}
			if !strings.Contains(err.Error(), "task-scoped CLI context") {
				t.Fatalf("error = %q, want task-scope explanation", err)
			}
		})
	}
}

func TestTaskScopedCLIRejectsRootConfigurationOverrides(t *testing.T) {
	for _, flag := range []string{"--profile", "--server-url", "--workspace-id"} {
		flag := flag
		t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) {
			clearTaskScopeContext(t)
			t.Setenv("MULTICA_TASK_ID", "task-123")

			ran, err := executeTaskScopeTestCommand(t, []string{"issue", "get"}, flag, "override")
			if err == nil {
				t.Fatalf("%s override returned no error", flag)
			}
			if ran {
				t.Fatalf("%s override reached RunE", flag)
			}
			if !strings.Contains(err.Error(), flag) {
				t.Fatalf("error = %q, want rejected flag %q", err, flag)
			}
		})
	}
}

func TestTaskScopedCLIRecognizesEveryManagedContextSignal(t *testing.T) {
	contexts := []struct {
		name  string
		setup func(*testing.T)
	}{
		{name: "task token", setup: func(t *testing.T) { t.Setenv("MULTICA_TOKEN", "mat_token") }},
		{name: "agent id", setup: func(t *testing.T) { t.Setenv("MULTICA_AGENT_ID", "agent-123") }},
		{name: "task id", setup: func(t *testing.T) { t.Setenv("MULTICA_TASK_ID", "task-123") }},
		{name: "daemon port", setup: func(t *testing.T) { t.Setenv("MULTICA_DAEMON_PORT", "27182") }},
		{name: "task marker", setup: func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, execenv.TaskContextMarkerRelPath)
			if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
				t.Fatalf("mkdir marker parent: %v", err)
			}
			body := fmt.Sprintf(`{"managed_by":%q}`, execenv.TaskContextMarkerManagedBy)
			if err := os.WriteFile(marker, []byte(body), 0o644); err != nil {
				t.Fatalf("write marker: %v", err)
			}
			t.Chdir(dir)
		}},
	}

	for _, tc := range contexts {
		t.Run(tc.name, func(t *testing.T) {
			clearTaskScopeContext(t)
			tc.setup(t)
			ran, err := executeTaskScopeTestCommand(t, []string{"workspace", "update"})
			if err == nil {
				t.Fatal("workspace update returned no error")
			}
			if ran {
				t.Fatal("workspace update reached RunE")
			}
		})
	}
}

func TestTaskScopedCLILeavesHumanContextUnchanged(t *testing.T) {
	clearTaskScopeContext(t)

	ran, err := executeTaskScopeTestCommand(t, []string{"workspace", "update"}, "--profile", "human")
	if err != nil {
		t.Fatalf("human command returned error: %v", err)
	}
	if !ran {
		t.Fatal("human command did not execute")
	}
}

const (
	taskScopedCLIWorkspaceID = "10000000-0000-0000-0000-000000000001"
	taskScopedCLIIssueID     = "10000000-0000-0000-0000-000000000002"
	taskScopedCLITaskID      = "10000000-0000-0000-0000-000000000003"
	taskScopedCLIAgentID     = "10000000-0000-0000-0000-000000000004"
	taskScopedCLICommentID   = "10000000-0000-0000-0000-000000000005"
)

type taskScopedCLIRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

type taskScopedCLIRecorder struct {
	mu       sync.Mutex
	requests []taskScopedCLIRequest
}

func (r *taskScopedCLIRecorder) append(req taskScopedCLIRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
}

func (r *taskScopedCLIRecorder) snapshot() []taskScopedCLIRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]taskScopedCLIRequest(nil), r.requests...)
}

func newTaskScopedCLIContractServer(t *testing.T) (*httptest.Server, *taskScopedCLIRecorder) {
	t.Helper()
	recorder := &taskScopedCLIRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if req.Body != nil && req.Method != http.MethodGet {
			decoder := json.NewDecoder(req.Body)
			if err := decoder.Decode(&body); err != nil {
				t.Errorf("decode %s %s body: %v", req.Method, req.URL.RequestURI(), err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		recorder.append(taskScopedCLIRequest{Method: req.Method, Path: req.URL.RequestURI(), Body: body})
		w.Header().Set("Content-Type", "application/json")

		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/api/issues/"+taskScopedCLIIssueID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": taskScopedCLIIssueID, "identifier": "ATH-75", "title": "Bound issue",
				"status": "in_progress", "priority": "high", "assignee_type": "agent",
				"assignee_id": taskScopedCLIAgentID,
			})
		case req.Method == http.MethodGet && req.URL.Path == "/api/issues/"+taskScopedCLIIssueID+"/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": taskScopedCLICommentID, "author_type": "agent", "author_id": taskScopedCLIAgentID,
				"type": "comment", "content": "bounded", "created_at": "2026-07-15T01:02:03Z",
			}})
		case req.Method == http.MethodPost && req.URL.Path == "/api/issues/"+taskScopedCLIIssueID+"/comments":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": taskScopedCLICommentID, "content": body["content"]})
		case req.Method == http.MethodPut && req.URL.Path == "/api/issues/"+taskScopedCLIIssueID:
			result := map[string]any{"id": taskScopedCLIIssueID, "identifier": "ATH-75"}
			for key, value := range body {
				result[key] = value
			}
			_ = json.NewEncoder(w).Encode(result)
		case req.Method == http.MethodGet && req.URL.Path == "/api/issues/"+taskScopedCLIIssueID+"/task-runs":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": taskScopedCLITaskID, "agent_id": taskScopedCLIAgentID, "status": "completed",
			}})
		case req.Method == http.MethodPost && req.URL.Path == "/api/issues/"+taskScopedCLIIssueID+"/rerun":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": taskScopedCLITaskID, "agent_id": taskScopedCLIAgentID, "status": "queued",
			})
		case req.Method == http.MethodGet && req.URL.Path == "/api/tasks/"+taskScopedCLITaskID+"/messages":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"seq": 1, "type": "assistant", "content": "done"}})
		case req.Method == http.MethodGet && req.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": taskScopedCLIAgentID, "name": "Human-visible agent"}})
		case req.Method == http.MethodGet && req.URL.Path == "/api/workspaces/"+taskScopedCLIWorkspaceID+"/members":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case req.Method == http.MethodGet && req.URL.Path == "/api/squads":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected_request"}`))
		}
	}))
	return server, recorder
}

func setTaskScopedCLIContractEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", taskScopedCLIWorkspaceID)
	t.Setenv("MULTICA_TOKEN", "mat_contract_test")
	t.Setenv("MULTICA_TASK_ID", taskScopedCLITaskID)
	t.Setenv("MULTICA_AGENT_ID", taskScopedCLIAgentID)
	t.Setenv("MULTICA_DAEMON_PORT", "27182")
}

func assertTaskScopedCLIRequests(t *testing.T, recorder *taskScopedCLIRecorder, expected ...string) {
	t.Helper()
	requests := recorder.snapshot()
	actual := make([]string, 0, len(requests))
	for _, request := range requests {
		actual = append(actual, request.Method+" "+request.Path)
		if strings.HasPrefix(request.Path, "/api/workspaces") ||
			strings.HasPrefix(request.Path, "/api/projects") ||
			strings.HasPrefix(request.Path, "/api/repos") ||
			strings.HasPrefix(request.Path, "/api/agents") ||
			strings.HasPrefix(request.Path, "/api/runtimes") ||
			strings.HasPrefix(request.Path, "/api/autopilots") ||
			strings.HasPrefix(request.Path, "/api/skills") ||
			strings.HasPrefix(request.Path, "/api/squads") {
			t.Fatalf("task-scoped CLI performed management lookup: %s %s", request.Method, request.Path)
		}
	}
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(actual, "\n"), strings.Join(expected, "\n"))
	}
}

func newTaskScopedIssueGetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "table", "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func newTaskScopedIssueCommentListCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().String("thread", "", "")
	cmd.Flags().Int("tail", 0, "")
	cmd.Flags().Int("recent", 0, "")
	cmd.Flags().Bool("roots-only", false, "")
	cmd.Flags().Bool("summary", false, "")
	cmd.Flags().Bool("full", false, "")
	cmd.Flags().String("before", "", "")
	cmd.Flags().String("before-id", "", "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func newTaskScopedIssueAssignCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "assign"}
	cmd.Flags().String("to", "", "")
	cmd.Flags().String("to-id", "", "")
	cmd.Flags().String("to-type", "", "")
	cmd.Flags().Bool("unassign", false, "")
	cmd.Flags().String("output", "json", "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func newTaskScopedIssueStatusCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "status"}
	cmd.Flags().String("output", "json", "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func newTaskScopedIssueRunsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "runs"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func newTaskScopedIssueRerunCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rerun"}
	cmd.Flags().String("output", "table", "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func newTaskScopedIssueRunMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run-messages"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Int("since", 0, "")
	cmd.Flags().String("issue", "", "")
	bindTaskScopeTestAuthority(cmd)
	return cmd
}

func TestTaskScopedIssueCLIUsesOnlyBoundIssueAPIs(t *testing.T) {
	tests := []struct {
		name     string
		run      func(*testing.T) error
		expected []string
	}{
		{
			name: "get table",
			run: func(t *testing.T) error {
				_, err := captureStdout(t, func() error { return runIssueGet(newTaskScopedIssueGetCmd(), []string{taskScopedCLIIssueID}) })
				return err
			},
			expected: []string{"GET /api/issues/" + taskScopedCLIIssueID, "GET /api/issues/" + taskScopedCLIIssueID},
		},
		{
			name: "comment list table",
			run: func(t *testing.T) error {
				_, err := captureStdout(t, func() error {
					return runIssueCommentList(newTaskScopedIssueCommentListCmd(), []string{taskScopedCLIIssueID})
				})
				return err
			},
			expected: []string{"GET /api/issues/" + taskScopedCLIIssueID, "GET /api/issues/" + taskScopedCLIIssueID + "/comments?fold=true"},
		},
		{
			name: "comment add",
			run: func(t *testing.T) error {
				cmd := newIssueCommentAddTestCmd()
				bindTaskScopeTestAuthority(cmd)
				_ = cmd.Flags().Set("content", "bounded comment")
				_, err := captureStdout(t, func() error { return runIssueCommentAdd(cmd, []string{taskScopedCLIIssueID}) })
				return err
			},
			expected: []string{"GET /api/issues/" + taskScopedCLIIssueID, "POST /api/issues/" + taskScopedCLIIssueID + "/comments"},
		},
		{
			name: "assign explicit identity",
			run: func(t *testing.T) error {
				cmd := newTaskScopedIssueAssignCmd()
				_ = cmd.Flags().Set("to-id", taskScopedCLIAgentID)
				_ = cmd.Flags().Set("to-type", "agent")
				_, err := captureStdout(t, func() error { return runIssueAssign(cmd, []string{taskScopedCLIIssueID}) })
				return err
			},
			expected: []string{"GET /api/issues/" + taskScopedCLIIssueID, "PUT /api/issues/" + taskScopedCLIIssueID},
		},
		{
			name: "status",
			run: func(t *testing.T) error {
				_, err := captureStdout(t, func() error {
					return runIssueStatus(newTaskScopedIssueStatusCmd(), []string{taskScopedCLIIssueID, "in_review"})
				})
				return err
			},
			expected: []string{"GET /api/issues/" + taskScopedCLIIssueID, "PUT /api/issues/" + taskScopedCLIIssueID},
		},
		{
			name: "run messages full uuid",
			run: func(t *testing.T) error {
				_, err := captureStdout(t, func() error {
					return runIssueRunMessages(newTaskScopedIssueRunMessagesCmd(), []string{taskScopedCLITaskID})
				})
				return err
			},
			expected: []string{"GET /api/tasks/" + taskScopedCLITaskID + "/messages"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, recorder := newTaskScopedCLIContractServer(t)
			defer server.Close()
			setTaskScopedCLIContractEnv(t, server.URL)
			if err := tc.run(t); err != nil {
				t.Fatalf("command failed: %v", err)
			}
			assertTaskScopedCLIRequests(t, recorder, tc.expected...)
		})
	}
}

func TestTaskScopedCLIRejectsRunEnumerationAndRerunBeforeHTTP(t *testing.T) {
	server, recorder := newTaskScopedCLIContractServer(t)
	defer server.Close()
	setTaskScopedCLIContractEnv(t, server.URL)

	for _, path := range [][]string{{"issue", "runs"}, {"issue", "rerun"}} {
		path := path
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			ran, err := executeTaskScopeTestCommand(t, path)
			if err == nil || !strings.Contains(err.Error(), "task-scoped CLI context") {
				t.Fatalf("task-scoped command %q error = %v", strings.Join(path, " "), err)
			}
			if ran {
				t.Fatalf("task-scoped command %q reached RunE", strings.Join(path, " "))
			}
		})
	}

	if requests := recorder.snapshot(); len(requests) != 0 {
		t.Fatalf("task-scoped run commands performed HTTP requests: %#v", requests)
	}
}

func TestTaskScopedIssueCLILocallyRejectsManagementLookups(t *testing.T) {
	server, recorder := newTaskScopedCLIContractServer(t)
	defer server.Close()
	setTaskScopedCLIContractEnv(t, server.URL)

	assign := newTaskScopedIssueAssignCmd()
	_ = assign.Flags().Set("to", "agent-name")
	if err := runIssueAssign(assign, []string{taskScopedCLIIssueID}); err == nil || !strings.Contains(err.Error(), "requires --to-id") {
		t.Fatalf("task assign by name error = %v", err)
	}

	comment := newIssueCommentAddTestCmd()
	bindTaskScopeTestAuthority(comment)
	_ = comment.Flags().Set("content", "bounded")
	_ = comment.Flags().Set("attachment", "artifact.txt")
	if err := runIssueCommentAdd(comment, []string{taskScopedCLIIssueID}); err == nil || !strings.Contains(err.Error(), "prohibits attachment") {
		t.Fatalf("task attachment error = %v", err)
	}

	if requests := recorder.snapshot(); len(requests) != 0 {
		t.Fatalf("local task-scope rejections performed HTTP requests: %#v", requests)
	}
}

func TestHumanIssueGetTableKeepsActorDisplayLookup(t *testing.T) {
	server, recorder := newTaskScopedCLIContractServer(t)
	defer server.Close()
	clearTaskScopeContext(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", taskScopedCLIWorkspaceID)
	t.Setenv("MULTICA_TOKEN", "mul_human_test")

	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "table", "")
	out, err := captureStdout(t, func() error {
		return runIssueGet(cmd, []string{taskScopedCLIIssueID})
	})
	if err != nil {
		t.Fatalf("human issue get: %v", err)
	}
	if !strings.Contains(out, "agent:Human-visible agent") {
		t.Fatalf("human table output did not resolve actor name:\n%s", out)
	}
	requests := recorder.snapshot()
	foundAgentLookup := false
	for _, request := range requests {
		if request.Method == http.MethodGet && strings.HasPrefix(request.Path, "/api/agents?") {
			foundAgentLookup = true
		}
	}
	if !foundAgentLookup {
		t.Fatalf("human context did not preserve agent display lookup: %#v", requests)
	}
}

func TestTaskAuthorityLocatorEnvironmentCannotRedirectFixedPath(t *testing.T) {
	authorityServer, _ := newTaskScopedCLIContractServer(t)
	defer authorityServer.Close()

	var attackerRequests int
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attackerRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + taskScopedCLIIssueID + `","identifier":"ATH-75","title":"stolen"}`))
	}))
	defer attacker.Close()

	clearTaskScopeContext(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", attacker.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", taskScopedCLIWorkspaceID)
	t.Setenv("MULTICA_TOKEN", "mat_contract_test")
	t.Setenv("MULTICA_TASK_ID", taskScopedCLITaskID)
	t.Setenv("MULTICA_AGENT_ID", taskScopedCLIAgentID)

	t.Setenv("MULTICA_TASK_AUTHORITY_PATH", filepath.Join(t.TempDir(), "attacker-authority.json"))

	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "table", "")
	err := runIssueGet(cmd, []string{taskScopedCLIIssueID})
	if err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatalf("environment redirect error = %v, want authority mismatch rejection", err)
	}
	if attackerRequests != 0 {
		t.Fatalf("task credential was redirected to attacker endpoint (%d requests)", attackerRequests)
	}
}
