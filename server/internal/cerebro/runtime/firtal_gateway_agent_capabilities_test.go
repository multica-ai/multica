package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
)

type capabilitiesCardProvider struct {
	card handler.AgentCapabilities
}

func (p capabilitiesCardProvider) BuildAgentCapabilitiesCard(context.Context, pgtype.UUID) (handler.AgentCapabilities, error) {
	return p.card, nil
}

type rejectingTaskMandates struct {
	rejected map[string]bool
}

func (rejectingTaskMandates) Issue(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, []string, time.Time) error {
	return nil
}

func (m rejectingTaskMandates) Authorize(_ context.Context, _, _, _ pgtype.UUID, tool string) error {
	if m.rejected[tool] {
		return errors.New("tool is outside the issued task mandate")
	}
	return nil
}

func TestGetAgentCapabilitiesKeepsPlatformPermissionsAndAppliesConnectionTaskMandate(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	tool := FirtalGetAgentCapabilitiesTool{
		provider: capabilitiesCardProvider{card: handler.AgentCapabilities{Tools: []handler.AgentCapabilityTool{
			{Key: "allowed_tool", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
			{Key: "rejected_tool", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
		}, Connections: []handler.AgentCapabilityConnection{
			{
				Name: "atlas-mcp",
				Tools: []handler.AgentCapabilityConnTool{
					{Name: "getViewerStatus", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
				},
			},
			{
				Name: "company-brain",
				Tools: []handler.AgentCapabilityConnTool{
					{Name: "whoami", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
				},
			},
			{
				Name: "infisical-admin",
				Endpoints: []handler.AgentCapabilityConnEndpoint{
					{Path: "/secrets", Methods: []string{"GET"}, Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
				},
			},
		}}},
		tctx: ToolContext{
			AgentID:                id,
			WorkspaceID:            id,
			TaskID:                 id,
			TaskMandateEnforcement: func(context.Context, pgtype.UUID) bool { return true },
			TaskMandates: rejectingTaskMandates{rejected: map[string]bool{
				"rejected_tool":                true,
				"mcp__company-brain__whoami":   true,
				"infisical_admin__get_secrets": true,
			}},
		},
	}

	raw, err := tool.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	var card handler.AgentCapabilities
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}
	if got := card.Tools[0].Permission; got != "allow" {
		t.Errorf("allowed tool permission = %q, want allow", got)
	}
	if got := card.Tools[1].Permission; got != "allow" {
		t.Errorf("platform tool permission = %q, want allow", got)
	}
	if !card.Tools[1].Allowed || !card.Tools[1].Callable || card.Tools[1].BlockedReason != "" {
		t.Fatalf("task mandate changed a platform permission after the FIR-4076 rollback: %+v", card.Tools[1])
	}
	if got := card.Connections[0].Tools[0].Permission; got != "allow" {
		t.Errorf("mandate-allowed connection tool permission = %q, want allow", got)
	}
	rejectedConnectionTool := card.Connections[1].Tools[0]
	if rejectedConnectionTool.Permission != "deny" || rejectedConnectionTool.Allowed || rejectedConnectionTool.Callable || rejectedConnectionTool.BlockedReason == "" {
		t.Fatalf("rejected connection tool left a positive or unexplained verdict: %+v", rejectedConnectionTool)
	}
	rejectedEndpoint := card.Connections[2].Endpoints[0]
	if rejectedEndpoint.Permission != "deny" || rejectedEndpoint.Allowed || rejectedEndpoint.Callable || rejectedEndpoint.BlockedReason == "" || rejectedEndpoint.HowToFix == "" {
		t.Fatalf("rejected API endpoint left a positive or unexplained verdict: %+v", rejectedEndpoint)
	}
}

// The agent must receive the same result in its self-service Capabilities card
// that the gateway returns when it tries the call: a mandate-denied API endpoint
// is visible as blocked with a remedy, and the executor rejects it before any
// connection dispatch can happen.
func TestTaskMandateDenialMatchesCapabilitiesAndGatewayCall(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	const endpoint = "infisical_admin__get_secrets"
	mandates := rejectingTaskMandates{rejected: map[string]bool{endpoint: true}}
	lookup := FirtalGetAgentCapabilitiesTool{
		provider: capabilitiesCardProvider{card: handler.AgentCapabilities{Connections: []handler.AgentCapabilityConnection{{
			Name: "infisical-admin",
			Endpoints: []handler.AgentCapabilityConnEndpoint{{
				Path: "/secrets", Methods: []string{"GET"}, Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true,
			}},
		}}}},
		tctx: ToolContext{AgentID: id, WorkspaceID: id, TaskID: id, TaskMandates: mandates, TaskMandateEnforcement: func(context.Context, pgtype.UUID) bool { return true }},
	}

	raw, err := lookup.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("capabilities lookup: %v", err)
	}
	var card handler.AgentCapabilities
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("decode capabilities card: %v", err)
	}
	capability := card.Connections[0].Endpoints[0]
	if capability.Permission != "deny" || capability.Allowed || capability.Callable || capability.BlockedReason == "" || capability.HowToFix == "" {
		t.Fatalf("Capabilities must expose the mandate denial: %+v", capability)
	}

	reg := NewRegistry(nil)
	reg.Register(&APIConnectionTool{toolName: endpoint, connName: "infisical-admin", method: "GET", path: "/secrets"})
	executor := (&FirtalGatewayExecutor{
		taskMandateEnforcement: func(context.Context, pgtype.UUID) bool { return true },
	}).SetTaskMandates(mandates)
	allowed, reason := executor.guardToolCall(context.Background(), id, id, endpoint, nil, reg, GatewayRequestMeta{TaskID: util.UUIDToString(id)})
	if allowed || !strings.Contains(reason, "outside the issued task mandate") {
		t.Fatalf("gateway call = (%v, %q), want the same mandate denial", allowed, reason)
	}
}

func TestGetAgentCapabilitiesTaskMandateOffPreservesAllResolvedPermissions(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	mandates := rejectingTaskMandates{rejected: map[string]bool{
		"mcp__company-brain__allowed": true,
		"mcp__company-brain__denied":  true,
	}}
	lookup := FirtalGetAgentCapabilitiesTool{
		provider: capabilitiesCardProvider{card: handler.AgentCapabilities{Connections: []handler.AgentCapabilityConnection{{
			Name: "company-brain",
			Tools: []handler.AgentCapabilityConnTool{
				{Name: "allowed", Permission: "allow", Allowed: true, Available: true, Enforced: true, Callable: true},
				{Name: "denied", Permission: "deny", Allowed: false, Available: true, Enforced: true, Callable: false, BlockedReason: "Tool Policy denied the capability"},
			},
		}}}},
		tctx: ToolContext{
			AgentID: id, WorkspaceID: id, TaskID: id, TaskMandates: mandates,
			TaskMandateEnforcement: func(context.Context, pgtype.UUID) bool { return false },
		},
	}

	raw, err := lookup.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("capabilities lookup: %v", err)
	}
	var card handler.AgentCapabilities
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("decode capabilities card: %v", err)
	}
	allowed := card.Connections[0].Tools[0]
	if allowed.Permission != "allow" || !allowed.Allowed || !allowed.Callable || allowed.BlockedReason != "" {
		t.Fatalf("Task Mandate off changed Tool Policy Allow: %+v", allowed)
	}
	denied := card.Connections[0].Tools[1]
	if denied.Permission != "deny" || denied.Allowed || denied.Callable || denied.BlockedReason != "Tool Policy denied the capability" {
		t.Fatalf("Task Mandate off changed Tool Policy Deny: %+v", denied)
	}
}
