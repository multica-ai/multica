package handler

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Observe the columns PostgreSQL actually sends, not just the SQL's spelling.
type agentAccessProjectionDB struct {
	db.DBTX
	t           *testing.T
	agentReads  int
	targetReads int
}

func (d *agentAccessProjectionDB) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	rows, err := d.DBTX.Query(ctx, query, args...)
	if err != nil {
		return rows, err
	}
	if strings.Contains(query, "FROM agent_invocation_target") {
		d.targetReads++
	}
	if strings.Contains(query, "FROM agent\n") {
		d.agentReads++
		allowed := map[string]bool{"id": true, "owner_id": true, "permission_mode": true, "kind": true}
		for _, field := range rows.FieldDescriptions() {
			if !allowed[field.Name] {
				d.t.Errorf("visibility lookup loads unnecessary agent column %q", field.Name)
				break
			}
		}
	}
	return rows, nil
}

func TestAgentVisibilityUsesAccessProjection(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	workspaceID := dbfx.Workspace(t, "Access projection", "access-projection-"+uuid.NewString())
	memberID := dbfx.User(t, "Access viewer", uuid.NewString()+"@multica.test")
	fx := testutil.New(testPool, workspaceID, testUserID)
	owned := fx.Agent(t, "Owned", "", testutil.Cols{"owner_id": memberID})
	private := fx.Agent(t, "Private", "")
	shared := fx.Agent(t, "Shared", "", testutil.Cols{"permission_mode": "public_to"})
	archived := fx.Agent(t, "Archived", "", testutil.Cols{"owner_id": memberID, "archived_at": time.Now()})
	carrier := fx.Agent(t, "System carrier", "", testutil.Cols{"kind": "system", "system_key": "agent_builder:" + uuid.NewString()})
	fx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
		"agent_id": shared, "target_type": "workspace", "target_id": workspaceID,
	}, "agent_id = $1", shared)
	// A different workspace must never contribute to any visibility set.
	dbfx.Agent(t, "Other workspace", "")
	ctx := context.Background()
	set := func(ids ...string) map[string]struct{} {
		out := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			out[id] = struct{}{}
		}
		return out
	}
	for _, actor := range []struct {
		name, actorType, role string
		privileged            bool
	}{
		{"member", "member", "member", false},
		{"owner", "member", "owner", true},
		{"admin", "member", "admin", true},
		{"agent", "agent", "", true},
	} {
		for _, surface := range []string{"accessible", "restricted", "dingtalk"} {
			if surface == "dingtalk" && actor.actorType == "agent" {
				continue // Settings inventory is a member-only surface.
			}
			t.Run(actor.name+"/"+surface, func(t *testing.T) {
				spy := &agentAccessProjectionDB{DBTX: testPool, t: t}
				h := *testHandler
				h.Queries = db.New(spy)
				want := set(owned, shared, archived)
				if actor.privileged {
					want[private] = struct{}{}
				}
				var got map[string]struct{}
				var ok bool
				switch surface {
				case "accessible":
					got, ok = h.accessibleAgentIDs(ctx, workspaceID, actor.actorType, memberID, actor.role)
				case "restricted":
					want = set(carrier)
					if !actor.privileged {
						want[private] = struct{}{}
					}
					got, ok = h.restrictedAgentIDs(ctx, workspaceID, actor.actorType, memberID, actor.role)
				case "dingtalk":
					var available map[string]struct{}
					available, got, ok = h.dingtalkAgentVisibility(ctx, parseUUID(workspaceID), memberID, db.Member{Role: actor.role})
					if !reflect.DeepEqual(available, set(owned, private, shared, archived)) {
						t.Errorf("available agents = %v", available)
					}
				}
				if !ok || !reflect.DeepEqual(got, want) {
					t.Errorf("visibility = %v, ok=%v; want %v", got, ok, want)
				}
				if spy.agentReads != 1 {
					t.Errorf("agent reads = %d; want one batch", spy.agentReads)
				}
				wantTargets := 1
				if actor.privileged {
					wantTargets = 0
				}
				if spy.targetReads != wantTargets {
					t.Errorf("invocation-target reads = %d; want %d", spy.targetReads, wantTargets)
				}
			})
		}
	}
}

func BenchmarkAgentAccessProjection(b *testing.B) {
	if testHandler == nil {
		b.Fatal("database-backed handler fixture is required")
	}
	workspaceID := dbfx.Workspace(b, "Access benchmark", "access-benchmark-"+uuid.NewString())
	fx := testutil.New(testPool, workspaceID, testUserID)
	for i := 0; i < 40; i++ {
		fx.Agent(b, uuid.NewString(), "", testutil.Cols{
			"instructions": strings.Repeat("instruction ", 1200),
			"mcp_config":   `{"example":"` + strings.Repeat("x", 5000) + `"}`,
		})
	}
	ctx := context.Background()
	wsUUID := parseUUID(workspaceID)
	b.Run("full_agent", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := testHandler.Queries.ListAllAgents(ctx, wsUUID); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("access_projection", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := testHandler.Queries.ListAgentAccessRows(ctx, wsUUID); err != nil {
				b.Fatal(err)
			}
		}
	})
}
