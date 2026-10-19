package wecom

// outbound_sender_route_test.go — the sender-routed inbox push (SHAR-155).
// Comment and mention notifications authored by an AGENT leave from, in
// order: (1) the author's own wecom bot, (2) the issue's project lead's
// wecom bot, (3) the recipient's own notification binding (SHAR-146). Every
// sender-derived rule needs the recipient to be bound inside the chosen
// installation — that is the only address a WeCom bot can reach, and a miss
// falls through rather than forcing a push to a stranger. System messages and
// member-authored items keep the rule-3 address they always had.

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// recipient and workspace are the two ids every one of these tests shares;
// they are deterministic UUIDs so the fake's per-installation map keys are
// stable.
const (
	recipient = "33333333-3333-3333-3333-333333333333"
	workspace = "44444444-4444-4444-4444-444444444444"
	actorID   = "55555555-5555-5555-5555-555555555555"
	issueID   = "66666666-6666-6666-6666-666666666666"
)

// leadBytes is a deterministic, valid-UUID byte pattern for the lead id.
var leadBytes = [16]byte{0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77}

// distinctTestUUIDs parses n distinct installation UUIDs, because mustTestUUID
// always returns the same value — which would collapse two "distinct" bots
// into one and defeat the whole point of this rig.
func distinctTestUUIDs(t *testing.T, n int) []pgtype.UUID {
	t.Helper()
	ids := []string{
		"aaaaaaaa-1111-1111-1111-111111111111",
		"bbbbbbbb-2222-2222-2222-222222222222",
		"cccccccc-3333-3333-3333-333333333333",
		"dddddddd-4444-4444-4444-444444444444",
	}
	out := make([]pgtype.UUID, 0, n)
	for i := 0; i < n; i++ {
		u, err := util.ParseUUID(ids[i])
		if err != nil {
			t.Fatalf("parse installation uuid %d: %v", i, err)
		}
		out = append(out, u)
	}
	return out
}

// routeItem is an inbox:new item for a new_comment notification on an issue,
// authored by whoever actorType names. recipientType is always member: agents
// receive nothing through chat channels. actorType is the in-process shape
// inboxItemToResponse writes — a *string — unless a test passes a plain one to
// cover a JSON round trip.
func routeItem(actorType any) map[string]any {
	return map[string]any{
		"recipient_type": "member",
		"recipient_id":   recipient,
		"workspace_id":   workspace,
		"type":           "new_comment",
		"title":          "SHAR-1",
		"actor_type":     actorType,
		"actor_id":       actorID,
		"issue_id":       issueID,
	}
}

// newOutboundWithConns registers one live recording sender per installation,
// the way a two-bot deployment holds two sockets — so a test can tell which
// bot a push actually left from.
func newOutboundWithConns(t *testing.T, q outboundQueries, installations ...pgtype.UUID) (*Outbound, map[string]*recordingConn) {
	t.Helper()
	reg := newSendersRegistry()
	byInst := make(map[string]*recordingConn)
	for _, id := range installations {
		conn := &recordingConn{}
		reg.set(id, conn.autoAck(newWSSender(conn, nil)))
		byInst[uuidStringPub(id)] = conn
	}
	return NewOutbound(q, reg, nil, slog.Default()), byInst
}

