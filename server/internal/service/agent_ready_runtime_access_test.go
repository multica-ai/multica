package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The admission half of MUL-6704, and why the teardown is not enough alone. #7571
// made a reclaimed private runtime refuse to CLAIM another owner's agent, but
// admission only asked "bound, and online?" — so every new trigger still enqueued,
// the fence refused it, and the 2h TTL mislabelled it `queued_expired`. The agents
// that hit this are the ones the teardown deliberately leaves bound (system
// carriers), so both halves ship together.
func TestAgentReadinessRuntimeAccess(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name       string
		visibility string
		sameOwner  bool
		wantReady  bool
	}{
		{"public runtime admits a foreign agent", "public", false, true},
		{"private runtime admits its owner's agent", "private", true, true},
		{"reclaimed private runtime blocks a foreign agent", "private", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := newResolveOriginatorPool(t)
			bootstrap := testutil.New(pool, "", "")
			suffix := time.Now().UnixNano()

			runtimeOwnerID := bootstrap.User(t,
				fmt.Sprintf("readiness-runtime-owner-%d", suffix),
				fmt.Sprintf("readiness-runtime-owner-%d@example.com", suffix))
			agentOwnerID := runtimeOwnerID
			if !tt.sameOwner {
				agentOwnerID = bootstrap.User(t,
					fmt.Sprintf("readiness-agent-owner-%d", suffix),
					fmt.Sprintf("readiness-agent-owner-%d@example.com", suffix))
			}
			workspaceID := bootstrap.Workspace(t,
				fmt.Sprintf("readiness-access-%d", suffix),
				fmt.Sprintf("readiness-access-%d", suffix))
			fx := testutil.New(pool, workspaceID, runtimeOwnerID)
			fx.Member(t, workspaceID, runtimeOwnerID, "owner")
			if agentOwnerID != runtimeOwnerID {
				fx.Member(t, workspaceID, agentOwnerID, "member")
			}
			runtimeID := fx.Runtime(t, "readiness-runtime", testutil.Cols{
				"visibility": tt.visibility,
				"owner_id":   runtimeOwnerID,
				"status":     "online",
			})
			agentID := fx.Agent(t, "readiness-agent", runtimeID, testutil.Cols{"owner_id": agentOwnerID})

			q := db.New(pool)
			agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
			if err != nil {
				t.Fatalf("load agent: %v", err)
			}
			verdict, err := AgentReadiness(ctx, q, agent)
			if err != nil {
				t.Fatalf("AgentReadiness: %v", err)
			}
			if verdict.Ready() != tt.wantReady {
				t.Fatalf("Ready() = %v (reason %q), want %v", verdict.Ready(), verdict.Reason, tt.wantReady)
			}
			if tt.wantReady {
				return
			}
			// Blocked, not waitable: waiting for an online machine to permit you is
			// not a plan, so the caller must refuse rather than queue.
			if !verdict.Blocked() {
				t.Fatalf("verdict must be BLOCKED so callers refuse instead of queueing; got %v", verdict.Availability)
			}
			if verdict.Reason != dispatch.ReasonRuntimeAccessRevoked {
				t.Fatalf("reason = %q, want %q", verdict.Reason, dispatch.ReasonRuntimeAccessRevoked)
			}
		})
	}
}
