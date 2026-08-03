package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/spf13/cobra"
)

// newDiskUsageTestCmd mirrors the flag set registered on daemonDiskUsageCmd so
// runDaemonDiskUsage can be driven directly without touching the global command.
func newDiskUsageTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.Bool("by-workspace", false, "")
	f.Bool("by-task", false, "")
	f.Int("top", 0, "")
	f.String("output", "table", "")
	f.String("workspaces-root", "", "")
	f.Bool("all-profiles", false, "")
	f.String("profile", "", "")
	f.String("server-url", "", "")
	return cmd
}

// gcCheckRecorder is a stand-in for the server's batch gc-check endpoint. It
// records every request so tests can assert on both call count and the
// credentials each call carried.
type gcCheckRecorder struct {
	mu sync.Mutex
	// tokenByIssue maps a requested issue id to the bearer token it was
	// requested with, so --all-profiles can be checked per profile.
	tokenByIssue map[string]string
	calls        int
	statusCode   int
}

func newGCCheckServer(t *testing.T, rec *gcCheckRecorder) *httptest.Server {
	t.Helper()
	rec.tokenByIssue = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues/gc-check") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			IssueIDs []string `json:"issue_ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		rec.mu.Lock()
		rec.calls++
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		for _, id := range body.IssueIDs {
			rec.tokenByIssue[id] = token
		}
		code := rec.statusCode
		rec.mu.Unlock()

		if code != 0 && code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		issues := make([]map[string]any, 0, len(body.IssueIDs))
		for _, id := range body.IssueIDs {
			issues = append(issues, map[string]any{
				"id": id, "found": true, "status": "in_review",
				"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": issues})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *gcCheckRecorder) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// writeDiskUsageIssueTask creates a task dir carrying issue-kind GC metadata,
// which is what makes it eligible for status resolution.
func writeDiskUsageIssueTask(t *testing.T, root, wsID, taskShort, issueID string) {
	t.Helper()
	taskDir := filepath.Join(root, wsID, taskShort)
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "workdir", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{
		"kind":         "issue",
		"issue_id":     issueID,
		"workspace_id": wsID,
		"completed_at": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, ".gc_meta.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupDiskUsageProfile points a profile's CLI config at srv and seeds one
// issue-kind task dir under that profile's workspaces root.
func setupDiskUsageProfile(t *testing.T, home, profile, token, serverURL, wsID, taskShort, issueID string) {
	t.Helper()
	root := filepath.Join(home, "multica_workspaces")
	if profile != "" {
		root += "_" + profile
		mkdirProfile(t, home, profile)
	}
	writeDiskUsageIssueTask(t, root, wsID, taskShort, issueID)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{
		ServerURL: serverURL,
		Token:     token,
	}, profile); err != nil {
		t.Fatal(err)
	}
}

// TestDiskUsageNeedsParentStatus pins the gate itself: the per-task table and
// any JSON output need statuses; the --by-workspace table has no STATUS column
// and must not trigger a request.
func TestDiskUsageNeedsParentStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		byWorkspace bool
		output      string
		want        bool
	}{
		{false, "table", true}, // per-task table renders STATUS
		{false, "json", true},
		{true, "json", true},   // JSON always carries the task array
		{true, "table", false}, // per-workspace table has no STATUS column
	}
	for _, tc := range cases {
		if got := diskUsageNeedsParentStatus(tc.byWorkspace, tc.output); got != tc.want {
			t.Errorf("diskUsageNeedsParentStatus(byWorkspace=%v, output=%q) = %v, want %v",
				tc.byWorkspace, tc.output, got, tc.want)
		}
	}
}

// TestRunDaemonDiskUsageByWorkspaceTableMakesNoRequest is the offline-regression
// guard: with valid credentials on disk, the --by-workspace table must still
// resolve nothing, so an unreachable server cannot make a local diagnostic hang.
func TestRunDaemonDiskUsageByWorkspaceTableMakesNoRequest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	t.Setenv("MULTICA_SERVER_URL", "")

	rec := &gcCheckRecorder{}
	srv := newGCCheckServer(t, rec)
	setupDiskUsageProfile(t, home, "", "token-default", srv.URL,
		"11111111-1111-1111-1111-111111111111", "aaaaaaaa", "issue-1")

	cmd := newDiskUsageTestCmd(t)
	if err := cmd.Flags().Set("by-workspace", "true"); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runDaemonDiskUsage(cmd, nil) })
	if err != nil {
		t.Fatalf("runDaemonDiskUsage: %v", err)
	}
	if rec.callCount() != 0 {
		t.Errorf("made %d gc-check request(s); --by-workspace table must not go to the network", rec.callCount())
	}
	if !strings.Contains(out, "Workspaces root:") {
		t.Errorf("expected a per-workspace table, got:\n%s", out)
	}
}

// TestRunDaemonDiskUsageTaskTableResolvesStatus is the positive half: the
// default per-task view does resolve, and the status reaches the rendered table.
func TestRunDaemonDiskUsageTaskTableResolvesStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	t.Setenv("MULTICA_SERVER_URL", "")

	rec := &gcCheckRecorder{}
	srv := newGCCheckServer(t, rec)
	setupDiskUsageProfile(t, home, "", "token-default", srv.URL,
		"11111111-1111-1111-1111-111111111111", "aaaaaaaa", "issue-1")

	out, err := captureStdout(t, func() error { return runDaemonDiskUsage(newDiskUsageTestCmd(t), nil) })
	if err != nil {
		t.Fatalf("runDaemonDiskUsage: %v", err)
	}
	if rec.callCount() != 1 {
		t.Errorf("gc-check calls = %d, want 1", rec.callCount())
	}
	if !strings.Contains(out, "in_review") {
		t.Errorf("STATUS column missing the resolved status, got:\n%s", out)
	}
}

