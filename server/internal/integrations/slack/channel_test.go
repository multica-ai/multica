package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
