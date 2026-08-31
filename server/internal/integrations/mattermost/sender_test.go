package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestChunkMessage(t *testing.T) {
	t.Run("short text is one chunk", func(t *testing.T) {
		got := chunkMessage("hello", 100)
		if len(got) != 1 || got[0] != "hello" {
			t.Fatalf("chunkMessage = %#v, want one unchanged chunk", got)
		}
	})

	t.Run("splits on the last newline in the window", func(t *testing.T) {
		// 30 runes of "a", newline, then 30 of "b"; a 40-rune cap must break at
		// the newline rather than mid-word.
		text := strings.Repeat("a", 30) + "\n" + strings.Repeat("b", 30)
		got := chunkMessage(text, 40)
		if len(got) != 2 {
			t.Fatalf("got %d chunks, want 2: %#v", len(got), got)
		}
		if got[0] != strings.Repeat("a", 30) {
			t.Errorf("first chunk = %q, want the line before the newline", got[0])
		}
		if got[1] != strings.Repeat("b", 30) {
			t.Errorf("second chunk = %q, want the line after", got[1])
		}
	})

	t.Run("hard-splits when a newline break would leave a stub", func(t *testing.T) {
		// The only newline sits at rune 2, well under half the cap, so honoring
		// it would emit a 2-rune chunk. Splitting at the cap is better.
		text := "ab\n" + strings.Repeat("c", 100)
		got := chunkMessage(text, 40)
		if len([]rune(got[0])) != 40 {
			t.Fatalf("first chunk = %d runes, want a full 40", len([]rune(got[0])))
		}
	})

	t.Run("counts runes, not bytes", func(t *testing.T) {
		// Ten 4-byte emoji: 10 runes, 40 bytes. A 10-rune cap keeps them
		// together; a byte-based implementation would split mid-sequence.
		text := strings.Repeat("😀", 10)
		got := chunkMessage(text, 10)
		if len(got) != 1 {
			t.Fatalf("got %d chunks, want 1", len(got))
		}
		if got[0] != text {
			t.Error("emoji text was altered")
		}
	})

	t.Run("never splits a rune", func(t *testing.T) {
		text := strings.Repeat("😀", 25)
		for _, chunk := range chunkMessage(text, 10) {
			if strings.ContainsRune(chunk, '�') {
				t.Fatalf("chunk contains a replacement character: %q", chunk)
			}
		}
	})

	t.Run("reassembles to the original content", func(t *testing.T) {
		text := strings.Repeat("line of text\n", 500)
		var rebuilt strings.Builder
		for _, chunk := range chunkMessage(text, 200) {
			rebuilt.WriteString(chunk)
			rebuilt.WriteString("\n")
		}
		// Trailing newlines are trimmed per chunk, so compare on non-empty
		// lines rather than byte equality.
		want := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' })
		got := strings.FieldsFunc(rebuilt.String(), func(r rune) bool { return r == '\n' })
		if len(got) != len(want) {
			t.Fatalf("rebuilt %d lines, want %d", len(got), len(want))
		}
	})
}

// postRecorder is a fake Mattermost that records every created post.
type postRecorder struct {
	posts  []Post
	server *httptest.Server
}

