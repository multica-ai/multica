package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestValidatePlanLimitsSnapshot(t *testing.T) {
	t.Parallel()

	used := 42.0
	minutes := int64(300)
	reset := int64(1_800_000_000)
	valid := func() *protocol.PlanLimitsSnapshot {
		return &protocol.PlanLimitsSnapshot{
			Provider:   "codex",
			Status:     protocol.PlanLimitsStatusAvailable,
			ObservedAt: 1_700_000_000,
			Windows: []protocol.PlanLimitWindow{{
				Name:          "primary",
				UsedPercent:   &used,
				WindowMinutes: &minutes,
				ResetsAt:      &reset,
			}},
		}
	}

	tests := []struct {
		name     string
		mutate   func(*protocol.PlanLimitsSnapshot)
		provider string
		wantErr  bool
	}{
		{name: "valid", provider: "codex"},
		{name: "provider mismatch", provider: "claude", wantErr: true},
		{name: "missing observation", provider: "codex", mutate: func(s *protocol.PlanLimitsSnapshot) { s.ObservedAt = 0 }, wantErr: true},
		{name: "unknown status", provider: "codex", mutate: func(s *protocol.PlanLimitsSnapshot) { s.Status = "unknown" }, wantErr: true},
		{name: "available without windows", provider: "codex", mutate: func(s *protocol.PlanLimitsSnapshot) { s.Windows = nil }, wantErr: true},
		{name: "invalid percent", provider: "codex", mutate: func(s *protocol.PlanLimitsSnapshot) { value := 101.0; s.Windows[0].UsedPercent = &value }, wantErr: true},
		{name: "duplicate window", provider: "codex", mutate: func(s *protocol.PlanLimitsSnapshot) { s.Windows = append(s.Windows, s.Windows[0]) }, wantErr: true},
		{name: "exhausted without window", provider: "codex", mutate: func(s *protocol.PlanLimitsSnapshot) { s.Status = protocol.PlanLimitsStatusExhausted; s.Windows = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := valid()
			if tc.mutate != nil {
				tc.mutate(snapshot)
			}
			_, err := validatePlanLimitsSnapshot(snapshot, tc.provider)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
