package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestParseRuntimeFallbackConfig(t *testing.T) {
	raw := []byte(`{"fallback_runtime_id":"11111111-1111-1111-1111-111111111111","preferred_runtime_id":"22222222-2222-2222-2222-222222222222"}`)
	cfg := parseRuntimeFallbackConfig(raw)
	if cfg.FallbackRuntimeID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("fallback_runtime_id = %q", cfg.FallbackRuntimeID)
	}
	if cfg.PreferredRuntimeID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("preferred_runtime_id = %q", cfg.PreferredRuntimeID)
	}
}

func TestParseRuntimeRestoreContext(t *testing.T) {
	raw, err := json.Marshal(RuntimeRestoreContext{
		Type:               RuntimeRestoreContextType,
		PreferredRuntimeID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseRuntimeRestoreContext(raw)
	if !ok {
		t.Fatal("expected valid runtime_restore context")
	}
	if got.PreferredRuntimeID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("preferred = %q", got.PreferredRuntimeID)
	}
}

func TestRetryNotBeforeUsesSessionLimitResetForRestoreScheduling(t *testing.T) {
	now := time.Date(2026, 6, 25, 6, 0, 0, 0, time.UTC)
	parent := db.AgentTaskQueue{
		FailureReason: pgtype.Text{
			String: taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(),
			Valid:  true,
		},
		Error:   pgtype.Text{String: "Your limit resets 6:50am (UTC)", Valid: true},
		Attempt: 1,
	}
	got := retryNotBefore(parent, now)
	if !got.Valid {
		t.Fatal("expected retryNotBefore for capacity failure")
	}
	want := time.Date(2026, 6, 25, 6, 50, 0, 0, time.UTC)
	if !got.Time.Equal(want) {
		t.Fatalf("retryNotBefore = %s, want %s", got.Time, want)
	}
}

func TestMergeRuntimeFallbackConfigPreservesExistingKeys(t *testing.T) {
	raw := []byte(`{"fallback_runtime_id":"11111111-1111-1111-1111-111111111111","gateway":{"token":"secret"}}`)
	merged := mergeRuntimeFallbackConfig(raw, RuntimeFallbackConfig{
		PreferredRuntimeID: "22222222-2222-2222-2222-222222222222",
	})
	var out map[string]any
	if err := json.Unmarshal(merged, &out); err != nil {
		t.Fatal(err)
	}
	if out["preferred_runtime_id"] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("preferred_runtime_id missing after merge: %#v", out)
	}
	if out["fallback_runtime_id"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("fallback_runtime_id missing after merge: %#v", out)
	}
	if _, ok := out["gateway"]; !ok {
		t.Fatalf("expected unrelated runtime_config keys to survive merge: %#v", out)
	}
}
