package accessdecision

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/toolaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

type livePermissionCapabilityLister struct {
	views []capabilityregistry.View
}

func (l livePermissionCapabilityLister) List(
	context.Context,
	pgtype.UUID,
	*capabilityregistry.Subject,
	[]string,
) ([]capabilityregistry.View, error) {
	return l.views, nil
}

// TestEveryRegisteredPermissionRunsThroughTheLiveDecisionContract replaces the
// former hand-maintained status list with behavior. For every engine-owned
// platform permission it writes a real cerebro_tool_policy row, reads the
// Settings/Capabilities projection, and calls the authoritative Policy Decision
// Service. Allow, Ask, Deny and Disable must agree at both surfaces. Lookup
// errors are then injected through the same gate and must deny every key.
//
// Externally managed permissions are not given cosmetic policy rows. Their
// catalog invariant requires a concrete ExternalSecurityOwner instead.
func TestEveryRegisteredPermissionRunsThroughTheLiveDecisionContract(t *testing.T) {
	if accessDecisionTestPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()
	workspaceID := observerUUID(70)
	agentID := observerUUID(71)
	runtimeID := observerUUID(72)
	ownerID := observerUUID(73)

	if _, err := accessDecisionTestPool.Exec(ctx, `
		INSERT INTO workspace (id, name, slug, description, issue_prefix)
		VALUES ($1, 'Permission Contract Test', 'permission-contract-test', '', 'PCT')
		ON CONFLICT (id) DO NOTHING
	`, workspaceID); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = accessDecisionTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	store := toolpolicy.NewStore(accessDecisionTestPool)
	service := NewService(store, nil, nil)
	engineCapabilities := make([]platformcatalog.Capability, 0, len(platformcatalog.All()))
	for _, capability := range platformcatalog.All() {
		if capability.ManagedExternally {
			if capability.ExternalSecurityOwner == "" {
				t.Errorf("%q has no external security owner", capability.Key)
			}
			continue
		}
		if !platformcatalog.Enforced(capability.Key) {
			t.Errorf("%q is engine-owned but not reported as enforced", capability.Key)
			continue
		}
		engineCapabilities = append(engineCapabilities, capability)
	}
	if len(engineCapabilities) == 0 {
		t.Fatal("catalog has no engine-owned permissions to probe")
	}
	capabilityViews := make([]capabilityregistry.View, 0, len(engineCapabilities))
	for _, capability := range engineCapabilities {
		capabilityViews = append(capabilityViews, capabilityregistry.View{
			Key:         capability.Key,
			Title:       capability.Title,
			Description: capability.Description,
			Source:      "builtin",
		})
	}
	agentToolService := toolaccess.New(livePermissionCapabilityLister{views: capabilityViews}, store)

	for _, authored := range []toolpolicy.Setting{
		toolpolicy.SettingAllow,
		toolpolicy.SettingAsk,
		toolpolicy.SettingDeny,
		toolpolicy.SettingDisable,
	} {
		t.Run(string(authored), func(t *testing.T) {
			if _, err := accessDecisionTestPool.Exec(ctx,
				`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1`, workspaceID); err != nil {
				t.Fatalf("clear policy rows: %v", err)
			}
			for _, capability := range engineCapabilities {
				if _, err := store.Set(ctx, toolpolicy.SetParams{
					WorkspaceID: workspaceID,
					ToolKey:     capability.Key,
					Layer:       toolpolicy.LayerWorkspace,
					SubjectID:   workspaceID,
					Setting:     authored,
				}); err != nil {
					t.Fatalf("author %s for %q: %v", authored, capability.Key, err)
				}
			}

			rows, err := store.Table(ctx, toolpolicy.TableQuery{
				WorkspaceID:     workspaceID,
				RuntimeID:       runtimeID,
				AgentID:         agentID,
				UserID:          ownerID,
				IncludePlatform: true,
				Base:            toolpolicy.SettingAllow,
			})
			if err != nil {
				t.Fatalf("read Settings table: %v", err)
			}
			tableRows := map[string]toolpolicy.TableRow{}
			for _, row := range rows {
				if row.Source == platformcatalog.Source && row.ResourcePattern == "" {
					tableRows[row.ToolKey] = row
				}
			}
			agentTools, err := agentToolService.ListEffectiveTools(ctx, toolaccess.Query{
				WorkspaceID:     workspaceID,
				RuntimeID:       runtimeID,
				RuntimeMode:     "cloud",
				RuntimeProvider: "firtal-gateway",
				AgentID:         agentID,
				UserID:          ownerID,
			})
			if err != nil {
				t.Fatalf("read agent tool contract: %v", err)
			}
			agentToolRows := map[string]toolaccess.EffectiveTool{}
			for _, row := range agentTools {
				agentToolRows[row.Descriptor.ToolKey] = row
			}

			for _, capability := range engineCapabilities {
				row, ok := tableRows[capability.Key]
				if !ok {
					t.Errorf("%q missing from Settings/Capabilities projection", capability.Key)
					continue
				}
				if row.Layers[toolpolicy.LayerWorkspace] != authored {
					t.Errorf("%q workspace row = %q, want authored %q",
						capability.Key, row.Layers[toolpolicy.LayerWorkspace], authored)
				}

				entry := service.Decide(ctx, Call{
					WorkspaceID:           workspaceID,
					AgentID:               agentID,
					RuntimeID:             runtimeID,
					OwnerID:               ownerID,
					CanonicalCapabilityID: "platform:" + capability.Key,
					ObservedToolName:      capability.Key,
					EvidenceLevel:         availabilityevidence.LevelVerified,
				})
				wantPolicy := policyDecisionForSetting(row.Effective.Setting)
				if entry.PolicyDecision != wantPolicy {
					t.Errorf("%q Settings=%q but live gate=%q, want %q",
						capability.Key, row.Effective.Setting, entry.PolicyDecision, wantPolicy)
				}
				wantDecision := DecisionDeny
				if wantPolicy == PolicyAllow {
					wantDecision = DecisionAllow
				}
				if entry.Decision != wantDecision {
					t.Errorf("%q live gate decision=%q, want %q for %q",
						capability.Key, entry.Decision, wantDecision, row.Effective.Setting)
				}
				agentTool, ok := agentToolRows[capability.Key]
				if !ok {
					t.Errorf("%q missing from agent tool contract", capability.Key)
					continue
				}
				if agentTool.Policy.Effective != string(row.Effective.Setting) {
					t.Errorf("%q Settings=%q but agent tool contract=%q",
						capability.Key, row.Effective.Setting, agentTool.Policy.Effective)
				}
				if row.Effective.Setting == toolpolicy.SettingDeny && agentTool.ExposureEffective.Effective {
					t.Errorf("%q is denied by the live gate but exposed to the agent", capability.Key)
				}
			}
		})
	}

	t.Run("lookup error", func(t *testing.T) {
		failing := NewService(
			observerPolicy{err: errors.New("policy lookup failed")},
			nil,
			nil,
		)
		for _, capability := range engineCapabilities {
			entry := failing.Decide(ctx, Call{
				WorkspaceID:           workspaceID,
				AgentID:               agentID,
				RuntimeID:             runtimeID,
				OwnerID:               ownerID,
				CanonicalCapabilityID: "platform:" + capability.Key,
				ObservedToolName:      capability.Key,
				EvidenceLevel:         availabilityevidence.LevelVerified,
			})
			if entry.PolicyDecision != PolicyError || entry.Decision != DecisionDeny {
				t.Errorf("%q lookup error = (%q, %q), want error + deny",
					capability.Key, entry.PolicyDecision, entry.Decision)
			}
		}
		_, err := toolaccess.New(
			livePermissionCapabilityLister{views: capabilityViews},
			observerPolicy{err: errors.New("policy lookup failed")},
		).ListEffectiveTools(ctx, toolaccess.Query{
			WorkspaceID: workspaceID,
			RuntimeID:   runtimeID,
			AgentID:     agentID,
			UserID:      ownerID,
		})
		if err == nil {
			t.Fatal("agent tool contract must fail closed when policy lookup fails")
		}
	})
}

func policyDecisionForSetting(setting toolpolicy.Setting) PolicyDecision {
	switch setting {
	case toolpolicy.SettingAllow:
		return PolicyAllow
	case toolpolicy.SettingAsk:
		return PolicyAsk
	case toolpolicy.SettingDeny, toolpolicy.SettingDisable:
		return PolicyDeny
	default:
		return PolicyError
	}
}
