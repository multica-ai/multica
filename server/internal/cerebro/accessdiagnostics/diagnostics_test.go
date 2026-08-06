package accessdiagnostics

import (
	"testing"
	"time"
)

func TestBuildRuntimeDiagnosticsSeparatesProviderProbeFromMCPDiscovery(t *testing.T) {
	now := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	report := BuildRuntimeDiagnostics(RuntimeInput{
		RuntimeID: "runtime-1",
		Provider:  "claude",
		Status:    "online",
		Capabilities: map[string]any{
			"discovery_method": "probed",
			"tools":            []string{"Read", "Bash"},
			"mcp_servers":      []string{"multica"},
		},
		ProviderObservedAt: now.Add(-5 * time.Minute),
		Tools: []RuntimeToolEvidence{
			{Name: "get_issue", Source: "mcp", MCPServerName: "multica", LastScannedAt: now.Add(-5 * time.Minute)},
			{Name: "add_comment", Source: "mcp", MCPServerName: "multica", LastScannedAt: now.Add(-5 * time.Minute)},
		},
		Now:        now,
		StaleAfter: 30 * time.Minute,
	})

	if got := report.Diagnostics[0]; got.Code != CodeProviderProbe || got.State != StateSuccess || got.AffectedCapability != "provider:claude" {
		t.Fatalf("provider diagnostic = %#v", got)
	}
	if got := report.Diagnostics[1]; got.Code != CodeMCPDiscovery || got.State != StateSuccess || got.Version == "" || got.Count != 2 {
		t.Fatalf("MCP diagnostic = %#v", got)
	}
	if report.Diagnostics[0].Version == report.Diagnostics[1].Version {
		t.Fatal("provider and MCP versions must describe separate inventories")
	}
}

func TestBuildRuntimeDiagnosticsNamesUnavailableAndStaleRecovery(t *testing.T) {
	now := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	report := BuildRuntimeDiagnostics(RuntimeInput{
		RuntimeID: "runtime-1",
		Provider:  "hermes",
		Status:    "offline",
		Capabilities: map[string]any{
			"discovery_method": "not_measured",
			"mcp_servers":      []string{"company-brain"},
		},
		ProviderObservedAt: now.Add(-2 * time.Hour),
		Tools: []RuntimeToolEvidence{{
			Name: "search", Source: "mcp", MCPServerName: "company-brain", LastScannedAt: now.Add(-2 * time.Hour),
		}},
		Now:        now,
		StaleAfter: 30 * time.Minute,
	})

	if got := report.Diagnostics[0]; got.State != StateUnavailable || got.RecoveryAction == "" {
		t.Fatalf("provider diagnostic = %#v", got)
	}
	if got := report.Diagnostics[1]; got.State != StateStale || got.RecoveryAction == "" {
		t.Fatalf("MCP diagnostic = %#v", got)
	}
}

func TestBuildRuntimeDiagnosticsDoesNotCallUntimestampedProviderEvidenceCurrent(t *testing.T) {
	now := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	report := BuildRuntimeDiagnostics(RuntimeInput{
		RuntimeID: "runtime-1",
		Provider:  "claude",
		Status:    "online",
		Capabilities: map[string]any{
			"discovery_method": "probed",
			"tools":            []string{"Read"},
		},
		Now: now,
	})

	if got := report.Diagnostics[0]; got.State != StateUnavailable || got.ObservedAt != "" {
		t.Fatalf("provider diagnostic = %#v", got)
	}
}

func TestBuildRuntimeDiagnosticsChecksEveryConfiguredMCPServer(t *testing.T) {
	now := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		tools []RuntimeToolEvidence
		want  State
	}{
		{
			name: "one fresh server and one missing server is partial",
			tools: []RuntimeToolEvidence{
				{Name: "search", Source: "mcp", MCPServerName: "fresh", LastScannedAt: now.Add(-5 * time.Minute)},
			},
			want: StatePartial,
		},
		{
			name: "one fresh server and one stale server is stale",
			tools: []RuntimeToolEvidence{
				{Name: "search", Source: "mcp", MCPServerName: "fresh", LastScannedAt: now.Add(-5 * time.Minute)},
				{Name: "read", Source: "mcp", MCPServerName: "old", LastScannedAt: now.Add(-2 * time.Hour)},
			},
			want: StateStale,
		},
		{
			name: "one fresh server and one untimestamped server is partial",
			tools: []RuntimeToolEvidence{
				{Name: "search", Source: "mcp", MCPServerName: "fresh", LastScannedAt: now.Add(-5 * time.Minute)},
				{Name: "read", Source: "mcp", MCPServerName: "old"},
			},
			want: StatePartial,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := BuildRuntimeDiagnostics(RuntimeInput{
				RuntimeID: "runtime-1",
				Provider:  "claude",
				Status:    "online",
				Capabilities: map[string]any{
					"discovery_method": "probed",
					"tools":            []string{"Read"},
					"mcp_servers":      []string{"fresh", "old"},
				},
				ProviderObservedAt: now.Add(-5 * time.Minute),
				Tools:              tt.tools,
				Now:                now,
				StaleAfter:         30 * time.Minute,
			})
			if got := report.Diagnostics[1]; got.State != tt.want {
				t.Fatalf("MCP diagnostic = %#v, want %s", got, tt.want)
			}
		})
	}
}

