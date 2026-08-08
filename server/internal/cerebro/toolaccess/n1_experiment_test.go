package toolaccess

// FIR-3781 controlled experiment. Not part of the normal suite: it needs a real
// Postgres and is skipped unless N1_EXPERIMENT_DATABASE_URL is set.
//
// The 25 July incident put the claim response build at 50-65s against a stable
// ~3s five-day baseline, and drove database query volume from ~2k to ~140k per
// 15s window — with no extra HTTP requests, no slower HTTP requests, and no
// pool contention (24 idle connections, zero acquire wait). Reading the diff
// accounts for ONE extra query per capability (loadInput's 3, plus the
// MemberOverrideEnabled lookup that ResolveDeclared added). That is 1.33x, not
// 65x.
//
// This test settles the gap by counting actual queries instead of inferring
// them: it drives the real toolpolicy.Store against a real database with a
// realistic capability count and reports queries-per-capability. Run it on the
// production ref and on main and compare the two numbers.
//
//	createdb multica_n1_experiment
//	go run ./cmd/migrate up   # with DATABASE_URL pointed at it
//	N1_EXPERIMENT_DATABASE_URL=postgres://... go test ./internal/cerebro/toolaccess/ \
//	    -run TestN1QueryCountPerCapability -v

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// capabilityCount mirrors what production actually resolves for one agent:
// `multica agent capabilities <lone>` reports 442 tools on the workspace that
// stalled. Override with N1_EXPERIMENT_N to measure how cost scales: linear
// growth points at per-capability work, quadratic at a loop inside a loop.
func capabilityCount() int {
	if raw := os.Getenv("N1_EXPERIMENT_N"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 442
}

// queryCounter is a pgx QueryTracer that counts every statement the pool runs.
// Counting at the driver seam catches queries no matter how deep in the policy
// chain they are issued, which is the whole point — the diff review missed the
// multiplier precisely because it looked at call sites.
type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestN1QueryCountPerCapability(t *testing.T) {
	dsn := os.Getenv("N1_EXPERIMENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("set N1_EXPERIMENT_DATABASE_URL to run the controlled N+1 experiment")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	n := capabilityCount()
	views := make([]capabilityregistry.View, 0, n)
	for i := range n {
		views = append(views, capabilityregistry.View{
			Key:    fmt.Sprintf("mcp__probe__tool_%03d", i),
			Title:  fmt.Sprintf("Probe tool %03d", i),
			Source: "scan",
		})
	}

	service := New(capabilityListStub{views: views}, toolpolicy.NewStore(pool))

	q := Query{
		WorkspaceID:     newUUID(t),
		RuntimeID:       newUUID(t),
		AgentID:         newUUID(t),
		UserID:          newUUID(t),
		RuntimeMode:     "local",
		RuntimeProvider: "claude",
	}

	// Warm the pool so connection setup is not counted as policy work.
	if _, err := service.ListEffectiveTools(ctx, q); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	counter.n.Store(0)
	start := time.Now()
	rows, err := service.ListEffectiveTools(ctx, q)
	if err != nil {
		t.Fatalf("list effective tools: %v", err)
	}
	elapsed := time.Since(start)
	total := counter.n.Load()

	t.Logf("N=%d queries=%d q_per_cap=%.2f ms=%d us_per_cap=%.1f rows=%d",
		n, total, float64(total)/float64(n),
		elapsed.Milliseconds(), float64(elapsed.Microseconds())/float64(n), len(rows))
}

func newUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
