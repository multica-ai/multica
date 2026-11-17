package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const directoryTestAgentID = "12ab5678-1234-4567-890a-123456789abc"

func TestParseAgentLocalDirectories(t *testing.T) {
	directory := t.TempDir()
	encoded, err := json.Marshal(map[string]string{strings.ToUpper(directoryTestAgentID): directory})
	if err != nil {
		t.Fatal(err)
	}
	pathJSON, err := json.Marshal(directory)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := `{"` + directoryTestAgentID + `":` + string(pathJSON) + `,"` + directoryTestAgentID + `":` + string(pathJSON) + `}`
	malformedID := `{"` + strings.ReplaceAll(directoryTestAgentID, "-", "_") + `":` + string(pathJSON) + `}`
	for _, tc := range []struct {
		name, input string
		want        map[string]string
		bad         bool
	}{
		{name: "unset"},
		{name: "empty object", input: "{}", want: map[string]string{}},
		{name: "canonical agent ID", input: string(encoded), want: map[string]string{directoryTestAgentID: directory}},
		{name: "invalid JSON", input: "{", bad: true},
		{name: "duplicate UUID key", input: duplicate, bad: true},
		{name: "malformed UUID separators", input: malformedID, bad: true},
		{name: "null", input: "null", bad: true},
		{name: "array", input: "[]", bad: true},
		{name: "trailing object", input: "{}{}", bad: true},
		{name: "trailing garbage", input: "{} invalid", bad: true},
		{name: "string", input: `"directory"`, bad: true},
		{name: "invalid agent ID", input: `{"agent":"/tmp/work"}`, bad: true},
		{name: "non-string path", input: `{"` + directoryTestAgentID + `":42}`, bad: true},
		{name: "empty path", input: `{"` + directoryTestAgentID + `":""}`, bad: true},
		{name: "relative path", input: `{"` + directoryTestAgentID + `":"work"}`, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAgentLocalDirectories(tc.input)
			if (err != nil) != tc.bad {
				t.Fatalf("error = %v, want error %v", err, tc.bad)
			}
			if !tc.bad && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("directories = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadConfigAgentLocalDirectories(t *testing.T) {
	stageFakeAgent(t)
	redirectTestHome(t, t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	directory := t.TempDir()
	raw, _ := json.Marshal(map[string]string{directoryTestAgentID: directory})
	t.Setenv("MULTICA_AGENT_LOCAL_DIRECTORIES", string(raw))
	cfg, err := LoadConfig(Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentLocalDirectories[directoryTestAgentID] != directory {
		t.Fatalf("configured directory = %q, want %q", cfg.AgentLocalDirectories[directoryTestAgentID], directory)
	}
	t.Setenv("MULTICA_AGENT_LOCAL_DIRECTORIES", `{"bad":"relative"}`)
	if _, err := LoadConfig(Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()}); err == nil {
		t.Fatal("malformed directory mapping must reject startup")
	}
}

func TestLocalDirectoryAssignmentAgentDefault(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	projectDirectory := t.TempDir()
	defaults := map[string]string{directoryTestAgentID: directory}
	const daemonID = "daemon-default-directory"
	ref := func(path, owner string) []ProjectResourceData {
		data, err := json.Marshal(localDirectoryRef{LocalPath: path, DaemonID: owner})
		if err != nil {
			t.Fatal(err)
		}
		return []ProjectResourceData{{ResourceType: localDirectoryResourceType, ResourceRef: data}}
	}
	for _, tc := range []struct {
		name     string
		task     Task
		defaults map[string]string
		want     string
		bad      bool
	}{
		{name: "issue without project", task: Task{AgentID: directoryTestAgentID}, defaults: defaults, want: directory},
		{name: "chat without project", task: Task{AgentID: directoryTestAgentID, ChatSessionID: "chat"}, defaults: defaults, want: directory},
		{name: "unset", task: Task{AgentID: directoryTestAgentID}},
		{name: "other agent", task: Task{AgentID: "other"}, defaults: defaults},
		{name: "project wins", task: Task{AgentID: directoryTestAgentID, ProjectResources: ref(projectDirectory, daemonID)}, defaults: defaults, want: projectDirectory},
		{name: "foreign project binding", task: Task{AgentID: directoryTestAgentID, ProjectResources: ref(projectDirectory, "other-daemon")}, defaults: defaults},
		{name: "non-directory project resource", task: Task{AgentID: directoryTestAgentID, ProjectResources: []ProjectResourceData{{ResourceType: "github_repo"}}}, defaults: defaults, want: directory},
		{name: "coordinator", task: Task{AgentID: directoryTestAgentID, IsLeaderTask: true}, defaults: defaults},
		{name: "malformed project binding", task: Task{AgentID: directoryTestAgentID, ProjectResources: []ProjectResourceData{{ResourceType: localDirectoryResourceType, ResourceRef: json.RawMessage(`{`)}}}, defaults: defaults, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := localDirectoryAssignmentForTask(tc.task, daemonID, tc.defaults)
			if (err != nil) != tc.bad {
				t.Fatalf("error = %v, want error %v", err, tc.bad)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("unexpected assignment %+v", got)
				}
				return
			}
			if got == nil || got.AbsPath != tc.want {
				t.Fatalf("assignment = %+v, want directory %q", got, tc.want)
			}
			if got.UsesWorktree() {
				t.Fatal("default assignment must use existing in_place semantics")
			}
		})
	}
}

func TestAgentDefaultDirectoryUsesExistingPathLockAndChatExemption(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	d := &Daemon{cfg: Config{DaemonID: "default-daemon", AgentLocalDirectories: map[string]string{directoryTestAgentID: directory}}, client: NewClient(srv.URL), localPathLocks: NewLocalPathLocker(), logger: slog.Default(), cancelPollInterval: time.Hour}
	worker := Task{ID: "writer", AgentID: directoryTestAgentID, IssueID: "issue"}
	release, abort := d.acquireLocalDirectoryLockIfNeeded(t.Context(), worker, slog.Default())
	if abort || release == nil {
		t.Fatal("configured issue writer did not acquire its directory lock")
	}
	defer release()
	raw, _ := json.Marshal(localDirectoryRef{LocalPath: directory, DaemonID: d.cfg.DaemonID})
	project, err := findLocalDirectoryAssignment([]ProjectResourceData{{ResourceType: localDirectoryResourceType, ResourceRef: raw}}, d.cfg.DaemonID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiting := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		r, e := d.localPathLocks.Acquire(ctx, project.RealPath, "project-writer", func(string) { close(waiting) })
		if r != nil {
			r()
		}
		done <- e
	}()
	select {
	case <-waiting:
	case e := <-done:
		t.Fatalf("project writer bypassed default writer's lock: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("project writer never reached path lock")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting writer error = %v", err)
	}
	chatCtx, chatCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer chatCancel()
	chatRelease, abort := d.acquireLocalDirectoryLockIfNeeded(chatCtx, Task{ID: "chat", AgentID: directoryTestAgentID, ChatSessionID: "session"}, slog.Default())
	if chatRelease != nil {
		chatRelease()
	}
	if abort || chatRelease != nil {
		t.Fatal("chat did not retain its existing path-lock exemption")
	}
}

func TestAgentDefaultDirectoryMissingPathFailsBeforeExecution(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	failures := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			failures <- r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &Daemon{cfg: Config{DaemonID: "default-daemon", AgentLocalDirectories: map[string]string{directoryTestAgentID: missing}}, client: NewClient(srv.URL), localPathLocks: NewLocalPathLocker(), logger: slog.Default()}
	release, abort := d.acquireLocalDirectoryLockIfNeeded(t.Context(), Task{ID: "missing-directory-task", AgentID: directoryTestAgentID}, slog.Default())
	if release != nil {
		release()
	}
	if !abort {
		t.Fatal("missing configured directory silently fell back to managed execution")
	}
	select {
	case <-failures:
	default:
		t.Fatal("missing configured directory was not reported as a run failure")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("configured path must not be created: %v", err)
	}
}
