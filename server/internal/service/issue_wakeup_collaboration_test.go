package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/eventcontract"
)

// Exercise real writes rather than calling the capture function: every advertised
// event must be reachable from the persisted collaboration state.
func TestIssueWakeupCollaborationCapture(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: eventcontract.WakeupTypes, Instruction: "Read current state"})
	count := func(event string) int {
		return f.Count(t, "SELECT COALESCE(sum(COALESCE((payload->>'coalesced_count')::int,1)),0) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_type=$2", w.ID, event)
	}
	assert := func(event string, n int) {
		t.Helper()
		if got := count(event); got != n {
			t.Fatalf("%s: got %d captured facts, want %d", event, got, n)
		}
	}
	run := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	assert("task.queued", 1)
	for _, status := range []string{"dispatched", "running", "deferred", "queued", "deferred", "waiting_local_directory", "completed", "failed", "cancelled"} {
		f.Exec(t, "UPDATE agent_task_queue SET status=$2 WHERE id=$1", run, status)
	}
	assert("task.queued", 2)
	assert("task.deferred", 2)
	f.Exec(t, "UPDATE agent_task_queue SET status=status WHERE id=$1", run)
	assert("task.cancelled", 1)
	parent := f.Issue(t, "parent")
	project := f.Project(t, "project")
	f.Exec(t, "UPDATE issue SET title='changed',status='in_progress',assignee_type='agent',assignee_id=$2,parent_issue_id=$3,project_id=$4,properties='{\"priority-note\":\"private-value\"}',metadata='{\"build\":\"private-value\"}' WHERE id=$1", issue, agent, parent, project)
	assert("issue.updated", 1)
	f.Exec(t, "UPDATE issue SET title=title,revision=revision+1,updated_at=now() WHERE id=$1", issue)
	assert("issue.updated", 1)
	label := f.Insert(t, "issue_label", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "review", "color": "red"})
	f.InsertNoID(t, "issue_to_label", testutil.Cols{"issue_id": issue, "label_id": label}, "issue_id=$1 AND label_id=$2", issue, label)
	f.Exec(t, "DELETE FROM issue_to_label WHERE issue_id=$1 AND label_id=$2", issue, label)
	assert("issue.labels_changed", 2)
	comment := f.Comment(t, util.UUIDToString(issue), "private-comment")
	f.Exec(t, "UPDATE comment SET content='private-edited-comment' WHERE id=$1", comment)
	f.Exec(t, "UPDATE comment SET resolved_at=now(),resolved_by_type='member',resolved_by_id=$2 WHERE id=$1", comment, f.UserID)
	f.Exec(t, "UPDATE comment SET resolved_at=NULL,resolved_by_type=NULL,resolved_by_id=NULL WHERE id=$1", comment)
	for _, target := range []struct{ table, key, id string }{{"comment_reaction", "comment_id", comment}, {"issue_reaction", "issue_id", util.UUIDToString(issue)}} {
		reaction := f.Insert(t, target.table, testutil.Cols{target.key: target.id, "workspace_id": f.WorkspaceID, "actor_type": "member", "actor_id": f.UserID, "emoji": "👍"})
		f.Exec(t, "DELETE FROM "+target.table+" WHERE id=$1", reaction)
	}
	assert("reaction.added", 2)
	assert("reaction.removed", 2)
	attachment := f.Insert(t, "attachment", testutil.Cols{"workspace_id": f.WorkspaceID, "uploader_type": "member", "uploader_id": f.UserID, "filename": "secret.txt", "url": "https://private.invalid/secret", "content_type": "text/plain", "size_bytes": 1})
	assert("attachment.attached", 0) // upload without an owner is not attachment
	f.Exec(t, "UPDATE attachment SET issue_id=$2 WHERE id=$1", attachment, issue)
	f.Exec(t, "UPDATE attachment SET issue_id=NULL,comment_id=$2 WHERE id=$1", attachment, comment)
	f.Exec(t, "DELETE FROM attachment WHERE id=$1", attachment)
	assert("attachment.attached", 2)
	assert("attachment.detached", 2)
	f.Exec(t, "UPDATE comment SET deleted_at=now() WHERE id=$1", comment)
	f.Exec(t, "DELETE FROM comment WHERE id=$1", comment)
	assert("comment.deleted", 1) // tombstone cleanup is not another deletion
	for _, event := range eventcontract.WakeupTypes {
		if count(event) == 0 {
			t.Errorf("advertised event %s has no captured fact", event)
		}
	}
	rows, err := f.Pool.Query(context.Background(), "SELECT payload FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"event_id", "event_type", "version", "occurred_at", "workspace_id", "issue_id", "actor_type"} {
			if payload[field] == nil {
				t.Errorf("missing %s: %s", field, raw)
			}
		}
		if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "secret") {
			t.Fatalf("private content in payload: %s", raw)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wakeDispatch(t, s, w)
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); n != 1 {
		t.Fatalf("expected coalesced run, got %d", n)
	}
}

