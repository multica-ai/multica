package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/slack-go/slack"
)

// Use the real Slack client against a private server to exercise the wire
// contract, without credentials or access to a Slack workspace.
func TestThreadSourceMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, requested, root string
		dm                    bool
	}{
		{"own thread", "", "100.000000", false},
		{"explicit different thread", "200.000000", "200.000000", false},
		{"DM without thread", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channelID := "C1"
			binding := groupBinding("100.000000")
			if tc.dm {
				binding = dmBinding()
				channelID = "D1"
			}
			permalinkCalls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/conversations.replies", "/conversations.history":
					if r.Form.Get("channel") != channelID {
						t.Errorf("channel = %q", r.Form.Get("channel"))
					}
					w.Write([]byte(`{"ok":true,"messages":[{"ts":"201.000000","user":"U1","text":"hello"}]}`))
				case "/users.info":
					w.Write([]byte(`{"ok":true,"user":{"id":"U1","real_name":"Alice"}}`))
				case "/conversations.info":
					w.Write([]byte(`{"ok":true,"channel":{"id":"` + channelID + `","name":"engineering"}}`))
				case "/chat.getPermalink":
					permalinkCalls++
					if r.Form.Get("channel") != channelID || r.Form.Get("message_ts") != tc.root {
						t.Errorf("wrong permalink target: %v", r.Form)
					}
					w.Write([]byte(`{"ok":true,"permalink":"https://example.slack.com/archives/` + channelID + `/p100000000"}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			h := newTestHistory(&fakeHistoryQueries{binding: binding, inst: activeSlackInstall()}, slack.New("synthetic-bot-token", slack.OptionAPIURL(srv.URL+"/")))
			page, err := h.Thread(context.Background(), uid(9), tc.requested, channel.HistoryOptions{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(page)
			var got map[string]any
			json.Unmarshal(b, &got)
			if got["channel_id"] != channelID || got["channel_name"] != "engineering" {
				t.Errorf("source missing: %s", b)
			}
			if len(page.Messages) != 1 || page.Messages[0].Text != "hello" || page.NextCursor != "201.000000" {
				t.Errorf("history changed: %s", b)
			}
			if tc.root == "" {
				if got["thread_id"] != nil || got["permalink"] != nil || permalinkCalls != 0 {
					t.Errorf("fabricated DM root: %s", b)
				}
			} else if got["thread_id"] != tc.root || got["permalink"] == nil || permalinkCalls != 1 {
				t.Errorf("thread source missing: %s", b)
			}
		})
	}
}
