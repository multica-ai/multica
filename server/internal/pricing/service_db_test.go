package pricing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The integration test uses a fresh schema and the actual feature migrations.
// No test touches the application's catalog or workspace rows.
func TestDatabasePricingRefreshAndRevisions(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is required for database integration")
	}
	ctx := context.Background()
	bootstrap, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	schema := "pricing_test_" + uuid.New().String()[:8]
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := bootstrap.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Cleanup callbacks run before this pool closes and before the schema drops.
	t.Run("isolated database", func(t *testing.T) {
		fx := testutil.New(pool, "", "")
		fx.Exec(t, "CREATE TABLE workspace (id uuid NOT NULL)")
		for _, name := range []string{"450_model_pricing", "451_model_pricing_catalog_unique", "452_workspace_model_pricing_unique"} {
			sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", name+".up.sql"))
			if err != nil {
				t.Fatal(err)
			}
			fx.Exec(t, string(sql))
		}
		ws := fx.Insert(t, "workspace", testutil.Cols{"id": uuid.New()})
		var workspaceID pgtype.UUID
		if err := workspaceID.Scan(ws); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int32
		var fail atomic.Bool
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			if fail.Load() {
				w.WriteHeader(404)
				return
			}
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			if r.URL.Path == "/lite" {
				w.Write([]byte(`{"moonshot/kimi-k3":{"litellm_provider":"moonshot","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015}}`))
				return
			}
			w.Write([]byte(`{"kimi-for-coding":{"models":{"k3-256k":{"family":"kimi-k3","cost":{"input":0,"output":0}}}}}`))
		}))
		defer source.Close()
		svc := New(db.New(pool), pool)
		svc.LiteURL = source.URL + "/lite"
		svc.ModelsURL = source.URL + "/models"
		fail.Store(true)
		if err := svc.Refresh(ctx, false); !errors.Is(err, ErrRefresh) {
			t.Fatalf("initial failure was not reported: %v", err)
		}
		count := requests.Load()
		if err := svc.Refresh(ctx, false); !errors.Is(err, ErrRefresh) || requests.Load() != count {
			t.Fatalf("cooldown hid the failed refresh or repeated the request: %v", err)
		}
		fail.Store(false)
		fx.Exec(t, "UPDATE model_pricing_catalog SET checked_at = now() - interval '2 minutes'")
		if err := svc.Refresh(ctx, false); err != nil {
			t.Fatal(err)
		}
		first, err := svc.Snapshot(ctx, workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if first.SucceededAt == nil {
			t.Fatal("successful refresh was not persisted")
		}
		count = requests.Load()
		fx.Exec(t, "UPDATE model_pricing_catalog SET checked_at = now() - interval '2 minutes'")
		if err := svc.Refresh(ctx, false); err != nil || requests.Load() != count {
			t.Fatal("same-day refresh was repeated")
		}
		fx.Exec(t, "UPDATE model_pricing_catalog SET checked_at = now() - interval '2 minutes'")
		if err := svc.Refresh(ctx, true); err != nil {
			t.Fatal(err)
		}
		after304, err := svc.Snapshot(ctx, workspaceID)
		if err != nil || after304.Version != first.Version {
			t.Fatal("304 changed or lost prices")
		}
		fail.Store(true)
		fx.Exec(t, "UPDATE model_pricing_catalog SET checked_at = now() - interval '2 minutes'")
		if err := svc.Refresh(ctx, true); !errors.Is(err, ErrRefresh) {
			t.Fatalf("missing refresh failure: %v", err)
		}
		stale, err := svc.Snapshot(ctx, workspaceID)
		if err != nil || stale.Version != first.Version || stale.LastError == "" {
			t.Fatalf("last good snapshot lost: %v", err)
		}
		fail.Store(false)
		fx.Exec(t, "UPDATE model_pricing_catalog SET checked_at = now() - interval '2 minutes'")
		if err := svc.Refresh(ctx, false); err != nil {
			t.Fatal(err)
		}
		recovered, err := svc.Snapshot(ctx, workspaceID)
		if err != nil || recovered.LastError != "" || recovered.Version != first.Version {
			t.Fatalf("same-day retry did not recover the retained snapshot: %+v, %v", recovered, err)
		}
		overrides := map[string]Row{"kimi-k3": {Input: 7, Output: 9, CacheRead: 1, CacheWrite: 7}}
		result := make(chan error, 2)
		for range 2 {
			go func() { result <- svc.SaveOverrides(ctx, workspaceID, workspaceID, 0, overrides) }()
		}
		a, b := <-result, <-result
		if !((a == nil && errors.Is(b, ErrConflict)) || (b == nil && errors.Is(a, ErrConflict))) {
			t.Fatalf("concurrent revision writes: %v / %v", a, b)
		}
		saved, err := svc.Snapshot(ctx, workspaceID)
		if err != nil || saved.Revision != 1 || saved.Overrides["kimi-k3"].Input != 7 {
			t.Fatal("workspace override was not persisted")
		}
	})
}