func newPostRecorder(t *testing.T) *postRecorder {
	t.Helper()
	rec := &postRecorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiPath+"/posts" || r.Method != http.MethodPost {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var in Post
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in.ID = "created" + itoa(len(rec.posts)+1)
		rec.posts = append(rec.posts, in)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(in)
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestSenderSend(t *testing.T) {
	t.Run("posts into the channel", func(t *testing.T) {
		rec := newPostRecorder(t)
		s := newSender(newRESTClient(rec.server.URL, "tok", rec.server.Client()), nil)
		result, err := s.Send(context.Background(), channel.OutboundMessage{ChatID: "chan1", Text: "hello"})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if len(rec.posts) != 1 {
			t.Fatalf("created %d posts, want 1", len(rec.posts))
		}
		if rec.posts[0].ChannelID != "chan1" || rec.posts[0].Message != "hello" {
			t.Errorf("post = %+v, want the message in chan1", rec.posts[0])
		}
		// Markdown goes out verbatim: Mattermost renders it natively, so there
		// is no conversion pass to get wrong.
		if result.MessageID != "created1" {
			t.Errorf("MessageID = %q, want created1", result.MessageID)
		}
		if len(result.MessageIDs) != 1 {
			t.Errorf("MessageIDs = %#v, want one id", result.MessageIDs)
		}
	})

	t.Run("threads under ThreadID", func(t *testing.T) {
		rec := newPostRecorder(t)
		s := newSender(newRESTClient(rec.server.URL, "tok", rec.server.Client()), nil)
		if _, err := s.Send(context.Background(), channel.OutboundMessage{
			ChatID: "chan1", Text: "reply", ThreadID: "root7",
		}); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if rec.posts[0].RootID != "root7" {
			t.Errorf("RootID = %q, want root7", rec.posts[0].RootID)
		}
	})

	t.Run("falls back to ReplyTo when no thread is established", func(t *testing.T) {
		rec := newPostRecorder(t)
		s := newSender(newRESTClient(rec.server.URL, "tok", rec.server.Client()), nil)
		if _, err := s.Send(context.Background(), channel.OutboundMessage{
			ChatID: "chan1", Text: "reply", ReplyTo: "post3",
		}); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if rec.posts[0].RootID != "post3" {
			t.Errorf("RootID = %q, want post3", rec.posts[0].RootID)
		}
	})

	t.Run("later chunks join the thread the first one anchored", func(t *testing.T) {
		rec := newPostRecorder(t)
		s := newSender(newRESTClient(rec.server.URL, "tok", rec.server.Client()), nil)
		long := strings.Repeat("x", maxPostRunes+500)
		result, err := s.Send(context.Background(), channel.OutboundMessage{ChatID: "chan1", Text: long})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if len(rec.posts) != 2 {
			t.Fatalf("created %d posts, want 2", len(rec.posts))
		}
		if rec.posts[0].RootID != "" {
			t.Errorf("first chunk RootID = %q, want empty (it starts the thread)", rec.posts[0].RootID)
		}
		if rec.posts[1].RootID != "created1" {
			t.Errorf("second chunk RootID = %q, want created1", rec.posts[1].RootID)
		}
		// Every delivered post id is reported so provenance covers all of them.
		if len(result.MessageIDs) != 2 {
			t.Errorf("MessageIDs = %#v, want both ids", result.MessageIDs)
		}
		if result.MessageID != "created2" {
			t.Errorf("MessageID = %q, want the last id", result.MessageID)
		}
	})

	t.Run("refuses to post nothing", func(t *testing.T) {
		rec := newPostRecorder(t)
		s := newSender(newRESTClient(rec.server.URL, "tok", rec.server.Client()), nil)
		for _, text := range []string{"", "   \n  "} {
			if _, err := s.Send(context.Background(), channel.OutboundMessage{ChatID: "c", Text: text}); err == nil {
				t.Errorf("Send(%q) succeeded, want an error", text)
			}
		}
		if len(rec.posts) != 0 {
			t.Errorf("created %d posts for empty text, want 0", len(rec.posts))
		}
	})

	t.Run("requires a channel id", func(t *testing.T) {
		rec := newPostRecorder(t)
		s := newSender(newRESTClient(rec.server.URL, "tok", rec.server.Client()), nil)
		if _, err := s.Send(context.Background(), channel.OutboundMessage{Text: "hi"}); err == nil {
			t.Fatal("Send with no channel id succeeded, want an error")
		}
	})

	t.Run("reports the ids that landed when a later chunk fails", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls > 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"id":"api.post.create","message":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Post{ID: "created1"})
		}))
		defer srv.Close()

		s := newSender(newRESTClient(srv.URL, "tok", srv.Client()), nil)
		result, err := s.Send(context.Background(), channel.OutboundMessage{
			ChatID: "chan1", Text: strings.Repeat("x", maxPostRunes+500),
		})
		if err == nil {
			t.Fatal("Send succeeded, want the failure reported")
		}
		// The first chunk is visible to the user, so its id must not be lost.
		if len(result.MessageIDs) != 1 || result.MessageIDs[0] != "created1" {
			t.Errorf("MessageIDs = %#v, want the delivered id", result.MessageIDs)
		}
	})

	t.Run("without a client", func(t *testing.T) {
		s := newSender(nil, nil)
		if _, err := s.Send(context.Background(), channel.OutboundMessage{ChatID: "c", Text: "hi"}); err == nil {
			t.Fatal("Send with no client succeeded, want an error")
		}
	})
}
