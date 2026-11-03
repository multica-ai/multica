package lark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func receiptMessage() InboundMessage {
	return InboundMessage{EventType: "im.message.receive_v1", EventID: "ev-1", AppID: "cli_receipt", MessageID: "om-1", ChatID: "oc-1", ChatType: ChatTypeGroup, SenderOpenID: "ou-1", MessageType: "text", Content: `{"text":"hello"}`}
}

func TestReceiptMessageValidation(t *testing.T) {
	inst := Installation{AppID: "cli_receipt"}
	for _, tc := range []struct {
		name   string
		mutate func(*InboundMessage)
	}{
		{"other app", func(m *InboundMessage) { m.AppID = "other" }},
		{"no app", func(m *InboundMessage) { m.AppID = "" }},
		{"other event", func(m *InboundMessage) { m.EventType = "card.action.trigger" }},
		{"no event id", func(m *InboundMessage) { m.EventID = "" }},
		{"no message id", func(m *InboundMessage) { m.MessageID = "" }},
		{"no chat", func(m *InboundMessage) { m.ChatID = "" }},
		{"no sender", func(m *InboundMessage) { m.SenderOpenID = "" }},
		{"no message type", func(m *InboundMessage) { m.MessageType = "" }},
		{"unknown chat", func(m *InboundMessage) { m.ChatType = "unknown" }},
		{"broken content", func(m *InboundMessage) { m.Content = "{" }},
		{"null content", func(m *InboundMessage) { m.Content = "null" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := receiptMessage()
			tc.mutate(&m)
			if validReceiptMessage(inst, m) {
				t.Fatal("invalid event accepted as evidence")
			}
		})
	}
	if !validReceiptMessage(inst, receiptMessage()) {
		t.Fatal("unaddressed group message should prove receipt")
	}
}

type receiptVerifierFunc func(context.Context, Installation, InboundMessage) error

func (f receiptVerifierFunc) RecordInboundReceipt(ctx context.Context, inst Installation, msg InboundMessage) error {
	return f(ctx, inst, msg)
}

func TestWSReceiptBeforeRoutingAndWriteFailureDoesNotChangeACK(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write failure"}[fail], func(t *testing.T) {
			conn := newFakeWSConn()
			c := quietConnector(t, conn, NewLarkJSONFrameDecoder(), time.Hour)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			c.cfg.InboundVerifier = receiptVerifierFunc(func(ctx context.Context, inst Installation, msg InboundMessage) error {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Error("unbounded verification write")
				}
				if fail {
					return errors.New("database down")
				}
				return nil
			})
			// Control frames, unknown events and malformed events cannot verify receipt.
			conn.Push(NewPongFrame(7).Marshal())
			pushDataFrame(conn, []byte(`{"header":{"event_type":"card.action.trigger"}}`), "other")
			pushDataFrame(conn, []byte(`{"header":{"event_type":"im.message.receive_v1"},"event":{}}`), "bad")
			msg := receiptMessage()
			payload, _ := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"event_type": msg.EventType, "event_id": msg.EventID, "app_id": msg.AppID}, "event": map[string]any{"sender": map[string]any{"sender_id": map[string]string{"open_id": string(msg.SenderOpenID)}}, "message": map[string]string{"message_id": msg.MessageID, "chat_id": string(msg.ChatID), "chat_type": "group", "message_type": "text", "content": msg.Content}}})
			pushDataFrame(conn, payload, "valid1")
			pushDataFrame(conn, payload, "duplicate")
			emits := 0
			done := make(chan error, 1)
			go func() {
				done <- c.Run(ctx, Installation{AppID: msg.AppID}, func(context.Context, InboundMessage) (DispatchResult, error) {
					emits++
					if emits > 1 && calls == 0 {
						t.Error("valid receipt not recorded before routing")
					}
					if emits == 3 {
						cancel()
					}
					return DispatchResult{}, nil
				})
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("connector stuck")
			}
			want := 1
			if fail {
				want = 2
			}
			if calls != want {
				t.Fatalf("verification calls=%d want %d", calls, want)
			}
			// All data frames, including the business-filtered and duplicate messages,
			// retain the connector's normal success ACK behavior.
			if got := len(conn.snapshot()); got != 4 {
				t.Fatalf("ACK count=%d want 4", got)
			}
			for _, raw := range conn.snapshot() {
				f, err := UnmarshalFrame(raw)
				if err != nil {
					t.Fatal(err)
				}
				var ack struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(f.Payload, &ack)
				if f.Method == FrameMethodData && ack.Code != 200 {
					t.Fatalf("unexpected ACK %s", f.Payload)
				}
			}
		})
	}
}

