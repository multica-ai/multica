package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// govFixture is a workspace with an owner, one runtime, one agent that leads a
// squad, and that squad. The governance tests build on it.
type govFixture struct {
	*testutil.Fixture
	pool   *pgxpool.Pool
	q      *db.Queries
	ws     pgtype.UUID
	wsText string
	owner  string
	agent  string
	squad  string
	// now is the clock the tests pin: 06:00 UTC on 3 November 2026. Yesterday is
	// the window of the built-in metric run.
	now time.Time
}

var govSeq = int64(0)

func newGovFixture(t *testing.T) govFixture {
	t.Helper()
	pool := sharedTestPool(t)
	govSeq++
	n := govSeq + time.Now().UnixNano()
	base := testutil.New(pool, "", "")
	owner := base.User(t, "gov owner", fmt.Sprintf("gov-owner-%d@multica.test", n))
	ws := base.Workspace(t, "gov ws", fmt.Sprintf("gov-ws-%d", n))
	base.Member(t, ws, owner, "owner")
	fx := testutil.New(pool, ws, owner)
	rt := fx.Runtime(t, "gov runtime")
	agent := fx.Agent(t, "gov leader", rt)
	squad := fx.Squad(t, "gov squad", agent)
	return govFixture{
		Fixture: fx, pool: pool, q: db.New(pool), ws: util.MustParseUUID(ws), wsText: ws,
		owner: owner, agent: agent, squad: squad,
		now: time.Date(2026, time.November, 3, 6, 0, 0, 0, time.UTC),
	}
}

// memberActor is the boss acting in these tests.
func (f govFixture) memberActor() GoalActor {
	return GoalActor{Type: ReadingActorMember, ID: util.MustParseUUID(f.owner)}
}

// metricsService is the metric service over the fixture's database.
func (f govFixture) metricsService() *MetricService {
	return NewMetricService(f.q, f.pool)
}

// goalService is the goal service over the fixture's database, on the pinned clock.
func (f govFixture) goalService() *GoalService {
	g := NewGoalService(f.q, f.pool)
	g.Now = func() time.Time { return f.now }
	return g
}

// readingsOf reads the system readings of one scope, keyed by metric.
func (f govFixture) readingsOf(t *testing.T, scopeType, scopeID string) map[string]float64 {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT metric_key, value::float8 FROM metric_reading
		WHERE workspace_id = $1 AND scope_type = $2 AND scope_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
		  AND actor_type = 'system'`, f.wsText, scopeType, scopeID)
	if err != nil {
		t.Fatalf("read readings: %v", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var key string
		var v float64
		if err := rows.Scan(&key, &v); err != nil {
			t.Fatalf("scan reading: %v", err)
		}
		out[key] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate readings: %v", err)
	}
	return out
}

// pipelineStagesJSON is one stage with a 30 day SLA, enough for the stall rule.
func pipelineStagesJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal([]map[string]any{{"key": "define", "name": "Define", "sla_days": 30}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
