package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{int64(1.5 * 1024 * 1024), "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, "-"},
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-49 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := humanAge(c.t); got != c.want {
			t.Errorf("humanAge(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestScanWorkspacesDu(t *testing.T) {
	root := t.TempDir()

	// workspace dir with two task subdirs, one with gc_meta
	wsID := "ws-abc"
	wsDir := filepath.Join(root, wsID)
	mustMkdir(t, filepath.Join(wsDir, "task1", "workdir"))
	mustMkdir(t, filepath.Join(wsDir, "task2"))
	mustWriteBytes(t, filepath.Join(wsDir, "task1", "workdir", "big.bin"), 4096)
	mustWriteBytes(t, filepath.Join(wsDir, "task2", "small.txt"), 100)
	mustWriteString(t, filepath.Join(wsDir, "task1", ".gc_meta.json"),
		`{"issue_id":"issue-xyz","completed_at":"2026-04-24T10:00:00Z"}`)

	// .repos cache
	mustMkdir(t, filepath.Join(root, ".repos", "cached"))
	mustWriteBytes(t, filepath.Join(root, ".repos", "cached", "c.bin"), 200)

	// stray file at root should be ignored
	mustWriteBytes(t, filepath.Join(root, "stray.txt"), 50)

	report, err := scanWorkspacesDu(root)
	if err != nil {
		t.Fatalf("scanWorkspacesDu: %v", err)
	}

	if got, want := report.CacheBytes, int64(200); got != want {
		t.Errorf("CacheBytes = %d, want %d", got, want)
	}
	if got, want := len(report.Workspaces), 1; got != want {
		t.Fatalf("len(Workspaces) = %d, want %d", got, want)
	}
	if got := report.Workspaces[0].Tasks; got != 2 {
		t.Errorf("Tasks = %d, want 2", got)
	}
	if got := report.Workspaces[0].SizeBytes; got < 4096 {
		t.Errorf("SizeBytes = %d, want >= 4096", got)
	}
	if got, want := len(report.TopTasks), 2; got != want {
		t.Fatalf("len(TopTasks) = %d, want %d", got, want)
	}

	// task1 should have issue metadata populated
	var task1 *taskDu
	for i := range report.TopTasks {
		if report.TopTasks[i].TaskShort == "task1" {
			task1 = &report.TopTasks[i]
			break
		}
	}
	if task1 == nil {
		t.Fatal("task1 missing from TopTasks")
	}
	if task1.IssueID != "issue-xyz" {
		t.Errorf("task1.IssueID = %q, want issue-xyz", task1.IssueID)
	}
	if task1.CompletedAt != "2026-04-24T10:00:00Z" {
		t.Errorf("task1.CompletedAt = %q", task1.CompletedAt)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteBytes(t *testing.T, path string, size int) {
	t.Helper()
	buf := make([]byte, size)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustWriteString(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
