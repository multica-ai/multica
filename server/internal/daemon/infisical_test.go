package daemon

// CEREBRO-PATCH(agent-infisical-secrets): tests for spawn-time secret env
// injection. The backend hands the daemon resolved secrets via the claim
// payload; the daemon copies them into env (skipping blocked keys) and never
// hits Infisical itself. FIR-2192.

import (
	"context"
	"log/slog"
	"testing"
)

func TestPrepareInfisicalSpawnInjectsClaimedSecretsAndSkipsBlockedKeys(t *testing.T) {
	d := &Daemon{}
	got, err := d.prepareInfisicalSpawn(context.Background(), &AgentData{
		InfisicalSecrets: map[string]string{
			"DATABASE_URL":     "postgres://secret-value",
			"API_KEY":          "abc123",
			"MULTICA_INTERNAL": "should-not-inject",
			"":                 "skipped-empty-key",
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepareInfisicalSpawn returned error: %v", err)
	}
	if got["DATABASE_URL"] != "postgres://secret-value" {
		t.Fatalf("DATABASE_URL = %q", got["DATABASE_URL"])
	}
	if got["API_KEY"] != "abc123" {
		t.Fatalf("API_KEY = %q", got["API_KEY"])
	}
	if _, ok := got["MULTICA_INTERNAL"]; ok {
		t.Fatalf("blocked MULTICA_INTERNAL key was injected")
	}
	if _, ok := got[""]; ok {
		t.Fatalf("empty key was injected")
	}
}

func TestPrepareInfisicalSpawnNilAgentReturnsNil(t *testing.T) {
	d := &Daemon{}
	got, err := d.prepareInfisicalSpawn(context.Background(), nil, slog.Default())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil env for nil agent, got %#v", got)
	}
}

func TestPrepareInfisicalSpawnEmptySecretsReturnsNil(t *testing.T) {
	d := &Daemon{}
	got, err := d.prepareInfisicalSpawn(context.Background(), &AgentData{}, slog.Default())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil env when no secrets, got %#v", got)
	}
}
