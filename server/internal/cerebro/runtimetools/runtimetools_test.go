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
