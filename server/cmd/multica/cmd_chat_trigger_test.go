package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatTriggerCommand(t *testing.T) {
	cmd, args, err := chatCmd.Find([]string{"trigger"})
	if err != nil || cmd == chatCmd || len(args) != 0 {
		t.Fatalf("trigger command missing: %v", err)
	}
	if err := cmd.ParseFlags([]string{"--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.ValidateArgs([]string{"other-task"}); err == nil {
		t.Fatal("must not accept task selector")
	}
	for _, status := range []int{200, 409} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.RequestURI() != "/api/chat/trigger" || r.Header.Get("Authorization") != "Bearer mat_trigger-test" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(status)
				if status == 200 {
					w.Write([]byte(`{"task_id":"own","input":"finance","channel_type":"slack","channel_id":"D1","trigger_message_id":"1.1","thread_id":"1.1"}`))
				} else {
					w.Write([]byte(`{"error":"missing sealed input"}`))
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_trigger-test")
			err := cmd.RunE(cmd, nil)
			if (err != nil) != (status != 200) || calls != 1 {
				t.Fatalf("err=%v calls=%d status=%d", err, calls, status)
			}
		})
	}
}
