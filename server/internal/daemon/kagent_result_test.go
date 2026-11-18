package daemon

import (
	"github.com/multica-ai/multica/server/pkg/agent"
	"testing"
)

func TestKagentResultTimeoutDisposition(t *testing.T) {
	cases := []struct {
		in                      agent.Result
		status, reason, comment string
	}{
		{agent.Result{Status: "timeout", Error: "context deadline exceeded", SessionID: "session"}, "blocked", "timeout", "context deadline exceeded"},
		{agent.Result{Status: "cleanup_unconfirmed", Error: "remote cleanup unconfirmed", SessionID: "session"}, "failed", "cleanup_unconfirmed", "remote cleanup unconfirmed"},
		{agent.Result{Status: "cancelled", SessionID: "session"}, "cancelled", "", ""},
		{agent.Result{Status: "completed", Output: "done", SessionID: "session"}, "completed", "", "done"},
	}
	for _, tc := range cases {
		got := kagentTaskResult(tc.in)
		if got.Status != tc.status || got.FailureReason != tc.reason || got.Comment != tc.comment || got.SessionID != "session" {
			t.Errorf("%s => %+v", tc.in.Status, got)
		}
	}
}
