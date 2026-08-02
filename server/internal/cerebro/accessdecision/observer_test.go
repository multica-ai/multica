package accessdecision

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

type observerPolicy struct {
	settings map[[16]byte]toolpolicy.Setting
	declared *toolpolicy.Setting
	err      error
	calls    *int
}

func (p observerPolicy) ResolvePermission(_ context.Context, q toolpolicy.Query, _ platformaccess.Actor) (toolpolicy.Effective, error) {
	if p.calls != nil {
		*p.calls++
	}
	if p.err != nil {
		return toolpolicy.Effective{}, p.err
	}
	if p.declared != nil {
		return toolpolicy.Effective{Setting: *p.declared}, nil
	}
	return toolpolicy.Effective{Setting: p.settings[q.AgentID.Bytes]}, nil
}

// CEREBRO-PATCH(permission-contract-test-resolver): keep the cross-surface permission test on the full resolver contract.
func (p observerPolicy) Resolve(ctx context.Context, q toolpolicy.Query) (toolpolicy.Effective, error) {
	return p.ResolvePermission(ctx, q, platformaccess.Actor{Authenticated: true, Agent: q.AgentID.Valid})
}

type observerEvidence map[string]availabilityevidence.Evidence

func (e observerEvidence) Lookup(capabilityID string, rt availabilityevidence.RuntimeType) availabilityevidence.Evidence {
	if found, ok := e[capabilityID]; ok {
		found.RuntimeType = rt
		return found
	}
	return availabilityevidence.Evidence{
		CapabilityID: capabilityID,
		RuntimeType:  rt,
		Level:        availabilityevidence.LevelDeclared,
		Reason:       "no evidence",
	}
}

type observerWriter struct {
	entries []Entry
	err     error
}

type observerMandates struct {
	calls int
	err   error
}

func (m *observerMandates) Authorize(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, string) error {
	m.calls++
	return m.err
}

func (w *observerWriter) Append(_ context.Context, entry Entry) error {
	w.entries = append(w.entries, entry)
	return w.err
}

func observerUUID(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Valid = true
	id.Bytes[0] = b
	return id
}

func TestDecisionServiceRecordsCanonicalOutcomesForDifferentAgents(t *testing.T) {
	allowAgent := observerUUID(1)
	denyAgent := observerUUID(2)
	writer := &observerWriter{}
	service := NewService(
		observerPolicy{settings: map[[16]byte]toolpolicy.Setting{
			allowAgent.Bytes: toolpolicy.SettingAllow,
			denyAgent.Bytes:  toolpolicy.SettingDeny,
		}},
		observerEvidence{
			"platform:create_issue": {
				Level: availabilityevidence.LevelVerified,
			},
		},
		writer,
	)

	for _, call := range []Call{
		{
			WorkspaceID:           observerUUID(9),
			AgentID:               allowAgent,
			RuntimeID:             observerUUID(8),
			CanonicalCapabilityID: "platform:create_issue",
			ObservedToolName:      "create_issue",
		},
		{
			WorkspaceID:           observerUUID(9),
			AgentID:               denyAgent,
			RuntimeID:             observerUUID(8),
			CanonicalCapabilityID: "platform:create_issue",
			ObservedToolName:      "create_issue",
		},
	} {
		service.Decide(context.Background(), call)
	}

	if len(writer.entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(writer.entries))
	}
	if writer.entries[0].Decision != DecisionAllow || writer.entries[1].Decision != DecisionDeny {
		t.Fatalf("recorded decisions = (%q, %q), want allow and deny",
			writer.entries[0].Decision, writer.entries[1].Decision)
	}
}

func TestPolicyDecisionServiceAllowsOneAgentAndDeniesAnotherAcrossGatewayFamilies(t *testing.T) {
	allowAgent := observerUUID(1)
	denyAgent := observerUUID(2)
	service := NewService(
		observerPolicy{settings: map[[16]byte]toolpolicy.Setting{
			allowAgent.Bytes: toolpolicy.SettingAllow,
			denyAgent.Bytes:  toolpolicy.SettingDeny,
		}},
		nil,
		&observerWriter{},
	)

	capabilities := map[string]string{
		"platform":       "platform:get_issue",
		"web_fetch":      "platform:web_fetch",
		"create_issue":   "platform:create_issue",
		"memory":         "platform:memory_recall",
		"api_connection": "api-connection:infisical-admin:GET:/secrets:infisical_admin__get_secrets",
		"mcp_connection": "mcp-connection:customer_service:draft_reply",
	}
	for family, capabilityID := range capabilities {
		t.Run(family, func(t *testing.T) {
			for _, tt := range []struct {
				name  string
				agent pgtype.UUID
				want  Decision
			}{
				{name: "allow agent", agent: allowAgent, want: DecisionAllow},
				{name: "deny agent", agent: denyAgent, want: DecisionDeny},
			} {
				t.Run(tt.name, func(t *testing.T) {
					entry := service.Decide(context.Background(), Call{
						WorkspaceID:           observerUUID(9),
						AgentID:               tt.agent,
						RuntimeID:             observerUUID(8),
						CanonicalCapabilityID: capabilityID,
						ObservedToolName:      family,
						EvidenceLevel:         availabilityevidence.LevelDiscovered,
					})
					if entry.Decision != tt.want {
						t.Fatalf("decision = %q, want %q (%s)", entry.Decision, tt.want, entry.Reason)
					}
				})
			}
		})
	}
}

