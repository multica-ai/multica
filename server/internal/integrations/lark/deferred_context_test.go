package lark

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type deferredContextClient struct {
	*enricherFakeClient
	media *fakeSender
}

func (c *deferredContextClient) DownloadMessageResource(ctx context.Context, creds InstallationCredentials, p DownloadResourceParams) (DownloadedResource, error) {
	return c.media.DownloadMessageResource(ctx, creds, p)
}

func TestDeferredContextPreparationAndResolution(t *testing.T) {
	for _, kind := range []string{"quote-image", "quote-text", "recent-image", "forward-image", "new-with-quote", "bare-new"} {
		t.Run(kind, func(t *testing.T) {
			fake := newEnricherFake()
			fake.byID["parent"] = []LarkMessage{budgetImage("parent")}
			fake.byID["forward"] = []LarkMessage{{MessageID: "forward", MessageType: larkMsgTypeMergeForward}, budgetImage("child")}
			fake.byChat["group"] = []LarkMessage{budgetImage("recent")}
			in := InboundMessage{MessageID: "trigger", ParentID: "parent", Body: "inspect", CommandBody: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
			if kind == "quote-text" {
				fake.byID["parent"] = []LarkMessage{textMsg("parent", "source", "quoted original text", "1000")}
			}
			if kind == "recent-image" || kind == "forward-image" || kind == "bare-new" {
				in.ParentID = ""
			}
			if kind == "forward-image" {
				in.MessageID, in.MessageType = "forward", larkMsgTypeMergeForward
			}
			if kind == "bare-new" || kind == "new-with-quote" {
				in.Body, in.CommandBody = "/new", "/new"
			}
			cfg := InboundEnricherConfig{}
			if kind == "recent-image" || kind == "new-with-quote" || kind == "bare-new" {
				cfg.RecentContextSize = 5
			}
			prepared := NewInboundEnricher(fake, cfg).(*inboundEnricher).PrepareContext(in)
			if len(fake.calls)+len(fake.listCalls)+len(fake.userCalls) != 0 {
				t.Fatal("preparation performed remote I/O")
			}
			storage := &fakeMediaStorage{}
			api := &deferredContextClient{enricherFakeClient: fake, media: &fakeSender{downloaded: DownloadedResource{Data: pngBytes, ContentType: "image/png"}}}
			resolver := NewFeishuMediaResolver(api, fakeCreds{secret: "plain"}, storage, &fakeMediaLedger{}, newDiscardLogger())
			msg := channelMessageFromLark(prepared)
			if kind == "bare-new" {
				if prepared.DeferredContext != nil || resolver.HasMedia(msg) || prepared.Body != "" {
					t.Fatal("bare new command inherited context or queued a task")
				}
				return
			}
			if !resolver.HasMedia(msg) {
				t.Fatal("unresolved context did not defer the task")
			}
			resolved := resolver.ResolveMedia(t.Context(), testMediaInstallation(t), engine.ResolvedIdentity{}, uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), msg)
			if !resolved.BodyResolved || !strings.Contains(resolved.Text, "source") {
				// Source falls back to User 1 without directory permission.
				if !resolved.BodyResolved || !strings.Contains(resolved.Text, "User 1") {
					t.Fatalf("context body not resolved: %+v", resolved)
				}
			}
			if kind == "quote-text" {
				if len(resolved.MediaRefs) != 0 || !strings.Contains(resolved.Text, "quoted original text") {
					t.Fatalf("text-only quote lost: %+v", resolved)
				}
			} else if len(resolved.MediaRefs) != 1 || len(storage.uploads) != 1 {
				t.Fatalf("media missing or duplicated: %+v", resolved)
			}
			if kind == "new-with-quote" && len(fake.listCalls) != 0 {
				t.Fatal("new command fetched previous recent history")
			}
		})
	}
}

func TestPrepareContextClearCommand(t *testing.T) {
	for _, tc := range []struct {
		name, command, parent, content string
		wantDeferred, wantMedia        bool
	}{
		{name: "bare", command: "/clear"},
		{name: "whitespace", command: "/clear\n \t"},
		{name: "prompt", command: "/clear inspect", wantDeferred: true, wantMedia: true},
		{name: "quote", command: "/clear", parent: "parent", wantDeferred: true, wantMedia: true},
		{name: "own-image", command: "/clear", content: `{"content":[[{"tag":"text","text":"/clear"},{"tag":"img","image_key":"own-image"}]]}`, wantDeferred: true, wantMedia: true},
		{name: "own-file", command: "/clear", content: `{"content":[[{"tag":"text","text":"/clear"},{"tag":"file","file_key":"own-file","file_name":"notes.txt"}]]}`, wantDeferred: true, wantMedia: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newEnricherFake()
			client.byChat["group"] = []LarkMessage{budgetImage("recent")}
			msg := InboundMessage{MessageID: "trigger", MessageType: "text", ChatID: "group", ChatType: ChatTypeGroup,
				AddressedToBot: true, Body: tc.command, CommandBody: tc.command, ParentID: tc.parent, Content: tc.content}
			if tc.content != "" {
				msg.MessageType = "post"
			}
			enricher := NewInboundEnricher(client, InboundEnricherConfig{RecentContextSize: DefaultRecentContextSize}).(*inboundEnricher)
			prepared := enricher.PrepareContext(msg)
			resolver := NewFeishuMediaResolver(nil, nil, nil, nil, newDiscardLogger())
			if !prepared.ForceFreshSession || prepared.CommandBody != tc.command || prepared.HasSelectedContext != (tc.parent != "") {
				t.Fatalf("control command or explicit context changed: %+v", prepared)
			}
			if (prepared.DeferredContext != nil) != tc.wantDeferred || resolver.HasMedia(channelMessageFromLark(prepared)) != tc.wantMedia {
				t.Fatalf("automatic history changed the control command: deferred=%v media=%v", prepared.DeferredContext != nil, resolver.HasMedia(channelMessageFromLark(prepared)))
			}
			if len(client.calls)+len(client.listCalls)+len(client.userCalls) != 0 {
				t.Fatal("preparation performed remote I/O")
			}
			if !tc.wantDeferred {
				inline := enricher.Enrich(t.Context(), msg, InstallationCredentials{})
				if prepared.Body != "" || inline.Body != "" || len(client.listCalls) != 0 {
					t.Fatal("bare clear fetched or inherited automatic history")
				}
			}
		})
	}
}
