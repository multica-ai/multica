package inactivity

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubQuerier records the activity writes. Used by ApplyAgentActivity.
type stubQuerier struct {
	calls []pgtype.UUID
	err   error
}

func (s *stubQuerier) UpdateAgentTaskActivity(_ context.Context, id pgtype.UUID) error {
	s.calls = append(s.calls, id)
	return s.err
}

func TestComputeTaskMaxInactivity_TaskOverrideWins(t *testing.T) {
	agent := db.Agent{RuntimeConfig: []byte(`{"task_max_inactivity_secs":300}`)}
	workspace := db.Workspace{Settings: []byte(`{"task_max_inactivity_secs":600}`)}
	got := ComputeTaskMaxInactivity(900, agent, workspace, Defaults{DefaultMaxInactivitySecs: 1200})
	if got != 900 {
		t.Fatalf("task override should win: got %d want 900", got)
	}
}

func TestComputeTaskMaxInactivity_AgentOverridesWorkspace(t *testing.T) {
	agent := db.Agent{RuntimeConfig: []byte(`{"task_max_inactivity_secs":300}`)}
	workspace := db.Workspace{Settings: []byte(`{"task_max_inactivity_secs":600}`)}
	got := ComputeTaskMaxInactivity(0, agent, workspace, Defaults{DefaultMaxInactivitySecs: 1200})
	if got != 300 {
		t.Fatalf("agent override should win over workspace: got %d want 300", got)
	}
}

func TestComputeTaskMaxInactivity_WorkspaceOverridesDefaults(t *testing.T) {
	workspace := db.Workspace{Settings: []byte(`{"task_max_inactivity_secs":1800}`)}
	got := ComputeTaskMaxInactivity(0, db.Agent{}, workspace, Defaults{DefaultMaxInactivitySecs: 1200})
	if got != 1800 {
		t.Fatalf("workspace override should win over defaults: got %d want 1800", got)
	}
}

func TestComputeTaskMaxInactivity_FallsBackToDefaults(t *testing.T) {
	got := ComputeTaskMaxInactivity(0, db.Agent{}, db.Workspace{}, Defaults{DefaultMaxInactivitySecs: 1500})
	if got != 1500 {
		t.Fatalf("expected defaults value, got %d want 1500", got)
	}
}

func TestComputeTaskMaxInactivity_FallsBackToPackageDefault(t *testing.T) {
	got := ComputeTaskMaxInactivity(0, db.Agent{}, db.Workspace{}, Defaults{})
	if got != DefaultDefaultMaxInactivitySecs {
		t.Fatalf("expected package default, got %d want %d", got, DefaultDefaultMaxInactivitySecs)
	}
}

func TestComputeTaskMaxInactivity_ZeroOrNegativeTreatedAsUnset(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantValue  int
	}{
		{"zero", `{"task_max_inactivity_secs":0}`, 1200},
		{"negative", `{"task_max_inactivity_secs":-300}`, 1200},
		{"non-integer", `{"task_max_inactivity_secs":"oops"}`, 1200},
		{"float-truncated", `{"task_max_inactivity_secs":12.7}`, 1200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := db.Agent{RuntimeConfig: []byte(tc.raw)}
			got := ComputeTaskMaxInactivity(0, agent, db.Workspace{}, Defaults{DefaultMaxInactivitySecs: 1200})
			if got != tc.wantValue {
				t.Fatalf("got %d want %d", got, tc.wantValue)
			}
		})
	}
}

func TestComputeTaskMaxInactivity_NonPositiveDefaultsIgnored(t *testing.T) {
	got := ComputeTaskMaxInactivity(0, db.Agent{}, db.Workspace{}, Defaults{DefaultMaxInactivitySecs: 0})
	if got != DefaultDefaultMaxInactivitySecs {
		t.Fatalf("non-positive defaults should fall through to package constant: got %d want %d", got, DefaultDefaultMaxInactivitySecs)
	}
}

func TestResolveForTask_TaskValueWins(t *testing.T) {
	task := db.AgentTaskQueue{
		MaxInactivitySecs: pgtype.Int4{Int32: 600, Valid: true},
	}
	got := ResolveForTask(task, Defaults{DefaultMaxInactivitySecs: 1200})
	if got != 600 {
		t.Fatalf("expected task value 600, got %d", got)
	}
}

