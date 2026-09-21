package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupBusyRuleDoesNotBlockOtherWorkspace(t *testing.T) {
	for _, lock := range []string{"issue", "rule", "task", "workspace"} {
		t.Run(lock, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			other, _, otherIssue, otherAgent := wakeFixture(t)
			first := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "first"})
			second := wakeCreate(t, other, s, otherIssue, WakeupInput{AgentID: otherAgent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "second"})
			f.Comment(t, util.UUIDToString(issue), "first")
			wakeDispatch(t, s, first)
			f.Comment(t, util.UUIDToString(issue), "more")
			other.Comment(t, util.UUIDToString(otherIssue), "other workspace")
			f.Exec(t, "UPDATE issue_wakeup SET updated_at=now()-interval '1 day' WHERE id=$1", first.ID)
			holder, err := f.Pool.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			var sql string
			var key any
			switch lock {
			case "issue":
				sql, key = "SELECT id FROM issue WHERE id=$1 FOR NO KEY UPDATE", issue
			case "rule":
				sql, key = "SELECT id FROM issue_wakeup WHERE id=$1 FOR UPDATE", first.ID
			case "task":
				task, err := f.q.FindPendingWakeupTask(context.Background(), util.UUIDToString(first.ID))
				if err != nil {
					t.Fatal(err)
				}
				sql, key = "SELECT id FROM agent_task_queue WHERE id=$1 FOR UPDATE", task.ID
			case "workspace":
				sql, key = "SELECT id FROM workspace WHERE id=$1 FOR UPDATE", f.WorkspaceID
			}
			if _, err = holder.Exec(context.Background(), sql, key); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err = s.Tick(ctx); err == nil {
				t.Fatal("expected busy-rule diagnostic")
			}
			if ctx.Err() != nil {
				t.Fatal("busy rule consumed batch deadline")
			}
			if got := other.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(second.ID)); got != 1 {
				t.Fatalf("unrelated rule not dispatched: %d", got)
			}
			if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", first.ID); got != 1 {
				t.Fatalf("busy rule lost pending evidence: %d", got)
			}
		})
	}
}

func TestIssueWakeupPendingEventsAreBoundedAndLegacyInputsDrain(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "read all new comments"})
	// Old servers' receipts have no coalesce key and remain readable.
	f.Exec(t, `INSERT INTO issue_wakeup_receipt(id,wakeup_id,revision,event_key,event_type,payload) VALUES(gen_random_uuid(),$1,1,'legacy','comment.created','{"comment_id":"legacy-reference"}')`, w.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 80)
	for n := 0; n < 80; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.Pool.Exec(context.Background(), `INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'member',$3,'burst','comment')`, issue, f.WorkspaceID, f.UserID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	last := f.Comment(t, util.UUIDToString(issue), "latest")
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); got != 2 {
		t.Fatalf("unbounded pending receipts: %d", got)
	}
	var payload []byte
	if err := f.Pool.QueryRow(context.Background(), "SELECT payload FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND coalesce_key IS NOT NULL", w.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var fact struct {
		Count   int    `json:"coalesced_count"`
		Comment string `json:"comment_id"`
		First   string `json:"first_occurred_at"`
	}
	if err := json.Unmarshal(payload, &fact); err != nil {
		t.Fatal(err)
	}
	if fact.Count != 81 || fact.Comment != last || fact.First == "" {
		t.Fatalf("lost burst summary: %s", payload)
	}
	wakeDispatch(t, s, w)
	task, err := f.q.FindPendingWakeupTask(context.Background(), util.UUIDToString(w.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, last) || !strings.Contains(task.HandoffNote.String, "legacy-reference") || !strings.Contains(task.HandoffNote.String, wakeupOmittedEvidence) {
		t.Fatal("missing evidence or state-read instruction")
	}
	f.Comment(t, util.UUIDToString(issue), "after consumption")
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); got != 1 {
		t.Fatalf("new input after consumption lost: %d", got)
	}
}

func TestIssueWakeupCaptureRacingConsumptionKeepsNewInput(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "check"})
	f.Comment(t, util.UUIDToString(issue), "first")
	ctx := context.Background()
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := f.q.WithTx(tx)
	receipts, err := q.ListPendingWakeupReceipts(ctx, db.ListPendingWakeupReceiptsParams{WakeupID: w.ID, Revision: w.Revision})
	if err != nil {
		t.Fatal(err)
	}
	writerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.Pool.Exec(writerCtx, `INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,type) VALUES($1,$2,'member',$3,'racing','comment')`, issue, f.WorkspaceID, f.UserID)
		done <- err
	}()
	// A bounded wait proves capture cannot update evidence after dispatch read it.
	select {
	case err := <-done:
		t.Fatalf("writer bypassed receipt lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: []pgtype.UUID{receipts[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); got != 1 {
		t.Fatalf("racing input lost: %d", got)
	}
}

