package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/slack-go/slack"
)

func TestThreadSourceOptionalFailuresPreserveHistory(t *testing.T) {
	for _, tc := range []struct {
		name, linkError, nameError string
		linkTimeout, nameTimeout   bool
		emptyName                  bool
		wantLink, wantName         bool
		wantInfoCalls              int32
	}{
		{name: "success", wantLink: true, wantName: true, wantInfoCalls: 1},
		{name: "name missing scope", nameError: "missing_scope", wantLink: true, wantInfoCalls: 1},
		{name: "name private", nameError: "access_denied", wantLink: true, wantInfoCalls: 1},
		{name: "name not found", nameError: "channel_not_found", wantLink: true, wantInfoCalls: 1},
		{name: "unnamed conversation", emptyName: true, wantLink: true, wantInfoCalls: 1},
		{name: "link deleted", linkError: "message_not_found", wantName: true, wantInfoCalls: 1},
		{name: "link private", linkError: "access_denied", wantName: true, wantInfoCalls: 1},
		{name: "link missing scope", linkError: "missing_scope", wantName: true, wantInfoCalls: 1},
		{name: "link rate limited", linkError: "429", wantInfoCalls: 0},
		{name: "link rate limited without header", linkError: "429-no-header", wantInfoCalls: 0},
		{name: "link rate limited invalid header", linkError: "429-invalid-header", wantInfoCalls: 0},
		{name: "link API rate limited", linkError: "ratelimited", wantInfoCalls: 0},
		{name: "link malformed response", linkError: "malformed-json", wantInfoCalls: 0},
		{name: "link server failure", linkError: "http500", wantInfoCalls: 0},
		{name: "link unknown API failure", linkError: "unknown_synthetic_error", wantInfoCalls: 0},
		{name: "link transport failure", linkError: "transport", wantInfoCalls: 0},
		{name: "name rate limited", nameError: "429", wantLink: true, wantInfoCalls: 1},
		{name: "link timeout", linkTimeout: true, wantInfoCalls: 0},
		{name: "name timeout", nameTimeout: true, wantLink: true, wantInfoCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var linkCalls, infoCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				var failure string
				var timeout bool
				switch r.URL.Path {
				case "/conversations.replies":
					w.Write([]byte(`{"ok":true,"messages":[{"ts":"101.000000","text":"retained"}]}`))
					return
				case "/chat.getPermalink":
					linkCalls.Add(1)
					failure, timeout = tc.linkError, tc.linkTimeout
				case "/conversations.info":
					infoCalls.Add(1)
					failure, timeout = tc.nameError, tc.nameTimeout
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					return
				}
				if timeout {
					select {
					case <-r.Context().Done():
					case <-time.After(2 * sourceMetadataTimeout):
					}
					return
				}
				if failure == "transport" {
					// A truncated response body fails after headers, avoiding
					// net/http's automatic retry of a reused idle connection.
					w.Header().Set("Content-Length", "100")
					w.Write([]byte(`{"ok":true`))
					return
				}
				if failure == "malformed-json" {
					w.Write([]byte(`not-json`))
					return
				}
				if failure == "http500" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if strings.HasPrefix(failure, "429") {
					if failure == "429" {
						w.Header().Set("Retry-After", "60")
					}
					if failure == "429-invalid-header" {
						w.Header().Set("Retry-After", "invalid")
					}
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				if failure != "" {
					json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": failure, "response_metadata": map[string]any{"messages": []string{"synthetic-raw-secret"}}})
					return
				}
				if r.URL.Path == "/chat.getPermalink" {
					w.Write([]byte(`{"ok":true,"permalink":"https://example.slack.com/archives/C1/p100000000"}`))
				} else {
					name := "engineering"
					if tc.emptyName {
						name = ""
					}
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": map[string]any{"id": "C1", "name": name}})
				}
			}))
			defer srv.Close()
			h := newTestHistory(&fakeHistoryQueries{binding: groupBinding("100.000000"), inst: activeSlackInstall()}, slack.New("synthetic-bot-token", slack.OptionAPIURL(srv.URL+"/")))
			var logs bytes.Buffer
			h.logger = slog.New(slog.NewTextHandler(&logs, nil))
			start := time.Now()
			page, err := h.Thread(context.Background(), uid(9), "", channel.HistoryOptions{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("metadata exceeded bounded deadline")
			}
			if page.ChannelID != "C1" || page.ThreadID != "100.000000" || len(page.Messages) != 1 || page.Messages[0].Text != "retained" || page.NextCursor != "101.000000" {
				t.Fatalf("history changed: %+v", page)
			}
			if (page.Permalink != "") != tc.wantLink || (page.ChannelName != "") != tc.wantName {
				t.Errorf("optional values: %+v", page)
			}
			t.Logf("link=%d info=%d history_messages=%d channel_id=%s", linkCalls.Load(), infoCalls.Load(), len(page.Messages), page.ChannelID)
			if linkCalls.Load() != 1 || infoCalls.Load() != tc.wantInfoCalls {
				t.Errorf("unexpected calls: link=%d info=%d", linkCalls.Load(), infoCalls.Load())
			}
			encoded, _ := json.Marshal(page)
			for _, secret := range []string{"synthetic-bot-token", "synthetic-raw-secret", "missing_scope", "access_denied", "message_not_found", "ratelimited", "unknown_synthetic_error"} {
				if strings.Contains(string(encoded)+logs.String(), secret) {
					t.Errorf("metadata leaked %q", secret)
				}
			}
		})
	}
}

