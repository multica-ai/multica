package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestChunkMessage(t *testing.T) {
	if got := chunkMessage("short", 100); len(got) != 1 || got[0] != "short" {
		t.Errorf("short message should be one chunk: %v", got)
	}
	long := make([]rune, 250)
	for i := range long {
		long[i] = 'a'
	}
	chunks := chunkMessage(string(long), 100)
	if len(chunks) != 3 {
		t.Fatalf("250 runes / 100 = 3 chunks, got %d", len(chunks))
	}
	if len([]rune(chunks[0])) != 100 || len([]rune(chunks[2])) != 50 {
		t.Errorf("chunk sizes wrong: %d / %d", len([]rune(chunks[0])), len([]rune(chunks[2])))
	}
}

func TestSendWithMetadataTagsEverySlackMessage(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1.1"}`))
	}))
	defer srv.Close()
	c := newSlackSender(credentials{TeamID: "T1"}, slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")), nil)
	metadata := outboundMetadata(uid(3), 2, "control_ack")
	if _, err := c.SendWithMetadata(context.Background(), channel.OutboundMessage{ChatID: "C123", Text: "started"}, metadata); err != nil {
		t.Fatalf("SendWithMetadata: %v", err)
	}
	var got slack.SlackMetadata
	if err := json.Unmarshal([]byte(gotForm.Get("metadata")), &got); err != nil {
		t.Fatalf("decode metadata %q: %v", gotForm.Get("metadata"), err)
	}
	if got.EventType != slackOutboundMetadataEvent || got.EventPayload["kind"] != "control_ack" {
		t.Fatalf("metadata=%+v", got)
	}
}

