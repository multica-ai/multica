package lark

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeCallbackQueries struct {
	issuestatus.Querier // unused: the tests only use built-in statuses

	issue     db.Issue
	callbacks []db.IssueChannelCallback
	inst      Installation
	instErr   error
	ledger    map[string]db.IssueChannelCallbackDelivery
}

func (f *fakeCallbackQueries) GetIssue(context.Context, pgtype.UUID) (db.Issue, error) {
	return f.issue, nil
}

func (f *fakeCallbackQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{IssuePrefix: "MUL", Slug: "acme"}, nil
}

func (f *fakeCallbackQueries) GetLarkInstallation(context.Context, pgtype.UUID) (Installation, error) {
	return f.inst, f.instErr
}

func (f *fakeCallbackQueries) ListActiveIssueChannelCallbacks(_ context.Context, arg db.ListActiveIssueChannelCallbacksParams) ([]db.IssueChannelCallback, error) {
	if arg.ChannelType != channelTypeFeishu {
		return nil, nil
	}
	return f.callbacks, nil
}

func (f *fakeCallbackQueries) ClaimIssueChannelCallbackDelivery(_ context.Context, arg db.ClaimIssueChannelCallbackDeliveryParams) (db.IssueChannelCallbackDelivery, error) {
	key := uuidString(arg.CallbackID) + "|" + arg.DedupKey
	if _, ok := f.ledger[key]; ok {
		return db.IssueChannelCallbackDelivery{}, pgx.ErrNoRows
	}
	row := db.IssueChannelCallbackDelivery{CallbackID: arg.CallbackID, DedupKey: arg.DedupKey, Event: arg.Event, Status: "pending"}
	f.ledger[key] = row
	return row, nil
}

func (f *fakeCallbackQueries) FinishIssueChannelCallbackDelivery(_ context.Context, arg db.FinishIssueChannelCallbackDeliveryParams) error {
	key := uuidString(arg.CallbackID) + "|" + arg.DedupKey
	row := f.ledger[key]
	row.Status, row.ProviderMessageID, row.Error = arg.Status, arg.ProviderMessageID, arg.Error
	f.ledger[key] = row
	return nil
}

func (f *fakeCallbackQueries) only(t *testing.T) db.IssueChannelCallbackDelivery {
	t.Helper()
	if len(f.ledger) != 1 {
		t.Fatalf("ledger = %+v, want one entry", f.ledger)
	}
	for _, row := range f.ledger {
		return row
	}
	return db.IssueChannelCallbackDelivery{}
}

func newCallbackFixture(status string) (*fakeCallbackQueries, *fakeAPIClient, *IssueCallbackNotifier) {
	ws := testUUID(0x11)
	q := &fakeCallbackQueries{
		issue: db.Issue{
			ID: testUUID(0x22), WorkspaceID: ws, Number: 7, Title: "Fix login", Status: status,
			UpdatedAt: pgtype.Timestamptz{Time: time.Unix(1700000000, 0), Valid: true},
		},
		callbacks: []db.IssueChannelCallback{{
			ID: testUUID(0x33), WorkspaceID: ws, IssueID: testUUID(0x22), InstallationID: testUUID(0x44),
			ChannelType: "feishu", ChatID: "oc_group", ChatType: "group", AnchorMessageID: "om_issue_cmd",
		}},
		inst:   Installation{ID: testUUID(0x44), WorkspaceID: ws, AppID: "cli_app", Status: string(InstallationActive)},
		ledger: map[string]db.IssueChannelCallbackDelivery{},
	}
	client := &fakeAPIClient{textSendReturn: "om_notice"}
	n := NewIssueCallbackNotifier(q, fakeCredentials{secret: "s"}, client, "https://app.example.com", nil)
	return q, client, n
}

func testUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