func TestInboundReceiptDatabaseGenerationAndScope(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Receipt test", "receipt-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Receipt test", "receipt-"+suffix)
	runtime := fx.Runtime(t, "Receipt runtime")
	agent := fx.Agent(t, "Receipt agent", runtime)
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewInstallationService(db.New(pool), box)
	if err != nil {
		t.Fatal(err)
	}
	p := InstallationParams{WorkspaceID: util.MustParseUUID(fx.WorkspaceID), AgentID: util.MustParseUUID(agent), InstallerUserID: util.MustParseUUID(fx.UserID), AppID: "cli_receipt_" + suffix, AppSecret: "test", BotOpenID: "ou_bot"}
	ctx := context.Background()
	inst, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE id=$1", inst.ID)
	read := func() Installation {
		t.Helper()
		got, err := svc.GetInWorkspace(ctx, inst.ID, p.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	record := func(snapshot Installation) {
		t.Helper()
		msg := receiptMessage()
		msg.AppID = snapshot.AppID
		if err := svc.RecordInboundReceipt(ctx, snapshot, msg); err != nil {
			t.Fatal(err)
		}
	}
	if inst.Status != "active" || inst.FirstMessageReceivedAt != nil || inst.BindingGeneration == "" {
		t.Fatal("new install should be active, pending verification with a generation")
	}
	// No arrival (including GetBotInfo/install success) must leave no evidence.
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("silent connection marked verified")
	}
	record(inst)
	first := read()
	if first.FirstMessageReceivedAt == nil {
		t.Fatal("valid receipt not persisted")
	}
	// Block the metadata update after its read (the former RMW path), then
	// record receipt while holding the row lock. Its eventual update must merge
	// the latest config rather than erase the arrival or binding generation.
	fx.Exec(t, "UPDATE channel_installation SET config=config-'first_message_received_at' WHERE id=$1", inst.ID)
	metadataConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer metadataConn.Release()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT id FROM channel_installation WHERE id=$1 FOR UPDATE", inst.ID); err != nil {
		t.Fatal(err)
	}
	metadataDone := make(chan error, 1)
	metadataCtx, cancelMetadata := context.WithTimeout(ctx, 3*time.Second)
	defer cancelMetadata()
	go func() {
		metadataDone <- NewChannelStore(db.New(metadataConn)).SetLarkInstallationBotUnionID(metadataCtx, SetInstallationBotUnionIDParams{ID: inst.ID, BotUnionID: pgtype.Text{String: "on_bot", Valid: true}})
	}()
	blocked := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if err := pool.QueryRow(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", metadataConn.Conn().PgConn().PID()).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		_ = tx.Rollback(ctx)
		<-metadataDone
		t.Fatal("metadata update did not block on fixture row lock")
	}
	if err := db.New(tx).RecordFeishuInboundReceipt(ctx, db.RecordFeishuInboundReceiptParams{ID: inst.ID, WorkspaceID: p.WorkspaceID, AppID: inst.AppID, BindingGeneration: inst.BindingGeneration}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-metadataDone; err != nil {
		t.Fatal(err)
	}
	first = read()
	if first.FirstMessageReceivedAt == nil || first.BindingGeneration != inst.BindingGeneration {
		t.Fatal("concurrent identity backfill lost receipt or generation")
	}

	record(inst)
	if !read().FirstMessageReceivedAt.Equal(*first.FirstMessageReceivedAt) {
		t.Fatal("duplicate changed first receipt")
	}
	rebound, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.ID != inst.ID || rebound.BindingGeneration == inst.BindingGeneration || rebound.FirstMessageReceivedAt != nil {
		t.Fatal("rebind did not reset receipt and rotate generation")
	}
	record(inst)
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("late old socket verified new binding")
	}
	// Exercise the SQL fence directly, as if a rebind/revoke raced the read.
	q := db.New(pool)
	stale := db.RecordFeishuInboundReceiptParams{ID: inst.ID, WorkspaceID: p.WorkspaceID, AppID: inst.AppID, BindingGeneration: inst.BindingGeneration}
	if err := q.RecordFeishuInboundReceipt(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("atomic generation fence failed")
	}
	stale.BindingGeneration = rebound.BindingGeneration
	stale.WorkspaceID = util.MustParseUUID(uuid.NewString())
	if err := q.RecordFeishuInboundReceipt(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("workspace fence failed")
	}
	stale.WorkspaceID = p.WorkspaceID
	stale.AppID = "other"
	if err := q.RecordFeishuInboundReceipt(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("app fence failed")
	}
	if err := svc.Revoke(ctx, inst.ID); err != nil {
		t.Fatal(err)
	}
	record(rebound)
	stale.AppID = p.AppID
	if err := q.RecordFeishuInboundReceipt(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("revoked binding verified")
	}
	// The query must also reject another channel even if its app matches.
	fx.Exec(t, "UPDATE channel_installation SET status='active', channel_type='slack' WHERE id=$1", inst.ID)
	if err := q.RecordFeishuInboundReceipt(ctx, stale); err != nil {
		t.Fatal(err)
	}
	var marked bool
	if err := pool.QueryRow(ctx, "SELECT config ? 'first_message_received_at' FROM channel_installation WHERE id=$1", inst.ID).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked {
		t.Fatal("another channel was marked verified")
	}
	fx.Exec(t, "UPDATE channel_installation SET channel_type='feishu' WHERE id=$1", inst.ID)
	// Existing installs have no generation/evidence; a legacy socket can verify
	// its own binding, but cannot verify the next rebind's nonempty generation.
	fx.Exec(t, "UPDATE channel_installation SET status='active', config=config-'binding_generation' WHERE id=$1", inst.ID)
	legacy := read()
	record(legacy)
	if read().FirstMessageReceivedAt == nil {
		t.Fatal("legacy binding cannot verify")
	}
	inst, err = svc.Upsert(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	record(legacy)
	if read().FirstMessageReceivedAt != nil {
		t.Fatal("legacy socket verified new binding")
	}
	record(inst)
	if read().FirstMessageReceivedAt == nil {
		t.Fatal("current binding could not verify after stale callback")
	}
}

func TestReceiptDoesNotRotateConnectionButRebindDoes(t *testing.T) {
	row := db.ChannelInstallation{ChannelType: channelTypeFeishu, Config: []byte(`{"app_id":"app","binding_generation":"one"}`)}
	before := rowFingerprint(row)
	row.Config = []byte(`{"binding_generation":"one","first_message_received_at":"2026-10-29T00:00:00Z","app_id":"app"}`)
	if rowFingerprint(row) != before {
		t.Fatal("receipt evidence rotates healthy connection")
	}
	row.Config = []byte(`{"app_id":"app","binding_generation":"two"}`)
	if rowFingerprint(row) == before {
		t.Fatal("rebind did not rotate connection")
	}
}

func TestWSReceiptSurvivesDispatchFailure(t *testing.T) {
	conn := newFakeWSConn()
	msg := receiptMessage()
	c := quietConnector(t, conn, FrameDecoderFunc(func([]byte, Installation) (InboundMessage, bool, error) { return msg, true, nil }), time.Hour)
	recorded := false
	c.cfg.InboundVerifier = receiptVerifierFunc(func(context.Context, Installation, InboundMessage) error { recorded = true; return nil })
	pushDataFrame(conn, []byte(`{}`), "valid")
	err := c.Run(context.Background(), Installation{AppID: msg.AppID}, func(context.Context, InboundMessage) (DispatchResult, error) {
		if !recorded {
			t.Error("routing ran before receipt was recorded")
		}
		return DispatchResult{}, errors.New("dispatch failed")
	})
	if err == nil || !recorded {
		t.Fatal("routing failure lost receipt evidence")
	}
	writes := conn.snapshot()
	if len(writes) != 1 {
		t.Fatalf("expected one NACK, got %d writes", len(writes))
	}
	f, err := UnmarshalFrame(writes[0])
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(f.Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Code != 500 {
		t.Fatalf("dispatch failure ACK code %d want 500", ack.Code)
	}
}
