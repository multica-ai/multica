package accessdecision

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/util"
)

var accessDecisionTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil || pool.Ping(ctx) != nil {
		fmt.Println("Skipping accessdecision store integration tests: database not reachable")
		os.Exit(m.Run())
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('cerebro_access_decision_ledger') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		fmt.Println("Skipping accessdecision store integration tests: migration 9146 not applied")
		pool.Close()
		os.Exit(m.Run())
	}
	accessDecisionTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func TestStoreAppendsAndReportsPerAgentRuntimeAndTool(t *testing.T) {
	if accessDecisionTestPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()
	store := NewStore(accessDecisionTestPool)
	workspaceID := observerUUID(90)
	allowAgent := observerUUID(91)
	denyAgent := observerUUID(92)
	runtimeID := observerUUID(93)

	if _, err := accessDecisionTestPool.Exec(ctx, `
		INSERT INTO workspace (id, name, slug, description, issue_prefix)
		VALUES ($1, 'Access Decision Test', 'access-decision-test', '', 'ADT')
		ON CONFLICT (id) DO NOTHING
	`, workspaceID); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = accessDecisionTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	entries := []Entry{
		{
			WorkspaceID:           util.UUIDToString(workspaceID),
			AgentID:               util.UUIDToString(allowAgent),
			RuntimeID:             util.UUIDToString(runtimeID),
			ObservedToolName:      "create_issue",
			CanonicalCapabilityID: "platform:create_issue",
			LegacyDecision:        DecisionAllow,
			LegacyPath:            "platform_action",
			ShadowDecision:        DecisionAllow,
			PolicyDecision:        PolicyAllow,
			EvidenceLevel:         availabilityevidence.LevelVerified,
			Reason:                "verified allow",
		},
		{
			WorkspaceID:           util.UUIDToString(workspaceID),
			AgentID:               util.UUIDToString(denyAgent),
			RuntimeID:             util.UUIDToString(runtimeID),
			ObservedToolName:      "create_issue",
			CanonicalCapabilityID: "platform:create_issue",
			LegacyDecision:        DecisionDeny,
			LegacyPath:            "platform_action",
			ShadowDecision:        DecisionDeny,
			PolicyDecision:        PolicyDeny,
			EvidenceLevel:         availabilityevidence.LevelVerified,
			Reason:                "verified deny",
		},
	}
	for _, entry := range entries {
		if err := store.Append(ctx, entry); err != nil {
			t.Fatalf("Append(): %v", err)
		}
	}

	report, err := store.Report(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Report(): %v", err)
	}
	if report.Total != 2 || report.Diffs != 0 || len(report.Groups) != 2 {
		t.Fatalf("report = %+v, want two zero-diff groups", report)
	}
}
