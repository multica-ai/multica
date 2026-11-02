package lark

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNativeHistoryTransportPagingAndScope(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("container_id_type") != "thread" || q.Get("container_id") != "omt_topic" || q.Get("page_token") != "opaque+/=" || q.Get("page_size") != "50" || q.Has("end_time") {
			t.Errorf("wrong query: %v", q)
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"message_id": "om_1", "chat_id": "oc_chat", "thread_id": "omt_topic"}}, "has_more": true, "page_token": "next"}})
	})
	c := newTestClient(fake, time.Now)
	page, err := c.ListNativeHistoryPage(context.Background(), testCreds(), ListMessagesParams{ChatID: "oc_chat", ThreadID: "omt_topic"}, "opaque+/=")
	if err != nil || !page.HasMore || page.NextToken != "next" || len(page.Messages) != 1 || page.Messages[0].ChatID != "oc_chat" || page.Messages[0].ThreadID != "omt_topic" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}
func TestNativeHistoryTransportRefusalNoRetry(t *testing.T) {
	for _, body := range []string{`{"code":99991663}`, `{"code":230027}`, `{"data":{}}`, `{"code":0,"data":{"items":[]}}`, `{"code":0,"data":{"has_more":false,"page_token":"unexpected"}}`, `{"code":0,"data":{"has_more":true}}`, `{"code":0,"data":{"items":[]}}` + strings.Repeat(" ", 1<<20)} {
		t.Run(body[:min(25, len(body))], func(t *testing.T) {
			fake := newLarkFake(t)
			fake.stubToken("tok", 7200)
			calls := 0
			fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(body)) })
			c := newTestClient(fake, time.Now)
			_, err := c.ListNativeHistoryPage(context.Background(), testCreds(), ListMessagesParams{ChatID: "oc_chat"}, "")
			if err == nil || calls != 1 {
				t.Fatalf("refusal retried/accepted: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestNativeHistoryDocumentedAccessRefusals(t *testing.T) {
	// Codes are documented on the official IM v1 message/list endpoint.
	for _, tc := range []struct {
		status, code int
		unavailable  bool
	}{
		{400, 230002, true}, {400, 230006, true}, {400, 230013, true}, {400, 230027, true},
		{400, 230073, true}, {400, 230110, true}, {400, 231203, true}, {400, 231204, true},
		{200, 230027, true}, {403, 230027, true}, {500, 230027, false},
		{400, 230001, false}, {400, 230020, false}, {400, 999999, false}, {403, 0, false},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.status, tc.code), func(t *testing.T) {
			fake := newLarkFake(t)
			fake.stubToken("tok", 7200)
			calls := 0
			fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"code":%d,"msg":"private upstream detail"}`, tc.code)
			})
			_, err := newTestClient(fake, time.Now).ListNativeHistoryPage(context.Background(), testCreds(), ListMessagesParams{ChatID: "oc_chat"}, "")
			if err == nil || errors.Is(err, ErrNativeHistoryUnavailable) != tc.unavailable || strings.Contains(err.Error(), "private upstream detail") || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}
func TestNativeHistoryChatContainerMixedRoots(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	calls := 0
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if q.Get("container_id_type") != "chat" || q.Get("container_id") != "oc_chat" {
			t.Errorf("expanded scope: %v", q)
		}
		items := []any{}
		for _, it := range []struct{ id, ts, thread string }{{"trigger", "4000", ""}, {"root", "3000", "omt_other"}, {"plain", "2000", ""}, {"start", "1000", ""}} {
			// Topic roots carry thread_id while root_id/parent_id are absent.
			item := map[string]any{"message_id": it.id, "chat_id": "oc_chat", "create_time": it.ts, "msg_type": "text", "sender": map[string]any{"sender_type": "user", "id": "ou_test"}, "body": map[string]any{"content": fmt.Sprintf(`{"text":%q}`, it.id)}}
			if it.thread != "" {
				item["thread_id"] = it.thread
			}
			items = append(items, item)
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"has_more": false, "items": items}})
	})
	page, _, err := readNativeWindow(context.Background(), newTestClient(fake, time.Now), testCreds(), nativeHistoryTarget{chatID: "oc_chat", trigger: "trigger", start: "start"}, 10)
	if err != nil || len(page.Messages) != 3 || page.Messages[1].ID != "plain" || calls != 1 {
		t.Fatalf("page=%+v err=%v calls=%d", page, err, calls)
	}
}
