package daemon

// CEREBRO-PATCH(daemon-trace-upload-channel-label-test): unit test for the
// channel/dm surface classification added for FIR-2438.

import "testing"

// TestTraceSurface locks in the surface classification used as the trace
// origin label, including the channel/dm distinction added 30/5 (Jesper:
// "channel er issues med kind = channel").
func TestTraceSurface(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want string
	}{
		{"chat wins", Task{ChatSessionID: "c1", IssueID: "i1", IssueKind: "channel"}, "chat"},
		{"autopilot wins over issue", Task{AutopilotID: "a1", IssueID: "i1"}, "autopilot"},
		{"channel issue", Task{IssueID: "i1", IssueKind: "channel", TriggerCommentID: "cm1"}, "channel"},
		{"dm issue", Task{IssueID: "i1", IssueKind: "dm"}, "dm"},
		{"ordinary issue with comment", Task{IssueID: "i1", IssueKind: "issue", TriggerCommentID: "cm1"}, "issue"},
		{"ordinary issue empty kind with comment", Task{IssueID: "i1", TriggerCommentID: "cm1"}, "issue"},
		{"direct issue assignment", Task{IssueID: "i1", IssueKind: "issue"}, "issue_direct"},
		{"nothing", Task{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := traceSurface(tc.task); got != tc.want {
				t.Fatalf("traceSurface(%+v) = %q, want %q", tc.task, got, tc.want)
			}
		})
	}
}