func TestIssueWakeupNonterminalSubscriptionIsProspective(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	run := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"task.queued"}, FilterTaskID: run, Instruction: "inspect new queue input"})
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 0 {
		t.Fatal("registration replayed nonterminal current state")
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='deferred' WHERE id=$1", run)
	f.Exec(t, "UPDATE agent_task_queue SET status='queued' WHERE id=$1", run)
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 1 {
		t.Fatalf("missed future queue transition: %d", n)
	}
	for _, event := range []string{"issue.created", "issue.deleted"} {
		_, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), parseTestUUID(t, run), WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{event}, Instruction: "invalid self subscription"})
		if !errors.Is(err, ErrWakeupInput) || !strings.Contains(err.Error(), "cannot wake its own issue") {
			t.Fatalf("lifecycle scope rejection: %v", err)
		}
	}
}

func TestIssueWakeupReplyUnresolveKeepsSource(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	parent := f.Comment(t, util.UUIDToString(issue), "resolved thread", testutil.Cols{"resolved_at": testutil.Raw("now()"), "resolved_by_type": "member", "resolved_by_id": f.UserID})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.unresolved"}, Instruction: "inspect reply"})
	run := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	f.Cleanup(t, "DELETE FROM comment WHERE source_task_id=$1", run)
	f.Exec(t, "UPDATE agent_task_queue SET context=jsonb_build_object('wakeup_id',$2::text) WHERE id=$1", run, util.UUIDToString(w.ID))
	s.Tasks.createAgentComment(context.Background(), issue, parseTestUUID(t, agent), "own reply", "comment", parseTestUUID(t, parent), parseTestUUID(t, run))
	if n := f.Count(t, "SELECT count(*) FROM comment WHERE id=$1 AND resolved_at IS NULL", parent); n != 1 {
		t.Fatal("reply did not reopen thread")
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); n != 0 {
		t.Fatal("automatic unresolve caused self-loop")
	}
	f.Exec(t, "UPDATE agent_task_queue SET context='{}' WHERE id=$1", run)
	f.Exec(t, "UPDATE comment SET resolved_at=now(),resolved_by_type='member',resolved_by_id=$2 WHERE id=$1", parent, f.UserID)
	s.Tasks.createAgentComment(context.Background(), issue, parseTestUUID(t, agent), "external reply", "comment", parseTestUUID(t, parent), parseTestUUID(t, run))
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND payload->>'source_task_id'=$2", w.ID, run); n != 1 {
		t.Fatalf("external reply not attributed: %d", n)
	}
}

func TestIssueWakeupCollaborationTransactionAndActor(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	comment := f.Comment(t, util.UUIDToString(issue), "original", testutil.Cols{"author_type": "agent", "author_id": agent})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.updated", "issue.metadata_changed"}, FilterAgentID: agent, Instruction: "inspect"})
	run := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	ctx := context.Background()
	write := func(commit, own bool) {
		t.Helper()
		tx, err := f.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SELECT set_config('multica.source_task_id',$1,true),set_config('multica.actor_type','agent',true),set_config('multica.actor_id',$2,true)", run, agent); err != nil {
			t.Fatal(err)
		}
		if own {
			if _, err = tx.Exec(ctx, "UPDATE agent_task_queue SET context=jsonb_build_object('wakeup_id',$2::text) WHERE id=$1", run, util.UUIDToString(w.ID)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = tx.Exec(ctx, "UPDATE comment SET content=content||'!' WHERE id=$1", comment); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "UPDATE issue SET metadata=jsonb_build_object('changed',gen_random_uuid()) WHERE id=$1", issue); err != nil {
			t.Fatal(err)
		}
		if commit {
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := func() int { return f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID) }
	write(false, false)
	if count() != 0 {
		t.Fatal("rollback left receipts")
	}
	// The original author is not the actor of a later edit.
	f.Exec(t, "UPDATE comment SET content='system edit' WHERE id=$1", comment)
	if count() != 0 {
		t.Fatal("edit inherited original agent author")
	}
	write(true, false)
	if count() != 2 {
		t.Fatalf("trusted agent edits missing: %d", count())
	}
	write(true, true)
	if count() != 2 {
		t.Fatal("own wakeup edits caused a self-loop")
	}
	// Transaction-local identity must not leak to the next borrower.
	f.Exec(t, "UPDATE comment SET content='later system edit' WHERE id=$1", comment)
	if count() != 2 {
		t.Fatal("source identity leaked")
	}
}