// TestRunDaemonDiskUsageJSONSurvivesServerFailure pins the best-effort
// contract: a failing server must not fail the command or corrupt stdout, so
// scripts consuming --output json keep working when the API is down.
func TestRunDaemonDiskUsageJSONSurvivesServerFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	t.Setenv("MULTICA_SERVER_URL", "")

	rec := &gcCheckRecorder{statusCode: http.StatusInternalServerError}
	srv := newGCCheckServer(t, rec)
	setupDiskUsageProfile(t, home, "", "token-default", srv.URL,
		"11111111-1111-1111-1111-111111111111", "aaaaaaaa", "issue-1")

	cmd := newDiskUsageTestCmd(t)
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runDaemonDiskUsage(cmd, nil) })
	if err != nil {
		t.Fatalf("a failing gc-check must not fail the command, got: %v", err)
	}

	var report struct {
		Tasks []struct {
			ParentID     string `json:"parent_id"`
			ParentStatus string `json:"parent_status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout must stay valid JSON when resolution fails: %v\n%s", err, out)
	}
	if len(report.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(report.Tasks))
	}
	if report.Tasks[0].ParentStatus != "" {
		t.Errorf("parent_status = %q, want empty after a failed resolve", report.Tasks[0].ParentStatus)
	}
	if report.Tasks[0].ParentID != "issue-1" {
		t.Errorf("parent_id = %q, want issue-1 (local scan data survives)", report.Tasks[0].ParentID)
	}
}

// TestRunDaemonDiskUsageAllProfilesUsesPerProfileToken guards the credential
// wiring in aggregate mode: each root must be resolved with its own profile's
// token, not whichever one happened to be loaded first.
func TestRunDaemonDiskUsageAllProfilesUsesPerProfileToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	t.Setenv("MULTICA_SERVER_URL", "")

	rec := &gcCheckRecorder{}
	srv := newGCCheckServer(t, rec)
	setupDiskUsageProfile(t, home, "", "token-default", srv.URL,
		"11111111-1111-1111-1111-111111111111", "aaaaaaaa", "issue-default")
	setupDiskUsageProfile(t, home, "second", "token-second", srv.URL,
		"22222222-2222-2222-2222-222222222222", "bbbbbbbb", "issue-second")

	cmd := newDiskUsageTestCmd(t)
	if err := cmd.Flags().Set("all-profiles", "true"); err != nil {
		t.Fatal(err)
	}

	if _, err := captureStdout(t, func() error { return runDaemonDiskUsage(cmd, nil) }); err != nil {
		t.Fatalf("runDaemonDiskUsage --all-profiles: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := rec.tokenByIssue["issue-default"]; got != "token-default" {
		t.Errorf("default root resolved with token %q, want token-default", got)
	}
	if got := rec.tokenByIssue["issue-second"]; got != "token-second" {
		t.Errorf("second profile root resolved with token %q, want token-second", got)
	}
}

// TestPrintRepoCacheLineAppearsInEveryView guards the branch this first shipped
// wrong: both table renderers have a truncated (--top) path that returns early
// and a full-total path, and the repo cache line has to survive all of them.
// Otherwise the footprint that motivated the whole accounting change stays
// invisible in exactly the view users run most.
func TestPrintRepoCacheLineAppearsInEveryView(t *testing.T) {
	t.Parallel()

	const wantLine = "Repo cache (.repos): 4.0 KiB across 2 repo(s)"
	report := daemon.DiskUsageReport{
		WorkspacesRoot:     "/root",
		Tasks:              []daemon.TaskDiskUsage{{WorkspaceShort: "ws0", TaskShort: "t0", SizeBytes: 100}},
		Workspaces:         []daemon.WorkspaceDiskUsage{{WorkspaceShort: "ws0", TaskCount: 1, SizeBytes: 100}},
		RepoCacheSizeBytes: 4096,
		RepoCacheCount:     2,
	}

	cases := []struct {
		name  string
		print func(io.Writer, daemon.DiskUsageReport)
		// truncated makes len(slice) < Total*Count, taking the --top branch.
		truncated bool
	}{
		{"task table, full totals", printDiskUsageTaskTable, false},
		{"task table, --top truncated", printDiskUsageTaskTable, true},
		{"workspace table, full totals", printDiskUsageWorkspaceTable, false},
		{"workspace table, --top truncated", printDiskUsageWorkspaceTable, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := report
			r.TotalTaskCount = 1
			r.TotalWorkspaceCount = 1
			if tc.truncated {
				r.TotalTaskCount = 50
				r.TotalWorkspaceCount = 50
			}
			var buf bytes.Buffer
			tc.print(&buf, r)
			if !strings.Contains(buf.String(), wantLine) {
				t.Errorf("missing %q in:\n%s", wantLine, buf.String())
			}
		})
	}
}

// TestPrintRepoCacheLineSilentWhenEmpty keeps the line out of reports from
// machines that have never cached a repo.
func TestPrintRepoCacheLineSilentWhenEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printDiskUsageTaskTable(&buf, daemon.DiskUsageReport{
		WorkspacesRoot: "/root",
		Tasks:          []daemon.TaskDiskUsage{{WorkspaceShort: "ws0", TaskShort: "t0", SizeBytes: 100}},
		TotalTaskCount: 1,
	})
	if strings.Contains(buf.String(), "Repo cache") {
		t.Errorf("empty repo cache must not print a line:\n%s", buf.String())
	}
}
