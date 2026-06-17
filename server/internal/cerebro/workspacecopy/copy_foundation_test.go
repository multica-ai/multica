package workspacecopy

// Integration tests for the TECH-3742 foundation + per-item extensions, run
// against the same real Postgres as store_test.go (shared TestMain fixture).
// Each test seeds minimal source rows, copies, and asserts: the copy lands
// against the live schema, the source is untouched, and a re-run is idempotent.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func uuidString(id pgtype.UUID) string { return uuid.UUID(id.Bytes).String() }
func contains(s, sub string) bool      { return strings.Contains(s, sub) }

// makeTargetWS creates a throwaway target workspace with its own issue counter,
// so a test that copies issues does not perturb the shared tgtWS counter (which
// other tests assert absolute numbers against). The workspace is dropped on
// cleanup; its FK cascades clear everything copied into it.
func makeTargetWS(ctx context.Context, t *testing.T, prefix string) pgtype.UUID {
	t.Helper()
	id := newID()
	slug := "wscopy-" + uuidString(id)[:8]
	if _, err := testPool.Exec(ctx, `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1,$2,$3,$4)`,
		id, "WSCopy "+prefix, slug, prefix); err != nil {
		t.Fatalf("create target workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

// seedMember inserts a member row for testUser in the given workspace, so
// member-scoped subjects (role assignments, grants) can be remapped by user.
func seedMember(ctx context.Context, t *testing.T, ws pgtype.UUID) pgtype.UUID {
	t.Helper()
	id := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (id, workspace_id, user_id, role) VALUES ($1,$2,$3,'member')`,
		id, ws, testUser); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return id
}

func TestCopyFoundation_LabelsSkillsRolesGroupsSettings(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	// members in both workspaces so member-scoped grants remap by user_id.
	srcMember := seedMember(ctx, t, srcWS)
	seedMember(ctx, t, tgtWS)

	// label
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_label (id, workspace_id, name, color) VALUES ($1,$2,'foundation-label','#abc')`,
		newID(), srcWS); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	// skill + version + file
	srcSkill := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO skill (id, workspace_id, name, description, content, config, approver_ids, current_version, metadata)
		VALUES ($1,$2,'foundation-skill','desc','# body','{}','{}','1.0.0','{}')`,
		srcSkill, srcWS); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO skill_version (id, skill_id, version, content, files, description) VALUES ($1,$2,'1.0.0','# body','{}','v')`,
		newID(), srcSkill); err != nil {
		t.Fatalf("seed skill version: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO skill_file (id, skill_id, path, content) VALUES ($1,$2,'ref.md','content')`,
		newID(), srcSkill); err != nil {
		t.Fatalf("seed skill file: %v", err)
	}
	// role (cerebro_role) + a role-scoped workspace grant + a member role
	// assignment (cerebro_role_assignment, subject_type='member').
	srcRole := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO cerebro_role (id, workspace_id, name, description) VALUES ($1,$2,'reviewer','can review')`,
		srcRole, srcWS); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_workspace_grant (id, workspace_id, subject_type, subject_id, resource_pattern, capability)
		VALUES ($1,$2,'role',$3,'issue/*','issue.review')`, newID(), srcWS, srcRole); err != nil {
		t.Fatalf("seed role grant: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_at) VALUES ($1,'member',$2,now())`, srcRole, srcMember); err != nil {
		t.Fatalf("seed role assignment: %v", err)
	}
	// group + capability + member (cerebro_group_member is by user_id)
	srcGroup := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO cerebro_group (id, workspace_id, name) VALUES ($1,$2,'eng')`, srcGroup, srcWS); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO cerebro_group_capability (group_id, capability, granted_at) VALUES ($1,'create_agent',now())`, srcGroup); err != nil {
		t.Fatalf("seed group capability: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO cerebro_group_member (group_id, user_id, added_at) VALUES ($1,$2,now())`, srcGroup, testUser); err != nil {
		t.Fatalf("seed group member: %v", err)
	}
	// settings
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_workspace_settings (workspace_id, display_currency, updated_at, wakeup_max_self_per_issue, wakeup_min_interval_minutes)
		VALUES ($1,'DKK',now(),5,15)`, srcWS); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	res, err := s.CopyFoundation(ctx, newID(), srcWS, tgtWS)
	if err != nil {
		t.Fatalf("CopyFoundation: %v", err)
	}
	if res.Labels < 1 || res.Skills < 1 || res.SkillVersions < 1 || res.SkillFiles < 1 || res.Roles < 1 || res.Groups < 1 || res.Settings < 1 {
		t.Fatalf("foundation counts too low: %+v", res)
	}

	// target has the copied skill with its version + file
	var tgtSkill pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM skill WHERE workspace_id = $1 AND name = 'foundation-skill'`, tgtWS).Scan(&tgtSkill); err != nil {
		t.Fatalf("load target skill: %v", err)
	}
	var vCount, fCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM skill_version WHERE skill_id = $1`, tgtSkill).Scan(&vCount)
	testPool.QueryRow(ctx, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, tgtSkill).Scan(&fCount)
	if vCount != 1 || fCount != 1 {
		t.Fatalf("target skill children: versions=%d files=%d, want 1/1", vCount, fCount)
	}

	// target role carries its grant; role assignment remapped to target member
	var grantCount int
	var tgtRole pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM cerebro_role WHERE workspace_id = $1 AND name = 'reviewer'`, tgtWS).Scan(&tgtRole); err != nil {
		t.Fatalf("load target role: %v", err)
	}
	testPool.QueryRow(ctx, `SELECT count(*) FROM cerebro_workspace_grant WHERE workspace_id = $1 AND subject_type = 'role' AND subject_id = $2 AND capability = 'issue.review'`, tgtWS, tgtRole).Scan(&grantCount)
	if grantCount != 1 {
		t.Fatalf("target role grants = %d, want 1", grantCount)
	}
	var raCount int
	testPool.QueryRow(ctx, `
		SELECT count(*) FROM cerebro_role_assignment ra
		JOIN member m ON m.id = ra.subject_id AND m.workspace_id = $1
		WHERE ra.role_id = $2 AND ra.subject_type = 'member'`, tgtWS, tgtRole).Scan(&raCount)
	if raCount != 1 {
		t.Fatalf("target role assignment = %d, want 1 (remapped by user)", raCount)
	}

	// settings copied
	var currency string
	if err := testPool.QueryRow(ctx, `SELECT display_currency FROM cerebro_workspace_settings WHERE workspace_id = $1`, tgtWS).Scan(&currency); err != nil {
		t.Fatalf("load target settings: %v", err)
	}
	if currency != "DKK" {
		t.Fatalf("target currency = %q, want DKK", currency)
	}

	// idempotent: a second run adds no new skill
	if _, err := s.CopyFoundation(ctx, newID(), srcWS, tgtWS); err != nil {
		t.Fatalf("second CopyFoundation: %v", err)
	}
	var skillCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM skill WHERE workspace_id = $1 AND name = 'foundation-skill'`, tgtWS).Scan(&skillCount)
	if skillCount != 1 {
		t.Fatalf("target skill count after re-run = %d, want 1 (no duplicate)", skillCount)
	}

	// source untouched
	var srcSkillCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM skill WHERE workspace_id = $1 AND name = 'foundation-skill'`, srcWS).Scan(&srcSkillCount)
	if srcSkillCount != 1 {
		t.Fatalf("source skill count = %d, want 1 (source mutated)", srcSkillCount)
	}
}

