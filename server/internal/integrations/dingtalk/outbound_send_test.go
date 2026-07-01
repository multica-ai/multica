package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	// failFirstSendAuth makes the first send return 401 so the token-refresh
	// retry path is exercised.
	failFirstSendAuth bool
	sendCalls         int32
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
		case pathSendP2P, pathSendGroup:
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
			_, _ = w.Write([]byte(`{"processQueryKey":"pqk-1"}`))
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
