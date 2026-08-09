package pooltestdb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	missingDatabaseURLHelper         = "MULTICA_POOLTESTDB_MISSING_DATABASE_URL_HELPER"
	searchPathDatabaseFailureHelper  = "MULTICA_POOLTESTDB_SEARCH_PATH_FAILURE_HELPER"
	searchPathFailureMissingDatabase = "missing"
	searchPathFailureUnreachable     = "unreachable"
)

func TestPoolTestDatabaseRequiresEnvironment(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPoolTestDatabaseMissingEnvironmentHelper$")
	cmd.Env = append(environmentWithoutDatabaseURL(), missingDatabaseURLHelper+"=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("missing-DATABASE_URL helper succeeded; output:\n%s", output)
	}
	if !strings.Contains(string(output), "DATABASE_URL is required for Pool DB tests") {
		t.Fatalf("missing-DATABASE_URL failure = %q, want fail-fast diagnostic", output)
	}
}

func TestPoolTestDatabaseMissingEnvironmentHelper(t *testing.T) {
	if os.Getenv(missingDatabaseURLHelper) != "1" {
		return
	}
	Open(t)
	t.Fatal("Open returned without DATABASE_URL")
}

func TestPoolTestDatabaseIsReachable(t *testing.T) {
	pool := Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
		t.Fatalf("SELECT 1 from Pool test database: %v", err)
	}
	if got != 1 {
		t.Fatalf("SELECT 1 = %d, want 1", got)
	}
}

func TestPoolTestDatabaseWithSearchPathFailsFast(t *testing.T) {
	testCases := []struct {
		name        string
		mode        string
		databaseURL string
		want        string
	}{
		{
			name: "missing_database_url",
			mode: searchPathFailureMissingDatabase,
			want: "DATABASE_URL is required for Pool DB tests",
		},
		{
			name:        "unreachable_database",
			mode:        searchPathFailureUnreachable,
			databaseURL: "postgres://multica:multica@127.0.0.1:1/multica?sslmode=disable&connect_timeout=1",
			want:        "reach Pool test database",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestPoolTestDatabaseWithSearchPathFailureHelper$")
			cmd.Env = append(environmentWithoutDatabaseURL(), searchPathDatabaseFailureHelper+"="+tc.mode)
			if tc.databaseURL != "" {
				cmd.Env = append(cmd.Env, "DATABASE_URL="+tc.databaseURL)
			}
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("%s helper succeeded; output:\n%s", tc.mode, output)
			}
			if !strings.Contains(string(output), tc.want) {
				t.Fatalf("%s failure = %q, want diagnostic containing %q", tc.mode, output, tc.want)
			}
		})
	}
}

func TestPoolTestDatabaseWithSearchPathFailureHelper(t *testing.T) {
	if os.Getenv(searchPathDatabaseFailureHelper) == "" {
		return
	}
	OpenWithSearchPath(t, "public")
	t.Fatal("OpenWithSearchPath returned for a missing or unreachable DATABASE_URL")
}

func TestPoolTestDatabaseUsesConfiguredSearchPath(t *testing.T) {
	rootPool := Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	schema := fmt.Sprintf("pooltestdb_search_path_%d", time.Now().UnixNano())
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := rootPool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create search-path schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := rootPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop search-path schema: %v", err)
		}
	})

	scopedPool := OpenWithSearchPath(t, schema, "public")
	var currentSchema string
	if err := scopedPool.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil {
		t.Fatalf("read current schema: %v", err)
	}
	if currentSchema != schema {
		t.Fatalf("current_schema() = %q, want %q", currentSchema, schema)
	}
}

func environmentWithoutDatabaseURL() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "DATABASE_URL=") {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}
