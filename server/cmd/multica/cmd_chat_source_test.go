package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestChatThreadJSONPassesSourceMetadata(t *testing.T) {
	want := map[string]any{"channel_type": "slack", "channel_id": "C1", "channel_name": "engineering", "permalink": "https://example.slack.com/archives/C1/p100000000", "thread_id": "100.000000", "messages": []any{}, "next_cursor": "99.000000"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/chat/thread" || q.Get("id") != "100.000000" || q.Get("before") != "101.000000" || q.Get("limit") != "5" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	cmd := &cobra.Command{Use: "thread"}
	cmd.Flags().Int("limit", 5, "")
	cmd.Flags().String("before", "101.000000", "")
	cmd.Flags().String("output", "json", "")
	out, err := captureStdout(t, func() error { return runChatThread(cmd, []string{"100.000000"}) })
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"channel_type", "channel_id", "channel_name", "permalink", "thread_id", "next_cursor"} {
		if got[key] != want[key] {
			t.Errorf("%s = %v, want %v", key, got[key], want[key])
		}
	}
}