// TestTryDeliverInbox_AgentCommentUsesSenderBot: rule 1. The authoring agent
// owns an active wecom bot and the recipient is bound in it, so the push
// leaves through the sender's bot — the recipient's own binding (rule 3) is
// never consulted. actor_type arrives in-process as a *string (the shape
// inboxItemToResponse writes), and the assertion pins the sender-bot lookup
// ran exactly once while the fallback never did.
func TestTryDeliverInbox_AgentCommentUsesSenderBot(t *testing.T) {
	t.Parallel()
	insts := distinctTestUUIDs(t, 2)
	instSender, instRecipient := insts[0], insts[1]
	q := &fakeOutboundQueries{
		senderInstallation:        db.ChannelInstallation{ID: instSender, Status: string(InstallationActive)},
		installationMemberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:                 db.Workspace{Slug: "acme"},
		memberBinding:              db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_SHOULD_NOT_APPEAR"},
	}
	o, conns := newOutboundWithConns(t, q, instSender, instRecipient)
	item := routeItem(ptrTo("agent"))
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the sender's bot")
	}
	if n := len(conns[uuidStringPub(instRecipient)].sendFrames()); n != 0 {
		t.Errorf("rule-3 (recipient's own binding) received %d frame(s); expected 0 when rule 1 fired", n)
	}
	if n := len(conns[uuidStringPub(instSender)].sendFrames()); n != 1 {
		t.Fatalf("sender bot received %d frame(s); want 1", n)
	}
	body := conns[uuidStringPub(instSender)].sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("chatid = %v, want the recipient's userid inside the sender bot", body["chatid"])
	}
	if q.senderInstallGets != 1 {
		t.Errorf("sender bot lookup ran %d times; want exactly 1", q.senderInstallGets)
	}
	if q.memberBindingGets != 0 {
		t.Errorf("rule-3 binding lookup ran %d times; want 0 when rule 1 fired", q.memberBindingGets)
	}
}

// TestTryDeliverInbox_SenderBotMissFallsToProjectLead: rule 1 misses because
// the recipient has never talked to the sender's bot (the reverse lookup finds
// no binding in it). The issue's project lead is a member with their own
// wecom bot, and the recipient IS bound inside that bot — so the push leaves
// through the lead's bot, and the recipient's own binding is still not
// consulted.
func TestTryDeliverInbox_SenderBotMissFallsToProjectLead(t *testing.T) {
	t.Parallel()
	insts := distinctTestUUIDs(t, 3)
	instSender, instLead, instRecipient := insts[0], insts[1], insts[2]
	q := &fakeOutboundQueries{
		senderInstallation: db.ChannelInstallation{ID: instSender, Status: string(InstallationActive)},
		issueProjectLead: db.GetIssueProjectLeadRow{
			LeadType: pgtype.Text{String: "member", Valid: true},
			LeadID:   pgtype.UUID{Bytes: leadBytes, Valid: true},
		},
		leadBinding:   db.ChannelUserBinding{InstallationID: instLead, ChannelUserID: "T_LEAD"},
		memberBinding: db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_SHOULD_NOT_APPEAR"},
		workspace:     db.Workspace{Slug: "acme"},
		// Rule 1 asks about the sender bot and gets no row; rule 2 asks about
		// the lead's bot and gets a row. perInstallationBindings is how a
		// test expresses "no row here, yes row there" against the shared
		// lookup: instLead has an entry (recipient bound in the lead bot),
		// instSender does not (the default error is ErrNoRows).
		installationMemberErr: pgx.ErrNoRows,
		perInstallationBindings: map[string]db.ChannelUserBinding{
			uuidStringPub(instLead): {InstallationID: instLead, ChannelUserID: "T_USER_1"},
		},
	}
	o, conns := newOutboundWithConns(t, q, instSender, instLead, instRecipient)
	item := routeItem(ptrTo("agent"))
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the project lead's bot")
	}
	if n := len(conns[uuidStringPub(instRecipient)].sendFrames()); n != 0 {
		t.Errorf("rule-3 (recipient's own binding) received %d frame(s); expected 0 when rule 2 fired", n)
	}
	if n := len(conns[uuidStringPub(instSender)].sendFrames()); n != 0 {
		t.Errorf("sender bot received %d frame(s); expected 0 when the recipient is unbound in it", n)
	}
	if n := len(conns[uuidStringPub(instLead)].sendFrames()); n != 1 {
		t.Fatalf("project lead bot received %d frame(s); want 1", n)
	}
	body := conns[uuidStringPub(instLead)].sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("chatid = %v, want the recipient's userid inside the lead bot", body["chatid"])
	}
	if q.projectLeadGets != 1 {
		t.Errorf("project lead lookup ran %d times; want 1", q.projectLeadGets)
	}
	if q.leadBindingGets != 1 {
		t.Errorf("lead binding lookup ran %d times; want 1", q.leadBindingGets)
	}
	if q.memberBindingGets != 0 {
		t.Errorf("rule-3 binding lookup ran %d times; want 0 when rule 2 fired", q.memberBindingGets)
	}
}

