package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	wsUUID       = "12345678-1234-1234-1234-123456789012"
	issueUUID    = "825e846a-156f-47d3-82b0-a99a6d070b7d"
	assigneeUUID = "99999999-9999-9999-9999-999999999999"
)

// mockQueries satisfies outboundQueries for testing.
type mockQueries struct {
	settings []byte // workspace.settings JSONB
	issueErr error  // when set, GetIssue fails (title lookup tolerance)
}

func (m *mockQueries) GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{Settings: m.settings}, nil
}

func (m *mockQueries) GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error) {
	if m.issueErr != nil {
		return db.Issue{}, m.issueErr
	}
	return db.Issue{Title: "Loaded title"}, nil
}

func sp(s string) *string { return &s }

// capture records everything the receiver got, so tests can assert on the
// delivered payload contract (the one externally visible surface of this
// package), not just that a POST happened.
type capture struct {
	body   []byte
	header http.Header
	calls  int
}

func newCaptureServer() (*httptest.Server, *capture) {
	c := &capture{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.calls++
		c.body, _ = io.ReadAll(r.Body)
		c.header = r.Header.Clone()
		w.WriteHeader(200)
	}))
	return ts, c
}

func newOutbound(settings []byte, issueErr error) *Outbound {
	return NewOutbound(&mockQueries{settings: settings, issueErr: issueErr}, nil)
}

func settingsFor(url string) []byte {
	b, _ := json.Marshal(map[string]string{"webhook_url": url})
	return b
}

func decode(t *testing.T, c *capture) webhookPayload {
	t.Helper()
	var p webhookPayload
	if err := json.Unmarshal(c.body, &p); err != nil {
		t.Fatalf("unmarshal delivered payload: %v", err)
	}
	return p
}

func TestIssueUpdated_AssigneeChanged_FiresWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		ActorType:   "member",
		ActorID:     "user-1",
		Payload: map[string]any{
			// Struct dialect — the form the HTTP update paths publish.
			"issue": handler.IssueResponse{
				ID:           issueUUID,
				Title:        "Test issue",
				Status:       "todo",
				AssigneeType: sp("member"),
				AssigneeID:   sp(assigneeUUID),
			},
			"assignee_changed": true,
			"status_changed":   false,
		},
	})

	if c.calls != 1 {
		t.Fatalf("expected 1 webhook POST, got %d", c.calls)
	}
	p := decode(t, c)
	if p.Event != "issue_assigned" {
		t.Errorf("event: want issue_assigned, got %s", p.Event)
	}
	if p.IssueID != issueUUID {
		t.Errorf("issue_id: want %s, got %s", issueUUID, p.IssueID)
	}
	if p.IssueTitle != "Test issue" {
		t.Errorf("issue_title: want %q, got %q", "Test issue", p.IssueTitle)
	}
	if p.ActorType != "member" || p.ActorID != "user-1" {
		t.Errorf("actor: want member/user-1, got %s/%s", p.ActorType, p.ActorID)
	}
	if p.WorkspaceID != wsUUID {
		t.Errorf("workspace_id: want %s, got %s", wsUUID, p.WorkspaceID)
	}
	if p.Details["assignee_id"] != assigneeUUID {
		t.Errorf("details.assignee_id: want %s, got %s", assigneeUUID, p.Details["assignee_id"])
	}
	if got := c.header.Get("X-Multica-Event"); got != "issue_assigned" {
		t.Errorf("X-Multica-Event: want issue_assigned, got %s", got)
	}
}

func TestIssueUpdated_StatusToInReview_FiresWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		ActorType:   "agent",
		ActorID:     "agent-1",
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:     issueUUID,
				Title:  "Fix bug",
				Status: "in_review",
			},
			"assignee_changed": false,
			"status_changed":   true,
		},
	})

	if c.calls != 1 {
		t.Fatalf("expected 1 webhook POST, got %d", c.calls)
	}
	if p := decode(t, c); p.Event != "issue_in_review" {
		t.Errorf("event: want issue_in_review, got %s", p.Event)
	}
}

func TestIssueUpdated_Unassigned_SkipsWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	// assignee_changed with no new assignee: the notification system files
	// unassignment at info severity, so it must not reach the webhook.
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"issue":            handler.IssueResponse{ID: issueUUID, Title: "T", Status: "todo"},
			"assignee_changed": true,
		},
	})

	if c.calls != 0 {
		t.Errorf("expected no POST on unassignment, got %d", c.calls)
	}
}

