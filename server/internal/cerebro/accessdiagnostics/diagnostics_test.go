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
