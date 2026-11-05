package lark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueNoticeFixture struct {
	*testutil.Fixture
	issue, agent, runtime, task, session, installation, binding, member string
	q                                                                   *db.Queries
}

// Only synthetic sources are created; no Feishu API or agent runtime is invoked.
func newIssueNoticeFixture(t *testing.T, pool *pgxpool.Pool) *issueNoticeFixture {
	t.Helper()
	f := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	f.UserID = f.User(t, "Source reporter", "notice-"+suffix+"@example.invalid")
	f.WorkspaceID = f.Workspace(t, "Source notices", "notice-"+suffix, testutil.Cols{"issue_prefix": "TEST"})
	n := &issueNoticeFixture{Fixture: f, q: db.New(pool)}
	n.member = f.Member(t, f.WorkspaceID, f.UserID, "member")
	n.runtime = f.Runtime(t, "Synthetic test runtime")
	n.agent = f.Agent(t, "Source assistant", n.runtime)
	n.session = f.ChatSession(t, n.agent)
	n.installation = f.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": f.WorkspaceID, "agent_id": n.agent, "channel_type": "feishu", "installer_user_id": f.UserID,
		"config": testutil.Raw(`'{"app_id":"cli_notice","binding_generation":"original"}'::jsonb`),
	})
	n.binding = f.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": n.session, "installation_id": n.installation, "channel_type": "feishu", "chat_type": "group", "channel_chat_id": "oc_original",
	})
	f.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id": f.WorkspaceID, "multica_user_id": f.UserID, "installation_id": n.installation, "channel_type": "feishu", "channel_user_id": "ou_reporter",
	})
	n.task = f.Task(t, n.agent, testutil.Cols{
		"runtime_id": n.runtime, "chat_session_id": n.session, "status": "completed", "originator_user_id": f.UserID, "accountable_user_id": f.UserID,
	})
	f.InsertNoID(t, "channel_task_delivery", testutil.Cols{
		"task_id": n.task, "binding_id": n.binding, "installation_id": n.installation, "channel_type": "feishu", "chat_type": "group",
		"channel_chat_id": "oc_original", "channel_sender_id": "ou_reporter", "channel_message_id": "om_original", "route_revision": 1,
	}, "task_id=$1", n.task)
	n.issue = f.Issue(t, "Source request", testutil.Cols{"creator_type": "agent", "creator_id": n.agent, "origin_type": "agent_create", "origin_id": n.task})
	f.Cleanup(t, "DELETE FROM channel_issue_notice WHERE workspace_id=$1", f.WorkspaceID)
	return n
}

func (n *issueNoticeFixture) transition(t *testing.T, status string) {
	t.Helper()
	n.Exec(t, "UPDATE issue SET status=$1,revision=revision+1 WHERE id=$2", status, n.issue)
}

func (n *issueNoticeFixture) assertNotice(t *testing.T, status string, attempts int) {
	t.Helper()
	var got string
	var count int
	n.QueryRow(t, "SELECT status,attempts FROM channel_issue_notice WHERE issue_id=$1 ORDER BY created_at DESC LIMIT 1", n.issue).Scan(&got, &count)
	if got != status || count != attempts {
		t.Fatalf("notice status=%s attempts=%d; want %s/%d", got, count, status, attempts)
	}
}