func TestIssueUpdated_SquadAssignee_SkipsWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueUUID,
				Title:        "T",
				Status:       "todo",
				AssigneeType: sp("squad"),
				AssigneeID:   sp(assigneeUUID),
			},
			"assignee_changed": true,
		},
	})

	if c.calls != 0 {
		t.Errorf("expected no POST for squad assignee, got %d", c.calls)
	}
}

func TestIssueUpdated_StatusToDone_SkipsWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"issue":          handler.IssueResponse{ID: issueUUID, Title: "T", Status: "done"},
			"status_changed": true,
		},
	})

	if c.calls != 0 {
		t.Errorf("expected no POST for non-in_review status, got %d", c.calls)
	}
}

func TestIssueUpdated_MapPayload_Ignored(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	// Map dialect — only background resets (broadcastIssueUpdated) publish
	// this form, and the notification listeners deliberately skip it. Even an
	// in_review transition in map form must not fire the webhook.
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"issue":          map[string]any{"id": issueUUID, "title": "T", "status": "in_review"},
			"status_changed": true,
			"prev_status":    "in_progress",
		},
	})

	if c.calls != 0 {
		t.Errorf("expected no POST for map-dialect payload, got %d", c.calls)
	}
}

func TestTaskFailed_TerminalOnly_FiresWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleTaskFailed(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"agent_id":       "agent-1",
			"issue_id":       issueUUID,
			"failure_reason": "timeout",
			"retry_pending":  false,
		},
	})

	if c.calls != 1 {
		t.Fatalf("expected 1 webhook POST, got %d", c.calls)
	}
	p := decode(t, c)
	if p.Event != "task_failed" {
		t.Errorf("event: want task_failed, got %s", p.Event)
	}
	if p.IssueTitle != "Loaded title" {
		t.Errorf("issue_title: want %q (from GetIssue), got %q", "Loaded title", p.IssueTitle)
	}
	if p.ActorType != "agent" || p.ActorID != "agent-1" {
		t.Errorf("actor: want agent/agent-1, got %s/%s", p.ActorType, p.ActorID)
	}
	if p.Details["failure_reason"] != "timeout" {
		t.Errorf("details.failure_reason: want timeout, got %s", p.Details["failure_reason"])
	}
}

func TestTaskFailed_RetryPending_SkipsWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleTaskFailed(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"agent_id":      "agent-1",
			"issue_id":      issueUUID,
			"retry_pending": true,
		},
	})

	if c.calls != 0 {
		t.Error("expected no webhook POST for retry_pending task")
	}
}

func TestTaskFailed_NoIssueID_SkipsWebhook(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)
	o.handleTaskFailed(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"agent_id": "agent-1",
		},
	})

	if c.calls != 0 {
		t.Error("expected no webhook POST when issue_id is absent")
	}
}

func TestTaskFailed_GetIssueError_StillFires(t *testing.T) {
	ts, c := newCaptureServer()
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), context.DeadlineExceeded)
	o.handleTaskFailed(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"agent_id": "agent-1",
			"issue_id": issueUUID,
		},
	})

	if c.calls != 1 {
		t.Fatalf("expected delivery despite GetIssue failure, got %d calls", c.calls)
	}
	if p := decode(t, c); p.IssueTitle != "" {
		t.Errorf("issue_title: want empty on lookup failure, got %q", p.IssueTitle)
	}
}

func TestNoWebhookConfigured_SilentSkip(t *testing.T) {
	o := newOutbound([]byte(`{}`), nil)

	// Must neither panic nor block when no URL is configured.
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"issue":            handler.IssueResponse{ID: issueUUID, Title: "T", Status: "in_review"},
			"status_changed":   true,
			"assignee_changed": true,
		},
	})
}

func TestServerError_NonBlocking(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	o := newOutbound(settingsFor(ts.URL), nil)

	// Best-effort: a 500 from the receiver must not panic or bubble up.
	o.handleIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsUUID,
		Payload: map[string]any{
			"issue":          handler.IssueResponse{ID: issueUUID, Title: "T", Status: "in_review"},
			"status_changed": true,
		},
	})
}
