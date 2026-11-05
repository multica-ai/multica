package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Completing a run is not completing an issue; content stays limited to status.
func TestIssueNoticeText(t *testing.T) {
	for _, kind := range []string{"completed", "blocked", "failed"} {
		text := issueNoticeText("Assistant", "TEAM-42", `<at user_id="all">Title</at>`, kind, "https://example.invalid/team/issues/id")
		if !strings.Contains(text, "TEAM-42") || !strings.Contains(text, "https://example.invalid/team/issues/id") || strings.Contains(text, `<at user_id="all">`) {
			t.Fatalf("unsafe or incomplete notification: %s", text)
		}
		if kind == "failed" && strings.Contains(text, "completed") {
			t.Fatal("failed run reported completion")
		}
	}
}

// Ambiguous sends must reuse the event UUID across retries or process restarts.
func TestHTTPClientSendTextIdempotencyKey(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			io.WriteString(w, `{"code":0,"tenant_access_token":"test","expire":7200}`)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		key, ok := body["uuid"].(string)
		if !ok || key == "" {
			t.Error("missing stable message UUID")
		}
		keys = append(keys, key)
		io.WriteString(w, `{"code":0,"data":{"message_id":"om_notification"}}`)
	}))
	defer srv.Close()
	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	for i := 0; i < 2; i++ {
		id, err := client.SendTextMessage(context.Background(), SendTextParams{
			InstallationID: InstallationCredentials{AppID: "cli_test", AppSecret: "secret"},
			ChatID:         "oc_original", Text: "Issue completed", UUID: "8a0151ea-210a-4d9d-87ce-eb030c340119",
		})
		if err != nil || id != "om_notification" {
			t.Fatalf("receipt=%q err=%v", id, err)
		}
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("keys=%v", keys)
	}
}