func TestCopyConnectionsAndCredentials(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	srcConn := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_connection (id, workspace_id, name, display_name, type, url, internal, auth_config, endpoint_permissions, enabled, tools)
		VALUES ($1,$2,'shopify','Shopify','mcp_http','https://x',false,'{}','{}',true,'{}')`,
		srcConn, srcWS); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	srcCred := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_credential (id, workspace_id, type, name, description, encrypted_value, value_hint, metadata, created_by_type, created_by_id)
		VALUES ($1,$2,'api_key','shopify-key','','\x00','***','{}','member',$3)`,
		srcCred, srcWS, testUser); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_credential_binding (id, credential_id, resource_type, resource_id, created_by_type, created_by_id)
		VALUES ($1,$2,'connection',$3,'member',$4)`,
		newID(), srcCred, srcConn, testUser); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	if _, err := s.CopyFoundation(ctx, newID(), srcWS, tgtWS); err != nil {
		t.Fatalf("CopyFoundation: %v", err)
	}

	var tgtConn pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM workspace_connection WHERE workspace_id = $1 AND name = 'shopify'`, tgtWS).Scan(&tgtConn); err != nil {
		t.Fatalf("load target connection: %v", err)
	}
	var tgtCred pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM cerebro_credential WHERE workspace_id = $1 AND name = 'shopify-key'`, tgtWS).Scan(&tgtCred); err != nil {
		t.Fatalf("load target credential: %v", err)
	}
	// binding remapped to the copied connection
	var boundTo pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT resource_id FROM cerebro_credential_binding WHERE credential_id = $1`, tgtCred).Scan(&boundTo); err != nil {
		t.Fatalf("load target binding: %v", err)
	}
	if boundTo != tgtConn {
		t.Fatalf("binding resource_id = %v, want copied connection %v", boundTo, tgtConn)
	}
}

