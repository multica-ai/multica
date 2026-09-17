package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dingtalkSendServer stubs the access-token mint plus the two robot send
// endpoints, recording the last send it saw.
type dingtalkSendServer struct {
	srv        *httptest.Server
	tokenCalls int32
	lastPath   string
	lastBody   map[string]any
	sendBodies []map[string]any
	// failFirstSendAuth makes the first send return 401 so the token-refresh
	// retry path is exercised.
	failFirstSendAuth   bool
	emotionSuccessFalse bool
	sendCalls           int32
}

func newDingtalkSendServer(t *testing.T) *dingtalkSendServer {
	t.Helper()
	d := &dingtalkSendServer{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case accessTokenPath:
			atomic.AddInt32(&d.tokenCalls, 1)
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
		case pathSendP2P, pathSendGroup, pathReplyEmotion, pathRecallEmotion:
			n := atomic.AddInt32(&d.sendCalls, 1)
			if d.failFirstSendAuth && n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"unauthorized","message":"token expired"}`))
				return
			}
			body, _ := io.ReadAll(r.Body)
			d.lastPath = r.URL.Path
			d.lastBody = map[string]any{}
			_ = json.Unmarshal(body, &d.lastBody)
			d.sendBodies = append(d.sendBodies, d.lastBody)
			if r.URL.Path == pathReplyEmotion || r.URL.Path == pathRecallEmotion {
				if d.emotionSuccessFalse {
					_, _ = w.Write([]byte(`{"success":false}`))
				} else {
					_, _ = w.Write([]byte(`{"success":true}`))
				}
			} else {
				_, _ = w.Write([]byte(`{"processQueryKey":"pqk-1"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func newTestSender(client *Client) *sender {
	return &sender{client: client, robotCode: "robot-1", appKey: "ak", appSecret: "as"}
}

func TestSender_P2PSendHitsBatchSend(t *testing.T) {
	d := newDingtalkSendServer(t)
	s := newTestSender(NewClient(nil, d.srv.URL))

	key, err := s.send(context.Background(), sendTarget{ConversationType: convTypeP2P, ConversationID: "cid", StaffID: "staff-1"}, "hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if key != "pqk-1" {
		t.Errorf("key = %q, want pqk-1", key)
	}
	if d.lastPath != pathSendP2P {
		t.Errorf("path = %q, want batchSend", d.lastPath)
	}
	if d.lastBody["robotCode"] != "robot-1" {
		t.Errorf("robotCode = %v", d.lastBody["robotCode"])
	}
	if ids, ok := d.lastBody["userIds"].([]any); !ok || len(ids) != 1 || ids[0] != "staff-1" {
		t.Errorf("userIds = %v", d.lastBody["userIds"])
	}
	if got := d.lastBody["msgKey"]; got != "sampleMarkdown" {
		t.Fatalf("msgKey = %v, want documented sampleMarkdown", got)
	}
	if _, ok := d.lastBody["atUserIds"]; ok {
		t.Fatalf("1:1 send must not contain atUserIds: %v", d.lastBody)
	}
	paramRaw, ok := d.lastBody["msgParam"].(string)
	if !ok {
		t.Fatalf("msgParam = %T", d.lastBody["msgParam"])
	}
	var param markdownParam
	if err := json.Unmarshal([]byte(paramRaw), &param); err != nil {
		t.Fatalf("decode msgParam: %v", err)
	}
	if param.Text != "hi" {
		t.Fatalf("1:1 Markdown = %q, want no sender mention", param.Text)
	}
}

func TestSender_GroupSendHitsGroupMessages(t *testing.T) {
	d := newDingtalkSendServer(t)
	s := newTestSender(NewClient(nil, d.srv.URL))

	if _, err := s.send(context.Background(), sendTarget{ConversationType: convTypeGroup, ConversationID: "cid-g"}, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if d.lastPath != pathSendGroup {
		t.Errorf("path = %q, want groupMessages/send", d.lastPath)
	}
	if d.lastBody["openConversationId"] != "cid-g" {
		t.Errorf("openConversationId = %v", d.lastBody["openConversationId"])
	}
	if got := d.lastBody["msgKey"]; got != "sampleMarkdown" {
		t.Fatalf("msgKey = %v, want upstream sampleMarkdown", got)
	}
	if _, ok := d.lastBody["atUserIds"]; ok {
		t.Fatalf("group send without a sender target must not contain atUserIds: %v", d.lastBody)
	}
}

func TestPrependMarkdownQuoteSkipsEmptyQuote(t *testing.T) {
	if got := prependMarkdownQuote("answer", " \n "); got != "answer" {
		t.Fatalf("empty quote changed answer: %q", got)
	}
}

func TestPrependMarkdownQuoteSeparatesQuoteFromReply(t *testing.T) {
	if got := prependMarkdownQuote("answer", "question"); got != "> question\n\n---\n\nanswer" {
		t.Fatalf("quoted reply = %q", got)
	}
}

func TestSender_EmojiReactionUsesDefaultRobotReaction(t *testing.T) {
	if emotionAcknowledged != "收到" || emotionDone != "Done" {
		t.Fatalf("emoji reaction names = %q / %q", emotionAcknowledged, emotionDone)
	}
	d := newDingtalkSendServer(t)
	s := newTestSender(NewClient(nil, d.srv.URL))
	target := sendTarget{ConversationType: convTypeGroup, ConversationID: "cid-g", SourceMessageID: "msg-original"}

	if err := s.setEmojiReaction(context.Background(), target, emotionAcknowledged, false); err != nil {
		t.Fatalf("add emotion: %v", err)
	}
	if d.lastPath != pathReplyEmotion || d.lastBody["emotionType"] != float64(1) || d.lastBody["emotionName"] != emotionAcknowledged {
		t.Fatalf("reaction request path/body = %q / %v", d.lastPath, d.lastBody)
	}
	if _, exists := d.lastBody["textEmotion"]; exists {
		t.Fatalf("default emoji request contains textEmotion: %v", d.lastBody)
	}
	if err := s.setEmojiReaction(context.Background(), target, emotionDone, false); err != nil {
		t.Fatalf("add done emotion: %v", err)
	}
	if d.lastPath != pathReplyEmotion || d.lastBody["emotionType"] != float64(1) || d.lastBody["emotionName"] != emotionDone {
		t.Fatalf("done request path/body = %q / %v", d.lastPath, d.lastBody)
	}
	if err := s.setEmojiReaction(context.Background(), target, emotionAcknowledged, true); err != nil {
		t.Fatalf("recall emotion: %v", err)
	}
	if d.lastPath != pathRecallEmotion || d.lastBody["emotionType"] != float64(1) || d.lastBody["emotionName"] != emotionAcknowledged {
		t.Fatalf("recall request path/body = %q / %v", d.lastPath, d.lastBody)
	}
}

func TestSender_EmojiReactionRejectsUnknownName(t *testing.T) {
	s := newTestSender(NewClient(nil, "https://api.dingtalk.test"))
	target := sendTarget{ConversationID: "cid", SourceMessageID: "msg"}
	if err := s.setEmojiReaction(context.Background(), target, "未注册名称", false); err == nil {
		t.Fatal("unknown default emoji name must be rejected")
	}
}

func TestSender_EmojiReactionSupportsP2P(t *testing.T) {
	d := newDingtalkSendServer(t)
	s := newTestSender(NewClient(nil, d.srv.URL))
	target := sendTarget{
		ConversationType: convTypeP2P,
		ConversationID:   "cid-p2p",
		SourceMessageID:  "msg-p2p",
	}

	if err := s.setEmojiReaction(context.Background(), target, emotionAcknowledged, false); err != nil {
		t.Fatalf("add p2p emotion: %v", err)
	}
	if d.lastPath != pathReplyEmotion || d.lastBody["openConversationId"] != "cid-p2p" || d.lastBody["openMsgId"] != "msg-p2p" {
		t.Fatalf("p2p emotion request path/body = %q / %v", d.lastPath, d.lastBody)
	}
}

func TestSender_EmojiReactionRequiresConversationAndMessageIDs(t *testing.T) {
	s := newTestSender(NewClient(nil, "https://api.dingtalk.test"))
	if err := s.setEmojiReaction(context.Background(), sendTarget{ConversationID: "cid"}, emotionAcknowledged, false); err == nil {
		t.Fatal("missing source message id must fail before an API call")
	}
	if err := s.setEmojiReaction(context.Background(), sendTarget{SourceMessageID: "msg"}, emotionAcknowledged, false); err == nil {
		t.Fatal("missing conversation id must fail before an API call")
	}
}

func TestSender_EmojiReactionRefreshesTokenOn401(t *testing.T) {
	d := newDingtalkSendServer(t)
	d.failFirstSendAuth = true
	s := newTestSender(NewClient(nil, d.srv.URL))
	target := sendTarget{ConversationID: "cid", SourceMessageID: "msg"}
	if err := s.setEmojiReaction(context.Background(), target, emotionAcknowledged, false); err != nil {
		t.Fatalf("emotion should succeed after token refresh: %v", err)
	}
	if got := atomic.LoadInt32(&d.tokenCalls); got != 2 {
		t.Fatalf("token calls = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&d.sendCalls); got != 2 {
		t.Fatalf("emotion calls = %d, want 2", got)
	}
}

func TestSender_EmojiReactionRejectsSuccessFalse(t *testing.T) {
	d := newDingtalkSendServer(t)
	d.emotionSuccessFalse = true
	s := newTestSender(NewClient(nil, d.srv.URL))
	target := sendTarget{ConversationID: "cid", SourceMessageID: "msg"}
	if err := s.setEmojiReaction(context.Background(), target, emotionAcknowledged, false); err == nil {
		t.Fatal("success=false must fail")
	}
}

func TestSender_P2PWithoutStaffIDFails(t *testing.T) {
	d := newDingtalkSendServer(t)
	s := newTestSender(NewClient(nil, d.srv.URL))
	if _, err := s.send(context.Background(), sendTarget{ConversationType: convTypeP2P, ConversationID: "cid"}, "hi"); err == nil {
		t.Error("a 1:1 send without a recipient staff id must fail")
	}
}

func TestSender_RefreshesTokenOn401(t *testing.T) {
	d := newDingtalkSendServer(t)
	d.failFirstSendAuth = true
	s := newTestSender(NewClient(nil, d.srv.URL))

	if _, err := s.send(context.Background(), sendTarget{ConversationType: convTypeGroup, ConversationID: "cid-g"}, "hi"); err != nil {
		t.Fatalf("send should succeed after a token refresh: %v", err)
	}
	if got := atomic.LoadInt32(&d.tokenCalls); got != 2 {
		t.Errorf("token fetched %d times, want 2 (initial + refresh on 401)", got)
	}
	if got := atomic.LoadInt32(&d.sendCalls); got != 2 {
		t.Errorf("send attempted %d times, want 2 (401 then retry)", got)
	}
}

func TestClient_CachesAccessToken(t *testing.T) {
	d := newDingtalkSendServer(t)
	c := NewClient(nil, d.srv.URL)

	for i := 0; i < 3; i++ {
		if _, err := c.accessToken(context.Background(), "ak", "as"); err != nil {
			t.Fatalf("accessToken: %v", err)
		}
	}
	if got := atomic.LoadInt32(&d.tokenCalls); got != 1 {
		t.Errorf("token fetched %d times, want 1 (cached)", got)
	}
	// After invalidation the next call refetches.
	c.invalidate("ak")
	if _, err := c.accessToken(context.Background(), "ak", "as"); err != nil {
		t.Fatalf("accessToken after invalidate: %v", err)
	}
	if got := atomic.LoadInt32(&d.tokenCalls); got != 2 {
		t.Errorf("token fetched %d times after invalidate, want 2", got)
	}
}

func TestClient_RefreshesExpiredToken(t *testing.T) {
	d := newDingtalkSendServer(t)
	c := NewClient(nil, d.srv.URL)
	// Freeze time so we can step past the cached token's expiry deterministically.
	base := time.Unix(1_700_000_000, 0)
	cur := base
	c.now = func() time.Time { return cur }

	if _, err := c.accessToken(context.Background(), "ak", "as"); err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	// expireIn=7200s, margin=5m → cached ~7200-300s. Jump past it.
	cur = base.Add(2 * time.Hour)
	if _, err := c.accessToken(context.Background(), "ak", "as"); err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if got := atomic.LoadInt32(&d.tokenCalls); got != 2 {
		t.Errorf("token fetched %d times, want 2 (cache expired)", got)
	}
}

func TestClient_AccessToken_SingleflightOnConcurrentMiss(t *testing.T) {
	var tokenCalls int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != accessTokenPath {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		atomic.AddInt32(&tokenCalls, 1)
		<-release // hold the mint open so concurrent callers pile into the flight
		_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
	}))
	defer srv.Close()

	c := NewClient(nil, srv.URL)
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.accessToken(context.Background(), "ak", "as"); err != nil {
				errs <- err
			}
		}()
	}
	// Let every goroutine reach the singleflight before the mint completes: a
	// missing singleflight would let each concurrent miss fire its own request.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("accessToken: %v", err)
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 1 {
		t.Fatalf("concurrent cache misses minted %d tokens, want 1 (singleflight)", got)
	}
}

