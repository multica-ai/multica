package lark

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type budgetEnricherClient struct {
	*enricherFakeClient
	get  func(context.Context, string) ([]LarkMessage, error)
	list func(context.Context, ListMessagesParams) ([]LarkMessage, error)
}

func (f *budgetEnricherClient) GetMessage(ctx context.Context, _ InstallationCredentials, id string) ([]LarkMessage, error) {
	return f.get(ctx, id)
}

func (f *budgetEnricherClient) ListChatMessages(ctx context.Context, _ InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error) {
	return f.list(ctx, p)
}

func budgetImage(id string) LarkMessage {
	return LarkMessage{MessageID: id, MessageType: "image", Content: `{"image_key":"img_budget"}`, SenderID: "source", SenderType: "user", CreateTime: "1000"}
}

func TestEnrichSlowRecentDoesNotStarveAttachedContext(t *testing.T) {
	for _, kind := range []string{"quote", "forward", "quote_and_forward"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			listStarted := make(chan struct{})
			var getCalls, listCalls atomic.Int32
			client := &budgetEnricherClient{enricherFakeClient: newEnricherFake()}
			client.list = func(ctx context.Context, _ ListMessagesParams) ([]LarkMessage, error) {
				listCalls.Add(1)
				close(listStarted)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			client.get = func(ctx context.Context, id string) ([]LarkMessage, error) {
				getCalls.Add(1)
				select {
				case <-listStarted:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if id == "parent" {
					return []LarkMessage{budgetImage(id)}, nil
				}
				return []LarkMessage{{MessageID: id, MessageType: larkMsgTypeMergeForward}, budgetImage("child")}, nil
			}
			msg := InboundMessage{MessageID: "trigger", MessageType: "text", Body: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
			want := 1
			if kind != "forward" {
				msg.ParentID = "parent"
			}
			if kind != "quote" {
				msg.MessageType = larkMsgTypeMergeForward
			}
			if kind == "quote_and_forward" {
				want = 2
			}
			e := NewInboundEnricher(client, InboundEnricherConfig{RecentContextSize: 10, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			out := e.Enrich(ctx, msg, InstallationCredentials{})
			if len(out.ContextMedia) != want || int(getCalls.Load()) != want || listCalls.Load() != 1 {
				t.Fatalf("media=%+v gets=%d lists=%d; want %d attached images and no recent retry", out.ContextMedia, getCalls.Load(), listCalls.Load(), want)
			}
			if !strings.Contains(out.Body, recentContextUnavailableLine(recentContextFailureTimeout)) || !out.HasSelectedContext {
				t.Fatalf("recent failure or selected context missing: %s", out.Body)
			}
			if len(client.userCalls) != 0 {
				t.Fatal("name lookup must not start with an exhausted budget")
			}
			for _, media := range out.ContextMedia {
				if media.MessageID != "parent" && media.MessageID != "trigger" {
					t.Fatalf("wrong resource owner: %+v", media)
				}
			}
		})
	}
}

func TestEnrichSlowQuoteRetainsRecentMedia(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	quoteStarted := make(chan struct{})
	client := &budgetEnricherClient{enricherFakeClient: newEnricherFake()}
	client.get = func(ctx context.Context, _ string) ([]LarkMessage, error) {
		close(quoteStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	client.list = func(ctx context.Context, _ ListMessagesParams) ([]LarkMessage, error) {
		select {
		case <-quoteStarted:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []LarkMessage{budgetImage("recent")}, ctx.Err()
	}
	out := NewInboundEnricher(client, InboundEnricherConfig{RecentContextSize: 10}).Enrich(ctx, InboundMessage{MessageID: "trigger", ParentID: "parent", Body: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true}, InstallationCredentials{})
	if len(out.ContextMedia) != 1 || out.ContextMedia[0].MessageID != "recent" || !strings.Contains(out.Body, "quoted_message") || !strings.Contains(out.Body, "unavailable") {
		t.Fatalf("successful recent image lost or quote failure hidden: %+v", out)
	}
}

func TestEnrichCancellationJoinsAllFetches(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 3)
	var finished atomic.Int32
	block := func(ctx context.Context) ([]LarkMessage, error) {
		started <- struct{}{}
		<-ctx.Done()
		finished.Add(1)
		return nil, ctx.Err()
	}
	client := &budgetEnricherClient{enricherFakeClient: newEnricherFake()}
	client.get = func(ctx context.Context, _ string) ([]LarkMessage, error) { return block(ctx) }
	client.list = func(ctx context.Context, _ ListMessagesParams) ([]LarkMessage, error) { return block(ctx) }
	done := make(chan InboundMessage, 1)
	go func() {
		done <- NewInboundEnricher(client, InboundEnricherConfig{RecentContextSize: 10}).Enrich(ctx, InboundMessage{MessageID: "trigger", ParentID: "parent", MessageType: larkMsgTypeMergeForward, ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true}, InstallationCredentials{})
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("context reads did not start independently")
		}
	}
	cancel()
	select {
	case out := <-done:
		if finished.Load() != 3 || len(out.ContextMedia) != 0 || !out.HasSelectedContext {
			t.Fatalf("unjoined reads or invented media: finished=%d out=%+v", finished.Load(), out)
		}
	case <-time.After(time.Second):
		t.Fatal("enrichment did not stop on cancellation")
	}
}
