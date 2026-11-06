package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestMigrateClaudeWorktreeTranscriptConfiguredDir(t *testing.T) {
	home, configDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	priorDir, workDir := t.TempDir(), t.TempDir()
	const sessionID = "session-configured-dir"
	src := filepath.Join(configDir, "projects", encodeClaudeCwd(priorDir), sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	const transcript = "{\"type\":\"user\",\"message\":{\"content\":\"prior discussion\"}}\n"
	if err := os.WriteFile(src, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	env := &execenv.Environment{WorkDir: workDir, LocalWorktree: &execenv.LocalWorktree{Continued: true}}
	if !migrateClaudeWorktreeTranscript(env, sessionID, workDir, priorDir, nil, slog.Default()) {
		t.Fatal("continued worktree did not migrate the transcript from CLAUDE_CONFIG_DIR")
	}
	dst := filepath.Join(configDir, "projects", encodeClaudeCwd(workDir), sessionID+".jsonl")
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != transcript {
		t.Fatalf("migrated transcript = %q, %v; want %q", data, err, transcript)
	}
}

func TestMigrateClaudeWorktreeTranscriptRelativeConfigDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	priorDir, workDir := filepath.Join(root, "prior", "worktree"), filepath.Join(root, "next", "worktree")
	priorConfig, configDir := filepath.Join(root, "prior", "config"), filepath.Join(root, "next", "config")
	const sessionID = "session-relative-config"
	src := filepath.Join(priorConfig, "projects", encodeClaudeCwd(priorDir), sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("prior history"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := &execenv.Environment{WorkDir: workDir, LocalWorktree: &execenv.LocalWorktree{Continued: true}}
	if !migrateClaudeWorktreeTranscript(env, sessionID, workDir, priorDir, map[string]string{"CLAUDE_CONFIG_DIR": "../config"}, slog.Default()) {
		t.Fatal("relative config directory was not resolved separately for each worktree")
	}
	dst := filepath.Join(configDir, "projects", encodeClaudeCwd(workDir), sessionID+".jsonl")
	if data, err := os.ReadFile(dst); err != nil || string(data) != "prior history" {
		t.Fatalf("migrated transcript = %q, %v", data, err)
	}
}

func TestMigrateClaudeWorktreeTranscriptFailureLogs(t *testing.T) {
	t.Parallel()
	for _, blockedDestination := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing source", true: "unwritable destination"}[blockedDestination], func(t *testing.T) {
			t.Parallel()
			configDir, priorDir, workDir := t.TempDir(), t.TempDir(), t.TempDir()
			src := filepath.Join(configDir, "projects", encodeClaudeCwd(priorDir), "session-1.jsonl")
			wantMessage, wantPath := "claude: cannot read prior session transcript", src
			if blockedDestination {
				if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(src, []byte("history"), 0o600); err != nil {
					t.Fatal(err)
				}
				wantPath = filepath.Join(configDir, "projects", encodeClaudeCwd(workDir))
				if err := os.WriteFile(wantPath, []byte("blocking file"), 0o600); err != nil {
					t.Fatal(err)
				}
				wantMessage = "claude: cannot create transcript directory"
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			env := &execenv.Environment{WorkDir: workDir, LocalWorktree: &execenv.LocalWorktree{Continued: true}}
			if migrateClaudeWorktreeTranscript(env, "session-1", workDir, priorDir, map[string]string{"CLAUDE_CONFIG_DIR": configDir}, logger) {
				t.Fatal("migration failure was treated as resumable")
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("decode migration log: %v (%s)", err, &logs)
			}
			if entry["msg"] != wantMessage || entry["path"] != wantPath || entry["error"] == nil || entry["error"] == "" {
				t.Fatalf("migration diagnostic = %v", entry)
			}
		})
	}
}

func TestMigrateClaudeWorktreeTranscriptRejectsUnsafeSessionID(t *testing.T) {
	t.Parallel()
	configDir, priorDir, workDir := t.TempDir(), t.TempDir(), t.TempDir()
	projectsDir := filepath.Join(configDir, "projects")
	if err := os.MkdirAll(projectsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Without validation, ../victim resolves to this unrelated existing file
	// from both the source and destination project directories.
	victim := filepath.Join(projectsDir, "victim.jsonl")
	if err := os.WriteFile(victim, []byte("unrelated history"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := &execenv.Environment{WorkDir: workDir, LocalWorktree: &execenv.LocalWorktree{Continued: true}}
	for _, sessionID := range []string{".", "..", "../victim", `..\victim`, "C:session", "session\x00"} {
		if migrateClaudeWorktreeTranscript(env, sessionID, workDir, priorDir, map[string]string{"CLAUDE_CONFIG_DIR": configDir}, slog.Default()) {
			t.Fatalf("unsafe session id %q was accepted", sessionID)
		}
	}
	if entries, err := os.ReadDir(projectsDir); err != nil || len(entries) != 1 || entries[0].Name() != "victim.jsonl" {
		t.Fatalf("unsafe session id wrote into config directory: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "unrelated history" {
		t.Fatalf("unrelated history was modified: %q, %v", data, err)
	}
}

// Exercise real task preparation and launch with a fake CLI, so custom_env
// must reach both the migration gate and the subprocess without using an account.
func TestRunTask_ClaudeWorktreeResumeUsesCustomConfigDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}
	home, configDir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "inherited-config"))
	repo := createDaemonTestRepo(t)
	captureFile := filepath.Join(t.TempDir(), "args.txt")
	fakeBin := filepath.Join(t.TempDir(), "claude")
	writeTestExecutable(t, fakeBin, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_FILE"
printf '%s\n' "$CLAUDE_CONFIG_DIR" "$HOME" >> "$CAPTURE_FILE"
printf 'first turn work\n' > task-work.txt
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-custom-config"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-custom-config","result":"done"}'
`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client: NewClient(srv.URL), logger: logger, localPathLocks: NewLocalPathLocker(),
		workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
		cfg: Config{
			DaemonID: "daemon-config", WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second,
			ServerBaseURL: srv.URL, Agents: map[string]AgentEntry{"claude": {Path: fakeBin}},
		},
	}
	ref, err := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeWorktree})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "task-config-first", WorkspaceID: "ws-config", IssueID: "issue-config", IssueIdentifier: "MUL-8413",
		RuntimeID: "runtime-config", AgentID: "agent-config", AuthToken: "mat_config_test",
		Agent: &AgentData{ID: "agent-config", Name: "config-agent", CustomEnv: map[string]string{
			"CLAUDE_CONFIG_DIR": configDir, "HOME": filepath.Join(home, "blocked-home"), "CAPTURE_FILE": captureFile,
		}},
		ProjectResources: []ProjectResourceData{{ID: "repo-config", ResourceType: localDirectoryResourceType, ResourceRef: ref}},
	}
	first, err := d.runTask(context.Background(), task, "claude", 0, logger)
	if err != nil || first.Status != "completed" || first.BranchName == "" {
		t.Fatalf("first turn = %+v, %v", first, err)
	}
	if _, err := os.Stat(first.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("prior worktree was not removed: %v", err)
	}
	src := filepath.Join(configDir, "projects", encodeClaudeCwd(first.WorkDir), first.SessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	const transcript = "{\"type\":\"user\",\"message\":{\"content\":\"prior discussion\"}}\n"
	if err := os.WriteFile(src, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	task.ID, task.PriorSessionID, task.PriorWorkDir = "task-config-second", first.SessionID, first.WorkDir
	second, err := d.runTask(context.Background(), task, "claude", 0, logger)
	if err != nil || second.Status != "completed" || second.BranchName != first.BranchName {
		t.Fatalf("second turn = %+v, %v", second, err)
	}
	args, err := os.ReadFile(captureFile)
	if err != nil || !strings.Contains(string(args), "--resume\n"+first.SessionID+"\n") || !strings.HasSuffix(string(args), configDir+"\n"+home+"\n") {
		t.Fatalf("second turn did not receive the resume and effective env: %s, %v", args, err)
	}
	dst := filepath.Join(configDir, "projects", encodeClaudeCwd(second.WorkDir), first.SessionID+".jsonl")
	if data, err := os.ReadFile(dst); err != nil || string(data) != transcript {
		t.Fatalf("second turn transcript = %q, %v", data, err)
	}
}
