package cloudtoolscan

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
)

// pool is the shared db pool. Tests skip cleanly when DATABASE_URL is unset so
// `go test ./...` keeps passing without the integration DB up.
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		ctx := context.Background()
		if p, err := pgxpool.New(ctx, dbURL); err == nil {
			if err := p.Ping(ctx); err == nil {
				pool = p
				code := m.Run()
				p.Close()
				os.Exit(code)
			}
			p.Close()
		}
	}
	fmt.Println("cloudtoolscan: DATABASE_URL not set/reachable, skipping integration tests")
	os.Exit(m.Run())
}

// fakeReporter / fakeUpserter let the non-DB unit test assert the exact wire
// shape the scanner produces without standing up Postgres.
type fakeReporter struct {
	gotWorkspace pgtype.UUID
	gotReporter  capabilityregistry.Subject
	gotCaps      []capabilityregistry.ReportInput
}

func (f *fakeReporter) Report(_ context.Context, ws pgtype.UUID, reporter capabilityregistry.Subject, caps []capabilityregistry.ReportInput) ([]capabilityregistry.View, error) {
	f.gotWorkspace = ws
	f.gotReporter = reporter
	f.gotCaps = caps
	return nil, nil
}

type fakeUpserter struct {
	got []runtimetools.UpsertToolInput
}

func (f *fakeUpserter) UpsertTool(_ context.Context, in runtimetools.UpsertToolInput) (runtimetools.Tool, error) {
	f.got = append(f.got, in)
	return runtimetools.Tool{}, nil
}

func mustUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}

// TestScanWireShape pins the contract the scanner emits: capability key is the
// BARE tool name (so it binds to the gateway's policy enforcement, which
// resolves by tool name), category "tools", source "scan", and the legacy
// inventory upsert carries source=cloud with a last_scanned_at.
func TestScanWireShape(t *testing.T) {
	rep := &fakeReporter{}
	ups := &fakeUpserter{}
	s := New(rep, ups, []ToolMeta{
		{Name: "add_comment", Description: "Post a comment."},
		{Name: "", Description: "skipped — blank name"},
		{Name: "web_fetch", Description: "Fetch a URL."},
	})

	rtID := mustUUID(t)
	wsID := mustUUID(t)
	if err := s.Scan(context.Background(), rtID, wsID); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(rep.gotCaps) != 2 {
		t.Fatalf("reported %d caps, want 2 (blank name skipped)", len(rep.gotCaps))
	}
	if rep.gotReporter.Type != "runtime" {
		t.Errorf("reporter type = %q, want runtime", rep.gotReporter.Type)
	}
	first := rep.gotCaps[0]
	if first.Key != "add_comment" || first.Title != "add_comment" {
		t.Errorf("cap key/title = %q/%q, want bare tool name add_comment", first.Key, first.Title)
	}
	if first.Category != "tools" || first.Source != "scan" {
		t.Errorf("cap category/source = %q/%q, want tools/scan", first.Category, first.Source)
	}
	if len(ups.got) != 2 {
		t.Fatalf("upserted %d runtime tools, want 2", len(ups.got))
	}
	if ups.got[0].Source != runtimetools.SourceCloud {
		t.Errorf("runtime tool source = %q, want cloud", ups.got[0].Source)
	}
	if !ups.got[0].LastScannedAt.Valid {
		t.Error("runtime tool LastScannedAt not set")
	}
	if ups.got[0].Enabled != nil {
		t.Error("Enabled should be nil so a re-scan preserves admin overrides")
	}
}

func TestScanNilAndInvalid(t *testing.T) {
	var s *Scanner
	if err := s.Scan(context.Background(), pgtype.UUID{}, pgtype.UUID{}); err == nil {
		t.Error("nil scanner should error")
	}
	good := New(&fakeReporter{}, &fakeUpserter{}, []ToolMeta{{Name: "x"}})
	if err := good.Scan(context.Background(), pgtype.UUID{}, pgtype.UUID{}); err == nil {
		t.Error("invalid (zero) ids should error")
	}
}

// TestScanPopulatesBothInventories is the integration test: a real cloud-runtime
// scan must land rows in cerebro_capability (the unified table's source) AND
// cerebro_runtime_tool, and be idempotent.
func TestScanPopulatesBothInventories(t *testing.T) {
	if pool == nil {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()

	var wsID pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('cloudscan-test', $1) RETURNING id`,
		"cloudscan-test-"+t.Name(),
	).Scan(&wsID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	var rtID pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, 'cloud-rt', 'cloud', 'firtal-gateway', 'online') RETURNING id`,
		wsID,
	).Scan(&rtID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	builtins := []ToolMeta{
		{Name: "scan_tool_alpha", Description: "alpha"},
		{Name: "scan_tool_beta", Description: "beta"},
	}
	s := New(capabilityregistry.New(pool), runtimetools.New(pool), builtins)

	// Two passes prove idempotency (no duplicate capability rows).
	for i := 0; i < 2; i++ {
		if err := s.Scan(ctx, rtID, wsID); err != nil {
			t.Fatalf("scan pass %d: %v", i, err)
		}
	}

	keys := []string{"scan_tool_alpha", "scan_tool_beta"}
	var capCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_capability
		 WHERE workspace_id = $1 AND capability_key = ANY($2) AND source = 'scan' AND category = 'tools'`,
		wsID, keys,
	).Scan(&capCount); err != nil {
		t.Fatalf("count capabilities: %v", err)
	}
	if capCount != 2 {
		t.Fatalf("cerebro_capability rows = %d, want 2 (idempotent)", capCount)
	}

	var rtToolCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_runtime_tool
		 WHERE runtime_id = $1 AND source = 'cloud' AND last_scanned_at IS NOT NULL
		   AND tool_name = ANY($2)`,
		rtID, keys,
	).Scan(&rtToolCount); err != nil {
		t.Fatalf("count runtime tools: %v", err)
	}
	if rtToolCount != 2 {
		t.Fatalf("cerebro_runtime_tool rows = %d, want 2 with last_scanned_at", rtToolCount)
	}
}
