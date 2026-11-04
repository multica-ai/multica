package lark

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestRecentMediaStaysPlaceholderAcrossRepeatedTurns(t *testing.T) {
	for _, quote := range []bool{false, true} {
		t.Run(map[bool]string{false: "text-only", true: "explicit-quote"}[quote], func(t *testing.T) {
			fake := newEnricherFake()
			fake.byChat["group"] = []LarkMessage{budgetImage("recent"), textMsg("text", "source", "recent text", "1100")}
			fake.byID["parent"] = []LarkMessage{budgetImage("parent")}
			api, storage, ledger := &fakeSender{}, &fakeMediaStorage{}, &fakeMediaLedger{}
			resolver := NewFeishuMediaResolver(api, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
			for _, id := range []string{"turn-one", "turn-two"} {
				in := InboundMessage{MessageID: id, MessageType: "text", Body: "inspect", ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true, CreateTime: "2000"}
				if quote {
					in.ParentID = "parent"
				}
				out := enrich(t, fake, in, InboundEnricherConfig{RecentContextSize: 10})
				want := 0
				if quote {
					want = 1
				}
				if len(out.ContextMedia) != want || resolver.HasMedia(channelMessageFromLark(out)) != quote {
					t.Fatalf("automatic history acquired downloadable media or pending fence: %+v", out)
				}
				if !strings.Contains(out.Body, "recent text") || !strings.Contains(out.Body, "[Image]") {
					t.Fatalf("recent text/placeholders lost: %s", out.Body)
				}

				if quote && out.ContextMedia[0].MessageID != "parent" {
					t.Fatal("wrong explicit resource")
				}
				if !quote {
					resolved := resolver.ResolveMedia(t.Context(), testMediaInstallation(t), engine.ResolvedIdentity{}, uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(out))
					if len(resolved.MediaRefs) != 0 {
						t.Fatal("recent media was ingested")
					}
				}
			}
			if len(api.downloadCalls)+len(storage.uploads)+len(ledger.records) != 0 {
				t.Fatal("automatic recent media caused storage work")
			}
		})
	}
}

func TestInlineClearKeepsBareResetSeparateFromExplicitMedia(t *testing.T) {
	for _, tc := range []struct {
		name, command, parent, content string
		wantMedia                      bool
	}{
		{name: "bare", command: "/clear"},
		{name: "whitespace", command: "/clear\n \t"},
		{name: "prompt", command: "/clear inspect"},
		{name: "quote", command: "/clear", parent: "parent", wantMedia: true},
		{name: "own-media", command: "/clear", content: `{"image_key":"own"}`, wantMedia: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newEnricherFake()
			fake.byChat["group"] = []LarkMessage{budgetImage("recent")}
			fake.byID["parent"] = []LarkMessage{budgetImage("parent")}
			in := InboundMessage{MessageID: "trigger", Body: tc.command, CommandBody: tc.command, ParentID: tc.parent, MessageType: "text", Content: tc.content, ChatID: "group", ChatType: ChatTypeGroup, AddressedToBot: true}
			if tc.content != "" {
				in.MessageType = "image"
			}
			out := enrich(t, fake, in, InboundEnricherConfig{RecentContextSize: 10})
			resolver := NewFeishuMediaResolver(nil, nil, nil, nil, newDiscardLogger())
			if !out.ForceFreshSession || resolver.HasMedia(channelMessageFromLark(out)) != tc.wantMedia {
				t.Fatalf("reset intent/media changed: %+v", out)
			}
			if (tc.name == "bare" || tc.name == "whitespace") && (out.Body != "" || len(fake.listCalls) != 0) {
				t.Fatalf("bare reset acquired history: %+v", out)
			}
		})
	}
}