func TestBuildRuntimeDiagnosticsExcludesLegacyMCPRowsFromCurrentInventory(t *testing.T) {
	now := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	report := BuildRuntimeDiagnostics(RuntimeInput{
		RuntimeID: "runtime-1", Provider: "claude", Status: "online",
		Capabilities: map[string]any{
			"discovery_method": "probed",
			"tools":            []string{"Read"},
			"mcp_servers":      []string{"current"},
		},
		ProviderObservedAt: now.Add(-5 * time.Minute),
		Tools: []RuntimeToolEvidence{
			{Name: "search", Source: "mcp", MCPServerName: "current", LastScannedAt: now.Add(-5 * time.Minute)},
			{Name: "legacy", Source: "mcp", MCPServerName: "removed", LastScannedAt: now.Add(-5 * time.Minute)},
		},
		Now: now, StaleAfter: 30 * time.Minute,
	})
	got := report.Diagnostics[1]
	if got.State != StateSuccess || got.Count != 1 || got.Version != Version([]string{"current:search"}) {
		t.Fatalf("MCP diagnostic included deconfigured evidence: %#v", got)
	}
}

func TestBuildConnectionDiagnosticsPinsStableDiscoveryVersion(t *testing.T) {
	now := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	a := BuildConnectionDiagnostics(ConnectionInput{
		ConnectionType: "mcp_http",
		Reachable:      true,
		ToolNames:      []string{"search", "read"},
		Now:            now,
	})
	b := BuildConnectionDiagnostics(ConnectionInput{
		ConnectionType: "mcp_http",
		Reachable:      true,
		ToolNames:      []string{"read", "search"},
		Now:            now,
	})

	if len(a) != 1 || a[0].State != StateSuccess || a[0].Version == "" {
		t.Fatalf("connection diagnostics = %#v", a)
	}
	if a[0].Version != b[0].Version {
		t.Fatalf("version changed with input order: %q != %q", a[0].Version, b[0].Version)
	}
}

func TestBuildTaskDiagnosticsExplainsFrozenCeilingAndObservedDenial(t *testing.T) {
	diagnostics := BuildTaskDiagnostics(TaskInput{
		EnforcementEnabled: false,
		Status:             "active",
		LifecycleState:     "finalized",
		OfferedCount:       8,
		AuthorizedCount:    6,
		Ledger: []DecisionEvidence{{
			ObservedToolName:      "mcp__company-brain__search",
			CanonicalCapabilityID: "connection:company-brain/search",
			Decision:              "deny",
			PolicyDecision:        "deny",
			LegacyPath:            "policy_decision_service",
			ReasonCode:            "policy_denied",
			Reason:                "canonical policy did not allow the capability",
		}},
	})

	assertDiagnostic := func(code Code, state State) Diagnostic {
		t.Helper()
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == code && diagnostic.State == state {
				return diagnostic
			}
		}
		t.Fatalf("missing %s/%s in %#v", code, state, diagnostics)
		return Diagnostic{}
	}
	assertDiagnostic(CodeTaskObservationOnly, StateInfo)
	partial := assertDiagnostic(CodeTaskPartial, StatePartial)
	if partial.SourcePolicy != "Task Mandate" || partial.RecoveryAction == "" {
		t.Fatalf("partial diagnostic = %#v", partial)
	}
	denied := assertDiagnostic(CodeObservedDenial, StateDenied)
	if denied.AffectedCapability != "connection:company-brain/search" || denied.SourcePolicy != "Settings → Permissions" || denied.RecoveryAction == "" {
		t.Fatalf("denial diagnostic = %#v", denied)
	}
}

func TestBuildTaskDiagnosticsReportsDecisionLedgerReadFailure(t *testing.T) {
	diagnostics := BuildTaskDiagnostics(TaskInput{
		EnforcementEnabled: false,
		Status:             "active",
		LifecycleState:     "finalized",
		OfferedCount:       1,
		AuthorizedCount:    1,
		LedgerError:        "database unavailable",
		LedgerUnavailable:  true,
	})
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == CodeDecisionLedgerError && diagnostic.State == StateError {
			found = true
		}
		if diagnostic.Code == CodeTaskDecisionDiagnosticsUnavailable {
			t.Fatalf("specific Decision Ledger error must not emit a duplicate unavailable diagnostic: %#v", diagnostics)
		}
	}
	if !found {
		t.Fatalf("decision ledger error diagnostic missing: %#v", diagnostics)
	}
}

