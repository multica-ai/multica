package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// chatActionTestSecret signs the tokens the tests press. The handler under test
// verifies with the same secret, as the server does with the JWT secret.
const chatActionTestSecret = "channel-action-test-secret"

// chatActionEnv is a proposal in the shared test workspace, a chat binding for
// one sender, and the handler under test. Each test adds the senders it needs.
type chatActionEnv struct {
	Proposal    ProposalResponse
	Installs    string
	Actions     *ChannelActions
	Signer      *service.ActionSigner
	WorkspaceID string
}

func newChatActionEnv(t *testing.T) chatActionEnv {
	t.Helper()
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "chat button", nil)

	prevURL := testHandler.cfg.AppURL
	testHandler.cfg.AppURL = "https://app.example"
	t.Cleanup(func() { testHandler.cfg.AppURL = prevURL })

	return chatActionEnv{
		Proposal:    p,
		Installs:    uuid.NewString(),
		Actions:     NewChannelActions(testHandler, service.NewActionSigner([]byte(chatActionTestSecret))),
		Signer:      service.NewActionSigner([]byte(chatActionTestSecret)),
		WorkspaceID: testWorkspaceID,
	}
}

// bindChatSender links a platform user to a member of ws through the installation.
// It returns the platform user id the adapter would report.
func bindChatSender(t *testing.T, installationID, ws, userID string) string {
	t.Helper()
	channelUserID := "tg-" + uuid.NewString()
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": userID,
		"installation_id": installationID,
		"channel_type":    "telegram",
		"channel_user_id": channelUserID,
	})
	return channelUserID
}

func (e chatActionEnv) press(t *testing.T, installationID, channelUserID, data string) channel.PressResult {
	t.Helper()
	res, err := e.Actions.Press(context.Background(), channel.Press{
		Sender: channel.Sender{ChannelType: "telegram", InstallationID: chatUUID(t, installationID), ChannelUserID: channelUserID},
		Data:   data,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	return res
}

func chatUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e chatActionEnv) token(t *testing.T, proposalID, action string) string {
	t.Helper()
	id, err := util.ParseUUID(proposalID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := e.Signer.Sign(id, action)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func proposalSnoozedUntil(t *testing.T, proposalID string) *time.Time {
	t.Helper()
	var at pgtype.Timestamptz
	dbfx.QueryRow(t, `SELECT snoozed_until FROM proposal WHERE id = $1`, proposalID).Scan(&at)
	if !at.Valid {
		return nil
	}
	return &at.Time
}

func proposalDecidedAt(t *testing.T, proposalID string) pgtype.Timestamptz {
	t.Helper()
	var at pgtype.Timestamptz
	dbfx.QueryRow(t, `SELECT decided_at FROM proposal WHERE id = $1`, proposalID).Scan(&at)
	return at
}

func TestChannelPress_ApproveDecidesAndAReplayChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, env.Proposal.ID, service.ProposalDecisionApprove)

	first := env.press(t, env.Installs, sender, raw)
	if !first.Settled || !strings.HasPrefix(first.Reply, "Approved:") {
		t.Fatalf("first press = %+v, want an approval that settles the decision", first)
	}
	if got := proposalState(t, env.Proposal.ID); got != "approved" {
		t.Fatalf("state = %s, want approved", got)
	}
	decidedAt := proposalDecidedAt(t, env.Proposal.ID)

	second := env.press(t, env.Installs, sender, raw)
	if !second.Settled || !strings.Contains(second.Reply, "Already decided") {
		t.Fatalf("replay = %+v, want already decided", second)
	}
	if got := proposalState(t, env.Proposal.ID); got != "approved" {
		t.Fatalf("replay changed the state to %s", got)
	}
	if again := proposalDecidedAt(t, env.Proposal.ID); !again.Time.Equal(decidedAt.Time) {
		t.Fatalf("replay moved decided_at from %v to %v", decidedAt.Time, again.Time)
	}
}

func TestChannelPress_RejectDecidesWithoutANote(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)

	res := env.press(t, env.Installs, sender, env.token(t, p.ID, service.ProposalDecisionReject))
	if !res.Settled || !strings.HasPrefix(res.Reply, "Rejected:") {
		t.Fatalf("press = %+v, want a rejection", res)
	}
	if got := proposalState(t, p.ID); got != "rejected" {
		t.Fatalf("state = %s, want rejected", got)
	}
}

func TestChannelPress_SnoozeIsSevenDaysAndAReplayKeepsTheDate(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, p.ID, service.ProposalDecisionSnooze)

	before := time.Now()
	res := env.press(t, env.Installs, sender, raw)
	if res.Toast == "" || !strings.Contains(res.Reply, "Snoozed until") {
		t.Fatalf("snooze press = %+v", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("a snooze keeps the proposal pending, got %s", got)
	}
	until := proposalSnoozedUntil(t, p.ID)
	if until == nil {
		t.Fatal("snoozed_until is not set")
	}
	if d := until.Sub(before); d < service.ActionSnoozeDays*24*time.Hour-time.Minute || d > service.ActionSnoozeDays*24*time.Hour+time.Minute {
		t.Fatalf("snoozed for %v, want seven days", d)
	}

	again := env.press(t, env.Installs, sender, raw)
	if !strings.Contains(again.Reply, "Already snoozed") {
		t.Fatalf("second snooze press = %+v, want already snoozed", again)
	}
	if moved := proposalSnoozedUntil(t, p.ID); !moved.Equal(*until) {
		t.Fatalf("second press moved the snooze from %v to %v", *until, *moved)
	}
}

