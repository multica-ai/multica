package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newResumeWarningClient returns a client pointed at a server that records every
// resume-warning report and answers with status. The daemon reports this warning
// best effort, so the tests assert both that it is sent exactly once for a
// rejected resume and that nothing about a failure reaches the task.
func newResumeWarningClient(t *testing.T, status int) (*Client, *[]map[string]any) {
	t.Helper()
	reports := &[]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/runtimes/runtime-1/resume-warning" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*reports = append(*reports, body)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL), reports
}

func TestReportResumeWarningReportsOnceForARejectedResume(t *testing.T) {
	client, reports := newResumeWarningClient(t, http.StatusOK)
	d := &Daemon{client: client, logger: quietTaskLog()}

	// What runTask sees after the gates rejected a prior session: the gates clear
	// PriorSessionID and set the finalized flag.
	d.reportResumeWarning(context.Background(), Task{
		ID:                            "task-1",
		RuntimeID:                     "runtime-1",
		PriorSessionResumeUnavailable: true,
	}, quietTaskLog())

	if len(*reports) != 1 {
		t.Fatalf("reports = %d, want exactly one for one task", len(*reports))
	}
	report := (*reports)[0]
	if report["code"] != "prior_session_resume_unavailable" || report["task_id"] != "task-1" {
		t.Fatalf("report body = %#v", report)
	}
}

func TestReportResumeWarningSilentOnHealthyResumeAndColdStart(t *testing.T) {
	client, reports := newResumeWarningClient(t, http.StatusOK)
	d := &Daemon{client: client, logger: quietTaskLog()}

	// Healthy resume: a prior session existed and was reachable.
	d.reportResumeWarning(context.Background(), Task{
		ID: "task-1", RuntimeID: "runtime-1", PriorSessionID: "session-1",
	}, quietTaskLog())
	// Ordinary cold start: nothing was expected.
	d.reportResumeWarning(context.Background(), Task{ID: "task-2", RuntimeID: "runtime-1"}, quietTaskLog())

	if len(*reports) != 0 {
		t.Fatalf("reports = %d, want none for a healthy resume or a cold start", len(*reports))
	}
}

func TestReportResumeWarningFailureIsNonFatal(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound} {
		client, reports := newResumeWarningClient(t, status)
		d := &Daemon{client: client, logger: quietTaskLog()}
		// Neither the server failing nor an older server without the endpoint may
		// panic or propagate: the task continues with the fresh session it already
		// has, and the agent-facing continuity notice is unaffected.
		d.reportResumeWarning(context.Background(), Task{
			ID: "task-1", RuntimeID: "runtime-1", PriorSessionResumeUnavailable: true,
		}, quietTaskLog())
		if len(*reports) != 1 {
			t.Fatalf("status %d: reports = %d, want the attempt to be sent once", status, len(*reports))
		}
	}
}