func TestBuildTaskDiagnosticsUsesEnforcedDenialAndExactSource(t *testing.T) {
	tests := []struct {
		name           string
		evidence       DecisionEvidence
		wantCount      int
		wantState      State
		wantSource     string
		wantCapability string
	}{
		{
			name: "policy allow cannot hide a runtime absence denial",
			evidence: DecisionEvidence{
				ObservedToolName:      "mcp__company-brain__search",
				CanonicalCapabilityID: "connection:company-brain/search",
				Decision:              "deny",
				PolicyDecision:        "allow",
				ReasonCode:            "runtime_capability_unavailable",
				Reason:                "canonical capability is absent from the live runtime surface",
			},
			wantCount: 1, wantState: StateDenied, wantSource: "Runtime availability", wantCapability: "connection:company-brain/search",
		},
		{
			name: "task mandate policy error remains a denial",
			evidence: DecisionEvidence{
				ObservedToolName: "add_comment",
				Decision:         "deny",
				PolicyDecision:   "error",
				ReasonCode:       "task_generation_stale",
				Reason:           "task mandate task_generation_stale: The task claim generation is stale.",
			},
			wantCount: 1, wantState: StateDenied, wantSource: "Task Mandate", wantCapability: "add_comment",
		},
		{
			name: "an allowed enforced outcome is not a denial diagnostic",
			evidence: DecisionEvidence{
				ObservedToolName: "add_comment",
				Decision:         "allow",
				PolicyDecision:   "error",
				ReasonCode:       "task_tool_not_authorized",
				Reason:           "task mandate denied the call",
			},
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := BuildTaskDiagnostics(TaskInput{
				EnforcementEnabled: true,
				Status:             "active",
				LifecycleState:     "finalized",
				AuthorizedCount:    1,
				OfferedCount:       1,
				VerdictAllowed:     true,
				Ledger:             []DecisionEvidence{tt.evidence},
			})
			if len(diagnostics) != tt.wantCount {
				t.Fatalf("diagnostics = %#v, want count %d", diagnostics, tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			got := diagnostics[0]
			if got.State != tt.wantState || got.SourcePolicy != tt.wantSource || got.AffectedCapability != tt.wantCapability {
				t.Fatalf("diagnostic = %#v", got)
			}
		})
	}
}

func TestBuildTaskDiagnosticsUsesReasonCodeNotDisplayText(t *testing.T) {
	diagnostics := BuildTaskDiagnostics(TaskInput{
		EnforcementEnabled: true,
		Status:             "active",
		LifecycleState:     "finalized",
		OfferedCount:       1,
		AuthorizedCount:    1,
		VerdictAllowed:     true,
		Ledger: []DecisionEvidence{{
			ObservedToolName: "add_comment",
			Decision:         "deny",
			PolicyDecision:   "deny",
			ReasonCode:       "policy_denied",
			Reason:           "task mandate task_generation_stale appears only in display copy",
		}},
	})

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one denial", diagnostics)
	}
	if diagnostics[0].SourcePolicy != "Settings → Permissions" ||
		diagnostics[0].RecoveryAction != "Review the capability in Settings → Permissions and start a new task after changing access." {
		t.Fatalf("diagnostic classified from display Reason = %#v", diagnostics[0])
	}
}

func TestBuildTaskDiagnosticsMakesLedgerFailureExplicit(t *testing.T) {
	diagnostics := BuildTaskDiagnostics(TaskInput{
		EnforcementEnabled: false,
		Status:             "active",
		LifecycleState:     "finalized",
		OfferedCount:       1,
		AuthorizedCount:    1,
		VerdictAllowed:     true,
		LedgerUnavailable:  true,
	})

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == CodeTaskDecisionDiagnosticsUnavailable && diagnostic.State == StateUnavailable {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v, want explicit decision-ledger unavailability", diagnostics)
	}
}

func TestBuildTaskDiagnosticsMapsTaskVerdictCodesToRecovery(t *testing.T) {
	tests := []struct {
		code     string
		recovery string
	}{
		{code: "task_mandate_missing", recovery: "Retry the task claim and investigate Task Mandate persistence if the snapshot is still missing."},
		{code: "task_mandate_expired", recovery: "Start a new task to receive an active Task Mandate."},
		{code: "task_identity_mismatch", recovery: "Refresh the task context and retry with the matching task, workspace and agent identity."},
		{code: "task_generation_stale", recovery: "Retry the task claim so it can finalize against the current claim generation."},
		{code: "task_tool_not_authorized", recovery: "Review the frozen Task Mandate and start a new task after changing newly allowed access."},
		{code: "task_finalization_conflict", recovery: "Fix the provider inventory mismatch, then retry the task claim."},
		{code: "task_mandate_internal_error", recovery: "Retry the Task Mandate decision and investigate the server error if it repeats."},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			diagnostics := BuildTaskDiagnostics(TaskInput{
				EnforcementEnabled: true,
				Status:             "active",
				LifecycleState:     "finalized",
				OfferedCount:       1,
				AuthorizedCount:    1,
				VerdictAllowed:     true,
				Ledger: []DecisionEvidence{{
					ObservedToolName: "add_comment",
					Decision:         "deny",
					ReasonCode:       tt.code,
					Reason:           "display-only copy",
				}},
			})
			if len(diagnostics) != 1 || diagnostics[0].SourcePolicy != "Task Mandate" || diagnostics[0].RecoveryAction != tt.recovery {
				t.Fatalf("diagnostics = %#v, want Task Mandate recovery %q", diagnostics, tt.recovery)
			}
		})
	}
}
