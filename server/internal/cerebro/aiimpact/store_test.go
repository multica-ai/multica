package aiimpact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var storeTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Printf("Skipping AI Impact store integration test: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping AI Impact store integration test: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	storeTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func TestStoreAppendObservationIsWorkspaceScopedAndAppendOnly(t *testing.T) {
	if storeTestPool == nil {
		t.Skip("no test database")
	}

	ctx := context.Background()
	var workspaceID, otherWorkspaceID uuid.UUID
	for name, target := range map[string]*uuid.UUID{
		"AI Impact Store":       &workspaceID,
		"AI Impact Store Other": &otherWorkspaceID,
	} {
		if err := storeTestPool.QueryRow(ctx, `
			INSERT INTO workspace (name, slug, issue_prefix)
			VALUES ($1, 'ai-impact-store-' || gen_random_uuid(), 'AIT')
			RETURNING id`, name).Scan(target); err != nil {
			t.Fatalf("create workspace %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		_, _ = storeTestPool.Exec(context.Background(),
			`DELETE FROM workspace WHERE id = ANY($1::uuid[])`,
			[]uuid.UUID{workspaceID, otherWorkspaceID})
	})

	ownerID := uuid.New()
	store := NewStore(storeTestPool)
	function, err := store.CreateFunction(ctx, workspaceID, FunctionInput{
		Name:        "Customer Service",
		Description: "Resolve customer needs",
		OwnerType:   "member",
		OwnerID:     ownerID,
	})
	if err != nil {
		t.Fatalf("create function: %v", err)
	}
	if function.WorkspaceID != workspaceID || function.OwnerID != ownerID || !function.Active {
		t.Fatalf("created function = %+v", function)
	}
	var loopID, metricID uuid.UUID
	if err := storeTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_operating_loop
			(workspace_id, function_id, name)
		VALUES ($1, $2, 'Resolve customer need')
		RETURNING id`, workspaceID, function.ID).Scan(&loopID); err != nil {
		t.Fatalf("create operating loop: %v", err)
	}

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	if err := storeTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_metric
			(workspace_id, operating_loop_id, name, family, unit, direction,
			 baseline_start, baseline_end, source)
		VALUES ($1, $2, 'Needs solved', 'Outcome', 'needs', 'increase', $3, $4, 'support')
		RETURNING id`, workspaceID, loopID, periodStart.Add(-7*24*time.Hour), periodStart).Scan(&metricID); err != nil {
		t.Fatalf("create metric: %v", err)
	}

	input := ObservationInput{
		MetricID:       metricID,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Value:          12,
		EvidenceStatus: EvidenceMeasured,
		Confidence:     0.9,
		Source:         "support",
		Method:         "audited count",
	}
	first, err := store.AppendObservation(ctx, workspaceID, ownerID, "member", input)
	if err != nil {
		t.Fatalf("append first observation: %v", err)
	}
	if _, err := store.AppendObservation(ctx, otherWorkspaceID, ownerID, "member", input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace append error = %v, want ErrNotFound", err)
	}

	input.Value = 15
	second, err := store.AppendObservation(ctx, workspaceID, ownerID, "member", input)
	if err != nil {
		t.Fatalf("append second observation: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("appending evidence must create a new observation")
	}

	observations, err := store.ListObservations(ctx, workspaceID, metricID)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("observation count = %d, want both append-only records", len(observations))
	}
	if observations[0].Value != 12 || observations[1].Value != 15 {
		t.Fatalf("observation values = [%v, %v], want [12, 15]", observations[0].Value, observations[1].Value)
	}

	otherWorkspaceObservations, err := store.ListObservations(ctx, otherWorkspaceID, metricID)
	if err != nil {
		t.Fatalf("list observations from other workspace: %v", err)
	}
	if len(otherWorkspaceObservations) != 0 {
		t.Fatalf("other workspace received %d observations, want none", len(otherWorkspaceObservations))
	}
}
