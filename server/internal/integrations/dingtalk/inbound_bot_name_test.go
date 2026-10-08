package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

const verifiedTestBotName = "Multica Bot - Local"

func TestInboundFromCallbackWithBotName_PlainTextRemovesOnlyVerifiedBot(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		want        string
		wantCommand bool
	}{
		{
			name:        "bot before chat",
			content:     "@Multica Bot - Local /new inspect this",
			want:        "/new inspect this",
			wantCommand: true,
		},
		{
			name:        "bot after chat",
			content:     "/new @Multica Bot - Local inspect this",
			want:        "/new inspect this",
			wantCommand: true,
		},
		{
			name:        "bot after body",
			content:     "/new inspect this @Multica Bot - Local",
			want:        "/new inspect this",
			wantCommand: true,
		},
		{
			name:        "repeated bot mentions",
			content:     "@Multica Bot - Local @Multica Bot - Local /new inspect this",
			want:        "/new inspect this",
			wantCommand: true,
		},
		{
			name:        "other mention before bot",
			content:     "@Alice @Multica Bot - Local /new inspect this",
			want:        "@Alice /new inspect this",
			wantCommand: false,
		},
		{
			name:        "other mention after command",
			content:     "@Multica Bot - Local /new inspect this with @Alice",
			want:        "/new inspect this with @Alice",
			wantCommand: true,
		},
		{
			name:        "different bot preserved",
			content:     "@Multica Bot - DEV @Multica Bot - Local /new inspect this",
			want:        "@Multica Bot - DEV /new inspect this",
			wantCommand: false,
		},
		{
			name:        "longer name preserved",
			content:     "@Multica Bot - Locality /new inspect this",
			want:        "@Multica Bot - Locality /new inspect this",
			wantCommand: false,
		},
		{
			name:        "punctuation-extended name preserved",
			content:     "@Multica Bot - Local-DEV /new inspect this",
			want:        "@Multica Bot - Local-DEV /new inspect this",
			wantCommand: false,
		},
		{
			name:        "embedded literal preserved",
			content:     "quote@Multica Bot - Local /new inspect this",
			want:        "quote@Multica Bot - Local /new inspect this",
			wantCommand: false,
		},
		{
			name:        "mid sentence command stays prose",
			content:     "@Multica Bot - Local please /new later",
			want:        "please /new later",
			wantCommand: false,
		},
		{
			name:        "blank line before command",
			content:     "@Multica Bot - Local\n\n/new inspect this",
			want:        "/new inspect this",
			wantCommand: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeGroup, true)
			cb.Text.Content = tc.content
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", verifiedTestBotName)
			if !ok {
				t.Fatal("expected addressed group text")
			}
			if msg.Text != tc.want || msg.CommandText != tc.want {
				t.Fatalf("normalized text/command = %q/%q, want %q", msg.Text, msg.CommandText, tc.want)
			}
			_, command := engine.ParseNewChatCommand(msg.CommandText)
			if command != tc.wantCommand {
				t.Fatalf("ParseNewChatCommand(%q) = %v, want %v", msg.CommandText, command, tc.wantCommand)
			}
		})
	}
}

func TestInboundFromCallbackWithBotName_RichTextPreservesMediaAndOtherMentions(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantText        string
		wantCommandText string
		wantCommand     bool
	}{
		{
			name: "image bot command text",
			content: `{"richText":[
				{"type":"picture","downloadCode":"dl-1"},
				{"text":"@Multica Bot - Local /new inspect this"}
			]}`,
			wantText:        "[Image]\ninspect this",
			wantCommandText: "/new inspect this",
			wantCommand:     true,
		},
		{
			name: "bot image command text image",
			content: `{"richText":[
				{"text":"@Multica Bot - Local "},
				{"type":"picture","downloadCode":"dl-1"},
				{"text":"/new inspect this"},
				{"type":"picture","downloadCode":"dl-2"}
			]}`,
			wantText:        "[Image]\ninspect this\n[Image]",
			wantCommandText: "/new inspect this",
			wantCommand:     true,
		},
		{
			name: "command keeps colleague mention",
			content: `{"richText":[
				{"text":"@Multica Bot - Local /new ask @Alice"},
				{"type":"picture","downloadCode":"dl-1"}
			]}`,
			wantText:        "ask @Alice\n[Image]",
			wantCommandText: "/new ask @Alice",
			wantCommand:     true,
		},
		{
			name: "colleague before command remains anchor",
			content: `{"richText":[
				{"text":"@Alice @Multica Bot - Local /new inspect this"},
				{"type":"picture","downloadCode":"dl-1"}
			]}`,
			wantText:        "@Alice /new inspect this\n[Image]",
			wantCommandText: "@Alice /new inspect this",
			wantCommand:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeGroup, true)
			cb.Msgtype = "richText"
			cb.Content = json.RawMessage(tc.content)
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", verifiedTestBotName)
			if !ok {
				t.Fatal("expected addressed group richText")
			}
			if msg.Text != tc.wantText || msg.CommandText != tc.wantCommandText {
				t.Fatalf("normalized text/command = %q/%q, want %q/%q", msg.Text, msg.CommandText, tc.wantText, tc.wantCommandText)
			}
			_, command := engine.ParseNewChatCommand(msg.CommandText)
			if command != tc.wantCommand {
				t.Fatalf("ParseNewChatCommand(%q) = %v, want %v", msg.CommandText, command, tc.wantCommand)
			}
			if got := strings.Count(msg.Text, "[Image]"); got != strings.Count(tc.wantText, "[Image]") {
				t.Fatalf("image placeholder count = %d in %q", got, msg.Text)
			}
		})
	}
}