func TestResolveForTask_LegacyRowFallsToDefaults(t *testing.T) {
	task := db.AgentTaskQueue{MaxInactivitySecs: pgtype.Int4{Valid: false}}
	got := ResolveForTask(task, Defaults{DefaultMaxInactivitySecs: 1500})
	if got != 1500 {
		t.Fatalf("expected defaults 1500, got %d", got)
	}
}

func TestResolveForTask_LegacyRowFallsToPackageDefault(t *testing.T) {
	task := db.AgentTaskQueue{MaxInactivitySecs: pgtype.Int4{Valid: false}}
	got := ResolveForTask(task, Defaults{})
	if got != DefaultDefaultMaxInactivitySecs {
		t.Fatalf("expected package default %d, got %d", DefaultDefaultMaxInactivitySecs, got)
	}
}

func TestApplyAgentActivity_WritesForValidTask(t *testing.T) {
	stub := &stubQuerier{}
	id := pgtype.UUID{Bytes: [16]byte{7}, Valid: true}
	ApplyAgentActivity(context.Background(), stub, id)
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(stub.calls))
	}
	if stub.calls[0] != id {
		t.Fatalf("call id mismatch: got %v want %v", stub.calls[0], id)
	}
}

func TestApplyAgentActivity_InvalidTaskIsNoOp(t *testing.T) {
	stub := &stubQuerier{}
	ApplyAgentActivity(context.Background(), stub, pgtype.UUID{})
	if len(stub.calls) != 0 {
		t.Fatalf("expected zero calls for invalid taskID, got %d", len(stub.calls))
	}
}

func TestApplyAgentActivity_QueryErrorSwallowed(t *testing.T) {
	stub := &stubQuerier{err: context.DeadlineExceeded}
	// Must NOT panic; the helper logs and swallows.
	ApplyAgentActivity(context.Background(), stub, pgtype.UUID{Bytes: [16]byte{9}, Valid: true})
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 call even on error, got %d", len(stub.calls))
	}
}

func TestDescribe_FormatsResolutionSummary(t *testing.T) {
	got := Describe(900, "agent")
	if got != "max_inactivity=900s source=agent" {
		t.Fatalf("unexpected describe: %q", got)
	}
}

func TestDescribe_DefaultsSourceLabelWhenBlank(t *testing.T) {
	got := Describe(1200, "")
	if got != "max_inactivity=1200s source=default" {
		t.Fatalf("unexpected describe: %q", got)
	}
}

func TestReadPositiveIntFromJSON_HandlesMalformed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"empty", "", 0},
		{"not-json", "not-json", 0},
		{"missing-key", `{"other":1}`, 0},
		{"string-value", `{"task_max_inactivity_secs":"300"}`, 0},
		{"negative-int", `{"task_max_inactivity_secs":-300}`, 0},
		{"zero", `{"task_max_inactivity_secs":0}`, 0},
		{"valid-int", `{"task_max_inactivity_secs":300}`, 300},
		{"float-with-fraction", `{"task_max_inactivity_secs":300.5}`, 0},
		{"float-whole", `{"task_max_inactivity_secs":300.0}`, 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readPositiveIntFromJSON([]byte(tc.raw), "task_max_inactivity_secs")
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestComputeTaskMaxInactivity_RoundTripJSON(t *testing.T) {
	// Verifies that the JSON we read out of the column shape is what
	// ComputeTaskMaxInactivity expects to see, so a manual UPDATE
	// (e.g. an ops migration) can't accidentally trip the safeguard.
	type settingsBlob struct {
		TaskMaxInactivitySecs int `json:"task_max_inactivity_secs"`
	}
	raw, err := json.Marshal(settingsBlob{TaskMaxInactivitySecs: 1800})
	if err != nil {
		t.Fatal(err)
	}
	got := ComputeTaskMaxInactivity(0, db.Agent{}, db.Workspace{Settings: raw}, Defaults{DefaultMaxInactivitySecs: 1200})
	if got != 1800 {
		t.Fatalf("expected 1800 from JSON round-trip, got %d", got)
	}
}