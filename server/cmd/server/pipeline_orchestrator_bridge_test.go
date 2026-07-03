package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestPipelineBridgeIssueStatusAndCommentHandlers(t *testing.T) {
	bridge, recorder := newTestPipelineBridge(t)
	bridge.loadIssueContext = testIssueContextLoader(map[string]pipelineIssueContext{
		"issue-1": deliveryContext("issue-1"),
	})

	bridge.onIssueUpdated(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "workspace-1",
		ActorType:   "agent",
		ActorID:     "agent-1",
		Payload: map[string]any{
			"status_changed": true,
			"prev_status":    "in_progress",
			"issue": handler.IssueResponse{
				ID:          "issue-1",
				WorkspaceID: "workspace-1",
				Status:      "done",
				UpdatedAt:   "2026-06-08T00:00:00Z",
			},
		},
	})
	bridge.onCommentCreated(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: "workspace-1",
		ActorType:   "member",
		ActorID:     "member-1",
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:       "comment-1",
				IssueID:  "issue-1",
				Content:  "Pass\n\nship it",
				Type:     "comment",
				ParentID: nil,
			},
		},
	})

	recorder.expectTypes(t, pipelineEventIssueStatusChanged, pipelineEventIssueComment)
	if got := recorder.payloads[0].IssueID; got != "issue-1" {
		t.Fatalf("status issue_id = %q, want issue-1", got)
	}
	if got := recorder.payloads[1].CommentID; got != "comment-1" {
		t.Fatalf("comment_id = %q, want comment-1", got)
	}
}

func TestPipelineBridgeCommentFilteringAndHumanWait(t *testing.T) {
	bridge, recorder := newTestPipelineBridge(t)
	bridge.cfg.OrchestratorActorID = "orchestrator-agent"
	bridge.loadIssueContext = testIssueContextLoader(map[string]pipelineIssueContext{
		"delivery": deliveryContext("delivery"),
		"waiting":  waitingHumanContext("waiting"),
	})

	bridge.onCommentCreated(commentEvent("delivery", "system-comment", "system", "", "Pass"))
	bridge.onCommentCreated(commentEvent("delivery", "orchestrator-comment", "agent", "orchestrator-agent", "Pass"))
	bridge.onCommentCreated(commentEvent("waiting", "agent-waiting-comment", "agent", "agent-1", "Pass"))
	bridge.onCommentCreated(commentEvent("waiting", "owner-accept-comment", "member", "member-1", "验收通过"))

	recorder.expectTypes(t, pipelineEventIssueComment)
	if got := recorder.payloads[0].CommentID; got != "owner-accept-comment" {
		t.Fatalf("delivered comment_id = %q, want owner-accept-comment", got)
	}
}

func TestPipelineBridgeSkipsDiscussionAndNoDevIssues(t *testing.T) {
	bridge, recorder := newTestPipelineBridge(t)
	bridge.loadIssueContext = testIssueContextLoader(map[string]pipelineIssueContext{
		"discussion": {
			Issue:    testIssue("discussion"),
			Metadata: map[string]any{"workflow": "discussion", "pipeline_status": "discussion"},
		},
		"no-dev": {
			Issue:    testIssue("no-dev"),
			Metadata: map[string]any{"workflow": "delivery", "no_dev": true},
		},
	})

	bridge.onIssueUpdated(issueStatusEvent("discussion"))
	bridge.onCommentCreated(commentEvent("no-dev", "comment-1", "member", "member-1", "Pass"))

	recorder.expectTypes(t)
}

