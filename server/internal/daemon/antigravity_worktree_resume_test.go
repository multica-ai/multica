package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRunTaskAntigravityWorktreeResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}
	for _, tc := range []struct {
		name           string
		disruption     string
		wantMethods    []string
		wantSameCwd    bool
		wantSession    string
		wantSameBranch bool
	}{
		{"reclaimed root", "gc", []string{"session/resume"}, true, "original", true},
		{"read-only turn", "read-only", []string{"session/resume"}, true, "original", true},
		{"provider rejects resume", "reject", []string{"session/resume", "session/new"}, true, "fresh", true},
		{"old root locked", "locked", []string{"session/new"}, false, "fresh", true},
		{"old checkout occupied", "occupied", []string{"session/new"}, false, "fresh", true},
		{"conversation branch busy", "busy", []string{"session/new"}, false, "fresh", false},
		{"different local resource", "resource", []string{"session/new"}, false, "fresh", true},
		{"unexpected root owner", "owner", []string{"session/new"}, false, "fresh", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testAntigravityWorktreeResume(t, tc.disruption, tc.wantMethods, tc.wantSameCwd, tc.wantSession, tc.wantSameBranch)
		})
	}
}

func testAntigravityWorktreeResume(t *testing.T, disruption string, wantMethods []string, wantSameCwd bool, wantSession string, wantSameBranch bool) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	repo := createDaemonTestRepo(t)
	localPath := filepath.Join(repo, "service")
	if err := os.Mkdir(localPath, 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	fakeBin := filepath.Join(fixture, "fake-acp")
	writeTestExecutable(t, fakeBin, []byte("#!/bin/sh\nexec \"$TEST_AGY_ACP_EXECUTABLE\" -test.run=^TestAntigravityWorktreeFakeACP$ -- \"$@\"\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	workspacesRoot := filepath.Join(t.TempDir(), "workspaces-alias")
	if err := os.Symlink(t.TempDir(), workspacesRoot); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{
		client: NewClient(srv.URL), logger: logger, localPathLocks: NewLocalPathLocker(),
		workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
		cfg: Config{
			DaemonID: "daemon", WorkspacesRoot: workspacesRoot, AgentTimeout: 12 * time.Second,
			ServerBaseURL: srv.URL, Agents: map[string]AgentEntry{"antigravity": {Path: fakeBin}},
		},
	}
	if disruption == "gc" {
		d.executionEnvironmentCommand = func() ([]string, error) {
			return []string{executable, "-test.run=^TestSkillInvocationHelperProcess$", "--", skillInvocationHelperMode, "prepare"}, nil
		}
	}
	ref, err := json.Marshal(localDirectoryRef{LocalPath: localPath, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeWorktree})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "first-task", WorkspaceID: "test-workspace", IssueID: "issue", IssueIdentifier: "T-275",
		RuntimeID: "runtime", AgentID: "agent", AuthToken: "mat_test",
		Agent: &AgentData{ID: "agent", Name: "acp-agent", CustomArgs: []string{"--multica-antigravity-acp"}, CustomEnv: map[string]string{
			"TEST_AGY_ACP_EXECUTABLE": executable, "TEST_AGY_ACP_FIXTURE": fixture,
			"TEST_AGY_ACP_DISRUPTION": disruption, "GORACE": "atexit_sleep_ms=0",
		}},
		ProjectResources: []ProjectResourceData{{ID: "repo", ResourceType: localDirectoryResourceType, ResourceRef: ref}},
	}
	run := func() TaskResult {
		t.Helper()
		got, err := d.runTask(t.Context(), task, "antigravity", 0, logger)
		if err != nil || got.Status != "completed" || got.DurableWorkDir != localPath {
			t.Fatalf("%s = %+v, %v", task.ID, got, err)
		}
		return got
	}

	first := run()
	if first.SessionID != "original" || (first.BranchName == "") != (disruption == "read-only") {
		t.Fatalf("first turn = %+v", first)
	}
	if _, err := os.Lstat(first.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("first checkout still exists: %v", err)
	}
	methodsLog := filepath.Join(fixture, "methods")
	if err := os.WriteFile(methodsLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	task.LocalWorktreeResumeSource = &protocol.LocalWorktreeResumeSource{
		RootTaskID: task.ID, LocalPath: first.DurableWorkDir, Branch: first.BranchName,
	}
	switch disruption {
	case "gc":
		if err := os.RemoveAll(first.EnvRoot); err != nil {
			t.Fatal(err)
		}
	case "locked":
		oldRoot, err := os.OpenRoot(filepath.Dir(first.EnvRoot))
		if err != nil {
			t.Fatal(err)
		}
		defer oldRoot.Close()
		claim, _, err := execenv.LockEnvRootForReuse(oldRoot, filepath.Base(first.EnvRoot), first.EnvRoot)
		if err != nil || claim == nil {
			t.Fatalf("hold original root: %v", err)
		}
		defer claim.Release()
	case "occupied":
		if err := os.MkdirAll(first.WorkDir, 0o755); err != nil {
			t.Fatal(err)
		}
	case "busy":
		busyPath := filepath.Join(t.TempDir(), "busy")
		if out, err := exec.Command("git", "-C", repo, "worktree", "add", busyPath, first.BranchName).CombinedOutput(); err != nil {
			t.Fatalf("occupy branch: %v: %s", err, out)
		}
	case "resource":
		task.LocalWorktreeResumeSource.LocalPath = t.TempDir()
	case "owner":
		if err := os.WriteFile(filepath.Join(first.EnvRoot, ".task_owner"), []byte(`{"workspace_id":"test-workspace","task_id":"different-task"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	task.ID, task.PriorSessionID, task.PriorWorkDir = "second-task", first.SessionID, first.WorkDir
	second := run()
	if second.SessionID != wantSession || (second.WorkDir == first.WorkDir) != wantSameCwd ||
		(second.BranchName == first.BranchName) != wantSameBranch || second.EnvRoot == first.EnvRoot {
		t.Fatalf("unexpected continuity: first=%+v second=%+v", first, second)
	}
	if second.Comment == "" || (second.Comment == first.Comment) != wantSameCwd {
		t.Fatalf("effective tool cwd first=%q second=%q workdir=%q", first.Comment, second.Comment, second.WorkDir)
	}
	methods, err := os.ReadFile(methodsLog)
	if err != nil || !slices.Equal(strings.Fields(string(methods)), wantMethods) {
		t.Fatalf("second turn methods: %q, %v, want %v", methods, err, wantMethods)
	}
	if disruption == "reject" && second.RetiredSessionID != first.SessionID {
		t.Fatalf("rejected session was not retired: %+v", second)
	}
	if _, err := os.Lstat(second.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("second checkout still exists: %v", err)
	}
	if disruption == "occupied" {
		if _, err := os.Stat(first.WorkDir); err != nil {
			t.Fatalf("occupied checkout was disturbed: %v", err)
		}
	}
	if disruption == "gc" {
		task.ID, task.PriorSessionID, task.PriorWorkDir = "third-task", second.SessionID, second.WorkDir
		third := run()
		if third.SessionID != first.SessionID || third.WorkDir != first.WorkDir || third.Comment != first.Comment ||
			third.BranchName != first.BranchName || third.EnvRoot == second.EnvRoot {
			t.Fatalf("third turn did not reuse the original session and checkout: %+v", third)
		}
	}
}

func TestAntigravityWorktreeFakeACP(t *testing.T) {
	fixture := os.Getenv("TEST_AGY_ACP_FIXTURE")
	if fixture == "" {
		return
	}
	disruption := os.Getenv("TEST_AGY_ACP_DISRUPTION")
	if !slices.Equal(os.Args[3:], []string{"acp"}) {
		t.Fatalf("fake argv = %v", os.Args)
	}
	send := func(value any) {
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	sessionFile := filepath.Join(fixture, "original-cwd")
	var cwd, session string
	first := false
	decoder := json.NewDecoder(os.Stdin)
	for {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Cwd       string `json:"cwd"`
				SessionID string `json:"sessionId"`
			} `json:"params"`
		}
		if err := decoder.Decode(&req); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		switch req.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"sessionCapabilities": map[string]any{"resume": map[string]any{}},
			}})
		case "session/new", "session/resume":
			log, err := os.OpenFile(filepath.Join(fixture, "methods"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.WriteString(log, req.Method+"\n")
			if closeErr := log.Close(); err != nil || closeErr != nil {
				t.Fatalf("record method: %v, %v", err, closeErr)
			}
			original, err := os.ReadFile(sessionFile)
			switch {
			case req.Method == "session/resume":
				if req.Params.SessionID != "original" || err != nil {
					t.Fatalf("resume target = %q, %v", req.Params.SessionID, err)
				}
				if disruption == "reject" {
					send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{
						"code": -32603, "message": "session not found",
					}})
					continue
				}
				// Like agy 1.5.16, resume accepts a new cwd but tools still
				// use the path stored when the original session was created.
				session, cwd = "original", string(original)
			case os.IsNotExist(err):
				first, session, cwd = true, "original", req.Params.Cwd
				if err := os.WriteFile(sessionFile, []byte(cwd), 0o600); err != nil {
					t.Fatal(err)
				}
			case err != nil:
				t.Fatal(err)
			default:
				session, cwd = "fresh", req.Params.Cwd
			}
			reply(map[string]string{"sessionId": session})
		case "session/prompt":
			cmd := exec.Command("/bin/pwd", "-P")
			cmd.Dir = cwd
			out, err := cmd.CombinedOutput()
			if err != nil || !sameExistingDir(strings.TrimSpace(string(out)), cwd) {
				t.Fatalf("effective tool cwd %q: %v: %s", cwd, err, out)
			}
			if disruption != "read-only" {
				path := filepath.Join(cwd, "first-turn.txt")
				if first {
					if err := os.WriteFile(path, []byte("first turn"), 0o600); err != nil {
						t.Fatal(err)
					}
				} else if data, err := os.ReadFile(path); err != nil || string(data) != "first turn" {
					t.Fatalf("first turn work unavailable in tool cwd: %s, %v", data, err)
				}
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": session, "update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]string{"type": "text", "text": strings.TrimSpace(string(out))},
				},
			}})
			reply(map[string]string{"stopReason": "end_turn"})
			os.Exit(0)
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}
}