type sourceHistoryQueries struct {
	fakeHistoryQueries
	t                *testing.T
	wantInstallation pgtype.UUID
	bindingCalls     int
}

func (q *sourceHistoryQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	q.bindingCalls++
	return q.binding, q.bindingErr
}

func (q *sourceHistoryQueries) GetChannelInstallation(_ context.Context, p db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	if p.ID != q.wantInstallation || p.ChannelType != "slack" {
		q.t.Errorf("wrong installation lookup: %+v", p)
	}
	return q.inst, q.instErr
}

type sourceHistoryClient struct {
	fakeHistoryClient
	t                        *testing.T
	wantChannel, wantMessage string
	calls                    int
}

func (c *sourceHistoryClient) GetPermalinkContext(ctx context.Context, p *slack.PermalinkParameters) (string, error) {
	c.calls++
	if _, ok := ctx.Deadline(); !ok {
		c.t.Error("missing source timeout")
	}
	if p.Channel != c.wantChannel || p.Ts != c.wantMessage {
		c.t.Errorf("wrong snapshot target: %+v", p)
	}
	return "https://example.slack.com/archives/" + p.Channel + "/p100000000", nil
}

func (c *sourceHistoryClient) GetConversationInfoContext(_ context.Context, p *slack.GetConversationInfoInput) (*slack.Channel, error) {
	c.calls++
	return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: p.ChannelID}, Name: "engineering"}}, nil
}

func TestTaskSourceUsesImmutableDelivery(t *testing.T) {
	for _, tc := range []struct{ name, chatID, chatType, thread, message, wantMessage string }{
		{"thread reply", "C1:100.000000", "group", "100.000000", "101.000000", "100.000000"},
		{"top level root", "C1:100.000000", "group", "100.000000", "100.000000", "100.000000"},
		{"DM", "D1", "p2p", "", "101.000000", "101.000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channelID := strings.Split(tc.chatID, ":")[0]
			delivery := db.ChannelTaskDelivery{InstallationID: uid(2), ChannelType: "slack", ChannelChatID: tc.chatID, ChatType: tc.chatType, ChannelMessageID: pgtype.Text{String: tc.message, Valid: true}, ChannelThreadID: pgtype.Text{String: tc.thread, Valid: tc.thread != ""}, Config: []byte(`{"channel_id":"` + channelID + `"}`)}
			inst := activeSlackInstall()
			inst.ID = uid(2)
			inst.WorkspaceID = uid(3)
			inst.ChannelType = "slack"
			q := &sourceHistoryQueries{t: t, wantInstallation: uid(2), fakeHistoryQueries: fakeHistoryQueries{inst: inst, binding: groupBinding("999.000000")}}
			client := &sourceHistoryClient{t: t, wantChannel: channelID, wantMessage: tc.wantMessage}
			h := newTestHistory(q, client)
			for retry := 0; retry < 2; retry++ {
				source := h.TaskSource(context.Background(), delivery, uid(3))
				if source == nil || source.ChannelID != channelID || source.ThreadID != tc.thread || source.ChannelName != "engineering" || source.Permalink == "" {
					t.Fatalf("source = %+v", source)
				}
				q.binding = groupBinding("888.000000")
			}
			if q.bindingCalls != 0 || client.calls != 4 {
				t.Fatalf("mutable binding read or wrong calls: %d/%d", q.bindingCalls, client.calls)
			}
		})
	}
}

func TestTaskSourceIsolation(t *testing.T) {
	for _, name := range []string{"empty delivery", "non Slack", "wrong workspace", "wrong installation", "wrong platform", "revoked", "lookup error", "bad credentials"} {
		t.Run(name, func(t *testing.T) {
			inst := activeSlackInstall()
			inst.ID = uid(2)
			inst.WorkspaceID = uid(3)
			inst.ChannelType = "slack"
			d := db.ChannelTaskDelivery{InstallationID: uid(2), ChannelType: "slack", ChannelChatID: "C1", Config: []byte(`{"channel_id":"C1"}`)}
			q := &sourceHistoryQueries{t: t, wantInstallation: uid(2), fakeHistoryQueries: fakeHistoryQueries{inst: inst}}
			switch name {
			case "empty delivery":
				d = db.ChannelTaskDelivery{}
			case "non Slack":
				d.ChannelType = "feishu"
			case "wrong workspace":
				q.inst.WorkspaceID = uid(4)
			case "wrong installation":
				q.inst.ID = uid(4)
			case "wrong platform":
				q.inst.ChannelType = "feishu"
			case "revoked":
				q.inst.Status = "revoked"
			case "lookup error":
				q.instErr = errors.New("private database error")
			case "bad credentials":
				q.inst.Config = []byte(`{`)
			}
			h := newTestHistory(q, &fakeHistoryClient{})
			h.newClient = func(string) historyClient { t.Fatal("unexpected Slack client for rejected installation"); return nil }
			if source := h.TaskSource(context.Background(), d, uid(3)); source != nil {
				t.Fatalf("unexpected source: %+v", source)
			}
		})
	}
}