func TestChannelPress_ExpiredTokenIsRefusedWithAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)

	// Issued 25 hours ago: the 24-hour window has closed.
	old := service.NewActionSigner([]byte(chatActionTestSecret)).WithClock(func() time.Time { return time.Now().Add(-25 * time.Hour) })
	id, err := util.ParseUUID(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := old.Sign(id, service.ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}

	res := env.press(t, env.Installs, sender, raw)
	if res.Settled || !strings.Contains(res.Reply, "expired") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("expired press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("an expired press changed the state to %s", got)
	}
}

func TestChannelPress_TamperedTokenChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, p.ID, service.ProposalDecisionApprove)

	// Change the action byte to reject and keep the signature: the MAC no longer
	// matches the body, so the press is refused.
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	data[1] = 2
	forged := base64.RawURLEncoding.EncodeToString(data)
	res := env.press(t, env.Installs, sender, forged)
	if res.Settled || !strings.Contains(res.Toast, "not valid") {
		t.Fatalf("tampered press = %+v, want a refusal", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("a tampered press changed the state to %s", got)
	}
}

func TestChannelPress_ActionForARevisingProposalIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	reviseProposal(t, p.ID, "Narrow the ask.")
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)

	res := env.press(t, env.Installs, sender, env.token(t, p.ID, service.ProposalDecisionApprove))
	if res.Settled || !strings.Contains(res.Reply, "revising") {
		t.Fatalf("press on a revising proposal = %+v, want a refusal", res)
	}
	if got := proposalState(t, p.ID); got != "revising" {
		t.Fatalf("state = %s, want revising", got)
	}
}

func TestChannelPress_OnlyOwnersAndAdminsDecide(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	member := newWorkspaceMemberUser(t, "member")
	memberChat := bindChatSender(t, env.Installs, env.WorkspaceID, member)

	res := env.press(t, env.Installs, memberChat, env.token(t, p.ID, service.ProposalDecisionReject))
	if res.Settled || !strings.Contains(res.Reply, "owner or admin") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("member press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("a member's press changed the state to %s", got)
	}

	admin := newWorkspaceMemberUser(t, "admin")
	adminChat := bindChatSender(t, env.Installs, env.WorkspaceID, admin)
	if res := env.press(t, env.Installs, adminChat, env.token(t, p.ID, service.ProposalDecisionReject)); !res.Settled {
		t.Fatalf("admin press = %+v, want the rejection to go through", res)
	}
}

func TestChannelPress_AgentActorIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, owner)

	res, err := env.Actions.Press(context.Background(), channel.Press{
		Sender: channel.Sender{ChannelType: "telegram", InstallationID: chatUUID(t, env.Installs), ChannelUserID: sender, IsBot: true},
		Data:   env.token(t, p.ID, service.ProposalDecisionApprove),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Settled || !strings.Contains(res.Reply, "Agents cannot") {
		t.Fatalf("bot press = %+v, want an agent refusal", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("an agent's press changed the state to %s", got)
	}
}

func TestChannelPress_UnlinkedSenderIsRefusedWithAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal

	res := env.press(t, env.Installs, "nobody-"+uuid.NewString(), env.token(t, p.ID, service.ProposalDecisionApprove))
	if res.Settled || !strings.Contains(res.Reply, "Link your Multica account") || !strings.Contains(res.Reply, "https://app.example") {
		t.Fatalf("unlinked press = %+v, want the link prompt with the app link", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("an unlinked press changed the state to %s", got)
	}
}

// A binding whose member has left the workspace is refused, even though the
// binding row still exists (binding existence does not prove membership).
func TestChannelPress_RemovedMemberIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	gone := dbfx.User(t, "Gone", fmt.Sprintf("chat-gone-%s@example.com", uuid.NewString()))
	sender := bindChatSender(t, env.Installs, env.WorkspaceID, gone)

	res := env.press(t, env.Installs, sender, env.token(t, p.ID, service.ProposalDecisionApprove))
	if res.Settled || !strings.Contains(res.Reply, "no longer a member") {
		t.Fatalf("removed member press = %+v, want a refusal", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("a removed member's press changed the state to %s", got)
	}
}

// The proposal belongs to the test workspace; the sender is an owner of another.
// The press must find nothing there and change nothing, here or there.
func TestChannelPress_ProposalInAnotherWorkspaceChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	otherWS := dbfx.Workspace(t, "Chat other "+uuid.NewString(), "chat-other-"+uuid.NewString())
	owner := dbfx.User(t, "Other owner", fmt.Sprintf("chat-other-owner-%s@example.com", uuid.NewString()))
	dbfx.Member(t, otherWS, owner, "owner")
	sender := bindChatSender(t, env.Installs, otherWS, owner)

	res := env.press(t, env.Installs, sender, env.token(t, p.ID, service.ProposalDecisionApprove))
	if res.Settled || !strings.Contains(res.Reply, "not in your workspace") {
		t.Fatalf("cross-workspace press = %+v, want not found", res)
	}
	if got := proposalState(t, p.ID); got != "pending" {
		t.Fatalf("a cross-workspace press changed the state to %s", got)
	}
}

func TestChannelActionKeyGroupsButDoesNotVerify(t *testing.T) {
	env := newChatActionEnv(t)
	p := env.Proposal
	approve := env.token(t, p.ID, service.ProposalDecisionApprove)
	snooze := env.token(t, p.ID, service.ProposalDecisionSnooze)

	if key := env.Actions.ActionKey(approve); key != p.ID {
		t.Fatalf("ActionKey(approve) = %q, want the proposal id %q", key, p.ID)
	}
	if env.Actions.ActionKey(approve) != env.Actions.ActionKey(snooze) {
		t.Fatal("two buttons of one decision must share a key")
	}
	if key := env.Actions.ActionKey("not-a-token"); key != "" {
		t.Fatalf("ActionKey of junk = %q, want empty", key)
	}
}
