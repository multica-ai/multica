package lark

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

const messageGetPrefix = "/open-apis/im/v1/messages/"

// TestHTTPClient_GetMessageSingle exercises the happy path: a normal
// message comes back as a one-element items[] and is normalized with
// raw body.content, sender, and REST-shaped mentions intact.
func TestHTTPClient_GetMessageSingle(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc(messageGetPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("want GET, got %s", r.Method)
		}
		if id := strings.TrimPrefix(r.URL.Path, messageGetPrefix); id != "om_parent" {
			t.Errorf("path id = %q", id)
		}
		if r.URL.Query().Get("user_id_type") != "open_id" {
			t.Errorf("missing user_id_type=open_id: %q", r.URL.RawQuery)
		}
		writeJSON(w, map[string]any{
			"code": 0, "msg": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"message_id":  "om_parent",
						"msg_type":    "text",
						"create_time": "1000",
						"sender":      map[string]any{"id": "ou_a", "id_type": "open_id", "sender_type": "user"},
						"body":        map[string]any{"content": `{"text":"hi"}`},
						"mentions":    []any{map[string]any{"key": "@_user_1", "id": "ou_b", "name": "Bob"}},
					},
				},
			},
		})
	})

	c := newTestClient(fake, time.Now)
	items, err := c.GetMessage(context.Background(), testCreds(), "om_parent")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	m := items[0]
	if m.MessageID != "om_parent" || m.MessageType != "text" || m.Content != `{"text":"hi"}` {
		t.Errorf("normalized = %+v", m)
	}
	if m.SenderID != "ou_a" || m.SenderType != "user" {
		t.Errorf("sender = id:%q type:%q", m.SenderID, m.SenderType)
	}
	if len(m.Mentions) != 1 || m.Mentions[0].Key != "@_user_1" || m.Mentions[0].ID != "ou_b" || m.Mentions[0].Name != "Bob" {
		t.Errorf("mentions = %+v", m.Mentions)
	}
	if a := fake.lastAuth(); a != "Bearer tok" {
		t.Errorf("auth header = %q", a)
	}
}

// TestHTTPClient_GetMessageMergeForward pins the merge_forward contract:
// GetMessage(forward_id) returns the sentinel parent followed by the
// bundled child messages, all in one items[] array.
func TestHTTPClient_GetMessageMergeForward(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc(messageGetPrefix, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"code": 0, "msg": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{"message_id": "om_fwd", "msg_type": "merge_forward", "body": map[string]any{"content": `{"content":"Merged and Forwarded Message"}`}},
					map[string]any{"message_id": "c1", "msg_type": "text", "upper_message_id": "om_fwd", "create_time": "1000", "sender": map[string]any{"id": "ou_a", "sender_type": "user"}, "body": map[string]any{"content": `{"text":"one"}`}},
					map[string]any{"message_id": "c2", "msg_type": "text", "upper_message_id": "om_fwd", "create_time": "2000", "sender": map[string]any{"id": "ou_b", "sender_type": "user"}, "body": map[string]any{"content": `{"text":"two"}`}},
				},
			},
		})
	})

	c := newTestClient(fake, time.Now)
	items, err := c.GetMessage(context.Background(), testCreds(), "om_fwd")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (sentinel + 2 children)", len(items))
	}
	if items[0].MessageType != "merge_forward" {
		t.Errorf("items[0] should be the forward sentinel, got %q", items[0].MessageType)
	}
	if items[1].UpperMessageID != "om_fwd" || items[2].UpperMessageID != "om_fwd" {
		t.Errorf("children should link to the forward via upper_message_id")
	}
}

// TestHTTPClient_GetMessageErrorCode maps a Lark business error (e.g.
// deleted / not visible) to a Go error so the enricher can degrade.
func TestHTTPClient_GetMessageErrorCode(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc(messageGetPrefix, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 230110, "msg": "message has been deleted"})
	})
	c := newTestClient(fake, time.Now)
	if _, err := c.GetMessage(context.Background(), testCreds(), "om_gone"); err == nil {
		t.Fatal("expected error for non-zero Lark code")
	}
}

func TestHTTPClient_GetMessageEmptyID(t *testing.T) {
	fake := newLarkFake(t)
	c := newTestClient(fake, time.Now)
	if _, err := c.GetMessage(context.Background(), testCreds(), ""); err == nil {
		t.Fatal("expected error for empty message id")
	}
}
