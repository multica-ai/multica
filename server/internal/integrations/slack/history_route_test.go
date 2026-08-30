package slack

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestHistoryRouteBoundaryPendingReturnsEmptyWithoutSlackRead(t *testing.T) {
	binding := dmBinding()
	binding.HistoryBoundaryPending = true
	client := &fakeHistoryClient{historyMsgs: []slack.Message{msg("U1", "old", "1.0")}}
	page, err := newTestHistory(&fakeHistoryQueries{binding: binding, inst: activeSlackInstall()}, client).
		Thread(context.Background(), uid(1), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if client.historyCalls != 0 || len(page.Messages) != 0 {
		t.Fatalf("calls=%d messages=%v", client.historyCalls, page.Messages)
	}
}

func TestHistoryAppliesRouteAndContextIntersection(t *testing.T) {
	binding := dmBinding()
	binding.HistoryStartMessageID = pgtype.Text{String: "2.0", Valid: true}
	binding.HistoryEndMessageID = pgtype.Text{String: "5.0", Valid: true}
	client := &fakeHistoryClient{historyMsgs: []slack.Message{
		msg("U1", "before route", "1.0"), msg("U1", "route start", "2.0"),
		msg("U1", "context start", "3.0"), msg("U1", "context end", "4.0"),
		msg("U1", "route end", "5.0"),
	}}
	page, err := newTestHistory(&fakeHistoryQueries{binding: binding, inst: activeSlackInstall()}, client).
		Thread(context.Background(), uid(1), "", channel.HistoryOptions{After: "3.0", Until: "4.0"})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if client.lastHistory.Oldest != "3.0" || client.lastHistory.Latest != "4.0" || !client.lastHistory.Inclusive {
		t.Fatalf("params=%+v", client.lastHistory)
	}
	if len(page.Messages) != 1 || page.Messages[0].TS != "3.0" {
		t.Fatalf("messages=%+v", page.Messages)
	}
}

func TestHistoryNormalizesNewChatBoundaryMessage(t *testing.T) {
	binding := dmBinding()
	binding.HistoryStartMessageID = pgtype.Text{String: "2.0", Valid: true}
	client := &fakeHistoryClient{historyMsgs: []slack.Message{
		msg("U1", "<@UBOT> /new investigate latency\nwith traces", "2.0"),
	}}
	page, err := newTestHistory(&fakeHistoryQueries{binding: binding, inst: activeSlackInstall()}, client).
		Thread(context.Background(), uid(1), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Text != "investigate latency\nwith traces" {
		t.Fatalf("boundary message = %+v", page.Messages)
	}
}

func TestHistoryFiltersTaggedControlAckAndForeignRoute(t *testing.T) {
	binding := dmBinding()
	current := msg("UBOT", "current reply", "3.0")
	current.Metadata = outboundMetadata(binding.ID, 2, "task_reply")
	ack := msg("UBOT", "started", "2.5")
	ack.Metadata = outboundMetadata(binding.ID, 2, "control_ack")
	foreign := msg("UBOT", "late old reply", "2.0")
	foreign.Metadata = outboundMetadata(uid(9), 1, "task_reply")
	client := &fakeHistoryClient{historyMsgs: []slack.Message{current, ack, foreign}}
	page, err := newTestHistory(&fakeHistoryQueries{binding: binding, inst: activeSlackInstall()}, client).
		Thread(context.Background(), uid(1), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if !client.lastHistory.IncludeAllMetadata {
		t.Fatal("history read did not request Slack message metadata")
	}
	if len(page.Messages) != 1 || page.Messages[0].TS != "3.0" {
		t.Fatalf("metadata route filter returned %+v", page.Messages)
	}
}

func TestHistoryFiltersControlAckFromLedgerWithoutMetadata(t *testing.T) {
	binding := dmBinding()
	q := &fakeHistoryQueries{binding: binding, inst: activeSlackInstall(), outbound: []db.ChannelOutboundMessage{{
		ChannelMessageID: "2.0", BindingID: binding.ID, OutboundKind: "control_ack",
	}}}
	client := &fakeHistoryClient{historyMsgs: []slack.Message{msg("UBOT", "started", "2.0"), msg("U1", "hello", "3.0")}}
	page, err := newTestHistory(q, client).Thread(context.Background(), uid(1), "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].TS != "3.0" {
		t.Fatalf("ledger control filter returned %+v", page.Messages)
	}
}

func TestHistoryCursorAdvancesWhenFullRawPageIsRouteFiltered(t *testing.T) {
	binding := dmBinding()
	filtered := []slack.Message{
		msg("UBOT", "ack 3", "3.0"), msg("UBOT", "ack 2", "2.0"), msg("UBOT", "ack 1", "1.0"),
	}
	for i := range filtered {
		filtered[i].Metadata = outboundMetadata(binding.ID, 2, "control_ack")
	}
	client := &fakeHistoryClient{historyMsgs: filtered}
	page, err := newTestHistory(&fakeHistoryQueries{binding: binding, inst: activeSlackInstall()}, client).
		Thread(context.Background(), uid(1), "", channel.HistoryOptions{Limit: 3})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(page.Messages) != 0 || page.NextCursor != "1.0" {
		t.Fatalf("filtered page messages=%+v cursor=%q, want empty/1.0", page.Messages, page.NextCursor)
	}
}
