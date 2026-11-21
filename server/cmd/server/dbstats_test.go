package main

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// resolvedPoolConfig runs the production pool policy without opening a
// connection.
func resolvedPoolConfig(t *testing.T, dbURL, envMax, envMin string, sizing dbPoolSizing) *pgxpool.Config {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if envMax != "" {
		t.Setenv(sizing.maxConnsEnv, envMax)
	}
	if envMin != "" {
		t.Setenv(sizing.minConnsEnv, envMin)
	}
	applyPoolSizing(cfg, dbURL, sizing)
	return cfg
}

func resolvedPoolSizing(t *testing.T, dbURL, envMax, envMin string, sizing dbPoolSizing) (max, min int32) {
	cfg := resolvedPoolConfig(t, dbURL, envMax, envMin, sizing)
	return cfg.MaxConns, cfg.MinConns
}

func resolvedPrimaryPoolSizing(t *testing.T, dbURL, envMax, envMin string) (max, min int32) {
	return resolvedPoolSizing(t, dbURL, envMax, envMin, primaryPoolSizing)
}

func TestPoolSizing_URLParamsHonoredWhenEnvUnset(t *testing.T) {
	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40&pool_min_conns=8"
	max, min := resolvedPrimaryPoolSizing(t, url, "", "")
	if max != 40 || min != 8 {
		t.Fatalf("URL params should win when env unset; got max=%d min=%d", max, min)
	}
}

func TestPoolSizing_EnvOverridesURL(t *testing.T) {
	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40&pool_min_conns=8"
	max, min := resolvedPrimaryPoolSizing(t, url, "100", "20")
	if max != 100 || min != 20 {
		t.Fatalf("env should win over URL; got max=%d min=%d", max, min)
	}
}

func TestPoolSizing_PartialURLParam(t *testing.T) {
	// Only pool_max_conns is set in URL — pool_min_conns should fall back to
	// the code default, not pgx's built-in default (which would be 0).
	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40"
	max, min := resolvedPrimaryPoolSizing(t, url, "", "")
	if max != 40 {
		t.Fatalf("URL pool_max_conns should be honored; got max=%d", max)
	}
	if min != defaultMinConns {
		t.Fatalf("min should default; got min=%d, want %d", min, defaultMinConns)
	}
}

func TestPoolSizing_InvalidEnvFallsBackToCodeDefault(t *testing.T) {
	// Invalid env value with no URL pool param → code default, NOT pgx's
	// built-in 4. This is the regression that was fixed; pinning it here
	// so we don't silently fall back to the bad value again.
	max, min := resolvedPrimaryPoolSizing(t, "postgres://u:p@h/db?sslmode=disable", "not-a-number", "")
	if max != defaultMaxConns {
		t.Fatalf("invalid env should fall back to code default; got max=%d, want %d", max, defaultMaxConns)
	}
	if min != defaultMinConns {
		t.Fatalf("got min=%d, want %d", min, defaultMinConns)
	}
}

func TestPoolSizing_InvalidEnvFallsBackToURLParam(t *testing.T) {
	// Invalid env value with a URL pool param → URL param wins, NOT pgx
	// default. This is what makes the precedence chain end at "URL or code
	// default" rather than at "pgx default" on misconfiguration.
	url := "postgres://u:p@h/db?sslmode=disable&pool_max_conns=40"
	max, _ := resolvedPrimaryPoolSizing(t, url, "not-a-number", "")
	if max != 40 {
		t.Fatalf("invalid env should fall back to URL param; got max=%d, want 40", max)
	}
}

func TestReplicaPoolSizingHasIndependentSafeDefaults(t *testing.T) {
	max, min := resolvedPoolSizing(
		t,
		"postgres://u:p@replica/db?sslmode=disable",
		"",
		"",
		replicaPoolSizing,
	)
	if max != defaultReplicaMaxConns || min != 0 {
		t.Fatalf("replica defaults = %d/%d, want %d/0", max, min, defaultReplicaMaxConns)
	}
}

func TestReplicaPoolSizingAllowsExplicitZeroMinimum(t *testing.T) {
	max, min := resolvedPoolSizing(
		t,
		"postgres://u:p@replica/db?sslmode=disable&pool_max_conns=12&pool_min_conns=3",
		"8",
		"0",
		replicaPoolSizing,
	)
	if max != 8 || min != 0 {
		t.Fatalf("replica sizing = %d/%d, want 8/0", max, min)
	}
}

func TestReplicaPoolEnforcesReadOnlyAndShortConnectionLifetime(t *testing.T) {
	cfg := resolvedPoolConfig(
		t,
		"postgres://u:p@replica/db?sslmode=disable",
		"",
		"",
		replicaPoolSizing,
	)
	if !sameValidateConnect(
		cfg.ConnConfig.Config.ValidateConnect,
		pgconn.ValidateConnectTargetSessionAttrsReadOnly,
	) {
		t.Fatal("replica ValidateConnect does not enforce target_session_attrs=read-only")
	}
	if cfg.MaxConnLifetime != defaultReplicaMaxConnLifetime {
		t.Fatalf("replica max lifetime = %s, want %s", cfg.MaxConnLifetime, defaultReplicaMaxConnLifetime)
	}
}

func TestReplicaPoolPreservesStricterStandbyValidation(t *testing.T) {
	cfg := resolvedPoolConfig(
		t,
		"postgres://u:p@replica/db?sslmode=disable&target_session_attrs=standby",
		"",
		"",
		replicaPoolSizing,
	)
	if !sameValidateConnect(
		cfg.ConnConfig.Config.ValidateConnect,
		pgconn.ValidateConnectTargetSessionAttrsStandby,
	) {
		t.Fatal("replica ValidateConnect replaced target_session_attrs=standby")
	}
}

func TestReplicaPoolOverridesWritableValidation(t *testing.T) {
	cfg := resolvedPoolConfig(
		t,
		"postgres://u:p@replica/db?sslmode=disable&target_session_attrs=read-write",
		"",
		"",
		replicaPoolSizing,
	)
	if !sameValidateConnect(
		cfg.ConnConfig.Config.ValidateConnect,
		pgconn.ValidateConnectTargetSessionAttrsReadOnly,
	) {
		t.Fatal("replica ValidateConnect accepted target_session_attrs=read-write")
	}
}

func TestReplicaPoolOverridesPreferStandbyValidation(t *testing.T) {
	cfg := resolvedPoolConfig(
		t,
		"postgres://u:p@replica/db?sslmode=disable&target_session_attrs=prefer-standby",
		"",
		"",
		replicaPoolSizing,
	)
	if !sameValidateConnect(
		cfg.ConnConfig.Config.ValidateConnect,
		pgconn.ValidateConnectTargetSessionAttrsReadOnly,
	) {
		t.Fatal("replica ValidateConnect accepted target_session_attrs=prefer-standby")
	}
}

func TestReplicaPoolHonorsExplicitConnectionLifetime(t *testing.T) {
	cfg := resolvedPoolConfig(
		t,
		"postgres://u:p@replica/db?sslmode=disable&pool_max_conn_lifetime=11m",
		"",
		"",
		replicaPoolSizing,
	)
	if cfg.MaxConnLifetime != 11*time.Minute {
		t.Fatalf("replica max lifetime = %s, want 11m", cfg.MaxConnLifetime)
	}
}
