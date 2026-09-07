package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// seedMarker writes a daemon marker with the given body into a fresh temp
// directory and chdirs into a nested subdirectory, so the CLI's upward walk has
// to find it the same way it would in a user's repository. Returns the marker
// path.
func seedMarker(t *testing.T, body string) string {
	t.Helper()

	workDir := t.TempDir()
	markerPath := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("create marker dir: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	nested := filepath.Join(workDir, "repo", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested cwd: %v", err)
	}
	t.Chdir(nested)
	return markerPath
}

// issueTaskMarker is the marker an issue task writes: agent and issue both set.
func issueTaskMarker() string {
	return `{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `","agent_id":"agent-1","issue_id":"issue-1"}`
}

// chatTaskMarker is the marker a chat task writes. It has no issue, which is
// why task identity cannot be defined as "agent_id AND issue_id".
func chatTaskMarker() string {
	return `{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `","agent_id":"agent-1"}`
}

// workspacesRootMarker is the permanent node-wide guard: managed_by and nothing
// else. EnsureWorkspacesRootMarker writes exactly this.
func workspacesRootMarker() string {
	return `{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `"}`
}

func seedDaemonTaskMarker(t *testing.T) string {
	t.Helper()
	return seedMarker(t, issueTaskMarker())
}

// refusalPaths returns every CLI entry point that can refuse because of a
// leftover marker, so a new one cannot be added without deciding whether it
// reports the file.
func refusalPaths() map[string]func() error {
	return map[string]func() error{
		"human-local": func() error { return requireHumanLocalCommand("daemon stop") },
		"api": func() error {
			_, err := newAPIClient(testCmd())
			return err
		},
		"task-local-config": requireTaskLocalConfigRoot,
	}
}

func clearDaemonEnvSignals(t *testing.T) {
	t.Helper()
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv(cli.TaskConfigRootEnv, "")
}

// TestLeftoverMarkerReportedIdenticallyOnEveryRefusalPath is the regression test
// for the split the MUL-6132 reviews found: at first only the API path named the
// marker file, and after that requireTaskLocalConfigRoot was still left out, so
// whether a user could act on the refusal depended on which command they
// happened to run first. All three paths must point at the same file with the
// same remedy.
func TestLeftoverMarkerReportedIdenticallyOnEveryRefusalPath(t *testing.T) {
	for _, marker := range []struct{ name, body string }{
		{name: "issue task", body: issueTaskMarker()},
		{name: "chat task", body: chatTaskMarker()},
	} {
		t.Run(marker.name, func(t *testing.T) {
			markerPath := seedMarker(t, marker.body)
			clearDaemonEnvSignals(t)
			t.Setenv("MULTICA_TOKEN", "")
			t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")

			for name, refuse := range refusalPaths() {
				err := refuse()
				if err == nil {
					t.Fatalf("%s: got nil error; want refusal on a leftover marker", name)
				}
				if !strings.Contains(err.Error(), markerPath) {
					t.Fatalf("%s error should name the marker path %q; got %q", name, markerPath, err.Error())
				}
				if !strings.Contains(err.Error(), "leftover") {
					t.Fatalf("%s error should hint it may be a leftover; got %q", name, err.Error())
				}
			}

			// The CLI never removes the marker: taking down a fail-closed guard
			// needs a proof the CLI cannot produce, because the daemon's active
			// task count only rises after a claim returns.
			if _, statErr := os.Stat(markerPath); statErr != nil {
				t.Fatalf("CLI must not delete the marker: %v", statErr)
			}
		})
	}
}

// TestWorkspacesRootMarkerNeverReportedAsLeftover is the regression test for the
// permanent node-wide guard. It shares a filename with the per-task marker but
// is re-created by the daemon on every start and must never expire. Telling
// anyone to delete it — a user, or an agent subprocess that lost its
// environment and is reading this error — closes the hole it exists to cover.
func TestWorkspacesRootMarkerNeverReportedAsLeftover(t *testing.T) {
	markerPath := seedMarker(t, workspacesRootMarker())
	clearDaemonEnvSignals(t)
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")

	if got := leftoverDaemonTaskMarkerPath(); got != "" {
		t.Fatalf("leftoverDaemonTaskMarkerPath() = %q; want \"\" for the workspaces root marker", got)
	}

	for name, refuse := range refusalPaths() {
		err := refuse()
		if err == nil {
			t.Fatalf("%s: got nil error; want the plain refusal to stay in place", name)
		}
		if strings.Contains(err.Error(), "leftover") || strings.Contains(err.Error(), "remove it") {
			t.Fatalf("%s must not describe the workspaces root marker as removable; got %q", name, err.Error())
		}
	}

	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("workspaces root marker must survive: %v", statErr)
	}
}

// TestLeftoverMarkerNotReportedUnderRealTaskIdentity guards the boundary of the
// leftover treatment. A real daemon-managed task announces itself through the
// environment; those refusals are correct and must not be described to the user
// as a stale file to delete.
func TestLeftoverMarkerNotReportedUnderRealTaskIdentity(t *testing.T) {
	for _, tc := range []struct{ name, env, value string }{
		{name: "agent id", env: "MULTICA_AGENT_ID", value: "agent-1"},
		{name: "task id", env: "MULTICA_TASK_ID", value: "task-1"},
		{name: "task config root", env: cli.TaskConfigRootEnv, value: "/tmp/task-multica"},
		{name: "daemon port", env: "MULTICA_DAEMON_PORT", value: "20032"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markerPath := seedDaemonTaskMarker(t)
			clearDaemonEnvSignals(t)
			t.Setenv(tc.env, tc.value)

			if got := leftoverDaemonTaskMarkerPath(); got != "" {
				t.Fatalf("leftoverDaemonTaskMarkerPath() = %q; want \"\" under real task identity", got)
			}
			// MULTICA_DAEMON_PORT alone is not task identity, so it does not by
			// itself make a human-local command fail; the others do, and when
			// they do the message must stay the plain one.
			if err := requireHumanLocalCommand("daemon stop"); err != nil {
				if strings.Contains(err.Error(), "leftover") {
					t.Fatalf("env-signalled task context must not be reported as a leftover; got %q", err.Error())
				}
			}
			if _, statErr := os.Stat(markerPath); statErr != nil {
				t.Fatalf("marker must survive: %v", statErr)
			}
		})
	}
}
