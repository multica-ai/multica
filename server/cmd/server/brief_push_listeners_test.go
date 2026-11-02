package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outageNotifier stands in for a chat platform that is down: every send fails
// and the calls are counted.
type outageNotifier struct {
	calls atomic.Int32
}

func (n *outageNotifier) Send(context.Context, channel.Binding, channel.Message) error {
	n.calls.Add(1)
	return errors.New("telegram api: 503 Service Unavailable")
}

// TestBriefPushListener_PublishSucceedsWhenTheChatAppIsDown: the brief:created
// listener only queues rows, so a publish never waits on the platform, and the
// brief stands while the retry job's sends fail.
func TestBriefPushListener_PublishSucceedsWhenTheChatAppIsDown(t *testing.T) {
	fx := workspaceFixture(t)
	queries := db.New(testPool)
	stamp := time.Now().UnixNano()
	ws := fx.Workspace(t, "Brief push outage", "brief-push-outage-"+uuid.NewString())
	owner := fx.User(t, "Owner", fmt.Sprintf("brief-push-owner-%d@example.com", stamp))
	fx.Member(t, ws, owner, "owner")
	binding := fx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": owner,
		"installation_id": uuid.NewString(),
		"channel_type":    "telegram",
		"channel_user_id": "5150",
	})
	fx.Insert(t, "brief_push_preference", testutil.Cols{
		"workspace_id": ws, "member_id": owner, "brief_push": true, "binding_id": binding,
	})
	briefID := fx.Insert(t, "brief", testutil.Cols{
		"workspace_id":         ws,
		"kind":                 "daily",
		"period_start":         time.Now().Add(-24 * time.Hour),
		"period_end":           time.Now(),
		"title":                "Daily brief",
		"body":                 "## Headline\nQuiet day.\n\n## Needs your decision\nNone.\n",
		"proposal_ids":         []pgtype.UUID{},
		"created_by_member_id": owner,
	})
	fx.Cleanup(t, "DELETE FROM brief_delivery WHERE brief_id = $1", briefID)

	push := service.NewBriefPushService(queries, "https://app.example")
	down := &outageNotifier{}
	push.RegisterNotifier("telegram", down)
	bus := events.New()
	registerBriefPushListeners(bus, push)

	// Publish the way the handler does. It returns with nothing sent.
	bus.Publish(events.Event{
		Type: protocol.EventBriefCreated, WorkspaceID: ws, ActorType: "member", ActorID: owner,
		Payload: map[string]any{"brief": handler.BriefResponse{ID: briefID, WorkspaceID: ws, Kind: "daily", Title: "Daily brief"}},
	})
	if down.calls.Load() != 0 {
		t.Fatalf("publish reached the chat app %d times; sends belong to the retry job", down.calls.Load())
	}
	var state string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state FROM brief_delivery WHERE brief_id = $1 AND member_id = $2`, briefID, owner).Scan(&state); err != nil {
		t.Fatalf("read delivery row: %v", err)
	}
	if state != "pending" {
		t.Fatalf("delivery state after publish = %q, want pending", state)
	}

	// The retry job runs during the outage: it records the failure and returns
	// without an error, so the scheduler does not retry the whole run.
	if _, err := push.DeliverDue(context.Background()); err != nil {
		t.Fatalf("DeliverDue returned an error for a platform outage: %v", err)
	}
	if down.calls.Load() != 1 {
		t.Fatalf("chat app calls = %d, want 1", down.calls.Load())
	}
	var attempts int
	var lastError string
	if err := testPool.QueryRow(context.Background(),
		`SELECT attempts, last_error FROM brief_delivery WHERE brief_id = $1 AND member_id = $2`,
		briefID, owner).Scan(&attempts, &lastError); err != nil {
		t.Fatalf("read delivery row: %v", err)
	}
	if attempts != 1 || lastError == "" {
		t.Fatalf("after one failed send: attempts=%d last_error=%q", attempts, lastError)
	}
	var briefCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM brief WHERE id = $1`, briefID).Scan(&briefCount); err != nil {
		t.Fatal(err)
	}
	if briefCount != 1 {
		t.Fatal("the brief was lost during the outage")
	}
}
