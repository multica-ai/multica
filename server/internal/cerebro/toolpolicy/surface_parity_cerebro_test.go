package toolpolicy

// Surface-parity guard (FIR-3403). The per-agent capabilities card and the
// claim-time agent brief are two READ surfaces over the SAME tool-policy store,
// reached through two different Store methods:
//
//   - card  → Store.Table  (handler.agentCapabilityRows → CapabilityToolPolicy.Table,
//             read per TableRow.Effective.Setting)
//   - brief → Store.Resolve (toolaccess.Service.ListEffectiveTools → policy.Resolve,
//             read per Effective.Setting)
//
// Both are authored with Base = Allow and, in the enforced default configuration
// (member-override off — MemberOverrideEnabled resolves a missing workspace flag
// row to OFF), both fold the chain tighten-only. If a future refactor made either
// method diverge from the other for the same seeded context, an agent would see
// one verdict on its capabilities card and a different one in its brief for the
// exact same tool. This test pins that they agree per tool for one seeded agent.

import (
	"context"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
)

func TestEightSpecialPermissionsMatchTableExplainAndCallResolver(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()
	agent, user := uuidByte(3), tpTestUserID

	addCap(t, s, "tools:personal-browser", "Personal browser", "Tools", "builtin")
	addCap(t, s, "tools:test-as-user", "Test as user", "Tools", "builtin")

	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID:     tpTestWorkspaceID,
		AgentID:         agent,
		UserID:          user,
		Base:            SettingAllow,
		IncludePlatform: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]Setting{
		"hooks:read":                 SettingAllow,
		"hooks:write":                SettingDeny,
		"hooks:enforce":              SettingDeny,
		"hooks:manage_managed":       SettingDeny,
		"tools:personal-browser":     SettingDeny,
		"tools:test-as-user":         SettingDeny,
		"manage_workspace_overrides": SettingDeny,
		"manage_group_overrides":     SettingDeny,
	}
	for key, expected := range want {
		row, ok := findRow(rows, key)
		if !ok {
			t.Errorf("table is missing %q", key)
			continue
		}
		call, err := s.ResolvePermission(ctx, Query{
			WorkspaceID: tpTestWorkspaceID,
			ToolKey:     key,
			AgentID:     agent,
			UserID:      user,
			Base:        SettingAllow,
		}, platformaccess.Actor{Authenticated: true, Agent: true})
		if err != nil {
			t.Errorf("resolve %s: %v", key, err)
			continue
		}
		if row.Effective.Setting != expected || call.Setting != expected {
			t.Errorf("%s table=%q call=%q, want %q", key, row.Effective.Setting, call.Setting, expected)
		}
	}
}

// TestCardBriefSurfaceParity seeds one agent with an Allow/Ask/Deny/base spread
// and asserts Store.Table (card) and Store.Resolve (brief) return the same
// verdict for every tool.
func TestCardBriefSurfaceParity(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent, user := uuidByte(3), tpTestUserID

	// One capability per verdict shape the two surfaces must agree on.
	addCap(t, s, "web_fetch", "Fetch web page", "Network", "builtin")           // agent Ask
	addCap(t, s, "slack.post_message", "Post Slack message", "Slack", "scan")   // agent Allow, user Deny -> Deny
	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")             // no rows -> base Allow
	addCap(t, s, "gogcli_sheets_write", "Write Google Sheet", "Sheets", "scan") // workspace Deny
	addCap(t, s, "credential_list", "List credentials", "Secrets", "builtin")   // agent Deny

	set := func(tool string, layer Layer, subject any, setting Setting) {
		t.Helper()
		var subjectID = tpTestWorkspaceID
		switch v := subject.(type) {
		case string:
			if v == "agent" {
				subjectID = agent
			} else if v == "user" {
				subjectID = user
			}
		}
		if _, err := s.Set(ctx, SetParams{
			WorkspaceID: tpTestWorkspaceID,
			ToolKey:     tool,
			Layer:       layer,
			SubjectID:   subjectID,
			Setting:     setting,
		}); err != nil {
			t.Fatalf("set %s @ %s = %s: %v", tool, layer, setting, err)
		}
	}

	set("web_fetch", LayerAgent, "agent", SettingAsk)
	set("slack.post_message", LayerAgent, "agent", SettingAllow)
	set("slack.post_message", LayerUser, "user", SettingDeny)
	set("gogcli_sheets_write", LayerWorkspace, "workspace", SettingDeny)
	set("credential_list", LayerAgent, "agent", SettingDeny)

	// Card surface: the whole table read model, one row per tool.
	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID,
		AgentID:     agent,
		UserID:      user,
		Base:        SettingAllow,
	})
	if err != nil {
		t.Fatalf("card Table: %v", err)
	}

	// The five seeded tools plus the expected verdict both surfaces must report.
	want := map[string]Setting{
		"web_fetch":           SettingAsk,
		"slack.post_message":  SettingDeny,
		"add_comment":         SettingAllow,
		"gogcli_sheets_write": SettingDeny,
		"credential_list":     SettingDeny,
	}

	for tool, expected := range want {
		cardRow, ok := findRow(rows, tool)
		if !ok {
			t.Fatalf("card Table is missing tool %q", tool)
		}

		// Brief surface: the per-tool resolver the claim brief calls, same context.
		briefEff, err := s.Resolve(ctx, Query{
			WorkspaceID: tpTestWorkspaceID,
			ToolKey:     tool,
			AgentID:     agent,
			UserID:      user,
			Base:        SettingAllow,
		})
		if err != nil {
			t.Fatalf("brief Resolve %q: %v", tool, err)
		}

		if cardRow.Effective.Setting != expected {
			t.Errorf("tool %q: card verdict = %q, want %q", tool, cardRow.Effective.Setting, expected)
		}
		if briefEff.Setting != expected {
			t.Errorf("tool %q: brief verdict = %q, want %q", tool, briefEff.Setting, expected)
		}
		if cardRow.Effective.Setting != briefEff.Setting {
			t.Errorf("surface drift on %q: card=%q brief=%q — the two read surfaces disagree",
				tool, cardRow.Effective.Setting, briefEff.Setting)
		}
	}
}