func TestIssueWakeupReceiptExpiryIsBoundedAndKeepsPending(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "check"})
	f.Exec(t, `INSERT INTO issue_wakeup_receipt(id,wakeup_id,revision,event_key,event_type,payload,created_at,processed_at)
 SELECT gen_random_uuid(),$1,1,n::text,'comment.created','{}',now()-interval '10 days',now()-interval '8 days' FROM generate_series(1,1050) n`, w.ID)
	f.Exec(t, `INSERT INTO issue_wakeup_receipt(id,wakeup_id,revision,event_key,event_type,payload,created_at,processed_at)
 VALUES(gen_random_uuid(),$1,1,'pending','comment.created','{}',now()-interval '10 days',NULL),
 (gen_random_uuid(),$1,1,'recent','comment.created','{}',now()-interval '10 days',now())`, w.ID)
	removed, err := f.q.DeleteExpiredWakeupReceipts(context.Background(), pgtype.Timestamptz{Time: time.Now().Add(-7 * 24 * time.Hour), Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1000 {
		t.Fatalf("cleanup not bounded: %d", removed)
	}
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_key IN ('pending','recent')", w.ID); got != 2 {
		t.Fatal("expired pending input or recent dedup key")
	}
}

func TestIssueWakeupActivationCapacityAndReenable(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	in := WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "check"}
	var first db.IssueWakeup
	for n := 0; n < 32; n++ {
		w := wakeCreate(t, f, s, issue, in)
		if n == 0 {
			first = w
		}
	}
	_, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, in)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "issue_wakeup_active_limit" {
		t.Fatalf("capacity not enforced: %v", err)
	}
	disabled, err := s.Disable(context.Background(), issue, first.ID, parseTestUUID(t, f.UserID))
	if err != nil {
		t.Fatal(err)
	}
	wakeCreate(t, f, s, issue, in)
	_, err = s.Enable(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, first.ID, WakeupEnableInput{Revision: disabled.Revision})
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "issue_wakeup_active_limit" {
		t.Fatalf("re-enable bypassed capacity: %v", err)
	}
}

func TestIssueWakeupWorkspaceCapacitySerializesDifferentIssues(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	f.Cleanup(t, "DELETE FROM issue_wakeup WHERE workspace_id=$1", f.WorkspaceID)
	issues := []pgtype.UUID{issue}
	for n := 0; n < 33; n++ {
		issues = append(issues, parseTestUUID(t, f.Issue(t, "capacity issue")))
	}
	f.Exec(t, `INSERT INTO issue_wakeup(id,workspace_id,issue_id,agent_id,created_by,instruction,kind,mode)
 SELECT gen_random_uuid(),$1,($2::uuid[])[1+(n%34)],$3,$4,'check','event','continuous' FROM generate_series(1,999) n`, f.WorkspaceID, issues, agent, f.UserID)
	errs := make(chan error, 2)
	for _, id := range issues[:2] {
		go func(id pgtype.UUID) {
			_, err := s.Create(context.Background(), id, parseTestUUID(t, f.UserID), pgtype.UUID{}, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "check"})
			errs <- err
		}(id)
	}
	var ok, full int
	for n := 0; n < 2; n++ {
		err := <-errs
		var pgErr *pgconn.PgError
		if err == nil {
			ok++
		} else if errors.As(err, &pgErr) && pgErr.ConstraintName == "issue_wakeup_active_limit" {
			full++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || full != 1 {
		t.Fatalf("workspace capacity race: success=%d full=%d", ok, full)
	}
}

// Older consumers do not lock the receipt before building a prompt. A merged
// input must survive their later UPDATE by the original receipt ID.
func TestIssueWakeupLegacyConsumerCannotConsumeUnseenMerge(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "check"})
	f.Comment(t, util.UUIDToString(issue), "first")
	ctx := context.Background()
	var observed pgtype.UUID
	if err := f.Pool.QueryRow(ctx, "SELECT id FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	latest := f.Comment(t, util.UUIDToString(issue), "arrived after old consumer read")
	if err := f.q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: []pgtype.UUID{observed}}); err != nil {
		t.Fatal(err)
	}
	if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL AND payload->>'comment_id'=$2", w.ID, latest); got != 1 {
		t.Fatalf("old consumer lost new evidence: %d", got)
	}
	wakeDispatch(t, s, w)
	task, err := f.q.FindPendingWakeupTask(ctx, util.UUIDToString(w.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, latest) {
		t.Fatal("new dispatcher lost merged reference")
	}
}
