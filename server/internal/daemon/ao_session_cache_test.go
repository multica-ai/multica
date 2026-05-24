package daemon

import (
	"io"
	"log/slog"
	"testing"
)

func testDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAOIssueSessionFallbackAppliesToCommentTasks(t *testing.T) {
	d := New(Config{WorkspacesRoot: t.TempDir()}, testDiscardLogger())
	baseTask := Task{
		WorkspaceID: "workspace-1",
		AgentID:     "agent-1",
		IssueID:     "issue-1",
	}
	d.rememberAOIssueSession(baseTask, "ao", TaskResult{SessionID: "cg-7", WorkDir: "/tmp/workdir"}, testDiscardLogger())

	followup := baseTask
	followup.TriggerCommentID = "comment-1"
	d.applyAOIssueSessionFallback(&followup, "ao", testDiscardLogger())

	if followup.PriorSessionID != "cg-7" {
		t.Fatalf("PriorSessionID = %q, want cg-7", followup.PriorSessionID)
	}
	if followup.PriorWorkDir != "/tmp/workdir" {
		t.Fatalf("PriorWorkDir = %q, want /tmp/workdir", followup.PriorWorkDir)
	}
}

func TestAOIssueSessionFallbackIsNarrow(t *testing.T) {
	d := New(Config{WorkspacesRoot: t.TempDir()}, testDiscardLogger())
	baseTask := Task{WorkspaceID: "workspace-1", AgentID: "agent-1", IssueID: "issue-1"}
	d.rememberAOIssueSession(baseTask, "ao", TaskResult{SessionID: "cg-7", WorkDir: "/tmp/workdir"}, testDiscardLogger())

	direct := baseTask
	d.applyAOIssueSessionFallback(&direct, "ao", testDiscardLogger())
	if direct.PriorSessionID != "" {
		t.Fatalf("direct task unexpectedly got PriorSessionID %q", direct.PriorSessionID)
	}

	otherProvider := baseTask
	otherProvider.TriggerCommentID = "comment-1"
	d.applyAOIssueSessionFallback(&otherProvider, "codex", testDiscardLogger())
	if otherProvider.PriorSessionID != "" {
		t.Fatalf("non-AO task unexpectedly got PriorSessionID %q", otherProvider.PriorSessionID)
	}

	alreadyProvided := baseTask
	alreadyProvided.TriggerCommentID = "comment-1"
	alreadyProvided.PriorSessionID = "server-session"
	d.applyAOIssueSessionFallback(&alreadyProvided, "ao", testDiscardLogger())
	if alreadyProvided.PriorSessionID != "server-session" {
		t.Fatalf("server-provided PriorSessionID was overwritten: %q", alreadyProvided.PriorSessionID)
	}
}

func TestAOIssueSessionCachePersistsAcrossDaemonRestart(t *testing.T) {
	root := t.TempDir()
	logger := testDiscardLogger()
	d1 := New(Config{WorkspacesRoot: root}, logger)
	task := Task{WorkspaceID: "workspace-1", AgentID: "agent-1", IssueID: "issue-1"}
	d1.rememberAOIssueSession(task, "ao", TaskResult{SessionID: "cg-7", WorkDir: "/tmp/workdir"}, logger)

	d2 := New(Config{WorkspacesRoot: root}, logger)
	followup := task
	followup.TriggerCommentID = "comment-1"
	d2.applyAOIssueSessionFallback(&followup, "ao", logger)

	if followup.PriorSessionID != "cg-7" {
		t.Fatalf("persisted PriorSessionID = %q, want cg-7", followup.PriorSessionID)
	}
}