// TestTableGeneralGateSurfaceParityWithMemberOverride pins the production
// configuration that TestCardBriefSurfaceParity deliberately does not cover:
// the member-override flag is ON. The Settings > Permissions table and the
// call-time general gate must use the same openable resolution mode, both when
// an Agent Allow opens a Workspace Deny and when a User Deny closes it again.
func TestTableGeneralGateSurfaceParityWithMemberOverride(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent, user := uuidByte(31), tpTestUserID
	const tool = "web_fetch"
	addCap(t, s, tool, "Fetch web page", "Network", "builtin")

	if err := s.q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: tpTestWorkspaceID,
		FlagKey:     FlagMemberOverride,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("enable member override: %v", err)
	}
	t.Cleanup(func() {
		_ = s.q.DeleteCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: tpTestWorkspaceID,
			FlagKey:     FlagMemberOverride,
		})
	})

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		Layer:       LayerWorkspace,
		SubjectID:   tpTestWorkspaceID,
		Setting:     SettingDeny,
	}); err != nil {
		t.Fatalf("set workspace deny: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		Layer:       LayerAgent,
		SubjectID:   agent,
		Setting:     SettingAllow,
	}); err != nil {
		t.Fatalf("set agent allow: %v", err)
	}

	assertParity := func(want Setting) {
		t.Helper()
		rows, err := s.Table(ctx, TableQuery{
			WorkspaceID: tpTestWorkspaceID,
			AgentID:     agent,
			UserID:      user,
			Base:        SettingAllow,
		})
		if err != nil {
			t.Fatalf("table: %v", err)
		}
		row, ok := findRow(rows, tool)
		if !ok {
			t.Fatalf("table is missing %q", tool)
		}
		gate, err := s.ResolveGeneral(ctx, Query{
			WorkspaceID: tpTestWorkspaceID,
			ToolKey:     tool,
			AgentID:     agent,
			UserID:      user,
			Base:        SettingAllow,
		}, s.MemberOverrideEnabled(ctx, tpTestWorkspaceID))
		if err != nil {
			t.Fatalf("general gate: %v", err)
		}
		if row.Effective.Setting != want || gate.Setting != want {
			t.Fatalf("table=%q gate=%q, want %q", row.Effective.Setting, gate.Setting, want)
		}
		if row.Effective.Setting != gate.Setting {
			t.Fatalf("surface drift: Settings table=%q call-time gate=%q", row.Effective.Setting, gate.Setting)
		}
	}

	// Workspace Deny is a default in openable mode, so Agent Allow opens it.
	assertParity(SettingAllow)

	// A member's own Deny remains the ceiling and blocks that same Agent Allow.
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		Layer:       LayerUser,
		SubjectID:   user,
		Setting:     SettingDeny,
	}); err != nil {
		t.Fatalf("set user deny: %v", err)
	}
	assertParity(SettingDeny)
}