// TestTryDeliverInbox_ProjectLeadMissFallsToMemberBinding: rules 1 and 2 both
// miss (the author has no bot; the issue has no project at all —
// pgx.ErrNoRows from the lead lookup), so the push falls back to the
// recipient's own notification binding, the SHAR-146 address.
func TestTryDeliverInbox_ProjectLeadMissFallsToMemberBinding(t *testing.T) {
	t.Parallel()
	instRecipient := mustTestUUID(t)
	q := &fakeOutboundQueries{
		senderInstallErr: pgx.ErrNoRows, // the author has no wecom bot
		issueProjectErr:  pgx.ErrNoRows, // issue has no project
		memberBinding:    db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_USER_1"},
		workspace:        db.Workspace{Slug: "acme"},
	}
	o, conns := newOutboundWithConns(t, q, instRecipient)
	item := routeItem(ptrTo("agent"))
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the recipient's own binding")
	}
	body := conns[uuidStringPub(instRecipient)].sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("chatid = %v, want the recipient's own bound userid", body["chatid"])
	}
	if q.memberBindingGets != 1 {
		t.Errorf("rule-3 binding lookup ran %d times; want 1", q.memberBindingGets)
	}
	if q.projectLeadGets != 1 {
		t.Errorf("project lead lookup ran %d times; want 1 (rule 2 reached it before falling back)", q.projectLeadGets)
	}
}

// TestTryDeliverInbox_AgentLeadDoesNotReRoute: rule 2 only fires when the
// project lead is a MEMBER. An agent lead is skipped, and the push falls
// through to the recipient's own binding. This pins "agent-authored comment
// with an agent project lead → rule 3", which a fork with agent-as-lead
// would otherwise re-route into a second agent bot.
func TestTryDeliverInbox_AgentLeadDoesNotReRoute(t *testing.T) {
	t.Parallel()
	instRecipient := mustTestUUID(t)
	q := &fakeOutboundQueries{
		senderInstallErr: pgx.ErrNoRows,
		issueProjectLead: db.GetIssueProjectLeadRow{
			LeadType: pgtype.Text{String: "agent", Valid: true},
			LeadID:   pgtype.UUID{Bytes: leadBytes, Valid: true},
		},
		memberBinding: db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_USER_1"},
		workspace:     db.Workspace{Slug: "acme"},
	}
	o, conns := newOutboundWithConns(t, q, instRecipient)
	item := routeItem(ptrTo("agent"))
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the recipient's own binding")
	}
	if q.leadBindingGets != 0 {
		t.Errorf("lead binding lookup ran %d times; want 0 for an agent lead", q.leadBindingGets)
	}
	body := conns[uuidStringPub(instRecipient)].sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("chatid = %v, want the recipient's own bound userid", body["chatid"])
	}
}

// TestTryDeliverInbox_ActorTypeAsPlainString: a member is a member whether
// actor_type arrives as the in-process *string or as a plain string after a
// JSON round trip. The string form is the one a relay or external subscriber
// would see, and the guard must read it the same way.
func TestTryDeliverInbox_ActorTypeAsPlainString(t *testing.T) {
	t.Parallel()
	insts := distinctTestUUIDs(t, 2)
	instSender, instRecipient := insts[0], insts[1]
	q := &fakeOutboundQueries{
		senderInstallation:        db.ChannelInstallation{ID: instSender, Status: string(InstallationActive)},
		installationMemberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:                 db.Workspace{Slug: "acme"},
		memberBinding:              db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_SHOULD_NOT_APPEAR"},
	}
	o, conns := newOutboundWithConns(t, q, instSender, instRecipient)
	// actor_type as a plain string — the shape after a JSON round trip.
	item := routeItem("agent")
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the sender's bot")
	}
	if n := len(conns[uuidStringPub(instSender)].sendFrames()); n != 1 {
		t.Fatalf("sender bot received %d frame(s); want 1", n)
	}
	if q.memberBindingGets != 0 {
		t.Errorf("rule-3 binding lookup ran %d times; want 0", q.memberBindingGets)
	}
}