func TestPipelineBridgePullRequestAndCheckHandlers(t *testing.T) {
	bridge, recorder := newTestPipelineBridge(t)
	bridge.loadIssueContext = testIssueContextLoader(map[string]pipelineIssueContext{
		"issue-1": deliveryContext("issue-1"),
	})
	bridge.listPullRequestsByIssue = func(context.Context, pgtype.UUID) ([]db.ListPullRequestsByIssueRow, error) {
		return []db.ListPullRequestsByIssueRow{
			{
				ID:           uuidForTest("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				PrNumber:     42,
				ChecksTotal:  3,
				ChecksPassed: 3,
			},
		}, nil
	}

	mergeableState := "dirty"
	bridge.onPullRequestUpdated(events.Event{
		Type:        protocol.EventPullRequestUpdated,
		WorkspaceID: "workspace-1",
		ActorType:   "system",
		Payload: map[string]any{
			"linked_issue_ids": []string{"issue-1"},
			"pull_request": handler.GitHubPullRequestResponse{
				ID:             "pr-1",
				WorkspaceID:    "workspace-1",
				Number:         41,
				State:          "open",
				MergeableState: &mergeableState,
			},
		},
	})
	bridge.onPullRequestUpdated(events.Event{
		Type:        protocol.EventPullRequestUpdated,
		WorkspaceID: "workspace-1",
		ActorType:   "system",
		Payload: map[string]any{
			"linked_issue_ids": []any{"issue-1"},
		},
	})

	recorder.expectTypes(t, pipelineEventPRMergeConflict, pipelineEventPRChecksPassed)
	if got := recorder.payloads[0].PRNumber; got != 41 {
		t.Fatalf("merge-conflict pr_number = %d, want 41", got)
	}
	if got := recorder.payloads[1].PRNumber; got != 42 {
		t.Fatalf("checks pr_number = %d, want 42", got)
	}
}

func TestPipelineBridgeRunFailedUsesTriggerPayloadWhenRunHasNoIssueID(t *testing.T) {
	bridge, recorder := newTestPipelineBridge(t)
	bridge.loadIssueContext = testIssueContextLoader(map[string]pipelineIssueContext{
		"issue-1": deliveryContext("issue-1"),
	})
	bridge.getAutopilotRun = func(context.Context, pgtype.UUID) (db.AutopilotRun, error) {
		envelope, _ := json.Marshal(map[string]any{
			"event": "webhook.received",
			"eventPayload": map[string]any{
				"event_id":        "comment:comment-1",
				"event_type":      pipelineEventIssueComment,
				"workspace_id":    "workspace-1",
				"project_id":      "project-1",
				"issue_id":        "issue-1",
				"parent_issue_id": "parent-1",
				"comment_id":      "comment-1",
				"actor_type":      "member",
				"actor_id":        "member-1",
				"occurred_at":     "2026-06-08T00:00:00Z",
			},
		})
		return db.AutopilotRun{
			ID:             uuidForTest("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			Status:         "failed",
			TriggerPayload: envelope,
		}, nil
	}

	bridge.onAutopilotRunDone(events.Event{
		Type:        protocol.EventAutopilotRunDone,
		WorkspaceID: "workspace-1",
		ActorType:   "system",
		Payload: map[string]any{
			"status": "failed",
			"run_id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		},
	})

	recorder.expectTypes(t, pipelineEventRunFailed)
	got := recorder.payloads[0]
	if got.IssueID != "issue-1" || got.ParentIssueID != "parent-1" || got.ProjectID != "project-1" {
		t.Fatalf("run failed context = project:%q issue:%q parent:%q, want project-1/issue-1/parent-1", got.ProjectID, got.IssueID, got.ParentIssueID)
	}
}

func TestPipelineBridgeDispatchPostsOnceWithIdempotencyKey(t *testing.T) {
	bridge, recorder := newTestPipelineBridge(t)

	payload := pipelineWebhookPayload{
		EventID:     "issue-status:1",
		EventType:   pipelineEventIssueStatusChanged,
		WorkspaceID: "workspace-1",
		IssueID:     "issue-1",
		ActorType:   "agent",
		ActorID:     "agent-1",
		OccurredAt:  "2026-06-08T00:00:00Z",
	}

	bridge.dispatch(payload)
	bridge.dispatch(payload)

	recorder.expectTypes(t, pipelineEventIssueStatusChanged)
	if got := recorder.keys[0]; got != payload.EventID {
		t.Fatalf("idempotency key = %q, want %q", got, payload.EventID)
	}
}

func TestPipelineCommentKeyword(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		ok      bool
	}{
		{name: "pass", content: "Pass\n\nLGTM", want: "pass", ok: true},
		{name: "request changes", content: " request changes ", want: "request changes", ok: true},
		{name: "blocked", content: "Blocked", want: "blocked", ok: true},
		{name: "acceptance pass", content: "验收通过", want: "验收通过", ok: true},
		{name: "acceptance fail", content: "\n验收不通过\nneeds fix", want: "验收不通过", ok: true},
		{name: "ignored prose", content: "I think this should pass later", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pipelineCommentKeyword(tc.content)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("pipelineCommentKeyword(%q) = (%q, %v), want (%q, %v)", tc.content, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestPipelineIssueContextDeliveryFiltering(t *testing.T) {
	projectID := uuidForTest("11111111-1111-1111-1111-111111111111")
	otherProjectID := uuidForTest("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name     string
		ctx      pipelineIssueContext
		allowed  map[string]bool
		delivery bool
		waiting  bool
	}{
		{
			name: "parent delivery metadata qualifies child",
			ctx: pipelineIssueContext{
				Issue:          db.Issue{ProjectID: projectID},
				Metadata:       map[string]any{},
				ParentMetadata: map[string]any{"pipeline_status": "waiting_review"},
			},
			delivery: true,
		},
		{
			name: "discussion metadata rejected",
			ctx: pipelineIssueContext{
				Issue:    db.Issue{ProjectID: projectID},
				Metadata: map[string]any{"workflow": "discussion", "pipeline_status": "discussion"},
			},
			delivery: false,
		},
		{
			name: "no dev metadata rejected",
			ctx: pipelineIssueContext{
				Issue:    db.Issue{ProjectID: projectID},
				Metadata: map[string]any{"workflow": "delivery", "no_dev": true},
			},
			delivery: false,
		},
		{
			name: "project allow list rejected",
			ctx: pipelineIssueContext{
				Issue:    db.Issue{ProjectID: otherProjectID},
				Metadata: map[string]any{"workflow": "delivery"},
			},
			allowed:  map[string]bool{uuidPtrString(projectID): true},
			delivery: false,
		},
		{
			name: "waiting on human detected from parent",
			ctx: pipelineIssueContext{
				Issue:          db.Issue{ProjectID: projectID},
				Metadata:       map[string]any{"workflow": "delivery"},
				ParentMetadata: map[string]any{"waiting_on": "human"},
			},
			delivery: true,
			waiting:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ctx.isDeliveryIssue(tc.allowed); got != tc.delivery {
				t.Fatalf("isDeliveryIssue = %v, want %v", got, tc.delivery)
			}
			if got := tc.ctx.waitingOnHuman(); got != tc.waiting {
				t.Fatalf("waitingOnHuman = %v, want %v", got, tc.waiting)
			}
		})
	}
}

type pipelineRecorder struct {
	payloads []pipelineWebhookPayload
	keys     []string
}

func newTestPipelineBridge(t *testing.T) (*pipelineOrchestratorBridge, *pipelineRecorder) {
	t.Helper()
	recorder := &pipelineRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.keys = append(recorder.keys, r.Header.Get("Idempotency-Key"))
		var payload pipelineWebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		recorder.payloads = append(recorder.payloads, payload)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	bridge := newPipelineOrchestratorBridge(nil, pipelineBridgeConfig{
		WebhookURL: server.URL,
		Timeout:    time.Second,
	})
	bridge.async = false
	bridge.now = func() time.Time {
		return time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	}
	return bridge, recorder
}

func (r *pipelineRecorder) expectTypes(t *testing.T, want ...string) {
	t.Helper()
	if len(r.payloads) != len(want) {
		t.Fatalf("deliveries = %d, want %d: %+v", len(r.payloads), len(want), r.payloads)
	}
	for i, wantType := range want {
		if got := r.payloads[i].EventType; got != wantType {
			t.Fatalf("payload[%d].event_type = %q, want %q", i, got, wantType)
		}
	}
}

func testIssueContextLoader(contexts map[string]pipelineIssueContext) func(context.Context, string) (pipelineIssueContext, bool) {
	return func(_ context.Context, issueID string) (pipelineIssueContext, bool) {
		ctx, ok := contexts[issueID]
		return ctx, ok
	}
}

func deliveryContext(issueID string) pipelineIssueContext {
	return pipelineIssueContext{
		Issue:          testIssue(issueID),
		Parent:         &db.Issue{ID: uuidForTest("99999999-9999-9999-9999-999999999999"), ProjectID: uuidForTest("11111111-1111-1111-1111-111111111111")},
		Metadata:       map[string]any{"workflow": "delivery"},
		ParentMetadata: map[string]any{"pipeline_status": "waiting_review"},
	}
}

func waitingHumanContext(issueID string) pipelineIssueContext {
	ctx := deliveryContext(issueID)
	ctx.ParentMetadata["waiting_on"] = "human"
	return ctx
}

func testIssue(issueID string) db.Issue {
	return db.Issue{
		ID:            uuidFromLabel(issueID),
		WorkspaceID:   uuidForTest("33333333-3333-3333-3333-333333333333"),
		ProjectID:     uuidForTest("11111111-1111-1111-1111-111111111111"),
		ParentIssueID: uuidForTest("99999999-9999-9999-9999-999999999999"),
	}
}

func issueStatusEvent(issueID string) events.Event {
	return events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "workspace-1",
		ActorType:   "agent",
		ActorID:     "agent-1",
		Payload: map[string]any{
			"status_changed": true,
			"prev_status":    "in_progress",
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: "workspace-1",
				Status:      "done",
				UpdatedAt:   "2026-06-08T00:00:00Z",
			},
		},
	}
}

func commentEvent(issueID, commentID, actorType, actorID, content string) events.Event {
	return events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: "workspace-1",
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:      commentID,
				IssueID: issueID,
				Content: content,
				Type:    "comment",
			},
		},
	}
}

func uuidFromLabel(label string) pgtype.UUID {
	switch label {
	case "issue-1":
		return uuidForTest("44444444-4444-4444-4444-444444444444")
	case "delivery":
		return uuidForTest("55555555-5555-5555-5555-555555555555")
	case "waiting":
		return uuidForTest("66666666-6666-6666-6666-666666666666")
	case "discussion":
		return uuidForTest("77777777-7777-7777-7777-777777777777")
	case "no-dev":
		return uuidForTest("88888888-8888-8888-8888-888888888888")
	default:
		return uuidForTest("44444444-4444-4444-4444-444444444444")
	}
}

func uuidForTest(s string) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		panic(err)
	}
	return id
}