func TestIssueCallbackNotifierRepliesInThreadUnderTheCommand(t *testing.T) {
	q, client, n := newCallbackFixture(issuestatus.Done)
	if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err != nil {
		t.Fatalf("NotifyStatusChanged: %v", err)
	}
	if len(client.textSent) != 1 {
		t.Fatalf("sent = %+v, want one notice", client.textSent)
	}
	sent := client.textSent[0]
	if sent.ChatID != "oc_group" || sent.ReplyTarget != (ReplyTarget{MessageID: "om_issue_cmd", InThread: true}) {
		t.Fatalf("notice addressed to %q %+v", sent.ChatID, sent.ReplyTarget)
	}
	if want := "Done: MUL-7 — Fix login\nhttps://app.example.com/acme/issues/MUL-7"; sent.Text != want {
		t.Fatalf("text = %q, want %q", sent.Text, want)
	}
	row := q.only(t)
	if row.Status != "delivered" || row.ProviderMessageID != "om_notice" || row.Event != "status:done" {
		t.Fatalf("ledger row = %+v", row)
	}
}

func TestIssueCallbackNotifierReplayedEventSendsOnce(t *testing.T) {
	q, client, n := newCallbackFixture(issuestatus.InReview)
	for range 2 {
		if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err != nil {
			t.Fatalf("NotifyStatusChanged: %v", err)
		}
	}
	if len(client.textSent) != 1 {
		t.Fatalf("replayed event sent %d notices, want 1", len(client.textSent))
	}
	// The same status reached again later is a new change.
	q.issue.UpdatedAt.Time = q.issue.UpdatedAt.Time.Add(time.Minute)
	if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err != nil {
		t.Fatalf("NotifyStatusChanged: %v", err)
	}
	if len(client.textSent) != 2 {
		t.Fatalf("later change sent %d notices total, want 2", len(client.textSent))
	}
}

func TestIssueCallbackNotifierStaysSilentOnForwardProgress(t *testing.T) {
	q, client, n := newCallbackFixture(issuestatus.InProgress)
	if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err != nil {
		t.Fatalf("NotifyStatusChanged: %v", err)
	}
	if len(client.textSent) != 0 || len(q.ledger) != 0 {
		t.Fatalf("in_progress produced sends %+v ledger %+v", client.textSent, q.ledger)
	}
}

func TestIssueCallbackNotifierSkipsUnusableInstallation(t *testing.T) {
	for name, mutate := range map[string]func(*fakeCallbackQueries){
		"removed":         func(q *fakeCallbackQueries) { q.instErr = pgx.ErrNoRows },
		"revoked":         func(q *fakeCallbackQueries) { q.inst.Status = string(InstallationRevoked) },
		"other workspace": func(q *fakeCallbackQueries) { q.inst.WorkspaceID = testUUID(0x99) },
	} {
		t.Run(name, func(t *testing.T) {
			q, client, n := newCallbackFixture(issuestatus.Blocked)
			mutate(q)
			if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err != nil {
				t.Fatalf("NotifyStatusChanged: %v", err)
			}
			if len(client.textSent) != 0 {
				t.Fatalf("sent through an unusable installation: %+v", client.textSent)
			}
			if row := q.only(t); row.Status != "skipped" || row.Error == "" {
				t.Fatalf("ledger row = %+v, want skipped with a reason", row)
			}
		})
	}
}

func TestIssueCallbackNotifierRecordsFailedSend(t *testing.T) {
	q, client, n := newCallbackFixture(issuestatus.Cancelled)
	client.textSendErr = errors.New("lark: 500")
	if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err == nil {
		t.Fatal("a failed send must be reported")
	}
	if row := q.only(t); row.Status != "failed" || !strings.Contains(row.Error, "lark: 500") {
		t.Fatalf("ledger row = %+v, want failed with the send error", row)
	}
}

func TestIssueCallbackNotifierFallsBackWhenTheChatRejectsThreads(t *testing.T) {
	q, client, n := newCallbackFixture(issuestatus.Done)
	client.threadReplyErr = errThreadReplyClassified
	if err := n.NotifyStatusChanged(context.Background(), q.issue.ID); err != nil {
		t.Fatalf("NotifyStatusChanged: %v", err)
	}
	if len(client.textSent) != 2 || client.textSent[1].ReplyTarget.IsSet() {
		t.Fatalf("sends = %+v, want a thread attempt then a chat-level send", client.textSent)
	}
	if row := q.only(t); row.Status != "delivered" {
		t.Fatalf("ledger row = %+v", row)
	}
}
