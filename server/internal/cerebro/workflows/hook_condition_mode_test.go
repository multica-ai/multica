package workflows

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestHookPolicyConditionModeRoundTripsAny(t *testing.T) {
	var policy HookPolicy
	if err := json.Unmarshal([]byte(`{"condition_mode":"any"}`), &policy); err != nil {
		t.Fatal(err)
	}

	got, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"condition_mode":"any"`) {
		t.Fatalf("HookPolicy JSON = %s, want condition_mode any", got)
	}
}

func TestHookPolicyDefaultsOmittedConditionModeToAll(t *testing.T) {
	var policy HookPolicy
	if err := json.Unmarshal([]byte(`{}`), &policy); err != nil {
		t.Fatal(err)
	}

	if policy.ConditionMode != HookConditionAll {
		t.Fatalf("ConditionMode = %q, want %q", policy.ConditionMode, HookConditionAll)
	}
}

type hookRowScannerFunc func(...any) error

func (scan hookRowScannerFunc) Scan(dest ...any) error {
	return scan(dest...)
}

func TestScanHookPolicyReadsConditionMode(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	familyID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	createdByID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	now := pgtype.Timestamptz{Valid: true}

	row := hookRowScannerFunc(func(dest ...any) error {
		if len(dest) != 20 {
			return fmt.Errorf("scan destination count = %d, want 20 including readable contracts", len(dest))
		}
		*dest[0].(*pgtype.UUID) = id
		*dest[1].(*pgtype.UUID) = familyID
		*dest[2].(*pgtype.UUID) = workspaceID
		*dest[3].(*string) = "Any condition"
		*dest[4].(*string) = ""
		*dest[5].(*string) = "A readable rule"
		*dest[6].(*string) = "A readable outcome"
		*dest[7].(*int32) = 1
		*dest[8].(*string) = string(HookModeDryRun)
		*dest[9].(*string) = string(HookFailWarn)
		*dest[10].(*string) = string(HookConditionAny)
		*dest[11].(*[]byte) = []byte(`[]`)
		*dest[12].(*[]byte) = []byte(`[]`)
		*dest[13].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		*dest[14].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		*dest[15].(*pgtype.UUID) = createdByID
		*dest[16].(*string) = "agent"
		*dest[17].(*pgtype.UUID) = pgtype.UUID{}
		*dest[18].(*pgtype.Timestamptz) = now
		*dest[19].(*pgtype.Timestamptz) = now
		return nil
	})

	policy, _, err := scanHookPolicy(row)
	if err != nil {
		t.Fatalf("scanHookPolicy() error = %v", err)
	}
	if policy.ConditionMode != HookConditionAny {
		t.Fatalf("ConditionMode = %q, want %q", policy.ConditionMode, HookConditionAny)
	}
}
