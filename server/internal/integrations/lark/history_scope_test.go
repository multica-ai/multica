package lark

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"testing"
	"time"
)

type historyScopeQueries struct {
	task            db.AgentTaskQueue
	session         db.ChatSession
	binding         db.ChannelChatSessionBinding
	delivery        db.ChannelTaskDelivery
	installation    db.ChannelInstallation
	generation      db.ChannelChatContextGeneration
	genRevision     int64
	installReads    int
	revokeAfterRead bool
}

func (q *historyScopeQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return q.task, nil
}
func (q *historyScopeQueries) GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error) {
	return q.session, nil
}
func (q *historyScopeQueries) GetChannelTaskDelivery(_ context.Context, id pgtype.UUID) (db.ChannelTaskDelivery, error) {
	if id != q.delivery.TaskID {
		return db.ChannelTaskDelivery{}, errors.New("wrong input owner")
	}
	return q.delivery, nil
}
func (q *historyScopeQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return q.binding, nil
}
func (q *historyScopeQueries) GetChannelInstallationInWorkspace(_ context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	q.installReads++
	if arg.WorkspaceID != q.installation.WorkspaceID || arg.ID != q.installation.ID {
		return db.ChannelInstallation{}, errors.New("out of scope")
	}
	row := q.installation
	if q.revokeAfterRead && q.installReads > 1 {
		row.Status = "revoked"
	}
	return row, nil
}
func (q *historyScopeQueries) GetChannelChatContextGeneration(_ context.Context, arg db.GetChannelChatContextGenerationParams) (db.ChannelChatContextGeneration, error) {
	q.genRevision = arg.Revision
	return q.generation, nil
}
func historyUUID(n byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{n}, Valid: true} }
func nativeScopeFixture() (*History, *historyScopeQueries, *nativePages) {
	q := &historyScopeQueries{}
	q.task = db.AgentTaskQueue{ID: historyUUID(1), ChatSessionID: historyUUID(2), AgentID: historyUUID(3), ChannelContextRevision: pgtype.Int8{Int64: 2, Valid: true}}
	q.session = db.ChatSession{ID: historyUUID(2), WorkspaceID: historyUUID(4), AgentID: historyUUID(3)}
	q.binding = db.ChannelChatSessionBinding{ID: historyUUID(5), ChatSessionID: historyUUID(2), InstallationID: historyUUID(6), ChannelType: channelTypeFeishu, ChannelChatID: "oc_chat:omt_topic", Config: []byte(`{"chat_id":"oc_chat"}`), RouteRevision: 1, ContextRevision: 3}
	q.delivery = db.ChannelTaskDelivery{TaskID: historyUUID(1), BindingID: historyUUID(5), InstallationID: historyUUID(6), ChannelType: channelTypeFeishu, ChannelChatID: "oc_chat:omt_topic", ChannelMessageID: pgtype.Text{String: "trigger", Valid: true}, ChannelThreadID: pgtype.Text{String: "omt_topic", Valid: true}, RouteRevision: 1, CreatedAt: pgtype.Timestamptz{Time: time.Unix(2, 0), Valid: true}}
	q.installation = db.ChannelInstallation{ID: historyUUID(6), WorkspaceID: historyUUID(4), AgentID: historyUUID(3), ChannelType: channelTypeFeishu, Status: "active", Config: []byte(`{"app_id":"cli_test"}`), InstalledAt: pgtype.Timestamptz{Time: time.Unix(1, 0), Valid: true}}
	q.generation = db.ChannelChatContextGeneration{ChatSessionID: historyUUID(2), Revision: 2, HistoryStartMessageID: pgtype.Text{String: "start", Valid: true}}
	f := &nativePages{pages: []NativeHistoryPage{{Messages: []LarkMessage{nativeMessage("trigger", "3000"), nativeMessage("middle", "2000"), nativeMessage("start", "1000")}}}}
	return &History{q: q, credentials: fakeCredentials{secret: "test"}, client: f}, q, f
}
func TestNativeHistoryTaskScope(t *testing.T) {
	h, q, f := nativeScopeFixture()
	p, _, err := h.Current(context.Background(), q.task.ID, 10)
	if err != nil || len(p.Messages) != 3 || q.genRevision != 2 || f.calls != 1 {
		t.Fatalf("page=%+v err=%v revision=%d calls=%d", p, err, q.genRevision, f.calls)
	}
	// A retry uses the frozen input owner's delivery, never the latest route target.
	h, q, _ = nativeScopeFixture()
	q.task.ChatInputTaskID = q.delivery.TaskID
	q.task.ID = historyUUID(9)
	if _, _, err = h.Current(context.Background(), q.task.ID, 10); err != nil {
		t.Fatal(err)
	}
}
func TestNativeHistoryScopeFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*historyScopeQueries)
	}{
		{"legacy generation", func(q *historyScopeQueries) { q.task.ChannelContextRevision = pgtype.Int8{} }},
		{"pending binding", func(q *historyScopeQueries) { q.binding.HistoryBoundaryPending = true }},
		{"pending generation", func(q *historyScopeQueries) { q.generation.HistoryBoundaryPending = true }},
		{"retired route", func(q *historyScopeQueries) { q.binding.RetiredAt.Valid = true }},
		{"other installation", func(q *historyScopeQueries) { q.delivery.InstallationID = historyUUID(7) }},
		{"other binding", func(q *historyScopeQueries) { q.delivery.BindingID = historyUUID(7) }},
		{"other chat", func(q *historyScopeQueries) { q.delivery.ChannelChatID = "oc_other:omt_topic" }},
		{"other topic", func(q *historyScopeQueries) { q.delivery.ChannelThreadID.String = "omt_other" }},
		{"forged composite", func(q *historyScopeQueries) { q.binding.Config = []byte(`{"chat_id":"oc_other"}`) }},
		{"missing topic config", func(q *historyScopeQueries) { q.binding.Config = []byte(`{}`) }},
		{"wrong tenant", func(q *historyScopeQueries) { q.installation.WorkspaceID = historyUUID(7) }},
		{"wrong agent", func(q *historyScopeQueries) { q.installation.AgentID = historyUUID(7) }},
		{"revoked", func(q *historyScopeQueries) { q.installation.Status = "revoked" }},
		{"rebound installation", func(q *historyScopeQueries) { q.installation.InstalledAt.Time = time.Unix(3, 0) }},
		{"missing boundary", func(q *historyScopeQueries) { q.generation.HistoryStartMessageID = pgtype.Text{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, q, f := nativeScopeFixture()
			tc.mutate(q)
			p, _, err := h.Current(context.Background(), q.task.ID, 10)
			if err == nil || len(p.Messages) != 0 || f.calls != 0 {
				t.Fatalf("scope accepted: %+v err=%v calls=%d", p, err, f.calls)
			}
		})
	}
	h, q, f := nativeScopeFixture()
	q.revokeAfterRead = true
	p, _, err := h.Current(context.Background(), q.task.ID, 10)
	if err == nil || len(p.Messages) != 0 || f.calls != 1 {
		t.Fatalf("revocation during read accepted: %+v %v", p, err)
	}
}

func TestNativeHistoryChangesDuringRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*historyScopeQueries)
	}{
		{"clear", func(q *historyScopeQueries) {
			q.generation.HistoryEndMessageID = pgtype.Text{String: "new-boundary", Valid: true}
		}},
		{"new", func(q *historyScopeQueries) { q.binding.RetiredAt.Valid = true }},
		{"rebind", func(q *historyScopeQueries) { q.installation.Config = []byte(`{"app_id":"replacement"}`) }},
		{"pending", func(q *historyScopeQueries) { q.binding.HistoryBoundaryPending = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, q, f := nativeScopeFixture()
			f.onRead = func() { tc.change(q) }
			p, _, err := h.Current(context.Background(), q.task.ID, 10)
			if err == nil || len(p.Messages) != 0 {
				t.Fatalf("changed scope returned native text: %+v %v", p, err)
			}
		})
	}
}
func TestNativeHistoryLinearTarget(t *testing.T) {
	h, q, f := nativeScopeFixture()
	q.binding.ChannelChatID = "oc_chat"
	q.binding.Config = []byte(`{}`)
	q.delivery.ChannelChatID = "oc_chat"
	q.delivery.ChannelThreadID = pgtype.Text{}
	for i := range f.pages[0].Messages {
		f.pages[0].Messages[i].ThreadID = ""
	}
	p, _, err := h.Current(context.Background(), q.task.ID, 10)
	if err != nil || len(p.Messages) != 3 || len(f.requests) != 1 || f.requests[0].ChatID != "oc_chat" || f.requests[0].ThreadID != "" {
		t.Fatalf("page=%+v requests=%v err=%v", p, f.requests, err)
	}
}