func TestIssueNoticeDatabase(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit disposable DATABASE_URL with CREATEDB required")
	}
	// Claims span workspaces, so never consume another suite's or user's queue.
	pool := testutil.IsolatedDB(t, dsn)
	ctx := context.Background()

	t.Run("atomic transition and duplicate suppression", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		if _, err := tx.Exec(ctx, "UPDATE issue SET status='blocked',revision=revision+1 WHERE id=$1", n.issue); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue).Scan(&count); err != nil || count != 1 {
			t.Fatalf("notification not part of business transaction: count=%d err=%v", count, err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 0 {
			t.Fatal("rolled back transition left a notification")
		}
		n.transition(t, "blocked")
		n.Exec(t, "UPDATE issue SET status='blocked' WHERE id=$1", n.issue)
		n.Exec(t, "SELECT queue_channel_issue_notice($1,'blocked',NULL)", n.issue)
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 1 {
			t.Fatal("same transition enqueued twice")
		}
	})

	t.Run("source group and recipient remain frozen", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "done")
		n.Exec(t, "UPDATE channel_chat_session_binding SET last_message_id='om_later' WHERE id=$1", n.binding)
		api := &fakeAPIClient{textSendReturn: "om_notice"}
		w := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice)
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "sent", 1)
		if len(api.textSent) != 1 || api.textSent[0].ChatID != "oc_original" || !strings.Contains(api.textSent[0].Text, `user_id="ou_reporter"`) || strings.Contains(api.textSent[0].Text, "ou_other") {
			t.Fatalf("notification followed a moving session cursor: %+v", api.textSent)
		}
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		if len(api.textSent) != 1 {
			t.Fatal("settled notification sent twice")
		}
	})

	t.Run("expired lease fences stale owner and restart recovers", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "blocked")
		owner := util.MustParseUUID(uuid.NewString())
		first, err := n.q.ClaimChannelIssueNotice(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := n.q.ClaimChannelIssueNotice(ctx, util.MustParseUUID(uuid.NewString())); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("live lease duplicated: %v", err)
		}
		n.Exec(t, "UPDATE channel_issue_notice SET retry_at=now()-interval '1 second' WHERE id=$1", first.ID)
		api := &fakeAPIClient{textSendReturn: "om_recovered"}
		if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "sent", 2)
		if len(api.textSent) != 1 || api.textSent[0].UUID != util.UUIDToString(first.ID) {
			t.Fatal("restart changed idempotency key")
		}
		changed, err := n.q.FinishChannelIssueNotice(ctx, db.FinishChannelIssueNoticeParams{ID: first.ID, OwnerToken: owner, Status: "failed"})
		if err != nil || changed != 0 {
			t.Fatalf("stale owner overwrote receipt: changed=%d err=%v", changed, err)
		}
	})

	t.Run("bounded retry and stable send key", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "done")
		api := &fakeAPIClient{textSendErr: errors.New("ambiguous network response")}
		for attempt := 1; attempt <= 3; attempt++ {
			before := time.Now()
			if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
				t.Fatal(err)
			}
			state := "queued"
			if attempt == 3 {
				state = "failed"
			}
			n.assertNotice(t, state, attempt)
			var retry time.Time
			n.QueryRow(t, "SELECT retry_at FROM channel_issue_notice WHERE issue_id=$1", n.issue).Scan(&retry)
			if attempt < 3 && (retry.Before(before.Add(time.Duration(15<<attempt)*time.Second)) || retry.After(time.Now().Add(time.Duration((15<<attempt)+11)*time.Second))) {
				t.Fatalf("retry lacks bounded exponential backoff: %v", retry)
			}
			n.Exec(t, "UPDATE channel_issue_notice SET retry_at=now()-interval '1 second' WHERE issue_id=$1", n.issue)
		}
		if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
			t.Fatal(err)
		}
		if len(api.textSent) != 3 || api.textSent[0].UUID != api.textSent[1].UUID || api.textSent[1].UUID != api.textSent[2].UUID {
			t.Fatal("retry duplicated keys or exceeded its limit")
		}
	})

	t.Run("delivery window expires without replay", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "done")
		n.Exec(t, "UPDATE channel_issue_notice SET first_attempt_at=now()-interval '5 minutes',attempts=1 WHERE issue_id=$1", n.issue)
		api := &fakeAPIClient{}
		if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "failed", 1)
		if len(api.textSent) != 0 {
			t.Fatal("replayed beyond transport deduplication window")
		}
	})

	for _, revoke := range []struct{ name, sql string }{
		{"workspace membership", "DELETE FROM member WHERE id=$1"},
		{"installation", "UPDATE channel_installation SET status='revoked' WHERE id=$1"},
		{"installation generation", `UPDATE channel_installation SET config=config || '{"binding_generation":"replacement"}'::jsonb WHERE id=$1`},
		{"different workspace", "UPDATE channel_installation SET workspace_id=gen_random_uuid() WHERE id=$1"},
		{"private chat", "UPDATE channel_chat_session_binding SET chat_type='p2p' WHERE id=$1"},
		{"other channel", "UPDATE channel_chat_session_binding SET channel_type='slack' WHERE id=$1"},
		{"recipient relink", "UPDATE channel_user_binding SET multica_user_id=gen_random_uuid() WHERE installation_id=$1"},
		{"archived agent", "UPDATE agent SET archived_at=now() WHERE id=$1"},
	} {
		t.Run("cancel after revoking "+revoke.name, func(t *testing.T) {
			n := newIssueNoticeFixture(t, pool)
			n.transition(t, "blocked")
			id := n.installation
			switch revoke.name {
			case "workspace membership":
				id = n.member
			case "private chat", "other channel":
				id = n.binding
			case "archived agent":
				id = n.agent
			}
			n.Exec(t, revoke.sql, id)
			api := &fakeAPIClient{}
			if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
				t.Fatal(err)
			}
			n.assertNotice(t, "cancelled", 1)
			if len(api.textSent) != 0 {
				t.Fatal("sent after route permission was revoked")
			}
		})
	}

	t.Run("custom status categories and superseded transitions", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		for _, kind := range []string{"started", "done"} {
			n.Insert(t, "issue_status", testutil.Cols{"workspace_id": n.WorkspaceID, "key": "custom_" + kind, "name": "Custom " + kind, "category": kind, "color": "#123456"})
		}
		n.transition(t, "blocked")
		n.transition(t, "custom_done")
		n.transition(t, "done")
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 2 {
			t.Fatal("done category alias created a second completion")
		}
		api := &fakeAPIClient{textSendReturn: "om_current"}
		w := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice)
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		if len(api.textSent) != 1 || !strings.Contains(api.textSent[0].Text, "Completed") {
			t.Fatalf("superseded status delivered: %+v", api.textSent)
		}
		n.transition(t, "done")
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		if len(api.textSent) != 1 {
			t.Fatal("done-to-done category change repeated completion")
		}
	})

	t.Run("failed run is delayed and suppressed by a successor", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		failed := n.Task(t, n.agent, testutil.Cols{"issue_id": n.issue, "runtime_id": n.runtime})
		n.Exec(t, "UPDATE agent_task_queue SET status='failed' WHERE id=$1", failed)
		api := &fakeAPIClient{}
		w := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice)
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "queued", 0)
		n.Task(t, n.agent, testutil.Cols{"issue_id": n.issue, "runtime_id": n.runtime, "created_at": testutil.Raw("now()+interval '1 second'")})
		n.Exec(t, "UPDATE channel_issue_notice SET retry_at=now()-interval '1 second' WHERE issue_id=$1", n.issue)
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "cancelled", 1)
		if len(api.textSent) != 0 {
			t.Fatal("stale failure survived a new run")
		}
	})

	t.Run("web source and completed task do not imply completed issue", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		work := n.Task(t, n.agent, testutil.Cols{"issue_id": n.issue, "runtime_id": n.runtime})
		n.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", work)
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 0 {
			t.Fatal("task completion reported issue completion")
		}
		n.Exec(t, "UPDATE issue SET origin_type=NULL WHERE id=$1", n.issue)
		n.transition(t, "done")
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 0 {
			t.Fatal("invented a channel source for web issue")
		}
	})

	t.Run("invocation policy denial and transient lookup failure", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "blocked")
		api := &fakeAPIClient{}
		called := false
		w := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", func(_ context.Context, agent, user pgtype.UUID) (bool, error) {
			called = true
			if util.UUIDToString(agent) != n.agent || util.UUIDToString(user) != n.UserID {
				t.Fatal("invocation checked the wrong principal")
			}
			return false, errors.New("temporary lookup failure")
		})
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "queued", 1)
		n.Exec(t, "UPDATE channel_issue_notice SET retry_at=now()-interval '1 second' WHERE issue_id=$1", n.issue)
		w.mayInvoke = func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) { return false, nil }
		if err := w.deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "cancelled", 2)
		if !called || len(api.textSent) != 0 {
			t.Fatal("notification bypassed invocation policy")
		}
	})

	t.Run("unreceipted send remains retryable", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "done")
		api := &fakeAPIClient{}
		if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "queued", 1)
	})

	t.Run("run failure reports failure without raw error text", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		work := n.Task(t, n.agent, testutil.Cols{"issue_id": n.issue, "runtime_id": n.runtime})
		n.Exec(t, "UPDATE agent_task_queue SET status='failed',error='private runtime diagnostics' WHERE id=$1", work)
		n.Exec(t, "UPDATE channel_issue_notice SET retry_at=now()-interval '1 second' WHERE issue_id=$1", n.issue)
		api := &fakeAPIClient{textSendReturn: "om_failure"}
		if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "sent", 1)
		if len(api.textSent) != 1 || !strings.Contains(api.textSent[0].Text, "Run failed") || strings.Contains(api.textSent[0].Text, "private runtime diagnostics") {
			t.Fatal("failure notification leaked diagnostics or lost its state")
		}
	})

	t.Run("cross workspace delete cannot erase a receipt", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "done")
		if err := n.q.DeleteIssue(ctx, db.DeleteIssueParams{ID: util.MustParseUUID(n.issue), WorkspaceID: util.MustParseUUID(uuid.NewString())}); err != nil {
			t.Fatal(err)
		}
		n.assertNotice(t, "queued", 0)
		if err := n.q.DeleteIssue(ctx, db.DeleteIssueParams{ID: util.MustParseUUID(n.issue), WorkspaceID: util.MustParseUUID(n.WorkspaceID)}); err != nil {
			t.Fatal(err)
		}
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 0 {
			t.Fatal("issue deletion orphaned a notification")
		}
	})

	t.Run("untrusted source is rejected before enqueue", func(t *testing.T) {
		n := newIssueNoticeFixture(t, pool)
		n.Exec(t, "UPDATE channel_installation SET workspace_id=gen_random_uuid() WHERE id=$1", n.installation)
		n.transition(t, "done")
		if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE issue_id=$1", n.issue) != 0 {
			t.Fatal("cross tenant source entered the queue")
		}
	})

	t.Run("topic route cannot fall through to the parent group", func(t *testing.T) {
		for _, missingTrigger := range []bool{false, true} {
			t.Run(map[bool]string{false: "original topic", true: "missing trigger"}[missingTrigger], func(t *testing.T) {
				n := newIssueNoticeFixture(t, pool)
				n.Exec(t, `UPDATE channel_chat_session_binding SET channel_chat_id='oc_original:thread',config='{"chat_id":"oc_original"}'::jsonb WHERE id=$1`, n.binding)
				n.Exec(t, `UPDATE channel_task_delivery SET channel_chat_id='oc_original:thread',channel_thread_id='omt_original',config='{"chat_id":"oc_original"}'::jsonb WHERE task_id=$1`, n.task)
				if missingTrigger {
					n.Exec(t, "UPDATE channel_task_delivery SET channel_message_id=NULL WHERE task_id=$1", n.task)
				}
				n.transition(t, "blocked")
				api := &fakeAPIClient{textSendReturn: "om_topic"}
				if err := NewIssueNoticeWorker(n.q, fakeCredentials{secret: "test"}, api, "https://example.invalid", allowIssueNotice).deliver(ctx); err != nil {
					t.Fatal(err)
				}
				if missingTrigger {
					n.assertNotice(t, "cancelled", 1)
					if len(api.textSent) != 0 {
						t.Fatal("topic notification leaked into parent group")
					}
				} else if len(api.textSent) != 1 || api.textSent[0].ChatID != "oc_original" || api.textSent[0].ReplyTarget.MessageID != "om_original" || !api.textSent[0].ReplyTarget.InThread {
					t.Fatalf("lost original topic: %+v", api.textSent)
				}
			})
		}
	})

	for _, legacy := range []bool{false, true} {
		name := "workspace deletion leaf cleanup"
		if legacy {
			name = "legacy workspace deletion cleanup"
		}
		t.Run(name, func(t *testing.T) {
			n := newIssueNoticeFixture(t, pool)
			n.transition(t, "done")
			var err error
			if legacy {
				err = n.q.DeleteWorkspace(ctx, util.MustParseUUID(n.WorkspaceID))
			} else {
				err = n.q.DeleteWorkspaceLeafData(ctx, util.MustParseUUID(n.WorkspaceID))
			}
			if err != nil {
				t.Fatal(err)
			}
			if n.Count(t, "SELECT count(*) FROM channel_issue_notice WHERE workspace_id=$1", n.WorkspaceID) != 0 {
				t.Fatal("workspace cleanup orphaned a notice")
			}
		})
	}

	t.Run("new migrations roll back and reapply", func(t *testing.T) {
		// Validate rollback and concurrent index rebuilds only on this isolated DB.
		for _, direction := range []string{"down", "up"} {
			files, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*channel_issue_notice*."+direction+".sql"))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 7 {
				t.Fatalf("expected seven new %s migrations; got %d", direction, len(files))
			}
			if direction == "down" {
				sort.Sort(sort.Reverse(sort.StringSlice(files)))
			}
			for _, file := range files {
				if !strings.Contains(filepath.Base(file), "channel_issue_notice") {
					continue
				}
				sql, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, string(sql)); err != nil {
					t.Fatalf("%s: %v", file, err)
				}
			}
			var exists bool
			if err := pool.QueryRow(ctx, "SELECT to_regclass('channel_issue_notice') IS NOT NULL").Scan(&exists); err != nil || exists != (direction == "up") {
				t.Fatalf("%s table present=%v err=%v", direction, exists, err)
			}
		}
		n := newIssueNoticeFixture(t, pool)
		n.transition(t, "blocked")
		n.assertNotice(t, "queued", 0)
	})
}

func allowIssueNotice(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) { return true, nil }