func TestPolicyDecisionServiceUsesDeclaredPermissionContract(t *testing.T) {
	allow := toolpolicy.SettingAllow
	agent := observerUUID(1)
	service := NewService(
		observerPolicy{
			settings: map[[16]byte]toolpolicy.Setting{agent.Bytes: toolpolicy.SettingDeny},
			declared: &allow,
		},
		nil,
		nil,
	)

	entry := service.Decide(context.Background(), Call{
		WorkspaceID:           observerUUID(9),
		AgentID:               agent,
		RuntimeID:             observerUUID(8),
		CanonicalCapabilityID: "platform:hooks:read",
		ObservedToolName:      "hooks:read",
		EvidenceLevel:         availabilityevidence.LevelVerified,
	})
	if entry.PolicyDecision != PolicyAllow || entry.Decision != DecisionAllow {
		t.Fatalf("declared contract decision = %+v, want allow", entry)
	}
}

func TestDecisionServiceTaskMandateCircuitBreaker(t *testing.T) {
	allow := toolpolicy.SettingAllow
	call := Call{
		WorkspaceID:           observerUUID(9),
		AgentID:               observerUUID(1),
		TaskID:                observerUUID(7),
		CanonicalCapabilityID: "platform:web_fetch",
		ObservedToolName:      "web_fetch",
		EvidenceLevel:         availabilityevidence.LevelVerified,
	}

	t.Run("default off preserves the policy decision", func(t *testing.T) {
		mandates := &observerMandates{err: errors.New("task mandate missing")}
		service := NewService(observerPolicy{declared: &allow}, nil, nil).WithMandates(mandates)

		entry := service.Decide(context.Background(), call)

		if entry.Decision != DecisionAllow || mandates.calls != 0 {
			t.Fatalf("default-off decision = %q and calls = %d, want allow and zero mandate calls", entry.Decision, mandates.calls)
		}
	})

	t.Run("explicit on enforces the Task Mandate", func(t *testing.T) {
		mandates := &observerMandates{err: errors.New("task mandate missing")}
		service := NewService(observerPolicy{declared: &allow}, nil, nil).
			WithMandates(mandates).
			WithMandateEnforcement(func(context.Context, pgtype.UUID) bool { return true })

		entry := service.Decide(context.Background(), call)

		if entry.Decision != DecisionDeny || mandates.calls != 1 {
			t.Fatalf("enforced decision = %q and calls = %d, want deny and one mandate call", entry.Decision, mandates.calls)
		}
	})
}

func TestDecisionServiceFailsClosedOnUnknownCapabilityAndPolicyError(t *testing.T) {
	writer := &observerWriter{err: errors.New("ledger unavailable")}
	policyCalls := 0
	service := NewService(
		observerPolicy{err: errors.New("policy unavailable"), calls: &policyCalls},
		observerEvidence{},
		writer,
	)

	entry := service.Decide(context.Background(), Call{
		WorkspaceID:      observerUUID(9),
		AgentID:          observerUUID(1),
		RuntimeID:        observerUUID(8),
		ObservedToolName: "mystery_tool",
	})

	if entry.PolicyDecision != PolicyError {
		t.Fatalf("policy decision = %q, want error", entry.PolicyDecision)
	}
	if entry.Decision != DecisionDeny {
		t.Fatalf("decision result = %q, want deny", entry.Decision)
	}
	if len(writer.entries) != 1 {
		t.Fatalf("writer entries = %d, want 1 best-effort attempt", len(writer.entries))
	}
	if policyCalls != 0 {
		t.Fatalf("policy calls = %d, want 0 for an uncanonicalized tool", policyCalls)
	}
}

func TestDecisionServiceFailsClosedOnCanonicalPolicyLookupError(t *testing.T) {
	policyCalls := 0
	service := NewService(
		observerPolicy{err: errors.New("policy unavailable"), calls: &policyCalls},
		nil,
		&observerWriter{},
	)

	entry := service.Decide(context.Background(), Call{
		WorkspaceID:           observerUUID(9),
		AgentID:               observerUUID(1),
		RuntimeID:             observerUUID(8),
		CanonicalCapabilityID: "platform:web_fetch",
		ObservedToolName:      "web_fetch",
		EvidenceLevel:         availabilityevidence.LevelDiscovered,
	})

	if policyCalls != 1 {
		t.Fatalf("policy calls = %d, want one canonical lookup", policyCalls)
	}
	if entry.PolicyDecision != PolicyError || entry.Decision != DecisionDeny {
		t.Fatalf("lookup error = policy %q decision %q, want error/deny",
			entry.PolicyDecision, entry.Decision)
	}
}

func TestDisabledPermissionIsAClosedDecision(t *testing.T) {
	if got := policyDecisionFromSetting(toolpolicy.SettingDisable); got != PolicyDeny {
		t.Fatalf("disabled permission = %q, want deny", got)
	}
}
