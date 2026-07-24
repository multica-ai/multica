package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChannelInboundDeliveryQueueDedupOrderingRecoveryAndScrub(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	var tablePresent bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.channel_inbound_delivery') IS NOT NULL").Scan(&tablePresent); err != nil || !tablePresent {
		t.Skip("channel_inbound_delivery migration is not applied")
	}
	queries := db.New(pool)
	installationID := util.MustParseUUID("dd000000-0000-4000-8000-000000000001")
	defer queries.DeleteChannelInboundDeliveriesByInstallation(ctx, installationID)
	_ = queries.DeleteChannelInboundDeliveriesByInstallation(ctx, installationID)

	enqueue := func(messageID, sequence string) db.ChannelInboundDelivery {
		payload, _ := json.Marshal(InboundMessage{MessageID: messageID, Body: messageID})
		row, err := queries.EnqueueChannelInboundDelivery(ctx, db.EnqueueChannelInboundDeliveryParams{
			InstallationID: installationID, MessageID: messageID, SequenceKey: sequence, Payload: payload,
		})
		if err != nil {
			t.Fatalf("enqueue %s: %v", messageID, err)
		}
		return row
	}
	first := enqueue("om_first", "conversation-a")
	duplicate := enqueue("om_first", "conversation-a")
	if duplicate.ID != first.ID {
		t.Fatalf("duplicate got a new row: first=%v duplicate=%v", first.ID, duplicate.ID)
	}
	enqueue("om_second", "conversation-a")
	enqueue("om_parallel", "conversation-b")

	claim1, err := queries.ClaimChannelInboundDelivery(ctx)
	if err != nil || claim1.MessageID != "om_first" {
		t.Fatalf("first claim = %+v err=%v", claim1, err)
	}
	claim2, err := queries.ClaimChannelInboundDelivery(ctx)
	if err != nil || claim2.MessageID != "om_parallel" {
		t.Fatalf("parallel claim = %+v err=%v", claim2, err)
	}
	if _, err := queries.CompleteChannelInboundDelivery(ctx, db.CompleteChannelInboundDeliveryParams{
		ID: claim1.ID, LeaseToken: claim1.LeaseToken, Status: "completed",
	}); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	claim3, err := queries.ClaimChannelInboundDelivery(ctx)
	if err != nil || claim3.MessageID != "om_second" {
		t.Fatalf("next in conversation = %+v err=%v", claim3, err)
	}

	// A process crash leaves processing+expired. Another worker must reclaim it.
	if _, err := pool.Exec(ctx, `UPDATE channel_inbound_delivery SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, claim3.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := queries.ClaimChannelInboundDelivery(ctx)
	if err != nil || reclaimed.ID != claim3.ID || reclaimed.LeaseToken == claim3.LeaseToken {
		t.Fatalf("reclaimed = %+v err=%v", reclaimed, err)
	}
	completed, err := queries.CompleteChannelInboundDelivery(ctx, db.CompleteChannelInboundDeliveryParams{
		ID: reclaimed.ID, LeaseToken: reclaimed.LeaseToken, Status: "failed",
		LastError: pgtype.Text{String: "processing_failed", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Payload != nil || completed.Status != "failed" || !completed.LastError.Valid {
		t.Fatalf("terminal row not scrubbed: %+v", completed)
	}
	if _, err := queries.CompleteChannelInboundDelivery(ctx, db.CompleteChannelInboundDeliveryParams{
		ID: claim2.ID, LeaseToken: claim2.LeaseToken, Status: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.ClaimChannelInboundDelivery(ctx); err != pgx.ErrNoRows {
		t.Fatalf("unexpected remaining claim: %v", err)
	}

	if err := queries.DeleteChannelInboundDeliveriesByInstallation(ctx, installationID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_inbound_delivery WHERE installation_id = $1`, installationID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("installation cleanup count=%d err=%v", count, err)
	}
}

func TestInboundDeliveryWorkerRetriesAttachmentThenContinuesWithWarning(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	var tablePresent bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.channel_inbound_delivery') IS NOT NULL").Scan(&tablePresent); err != nil || !tablePresent {
		t.Skip("channel_inbound_delivery migration is not applied")
	}
	queries := db.New(pool)
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	installations, err := NewInstallationService(queries, box)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := installations.Upsert(ctx, InstallationParams{
		WorkspaceID:     util.MustParseUUID("de000000-0000-4000-8000-000000000001"),
		AgentID:         util.MustParseUUID("de000000-0000-4000-8000-000000000002"),
		InstallerUserID: util.MustParseUUID("de000000-0000-4000-8000-000000000003"),
		AppID:           "cli_inbound_worker_retry_test", AppSecret: "secret", BotOpenID: "ou_bot",
	})
	if err != nil {
		t.Fatalf("upsert installation: %v", err)
	}
	defer func() {
		_ = queries.DeleteChannelInboundDeliveriesByInstallation(ctx, inst.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, inst.ID)
	}()

	handlerCalls := 0
	worker := NewInboundDeliveryWorker(queries, installations, nil, func(_ context.Context, msg channel.InboundMessage) error {
		handlerCalls++
		if handlerCalls <= 3 {
			return fmt.Errorf("resolve media: %w", &messageResourceError{retryable: true, category: "upstream HTTP 429"})
		}
		var raw InboundMessage
		if err := json.Unmarshal(msg.Raw, &raw); err != nil {
			return err
		}
		if len(raw.Resources) != 0 || !strings.Contains(msg.Text, "attachment could not be downloaded") {
			return fmt.Errorf("fail-open message was not sanitized: raw=%+v text=%q", raw, msg.Text)
		}
		return nil
	}, nil)
	msg := InboundMessage{
		AppID: inst.AppID, MessageID: "om_worker_retry", ChatID: "oc_worker", ChatType: ChatTypeP2P,
		Body: "[File attachment: report.pdf]", Resources: []MessageResourceRef{{Type: "file", Key: "secret_resource", Filename: "report.pdf"}},
	}
	if err := worker.Enqueue(ctx, inst, msg); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < inboundDeliveryMaxAttempts; attempt++ {
		if attempt > 0 {
			_, _ = pool.Exec(ctx, `UPDATE channel_inbound_delivery SET available_at = now() WHERE installation_id = $1`, inst.ID)
		}
		worked, err := worker.ProcessNext(ctx)
		if err != nil || !worked {
			t.Fatalf("attempt %d: worked=%v err=%v", attempt+1, worked, err)
		}
	}
	var delivery db.ChannelInboundDelivery
	if err := pool.QueryRow(ctx, `SELECT id FROM channel_inbound_delivery WHERE installation_id = $1 AND message_id = $2`, inst.ID, msg.MessageID).Scan(&delivery.ID); err != nil {
		t.Fatal(err)
	}
	delivery, err = queries.GetChannelInboundDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "completed" || delivery.Payload != nil || !delivery.LastError.Valid || delivery.LastError.String != "attachments_unavailable" {
		t.Fatalf("delivery = %+v", delivery)
	}
	if handlerCalls != 4 {
		t.Fatalf("handler calls = %d, want three failed attempts plus one fail-open", handlerCalls)
	}
}