func TestSend(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1700000000.111111"}`))
	}))
	defer srv.Close()

	api := slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/"))
	c := newSlackSender(credentials{TeamID: "T1"}, api, nil)

	res, err := c.Send(context.Background(), channel.OutboundMessage{
		ChatID:   "C123",
		Text:     "reply body",
		ThreadID: "1700000000.000400",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.MessageID != "1700000000.111111" {
		t.Errorf("MessageID = %q", res.MessageID)
	}
	if len(res.MessageIDs) != 1 || res.MessageIDs[0] != "1700000000.111111" {
		t.Errorf("MessageIDs = %v", res.MessageIDs)
	}
	if gotForm.Get("channel") != "C123" || gotForm.Get("text") != "reply body" {
		t.Errorf("posted channel/text = %q / %q", gotForm.Get("channel"), gotForm.Get("text"))
	}
	if gotForm.Get("thread_ts") != "1700000000.000400" {
		t.Errorf("thread_ts = %q, want the inbound thread", gotForm.Get("thread_ts"))
	}
}

// TestSend_AppliesMrkdwn guards the wiring: Send must run the agent's Markdown
// through formatMrkdwn before posting, so Slack renders it instead of showing
// literal markup. (The converter itself is covered in mrkdwn_test.go.)
func TestSend_AppliesMrkdwn(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotText = r.PostForm.Get("text")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1.1"}`))
	}))
	defer srv.Close()

	api := slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/"))
	c := newSlackSender(credentials{TeamID: "T1"}, api, nil)

	if _, err := c.Send(context.Background(), channel.OutboundMessage{
		ChatID: "C1",
		Text:   "**bold** see [docs](http://x.com)",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotText != "*bold* see <http://x.com|docs>" {
		t.Errorf("Send must convert Markdown to mrkdwn before posting, got %q", gotText)
	}
}

func TestOutboundThreadTS(t *testing.T) {
	if got := outboundThreadTS(channel.OutboundMessage{ReplyTo: "111.1", ThreadID: "222.2"}); got != "111.1" {
		t.Errorf("explicit ReplyTo should win: %q", got)
	}
	if got := outboundThreadTS(channel.OutboundMessage{ThreadID: "222.2"}); got != "222.2" {
		t.Errorf("thread fallback: %q", got)
	}
	if got := outboundThreadTS(channel.OutboundMessage{}); got != "" {
		t.Errorf("top-level send has no thread: %q", got)
	}
}

func TestSendMarkdownWithMetadata_PostsMarkdownBlockAndFallbackText(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1700000000.222222"}`))
	}))
	defer srv.Close()

	c := newSlackSender(credentials{TeamID: "T1"}, slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")), nil)
	md := "- **状态**：`todo`\n  - 详情见 [docs](https://example.com)\n\n---\n\n```go\nfmt.Println(\"ok\")\n```"
	meta := outboundMetadata(uid(3), 4, "task_reply")

	res, err := c.SendMarkdownWithMetadata(context.Background(), channel.OutboundMessage{
		ChatID:   "C123",
		Text:     md,
		ThreadID: "1700000000.000400",
	}, meta)
	if err != nil {
		t.Fatalf("SendMarkdownWithMetadata: %v", err)
	}
	if res.MessageID != "1700000000.222222" {
		t.Errorf("MessageID = %q", res.MessageID)
	}
	if gotForm.Get("thread_ts") != "1700000000.000400" {
		t.Errorf("thread_ts = %q", gotForm.Get("thread_ts"))
	}
	if gotForm.Get("unfurl_links") != "false" {
		t.Errorf("unfurl_links = %q, want false", gotForm.Get("unfurl_links"))
	}
	if gotForm.Get("text") != formatMrkdwn(md) {
		t.Errorf("fallback text = %q, want %q", gotForm.Get("text"), formatMrkdwn(md))
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(gotForm.Get("blocks")), &blocks); err != nil {
		t.Fatalf("decode blocks %q: %v", gotForm.Get("blocks"), err)
	}
	if len(blocks) != 1 || blocks[0].Type != "markdown" || blocks[0].Text != md {
		t.Fatalf("blocks = %+v, want single markdown block with original markdown", blocks)
	}

	var gotMeta slack.SlackMetadata
	if err := json.Unmarshal([]byte(gotForm.Get("metadata")), &gotMeta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if gotMeta.EventType != slackOutboundMetadataEvent || gotMeta.EventPayload["kind"] != "task_reply" {
		t.Fatalf("metadata = %+v", gotMeta)
	}
}

func TestSendMarkdownWithMetadata_ChunksLongRepliesWithinLimit(t *testing.T) {
	var postedBlocks []string
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		call++
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(r.PostForm.Get("blocks")), &blocks); err != nil {
			t.Fatalf("decode blocks: %v", err)
		}
		if len(blocks) != 1 || blocks[0].Type != "markdown" {
			t.Fatalf("call %d blocks = %+v", call, blocks)
		}
		postedBlocks = append(postedBlocks, blocks[0].Text)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"channel":"C1","ts":"1.%d"}`, call)
	}))
	defer srv.Close()

	para := strings.Repeat("这是一段测试文本。", 400) // 3600 runes
	body := strings.Join([]string{para, para, para, para}, "\n\n")

	c := newSlackSender(credentials{TeamID: "T1"}, slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")), nil)
	res, err := c.SendMarkdownWithMetadata(context.Background(), channel.OutboundMessage{
		ChatID:   "C1",
		Text:     body,
		ThreadID: "1.0",
	}, slack.SlackMetadata{})
	if err != nil {
		t.Fatalf("SendMarkdownWithMetadata: %v", err)
	}
	if len(postedBlocks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(postedBlocks))
	}
	if len(res.MessageIDs) != len(postedBlocks) || res.MessageID != res.MessageIDs[len(res.MessageIDs)-1] {
		t.Fatalf("MessageID=%q MessageIDs=%v", res.MessageID, res.MessageIDs)
	}
	for i, b := range postedBlocks {
		if n := utf8.RuneCountInString(b); n > maxMarkdownBlockRunes {
			t.Errorf("chunk %d has %d runes > %d", i, n, maxMarkdownBlockRunes)
		}
	}
}

func TestSendMarkdownWithMetadata_ResplitsOnMsgBlocksTooLong(t *testing.T) {
	var accepted []string
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		attempts++
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(r.PostForm.Get("blocks")), &blocks); err != nil {
			t.Fatalf("decode blocks: %v", err)
		}
		if len(blocks) != 1 || blocks[0].Type != "markdown" {
			t.Fatalf("attempt %d blocks = %+v", attempts, blocks)
		}
		w.Header().Set("Content-Type", "application/json")
		// Simulate Slack expanding emoji to :shortcode: so a 1,900-rune chunk
		// exceeds the 12,000-character limit and triggers msg_blocks_too_long.
		if utf8.RuneCountInString(blocks[0].Text) > 800 {
			_, _ = w.Write([]byte(`{"ok":false,"error":"msg_blocks_too_long"}`))
			return
		}
		accepted = append(accepted, blocks[0].Text)
		_, _ = fmt.Fprintf(w, `{"ok":true,"channel":"C1","ts":"1.%d"}`, len(accepted))
	}))
	defer srv.Close()

	// 1,900 runes of emoji-heavy content fits under maxMarkdownBlockRunes on
	// the initial pass, then gets halved until every piece fits <= 800 runes.
	body := strings.Repeat("🚀", 1900)
	c := newSlackSender(credentials{TeamID: "T1"}, slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")), nil)
	res, err := c.SendMarkdownWithMetadata(context.Background(), channel.OutboundMessage{
		ChatID:   "C1",
		Text:     body,
		ThreadID: "1.0",
	}, slack.SlackMetadata{})
	if err != nil {
		t.Fatalf("SendMarkdownWithMetadata: %v", err)
	}
	if len(accepted) < 3 {
		t.Fatalf("expected >=3 accepted sub-chunks, got %d", len(accepted))
	}
	if strings.Join(accepted, "") != body {
		t.Fatalf("re-split lost content: got %d runes, want %d", utf8.RuneCountInString(strings.Join(accepted, "")), utf8.RuneCountInString(body))
	}
	if len(res.MessageIDs) != len(accepted) || res.MessageID != res.MessageIDs[len(res.MessageIDs)-1] {
		t.Fatalf("MessageID=%q MessageIDs=%v", res.MessageID, res.MessageIDs)
	}
}

func TestSendMarkdownWithMetadata_FailsLoudlyOnOtherSlackErrors(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	c := newSlackSender(credentials{TeamID: "T1"}, slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")), nil)
	if _, err := c.SendMarkdownWithMetadata(context.Background(), channel.OutboundMessage{
		ChatID: "C1",
		Text:   "hello",
	}, slack.SlackMetadata{}); err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt without retry, got %d", attempts)
	}
}

func TestChunkMarkdown(t *testing.T) {
	t.Run("short unchanged", func(t *testing.T) {
		got := chunkMarkdown("hello", 50)
		if len(got) != 1 || got[0] != "hello" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("prefers paragraph boundary", func(t *testing.T) {
		in := "para one\nline two\n\npara two\nline four"
		got := chunkMarkdown(in, 24)
		if len(got) != 2 || got[0] != "para one\nline two" || got[1] != "para two\nline four" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("closes and reopens fenced code block across chunks", func(t *testing.T) {
		in := "```go\nline1\nline2\nline3\nline4\n```"
		got := chunkMarkdown(in, 22)
		if len(got) < 2 {
			t.Fatalf("expected >=2 chunks, got %q", got)
		}
		for i, c := range got {
			if n := utf8.RuneCountInString(c); n > 22 {
				t.Errorf("chunk %d exceeds limit: %d > 22 (%q)", i, n, c)
			}
			if !strings.HasPrefix(c, "```go\n") || !strings.HasSuffix(c, "\n```") {
				t.Errorf("chunk %d must be a balanced go code block, got %q", i, c)
			}
		}
	})

	t.Run("handles single oversized CJK line", func(t *testing.T) {
		in := strings.Repeat("中", 25)
		got := chunkMarkdown(in, 10)
		if len(got) != 3 {
			t.Fatalf("expected 3 chunks, got %d (%q)", len(got), got)
		}
		if strings.Join(got, "") != in {
			t.Fatalf("content lost: %q", strings.Join(got, ""))
		}
		for i, c := range got {
			if n := utf8.RuneCountInString(c); n > 10 {
				t.Errorf("chunk %d = %d runes > 10", i, n)
			}
		}
	})

	t.Run("no empty chunk on blank line before oversized line", func(t *testing.T) {
		in := "hello\n\n" + strings.Repeat("a", 25)
		got := chunkMarkdown(in, 15)
		for i, c := range got {
			if strings.TrimSpace(c) == "" {
				t.Errorf("chunk %d is empty: %q", i, got)
			}
			if n := utf8.RuneCountInString(c); n > 15 {
				t.Errorf("chunk %d = %d runes > 15", i, n)
			}
		}
	})

	t.Run("no empty trailing chunk on trailing newline", func(t *testing.T) {
		in := strings.Repeat("a", 10) + "\n"
		got := chunkMarkdown(in, 10)
		if len(got) != 1 || got[0] != strings.Repeat("a", 10) {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("no empty fenced block when first code line overflows", func(t *testing.T) {
		in := "prose line\n```go\n" + strings.Repeat("x", 30) + "\n```"
		got := chunkMarkdown(in, 24)
		for i, c := range got {
			if strings.Contains(c, "```go\n```") {
				t.Errorf("chunk %d contains empty code block: %q", i, c)
			}
			if n := utf8.RuneCountInString(c); n > 24 {
				t.Errorf("chunk %d = %d runes > 24 (%q)", i, n, c)
			}
		}
	})

	t.Run("tiny limit with long fence info string never exceeds limit", func(t *testing.T) {
		in := "```typescript-react\nconst x = 1\n```"
		got := chunkMarkdown(in, 10)
		for i, c := range got {
			if n := utf8.RuneCountInString(c); n > 10 {
				t.Errorf("chunk %d = %d runes > 10 (%q)", i, n, c)
			}
		}
	})

	t.Run("tilde and 4-backtick fences", func(t *testing.T) {
		in := "~~~~markdown\n```go\nline1\nline2\n```\n~~~~"
		got := chunkMarkdown(in, 30)
		if len(got) < 2 {
			t.Fatalf("expected >=2 chunks, got %q", got)
		}
		for i, c := range got {
			if n := utf8.RuneCountInString(c); n > 30 {
				t.Errorf("chunk %d = %d runes > 30 (%q)", i, n, c)
			}
			if !strings.HasPrefix(c, "~~~~markdown\n") || !strings.HasSuffix(c, "\n~~~~") {
				t.Errorf("chunk %d unbalanced tilde fence: %q", i, c)
			}
		}
	})
}
