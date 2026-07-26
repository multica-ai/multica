package accessdecision

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

type observerPolicy struct {
	settings map[[16]byte]toolpolicy.Setting
	err      error
	calls    *int
}

func (p observerPolicy) ResolveGeneral(_ context.Context, q toolpolicy.Query, _ bool) (toolpolicy.Effective, error) {
	if p.calls != nil {
		*p.calls++
	}
	if p.err != nil {
		return toolpolicy.Effective{}, p.err
	}
	return toolpolicy.Effective{Setting: p.settings[q.AgentID.Bytes]}, nil
}

func (observerPolicy) MemberOverrideEnabled(context.Context, pgtype.UUID) bool { return false }

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

func TestObserverRecordsZeroDiffForAgentsWithDifferentPolicies(t *testing.T) {
	allowAgent := observerUUID(1)
	denyAgent := observerUUID(2)
	writer := &observerWriter{}
	observer := NewObserver(
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
			LegacyDecision:        DecisionAllow,
			LegacyPath:            "platform_action",
		},
		{
			WorkspaceID:           observerUUID(9),
			AgentID:               denyAgent,
			RuntimeID:             observerUUID(8),
			CanonicalCapabilityID: "platform:create_issue",
			ObservedToolName:      "create_issue",
			LegacyDecision:        DecisionDeny,
			LegacyPath:            "platform_action",
		},
	} {
		observer.Observe(context.Background(), call)
	}

	if len(writer.entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(writer.entries))
	}
	report := Summarize(writer.entries)
	if report.Total != 2 || report.Diffs != 0 {
		t.Fatalf("report = %+v, want two calls and zero diffs", report)
	}
}

func TestPolicyDecisionServiceAllowsOneAgentAndDeniesAnotherAcrossGatewayFamilies(t *testing.T) {
	allowAgent := observerUUID(1)
	denyAgent := observerUUID(2)
	service := NewObserver(
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
					if entry.ShadowDecision != tt.want {
						t.Fatalf("decision = %q, want %q (%s)", entry.ShadowDecision, tt.want, entry.Reason)
					}
				})
			}
		})
	}
}

func TestObserverFailsClosedOnUnknownCapabilityAndPolicyErrorWithoutAffectingCaller(t *testing.T) {
	writer := &observerWriter{err: errors.New("ledger unavailable")}
	policyCalls := 0
	observer := NewObserver(
		observerPolicy{err: errors.New("policy unavailable"), calls: &policyCalls},
		observerEvidence{},
		writer,
	)

	entry := observer.Observe(context.Background(), Call{
		WorkspaceID:      observerUUID(9),
		AgentID:          observerUUID(1),
		RuntimeID:        observerUUID(8),
		ObservedToolName: "mystery_tool",
		LegacyDecision:   DecisionAllow,
		LegacyPath:       "allow_gate_off",
	})

	if entry.PolicyDecision != PolicyError {
		t.Fatalf("policy decision = %q, want error", entry.PolicyDecision)
	}
	if entry.ShadowDecision != DecisionDeny || !entry.Differs {
		t.Fatalf("shadow result = (%q, %t), want deny and diff", entry.ShadowDecision, entry.Differs)
	}
	if len(writer.entries) != 1 {
		t.Fatalf("writer entries = %d, want 1 best-effort attempt", len(writer.entries))
	}
	if policyCalls != 0 {
		t.Fatalf("policy calls = %d, want 0 for an uncanonicalized tool", policyCalls)
	}
}