// TestTryDeliverInbox_SystemMessageKeepsMemberBinding: a status-change
// notification keeps the SHAR-146 address even when an agent's bot exists and
// the recipient is bound in it — system messages are not one of the two
// re-routable types, so the sender path is never entered. This is the pin
// behind "status changes keep the existing address".
func TestTryDeliverInbox_SystemMessageKeepsMemberBinding(t *testing.T) {
	t.Parallel()
	insts := distinctTestUUIDs(t, 2)
	instSender, instRecipient := insts[0], insts[1]
	q := &fakeOutboundQueries{
		senderInstallation:        db.ChannelInstallation{ID: instSender, Status: string(InstallationActive)},
		installationMemberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:                 db.Workspace{Slug: "acme"},
		memberBinding:              db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_SELF"},
	}
	o, conns := newOutboundWithConns(t, q, instSender, instRecipient)
	item := map[string]any{
		"recipient_type": "member",
		"recipient_id":   recipient,
		"workspace_id":   workspace,
		"type":           "issue_assigned",
		"title":          "SHAR-9",
		"actor_type":     ptrTo("member"),
		"actor_id":       actorID,
		"issue_id":       issueID,
	}
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the recipient's own binding")
	}
	if q.senderInstallGets != 0 {
		t.Errorf("sender bot lookup ran %d times; want 0 for a system (non-agent) item", q.senderInstallGets)
	}
	if q.projectLeadGets != 0 {
		t.Errorf("project lead lookup ran %d times; want 0 for a system (non-agent) item", q.projectLeadGets)
	}
	body := conns[uuidStringPub(instRecipient)].sendBody(t, 0)
	if body["chatid"] != "T_SELF" {
		t.Errorf("chatid = %v, want the recipient's own bound userid", body["chatid"])
	}
}

// TestTryDeliverInbox_MemberCommentKeepsMemberBinding: a new_comment whose
// actor is a member — not an agent — keeps the SHAR-146 address even though
// the type IS a re-routable one. The actor_type guard is what separates
// "agent authored" from "member authored".
func TestTryDeliverInbox_MemberCommentKeepsMemberBinding(t *testing.T) {
	t.Parallel()
	instRecipient := mustTestUUID(t)
	q := &fakeOutboundQueries{
		memberBinding: db.ChannelUserBinding{InstallationID: instRecipient, ChannelUserID: "T_SELF"},
		workspace:     db.Workspace{Slug: "acme"},
	}
	o, conns := newOutboundWithConns(t, q, instRecipient)
	item := routeItem(ptrTo("member"))
	if !o.tryDeliverInbox(context.Background(), item, recipient, workspace) {
		t.Fatal("tryDeliverInbox returned false; expected delivery via the recipient's own binding")
	}
	if q.senderInstallGets != 0 {
		t.Errorf("sender bot lookup ran %d times; want 0 when the actor is a member", q.senderInstallGets)
	}
	body := conns[uuidStringPub(instRecipient)].sendBody(t, 0)
	if body["chatid"] != "T_SELF" {
		t.Errorf("chatid = %v, want the recipient's own bound userid", body["chatid"])
	}
}

// ptrTo turns a string into a *string — the in-process shape the item map
// carries for actor_type (util.TextToPtr's output).
func ptrTo(s string) *string { return &s }