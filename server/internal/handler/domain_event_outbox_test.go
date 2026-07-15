package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/domainevent"
)

type outboxRow struct {
	Type           string
	Payload        []byte
	ID             string
	CorrelationID  string
	DispatchStatus string
	HopCount       int32
}

// eventsForSubject returns every domain_event about a specific subject, ordered
// by seq. Scoping by subject_id (not the shared test workspace) keeps the read
// isolated from other tests running against the same DB.
func eventsForSubject(t *testing.T, subjectType, subjectID string) []outboxRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT type, payload, id::text, correlation_id::text, dispatch_status, hop_count
		   FROM domain_event
		  WHERE subject_type = $1 AND subject_id = $2
		  ORDER BY seq`, subjectType, subjectID)
	if err != nil {
		t.Fatalf("query domain_event: %v", err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.Type, &r.Payload, &r.ID, &r.CorrelationID, &r.DispatchStatus, &r.HopCount); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func payloadField(t *testing.T, raw []byte, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	s, _ := m[key].(string)
	return s
}

// End-to-end: driving the real HTTP handlers must persist the transactional
// outbox events (MUL-4332). Proves issue.created / issue.status_changed /
// issue.assigned are written atomically by the create + update paths, with the
// root-event invariants (pending, hop 0, correlation = id) intact.
func TestOutboxEmittedByIssueHandlers(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database connection")
	}
	issueID := createTestIssue(t, "outbox e2e "+t.Name(), "todo", "none")
	t.Cleanup(func() {
		deleteTestIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, issueID)
	})

	// 1) create → exactly one issue.created, a root event.
	created := eventsForSubject(t, domainevent.SubjectIssue, issueID)
	if len(created) != 1 {
		t.Fatalf("expected 1 event after create, got %d (%+v)", len(created), created)
	}
	ev := created[0]
	if ev.Type != domainevent.TypeIssueCreated {
		t.Errorf("type = %q, want %q", ev.Type, domainevent.TypeIssueCreated)
	}
	if ev.DispatchStatus != domainevent.DispatchPending {
		t.Errorf("dispatch_status = %q, want pending", ev.DispatchStatus)
	}
	if ev.HopCount != 0 {
		t.Errorf("hop_count = %d, want 0", ev.HopCount)
	}
	if ev.CorrelationID != ev.ID {
		t.Errorf("root correlation_id (%s) must equal id (%s)", ev.CorrelationID, ev.ID)
	}
	if got := payloadField(t, ev.Payload, "status"); got != "todo" {
		t.Errorf("issue.created payload status = %q, want todo", got)
	}

	// 2) update status + assignee in one call → status_changed + assigned.
	uw := httptest.NewRecorder()
	ureq := newRequest("PATCH", "/api/issues/"+issueID, map[string]any{
		"status":        "in_progress",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	ureq = withURLParam(ureq, "id", issueID)
	testHandler.UpdateIssue(uw, ureq)
	if uw.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", uw.Code, uw.Body.String())
	}

	all := eventsForSubject(t, domainevent.SubjectIssue, issueID)
	var sawStatus, sawAssigned bool
	for _, e := range all {
		switch e.Type {
		case domainevent.TypeIssueStatusChanged:
			sawStatus = true
			if from := payloadField(t, e.Payload, "from"); from != "todo" {
				t.Errorf("status_changed from = %q, want todo", from)
			}
			if to := payloadField(t, e.Payload, "to"); to != "in_progress" {
				t.Errorf("status_changed to = %q, want in_progress", to)
			}
		case domainevent.TypeIssueAssigned:
			sawAssigned = true
			if to := payloadField(t, e.Payload, "to_assignee_id"); to != testUserID {
				t.Errorf("assigned to_assignee_id = %q, want %q", to, testUserID)
			}
		}
	}
	if !sawStatus {
		t.Errorf("expected an issue.status_changed event, got %+v", all)
	}
	if !sawAssigned {
		t.Errorf("expected an issue.assigned event, got %+v", all)
	}
}