// TestCopyGitHubInstallation_GloballyUniqueSkipped documents the truthful
// contract: a github_installation.installation_id is globally unique, so an
// installation still linked to the source cannot be duplicated into the target.
// The foundation copy skips it (no error, no target row, source untouched); the
// installation is reconnected in the target after the merge, like a runtime.
func TestCopyGitHubInstallation_GloballyUniqueSkipped(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO github_installation (id, workspace_id, installation_id, account_login, account_type)
		VALUES ($1,$2,$3,'firtal-group','Organization')`,
		newID(), srcWS, int64(998877)); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	res, err := s.CopyFoundation(ctx, newID(), srcWS, tgtWS)
	if err != nil {
		t.Fatalf("CopyFoundation: %v", err)
	}
	if res.GitHub != 0 {
		t.Fatalf("github linked = %d, want 0 (installation still held by source, cannot duplicate)", res.GitHub)
	}
	// no target row, source row intact
	var tgtCount, srcCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM github_installation WHERE workspace_id = $1 AND installation_id = $2`, tgtWS, int64(998877)).Scan(&tgtCount)
	testPool.QueryRow(ctx, `SELECT count(*) FROM github_installation WHERE workspace_id = $1 AND installation_id = $2`, srcWS, int64(998877)).Scan(&srcCount)
	if tgtCount != 0 || srcCount != 1 {
		t.Fatalf("github rows tgt=%d src=%d, want 0/1", tgtCount, srcCount)
	}
}

func TestCopyAgentSkillBinding_FoundationThenAgent(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	runID := newID()

	// skill in source + foundation copy carries it to the target.
	srcSkill := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO skill (id, workspace_id, name, description, content, config, approver_ids, current_version, metadata)
		VALUES ($1,$2,'bound-skill','d','c','{}','{}','1.0.0','{}')`,
		srcSkill, srcWS); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	srcAgent := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode, owner_id) VALUES ($1,$2,'BindBot','local',$3)`,
		srcAgent, srcWS, testUser); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO agent_skill (agent_id, skill_id, created_at) VALUES ($1,$2,now())`, srcAgent, srcSkill); err != nil {
		t.Fatalf("seed agent_skill: %v", err)
	}

	if _, err := s.CopyFoundation(ctx, runID, srcWS, tgtWS); err != nil {
		t.Fatalf("CopyFoundation: %v", err)
	}
	agentRes, err := s.CopyAgent(ctx, runID, tgtWS, srcAgent, conflictSkip)
	if err != nil {
		t.Fatalf("CopyAgent: %v", err)
	}
	var tgtSkill pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM skill WHERE workspace_id = $1 AND name = 'bound-skill'`, tgtWS).Scan(&tgtSkill); err != nil {
		t.Fatalf("load target skill: %v", err)
	}
	var bound int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentRes.TargetID, tgtSkill).Scan(&bound)
	if bound != 1 {
		t.Fatalf("copied agent skill binding = %d, want 1 (remapped to copied skill)", bound)
	}
}

