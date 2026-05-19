package runtimetools

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeToolsPool is the shared db pool used by every test in this file.
// Tests skip cleanly when DATABASE_URL is unset so `go test ./...` keeps
// passing on developer machines without the test DB up.
var runtimeToolsPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("runtimetools: DATABASE_URL not set, skipping integration tests")
		os.Exit(m.Run())
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("runtimetools: pgxpool.New failed (%v); skipping integration tests\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("runtimetools: ping failed (%v); skipping integration tests\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	runtimeToolsPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// TestServiceConstruction exercises New() without touching the DB so the
// package retains at least one test that runs in unit-test-only environments.
func TestServiceConstruction(t *testing.T) {
	// pgxpool.New is a no-op here; we just verify the type plumbing compiles.
	if New(runtimeToolsPool) == nil {
		t.Fatalf("New returned nil")
	}
}

// TestSourceConstants pins the wire constants so a rename in the service
// layer fails loudly — handlers and SQL CHECK constraints both rely on
// these strings.
func TestSourceConstants(t *testing.T) {
	if SourceCloud != "cloud" {
		t.Errorf("SourceCloud = %q, want %q", SourceCloud, "cloud")
	}
	if SourceMCP != "mcp" {
		t.Errorf("SourceMCP = %q, want %q", SourceMCP, "mcp")
	}
}

// TestUpsertToolValidatesSource confirms the service rejects bad source
// values before hitting the DB.
func TestUpsertToolValidatesSource(t *testing.T) {
	svc := New(runtimeToolsPool)
	_, err := svc.UpsertTool(context.Background(), UpsertToolInput{
		RuntimeID: pgtype.UUID{},
		ToolName:  "x",
		Source:    "bogus",
	})
	if err != ErrInvalidSource {
		t.Errorf("expected ErrInvalidSource, got %v", err)
	}
}

// TestUpsertMCPToolRequiresServerName confirms the service rejects MCP rows
// without an mcp_server_name (otherwise multiple MCP servers cannot expose
// tools with overlapping names).
func TestUpsertMCPToolRequiresServerName(t *testing.T) {
	svc := New(runtimeToolsPool)
	_, err := svc.UpsertTool(context.Background(), UpsertToolInput{
		RuntimeID: pgtype.UUID{},
		ToolName:  "x",
		Source:    SourceMCP,
	})
	if err != ErrMCPNeedsServer {
		t.Errorf("expected ErrMCPNeedsServer, got %v", err)
	}
}

// TestRecordScanSkipsServerErrors documents that a failing server (Error
// set) does not zero out the previously-known inventory: RecordScan iterates
// good servers only and returns nil so the daemon ack succeeds. We verify
// this behaviourally by feeding an Error-only payload through a service that
// has no DB pool — if it tried to call the DB the test would panic.
func TestRecordScanSkipsServerErrors(t *testing.T) {
	svc := New(nil) // intentionally nil pool: any DB call below would panic
	err := svc.RecordScan(context.Background(), pgtype.UUID{}, pgtype.Timestamptz{}, []ScannedServer{
		{Name: "broken", Error: "exec: command not found"},
	})
	if err != nil {
		t.Errorf("RecordScan with only error servers should be no-op, got %v", err)
	}
}

// TestRecordScanRejectsBlankServerName guards against a daemon bug where an
// MCP server name is empty: the registry's unique index allows a NULL
// mcp_server_name only for cloud tools, so feeding "" for an mcp source
// would explode in production. Catch it at the service edge with a clear
// error.
func TestRecordScanRejectsBlankServerName(t *testing.T) {
	svc := New(nil)
	err := svc.RecordScan(context.Background(), pgtype.UUID{}, pgtype.Timestamptz{}, []ScannedServer{
		{Name: "", Tools: []ScannedTool{{Name: "x"}}},
	})
	if err == nil {
		t.Fatal("expected error for blank server name, got nil")
	}
}