func TestClient_AccessToken_CancelledCallerDoesNotCancelSharedMint(t *testing.T) {
	var tokenCalls int32
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != accessTokenPath {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if atomic.AddInt32(&tokenCalls, 1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
	}))
	defer srv.Close()

	c := NewClient(nil, srv.URL)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.accessToken(firstCtx, "ak", "as")
		firstDone <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shared mint did not start")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := c.accessToken(context.Background(), "ak", "as")
		secondDone <- err
	}()

	cancelFirst()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first caller error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled caller stayed blocked on the shared mint")
	}

	close(release)
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second caller lost the shared mint: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second caller did not receive the shared mint result")
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 1 {
		t.Fatalf("cancelled first caller caused %d token mints, want 1", got)
	}
}

func TestSender_LongSingleLineAnswerBlockquotePreservesEveryChunk(t *testing.T) {
	// Use an answer blockquote: source QuoteText is capped before chunking.
	source := strings.Repeat("界", 15000)
	d := newDingtalkSendServer(t)
	_, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), sendTarget{
		ConversationType: convTypeGroup,
		ConversationID:   "group",
	}, "> "+source)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.sendBodies) < 2 {
		t.Fatal("long answer must exercise chunking")
	}
	var joined strings.Builder
	for i, body := range d.sendBodies {
		raw := body["msgParam"].(string)
		var param markdownParam
		if err := json.Unmarshal([]byte(raw), &param); err != nil {
			t.Fatal(err)
		}
		if len(raw) > 15000 || !strings.HasPrefix(param.Text, "> ") || param.Title != defaultMarkdownTitle {
			t.Fatalf("chunk %d lost its blockquote/title or exceeded the payload budget: %q", i, raw)
		}
		joined.WriteString(strings.TrimPrefix(param.Text, "> "))
	}
	if joined.String() != source {
		t.Fatal("answer blockquote content was lost or duplicated across chunks")
	}
}