func TestInboundFromCallbackWithBotName_FailClosedWithoutVerifiedName(t *testing.T) {
	tests := []struct {
		name    string
		msgtype string
		text    string
		content string
		want    string
		command bool
	}{
		{
			name: "plain single-token bot before command",
			text: "@YYClaw /new inspect this",
			want: "@YYClaw /new inspect this",
		},
		{
			name: "plain multiword bot before command",
			text: "@Multica Bot - Local /new inspect this",
			want: "@Multica Bot - Local /new inspect this",
		},
		{
			name:    "plain callback without visible bot",
			text:    "/new inspect this",
			want:    "/new inspect this",
			command: true,
		},
		{
			name:    "plain command before unknown bot",
			text:    "/new inspect this @Multica Bot - Local",
			want:    "/new inspect this @Multica Bot - Local",
			command: true,
		},
		{
			name:    "rich text preserves unknown bot",
			msgtype: "richText",
			content: `{"richText":[{"text":"@Multica Bot - Local /new inspect this"}]}`,
			want:    "@Multica Bot - Local /new inspect this",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeGroup, true)
			if tc.msgtype == "richText" {
				cb.Msgtype = tc.msgtype
				cb.Content = json.RawMessage(tc.content)
			} else {
				cb.Text.Content = tc.text
			}
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", "")
			if !ok || msg.Text != tc.want || msg.CommandText != tc.want {
				t.Fatalf("permissionless normalization: ok=%v text/command=%q/%q, want %q", ok, msg.Text, msg.CommandText, tc.want)
			}
			_, command := engine.ParseNewChatCommand(msg.CommandText)
			if command != tc.command {
				t.Fatalf("permissionless command classification = %v for %q, want %v", command, msg.CommandText, tc.command)
			}
		})
	}
}

func TestInboundFromCallbackWithBotName_DoesNotStripPrivateOrUnaddressedText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		convType string
		atBot    bool
	}{
		{name: "private", convType: convTypeP2P, atBot: false},
		{name: "unaddressed group", convType: convTypeGroup, atBot: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(tc.convType, tc.atBot)
			cb.Text.Content = "@Multica Bot - Local /new inspect this"
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", verifiedTestBotName)
			if !ok || msg.Text != cb.Text.Content {
				t.Fatalf("non-addressing text changed: ok=%v text=%q", ok, msg.Text)
			}
		})
	}
}

func TestInboundFromCallbackWithBotName_AllSharedCommandsKeepFirstTextLineContract(t *testing.T) {
	tests := []struct {
		name    string
		content string
		parse   func(string) bool
	}{
		{
			name:    "chat",
			content: "@Multica Bot - Local /new inspect this",
			parse: func(body string) bool {
				_, ok := engine.ParseNewChatCommand(body)
				return ok
			},
		},
		{
			name:    "new",
			content: "@Multica Bot - Local /clear inspect this",
			parse: func(body string) bool {
				_, ok := engine.ParseFreshSessionCommand(body)
				return ok
			},
		},
		{
			name:    "issue",
			content: "@Multica Bot - Local /issue login failed",
			parse: func(body string) bool {
				_, ok := engine.ParseIssueCommand(body)
				return ok
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeGroup, true)
			cb.Text.Content = tc.content
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", verifiedTestBotName)
			if !ok || !tc.parse(msg.CommandText) {
				t.Fatalf("normalized %s command was not recognized: ok=%v CommandText=%q", tc.name, ok, msg.CommandText)
			}

			cb.Text.Content = "@Multica Bot - Local please " + strings.TrimPrefix(tc.content, "@Multica Bot - Local ")
			msg, ok = inboundFromCallbackWithBotName(cb, "appkey-A", verifiedTestBotName)
			if !ok || tc.parse(msg.CommandText) {
				t.Fatalf("mid-sentence %s was promoted: ok=%v CommandText=%q", tc.name, ok, msg.CommandText)
			}
		})
	}
}
