package wecom

// outbound_sender_route_db_test.go — the sender-routed inbox push (SHAR-155)
// against a REAL database. The unit tests in outbound_sender_route_test.go
// prove the routing logic; this proves a real *db.Queries reaches the same
// routes — that rules 1 / 2 / 3 all actually fire when every lookup is
// answered by real SQL, not by a fake. The route tag
// (sender_bot / project_lead / member_binding) is emitted in the delivery log
// lines, which is how each branch is identified.
//
// This is the acceptance step the issue asks for: "构造智能体评论触发通知，从
// server 日志确认推送的 installation 符合三条规则预期选择（含顺延用例）."
// Like the other _db_ tests in this package, it skips when no migrated
// database is reachable.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seededRouteRows is every id the routing can address, after seedRouteRows
// installs a workspace-scoped sender-routing fixture. Three wecom
// installations each hold a recording socket; the recipient starts bound in
// all three; an issue sits in a project with a member lead; and the authoring
// agent owns the bots. The test advances a rule by dropping a binding, so the
// next rule takes the push.
type seededRouteRows struct {
	workspaceID  string
	recipientID  string
	agentID      string
	leadID       string
	agentBotInst string
	leadBotInst  string
	selfBotInst  string
	issueID      string
}

func seedRouteRows(t *testing.T, pool *pgxpool.Pool) seededRouteRows {
	t.Helper()
	ctx := context.Background()
	newID := func() string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			t.Fatalf("gen id: %v", err)
		}
		return id
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	ws := newID()
	recipient := newID()
	agent := newID()
	lead := newID()
	agentBot := newID()
	leadBot := newID()
	selfBot := newID()
	issue := newID()
	proj := newID()
	// Each wecom installation is owned by a distinct agent (the table's PK is
	// workspace_id + agent_id + channel_type), so the three routing branches
	// need three bot-owning agents. Only the first is the comment's author.
	leadBotOwner := newID()
	selfBotOwner := newID()

	exec(`INSERT INTO workspace (id, name, slug) VALUES ($1, 'ws', $2)`, ws, "ws_shar155_"+ws[:8])

	// The authoring agent — the row rule 1's FindActiveChannelInstallationByAgent
	// keys on, and the actor_id the inbox item carries.
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_config, visibility, status)
	      VALUES ($1, $2, 'SHAR-155 author', 'local', '{}', 'private', 'idle')`,
		agent, ws)
	// The two other bot owners: one for the lead's bot, one for the recipient's
	// own bot. They are names on installations, never actors.
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_config, visibility, status)
	      VALUES ($1, $2, 'SHAR-155 lead bot', 'local', '{}', 'private', 'idle')`,
		leadBotOwner, ws)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_config, visibility, status)
	      VALUES ($1, $2, 'SHAR-155 self bot', 'local', '{}', 'private', 'idle')`,
		selfBotOwner, ws)

	// Three wecom installations, one per routing branch, each owned by its own
	// agent. The recipient starts bound in all three.
	bots := []struct{ inst, owner string }{
		{agentBot, agent},
		{leadBot, leadBotOwner},
		{selfBot, selfBotOwner},
	}
	for _, b := range bots {
		exec(`INSERT INTO channel_installation
		        (id, workspace_id, agent_id, channel_type, status, config, installer_user_id)
		      VALUES ($1, $2, $3, 'wecom', 'active', '{}', $4)`,
			b.inst, ws, b.owner, recipient)
		exec(`INSERT INTO channel_user_binding
		        (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
		      VALUES ($1, $2, $3, 'wecom', $4)`,
			ws, recipient, b.inst, "T_USER_"+b.inst[:8])
	}
	// The project lead is a member with a binding of their own, in the lead
	// bot's installation — that binding is what rule 2 uses as its transport.
	exec(`INSERT INTO channel_user_binding
	        (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
	      VALUES ($1, $2, $3, 'wecom', $4)`,
		ws, lead, leadBot, "T_LEAD_"+leadBot[:8])

	// The issue and its project, so GetIssueProjectLead has a real row. The
	// project's lead is a member; the recipient holds their binding in
	// lead_bot above, which is what makes rule 2 able to deliver to them.
	exec(`INSERT INTO project
	        (id, workspace_id, title, lead_type, lead_id)
	      VALUES ($1, $2, 'SHAR-155 project', 'member', $3)`,
		proj, ws, lead)
	exec(`INSERT INTO issue
	        (id, workspace_id, title, status, creator_type, creator_id, project_id)
	      VALUES ($1, $2, 'SHAR-155', 'todo', 'member', $3, $4)`,
		issue, ws, recipient, proj)

	return seededRouteRows{
		workspaceID:  ws,
		recipientID:  recipient,
		agentID:      agent,
		leadID:       lead,
		agentBotInst: agentBot,
		leadBotInst:  leadBot,
		selfBotInst:  selfBot,
		issueID:      issue,
	}
}

// commentItemFor builds the inbox:new item map the way
// inboxItemToResponse (cmd/server) would for a new_comment authored by an
// agent, carrying the id fields the routing reads.
func commentItemFor(r seededRouteRows, issueID string) map[string]any {
	return map[string]any{
		"recipient_type": "member",
		"recipient_id":   r.recipientID,
		"workspace_id":   r.workspaceID,
		"type":           "new_comment",
		"title":          "SHAR-155",
		"issue_id":       issueID,
		"actor_type":     "agent",
		"actor_id":       r.agentID,
	}
}

// TestSenderRouteAgainstRealDB drives the sender-routed push through real SQL
// in three cases and asserts which installation delivered, by reading the
// route tag out of the delivery log. Each case drops the recipient's binding
// from the previous case's installation, so the next rule is the one that
// can deliver.
func TestSenderRouteAgainstRealDB(t *testing.T) {
	t.Parallel()
	pool := twoReplicaDB(t)
	r := seedRouteRows(t, pool)

	var agentInst, leadInst, selfInst pgtype.UUID
	mustScanInstallation(t, &agentInst, r.agentBotInst)
	mustScanInstallation(t, &leadInst, r.leadBotInst)
	mustScanInstallation(t, &selfInst, r.selfBotInst)

	// One Outbound per installation, each with its own socket and its own log
	// buffer, all against the real db.Queries. The route tag a push names in
	// its log is what this test asserts on.
	agentConn, agentLogs, agentO := newRouteReplica(pool, agentInst)
	leadConn, leadLogs, leadO := newRouteReplica(pool, leadInst)
	selfConn, selfLogs, selfO := newRouteReplica(pool, selfInst)

	item := commentItemFor(r, r.issueID)
	ctx := context.Background()

	// Case 1 — rule 1 fires. The recipient is bound in the agent bot, so that
	// is the bot a sender-routed push leaves from. No other socket is involved
	// in this case's delivery, and the log must name route=sender_bot.
	if !agentO.tryDeliverInbox(ctx, item, r.recipientID, r.workspaceID) {
		t.Fatalf("rule-1 push returned false; expected delivery through the sender bot")
	}
	if n := agentConn.frameCount(); n != 1 {
		t.Errorf("sender bot got %d frame(s); want 1", n)
	}
	if !strings.Contains(agentLogs.String(), "route=sender_bot") {
		t.Errorf("delivery log did not name route=sender_bot:\n%s", agentLogs.String())
	}

	// Case 2 — rule 1 misses, rule 2 fires. Strip the recipient's binding from
	// the agent bot so the reverse lookup in it now returns no row; the
	// recipient is still bound in the lead bot, which is the bot rule 2 names.
	// The log must say route=project_lead.
	dropBinding(t, pool, r.recipientID, r.agentBotInst)
	if !leadO.tryDeliverInbox(ctx, item, r.recipientID, r.workspaceID) {
		t.Fatalf("rule-2 push returned false; expected delivery through the lead's bot")
	}
	if n := leadConn.frameCount(); n != 1 {
		t.Errorf("lead bot got %d frame(s); want 1", n)
	}
	if !strings.Contains(leadLogs.String(), "route=project_lead") {
		t.Errorf("delivery log did not name route=project_lead:\n%s", leadLogs.String())
	}

	// Case 3 — rules 1 and 2 both miss, rule 3 fires. Strip the recipient from
	// the lead bot as well; the only remaining address is their own binding.
	// The log must say route=member_binding.
	dropBinding(t, pool, r.recipientID, r.leadBotInst)
	if !selfO.tryDeliverInbox(ctx, item, r.recipientID, r.workspaceID) {
		t.Fatalf("rule-3 push returned false; expected delivery through the recipient's own bot")
	}
	if n := selfConn.frameCount(); n != 1 {
		t.Errorf("self bot got %d frame(s); want 1", n)
	}
	if !strings.Contains(selfLogs.String(), "route=member_binding") {
		t.Errorf("delivery log did not name route=member_binding:\n%s", selfLogs.String())
	}
}

// newRouteReplica wires one recording socket + one log buffer + one Outbound
// against the real db.Queries for one installation.
func newRouteReplica(pool *pgxpool.Pool, inst pgtype.UUID) (*recordingConn, *strings.Builder, *Outbound) {
	reg := newSendersRegistry()
	conn := &recordingConn{}
	reg.set(inst, conn.autoAck(newWSSender(conn, nil)))
	logs := &strings.Builder{}
	// Register is what production does with the out-of-band bus; tryDeliverInbox
	// is called directly in the test, so Register is exercised here only to keep
	// the wiring shape honest.
	o := NewOutbound(db.New(pool), reg, nil,
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	bus := events.New()
	o.Register(bus)
	return conn, logs, o
}

// mustScanInstallation decodes a hex installation id into pgtype.UUID.
func mustScanInstallation(t *testing.T, dst *pgtype.UUID, id string) {
	t.Helper()
	if err := dst.Scan(id); err != nil {
		t.Fatalf("scan installation %s: %v", id, err)
	}
}

// dropBinding removes one (recipient, installation) pair's binding — the way a
// test advances a rule from "bound here" to "no row", so the next rule takes
// the push.
func dropBinding(t *testing.T, pool *pgxpool.Pool, multicaUserID, installationID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`DELETE FROM channel_user_binding
	      WHERE multica_user_id = $1 AND installation_id = $2`,
		multicaUserID, installationID); err != nil {
		t.Fatalf("drop binding: %v", err)
	}
}
