package lark

import (
	"context"
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