func TestCopyNote_TravelsWithArtifact(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)

	// a workspace-scoped artifact that is a note (cerebro_note extension + version).
	srcArt := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO artifact (id, workspace_id, kind, title, body, author_type, author_id)
		VALUES ($1,$2,'note','Standup note','# notes','member',$3)`,
		srcArt, srcWS, testUser); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_note (artifact_id, owner_id, visibility, pinned) VALUES ($1,$2,'workspace',true)`,
		srcArt, testUser); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_note_version (id, note_id, version_no, title, body, byte_size, reason, author_type, author_id)
		VALUES ($1,$2,1,'Standup note','# notes',7,'manual','member',$3)`,
		newID(), srcArt, testUser); err != nil {
		t.Fatalf("seed note version: %v", err)
	}

	if _, err := s.CopyWorkspaceArtifacts(ctx, newID(), srcWS, tgtWS); err != nil {
		t.Fatalf("CopyWorkspaceArtifacts: %v", err)
	}

	var tgtArt pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM artifact WHERE workspace_id = $1 AND title = 'Standup note'`, tgtWS).Scan(&tgtArt); err != nil {
		t.Fatalf("load target artifact: %v", err)
	}
	var pinned bool
	if err := testPool.QueryRow(ctx, `SELECT pinned FROM cerebro_note WHERE artifact_id = $1`, tgtArt).Scan(&pinned); err != nil {
		t.Fatalf("note extension not copied: %v", err)
	}
	if !pinned {
		t.Fatalf("copied note pinned=%v, want true", pinned)
	}
	var vCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM cerebro_note_version WHERE note_id = $1`, tgtArt).Scan(&vCount)
	if vCount != 1 {
		t.Fatalf("copied note versions = %d, want 1", vCount)
	}
}

func TestCopyIssue_WakeupsAndReminders(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	runID := newID()
	localTgt := makeTargetWS(ctx, t, "WK")

	// agent copied first (the fixed merge order), so a wakeup's agent remaps.
	srcAgent := newID()
	if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode, owner_id) VALUES ($1,$2,'WakeBot','local',$3)`,
		srcAgent, srcWS, testUser); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	agentRes, err := s.CopyAgent(ctx, runID, localTgt, srcAgent, conflictSkip)
	if err != nil {
		t.Fatalf("CopyAgent: %v", err)
	}

	srcIssue := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
		VALUES ($1,$2,'Has wakeup','todo','none','issue','member',$3,42,1.0)`,
		srcIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_agent_wakeup (id, workspace_id, agent_id, issue_id, prompt, trigger_type, fire_at, state, consecutive_postpones)
		VALUES ($1,$2,$3,$4,'wake up','time',now()+interval '1 day','pending',0)`,
		newID(), srcWS, srcAgent, srcIssue); err != nil {
		t.Fatalf("seed wakeup: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_issue_date_reminder (issue_id, kind, reminder_date) VALUES ($1,'due',current_date+1)`,
		srcIssue); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}

	res, err := s.CopyIssue(ctx, runID, localTgt, srcIssue)
	if err != nil {
		t.Fatalf("CopyIssue: %v", err)
	}
	if res.Wakeups != 1 || res.Reminders != 1 {
		t.Fatalf("issue extras: wakeups=%d reminders=%d, want 1/1", res.Wakeups, res.Reminders)
	}
	// the copied wakeup points at the copied agent + copied issue, pending state.
	var wAgent, wIssue pgtype.UUID
	var wState string
	if err := testPool.QueryRow(ctx, `SELECT agent_id, issue_id, state FROM cerebro_agent_wakeup WHERE issue_id = $1`, res.TargetID).
		Scan(&wAgent, &wIssue, &wState); err != nil {
		t.Fatalf("load copied wakeup: %v", err)
	}
	if wAgent != agentRes.TargetID || wIssue != res.TargetID || wState != "pending" {
		t.Fatalf("copied wakeup agent=%v issue=%v state=%q, want copied agent/issue/pending", wAgent, wIssue, wState)
	}
}

func TestRewriteReferences_AgentInstructions(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	runID := newID()
	localTgt := makeTargetWS(ctx, t, "FIR")

	// source issue number 71 (TECH prefix); copy it → target gets a fresh number.
	srcIssue := newID()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (id, workspace_id, title, status, priority, kind, creator_type, creator_id, number, position)
		VALUES ($1,$2,'Referenced','todo','none','issue','member',$3,71,1.0)`,
		srcIssue, srcWS, testUser); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issueRes, err := s.CopyIssue(ctx, runID, localTgt, srcIssue)
	if err != nil {
		t.Fatalf("CopyIssue: %v", err)
	}

	// agent whose instructions reference TECH-71 and the issue mention link.
	srcUUID := uuidString(srcIssue)
	srcAgent := newID()
	instr := "Follow up on TECH-71 — see [link](mention://issue/" + srcUUID + ")."
	if _, err := testPool.Exec(ctx, `INSERT INTO agent (id, workspace_id, name, runtime_mode, owner_id, instructions) VALUES ($1,$2,'RefBot','local',$3,$4)`,
		srcAgent, srcWS, testUser, instr); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := s.CopyAgent(ctx, runID, localTgt, srcAgent, conflictSkip); err != nil {
		t.Fatalf("CopyAgent: %v", err)
	}

	rew, err := s.RewriteInternalReferences(ctx, localTgt)
	if err != nil {
		t.Fatalf("RewriteInternalReferences: %v", err)
	}
	if rew.AgentsRewritten < 1 {
		t.Fatalf("agents rewritten = %d, want >= 1", rew.AgentsRewritten)
	}
	var tgtAgent pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 AND name = 'RefBot'`, localTgt).Scan(&tgtAgent); err != nil {
		t.Fatalf("load target agent: %v", err)
	}
	var gotInstr string
	testPool.QueryRow(ctx, `SELECT instructions FROM agent WHERE id = $1`, tgtAgent).Scan(&gotInstr)
	wantToken := fmt.Sprintf("FIR-%d", issueRes.TargetNumber)
	if !contains(gotInstr, wantToken) {
		t.Fatalf("agent instructions = %q, want it to contain %q", gotInstr, wantToken)
	}
	if !contains(gotInstr, "mention://issue/"+uuidString(issueRes.TargetID)) {
		t.Fatalf("agent instructions mention not rewritten to target uuid: %q", gotInstr)
	}
}
