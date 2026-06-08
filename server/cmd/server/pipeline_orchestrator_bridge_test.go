package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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

func TestPipelineBridgeDispatchPostsOnceWithIdempotencyKey(t *testing.T) {
	var got []pipelineWebhookPayload
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		var payload pipelineWebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		got = append(got, payload)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	bridge := newPipelineOrchestratorBridge(nil, pipelineBridgeConfig{
		WebhookURL: server.URL,
		Timeout:    time.Second,
	})
	bridge.async = false

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

	if len(got) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(got))
	}
	if got[0].EventID != payload.EventID || keys[0] != payload.EventID {
		t.Fatalf("event/idempotency key mismatch: payload=%q key=%q", got[0].EventID, keys[0])
	}
}

func uuidForTest(s string) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		panic(err)
	}
	return id
}
