package lark

import (
	"context"
	"net/http"
	"reflect"
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
		if r.URL.Query().Has("user_id_type") {
			t.Errorf("user_id_type must be omitted: %q", r.URL.RawQuery)
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
						"mentions": []any{
							map[string]any{"key": "@_user_1", "id": "ou_b", "id_type": "open_id", "name": "Same name"},
							map[string]any{"key": "@_user_2", "id": "ou_bot", "id_type": "open_id", "name": "Same name"},
						},
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
	wantMentions := []LarkMessageMention{
		{Key: "@_user_1", ID: "ou_b", Name: "Same name"},
		{Key: "@_user_2", ID: "ou_bot", Name: "Same name"},
	}
	if !reflect.DeepEqual(m.Mentions, wantMentions) {
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

func TestHTTPClient_DefaultMentionIDsInSelectedContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  InboundMessage
		want string
	}{
		{"quoted message", InboundMessage{MessageType: "text", MessageID: "om_trigger", ParentID: "om_parent", Body: "/issue help", CommandBody: "/issue help"}, `<quoted_message message_id="om_parent"`},
		{"forwarded messages", InboundMessage{MessageType: "merge_forward", MessageID: "om_forward", CommandBody: "forwarded message"}, `<forwarded_messages count="2">`},
		{"quoted forward", InboundMessage{MessageType: "text", MessageID: "om_trigger", ParentID: "om_forward", Body: "/issue help", CommandBody: "/issue help"}, `<quoted_message message_id="om_forward"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newLarkFake(t)
			fake.stubToken("tok", 7200)
			fake.mux.HandleFunc(messageGetPrefix, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("user_id_type") {
					t.Errorf("context request must use default mention IDs: %q", r.URL.RawQuery)
				}
				parent := map[string]any{
					"message_id": "om_parent", "msg_type": "text", "create_time": "1000",
					"sender": map[string]any{"id": "ou_author", "id_type": "open_id", "sender_type": "user"},
					"body":   map[string]any{"content": `{"text":"@_user_1 @_user_2 @_user_3 /issue quoted"}`},
					"mentions": []any{
						map[string]any{"key": "@_user_1", "id": "ou_author", "id_type": "open_id", "name": "Alice"},
						map[string]any{"key": "@_user_2", "id": "ou_bot_a", "id_type": "open_id", "name": "Bot A"},
						map[string]any{"key": "@_user_3", "id": "ou_bot_b", "id_type": "open_id", "name": "Bot B"},
					},
				}
				items := []any{parent}
				if strings.HasSuffix(r.URL.Path, "/om_forward") {
					items = []any{
						map[string]any{"message_id": "om_forward", "msg_type": "merge_forward", "sender": map[string]any{"id": "ou_author", "sender_type": "user"}},
						parent,
						map[string]any{"message_id": "om_bot_reply", "msg_type": "text", "create_time": "2000", "sender": map[string]any{"id": "cli_bot", "id_type": "app_id", "sender_type": "app"}, "body": map[string]any{"content": `{"text":"forwarded bot reply"}`}},
					}
				}
				writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"items": items}})
			})
			fake.mux.HandleFunc("/open-apis/contact/v3/users/batch", func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query()["user_ids"]; !reflect.DeepEqual(got, []string{"ou_author"}) {
					t.Errorf("speaker lookup = %v, want only the human author", got)
				}
				if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
					t.Errorf("Contact User still requires user_id_type=open_id, got %q", got)
				}
				writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"items": []any{}}})
			})
			client := newTestClient(fake, time.Now)
			got := NewInboundEnricher(client, InboundEnricherConfig{}).Enrich(context.Background(), tc.msg, testCreds())
			if !strings.Contains(got.Body, tc.want) || !strings.Contains(got.Body, "@Alice @Bot A @Bot B /issue quoted") {
				t.Fatalf("selected context = %q", got.Body)
			}
			if got.CommandBody != tc.msg.CommandBody {
				t.Errorf("trigger command = %q, want %q", got.CommandBody, tc.msg.CommandBody)
			}
			if strings.Contains(tc.name, "forward") && !strings.Contains(got.Body, "forwarded bot reply") {
				t.Errorf("forwarded bot content missing: %q", got.Body)
			}
			if strings.Contains(got.Body, "@_user_") {
				t.Errorf("unresolved mentions: %q", got.Body)
			}
		})
	}
}
