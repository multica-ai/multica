package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// The Slack press runs through the same ChannelActions as Telegram. These tests
// check the Slack sender reaches the same rules: the binding is a slack binding,
// the presser must be an owner or admin, an agent is refused, and a token is
// verified before anything changes.

// bindSlackSender links a Slack user id to a member of ws through the
// installation. It returns the Slack user id the adapter would report.
func bindSlackSender(t *testing.T, installationID, ws, userID string) string {
	t.Helper()
	slackUserID := "U" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": userID,
		"installation_id": installationID,
		"channel_type":    "slack",
		"channel_user_id": slackUserID,
	})
	return slackUserID
}

// pressSlack is one press from a Slack user through the installation.
func (e chatActionEnv) pressSlack(t *testing.T, installationID, slackUserID, data string, isBot bool) channel.PressResult {
	t.Helper()
	res, err := e.Actions.Press(context.Background(), channel.Press{
		Sender: channel.Sender{
			ChannelType:    "slack",
			InstallationID: chatUUID(t, installationID),
			ChannelUserID:  slackUserID,
			IsBot:          isBot,
		},
		Data: data,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	return res
}

func TestChannelPressSlack_OwnerDecidesAndAReplayChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, env.Proposal.ID, service.ProposalDecisionApprove)

	first := env.pressSlack(t, env.Installs, sender, raw, false)
	if !first.Settled || !strings.HasPrefix(first.Reply, "Approved:") {
		t.Fatalf("owner press = %+v, want an approval that settles the decision", first)
	}
	if got := proposalState(t, env.Proposal.ID); got != "approved" {
		t.Fatalf("state = %s, want approved", got)
	}
	decidedAt := proposalDecidedAt(t, env.Proposal.ID)

	second := env.pressSlack(t, env.Installs, sender, raw, false)
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

func TestChannelPressSlack_AdminDecides(t *testing.T) {
	env := newChatActionEnv(t)
	admin := newWorkspaceMemberUser(t, "admin")
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, admin)

	res := env.pressSlack(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionReject), false)
	if !res.Settled || !strings.HasPrefix(res.Reply, "Rejected:") {
		t.Fatalf("admin press = %+v, want the rejection", res)
	}
}

func TestChannelPressSlack_MemberIsRefusedWithTheAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	member := newWorkspaceMemberUser(t, "member")
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, member)

	res := env.pressSlack(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionReject), false)
	if res.Settled || !strings.Contains(res.Reply, "owner or admin") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("member press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a member's press changed the state to %s", got)
	}
}

func TestChannelPressSlack_AgentIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, owner)

	res := env.pressSlack(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionApprove), true)
	if res.Settled || !strings.Contains(res.Reply, "Agents cannot") {
		t.Fatalf("bot press = %+v, want an agent refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("an agent's press changed the state to %s", got)
	}
}

func TestChannelPressSlack_ExpiredTokenIsRefusedWithTheAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, owner)

	// Issued 25 hours ago: the 24-hour window has closed.
	old := service.NewActionSigner([]byte(chatActionTestSecret)).WithClock(func() time.Time { return time.Now().Add(-25 * time.Hour) })
	id := chatUUID(t, env.Proposal.ID)
	raw, err := old.Sign(id, service.ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}

	res := env.pressSlack(t, env.Installs, sender, raw, false)
	if res.Settled || !strings.Contains(res.Reply, "expired") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("expired press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("an expired press changed the state to %s", got)
	}
}

func TestChannelPressSlack_TamperedTokenChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, env.Proposal.ID, service.ProposalDecisionApprove)

	// The action byte becomes reject and the signature stays: the MAC no longer
	// matches the body, so the press is refused.
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	data[1] = 2
	forged := base64.RawURLEncoding.EncodeToString(data)

	res := env.pressSlack(t, env.Installs, sender, forged, false)
	if res.Settled || !strings.Contains(res.Toast, "not valid") {
		t.Fatalf("tampered press = %+v, want a refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a tampered press changed the state to %s", got)
	}
}

// A Slack user whose member has left the workspace is refused, as on Telegram.
func TestChannelPressSlack_RemovedMemberIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	gone := dbfx.User(t, "Gone", fmt.Sprintf("chat-slack-gone-%s@example.com", uuid.NewString()))
	sender := bindSlackSender(t, env.Installs, env.WorkspaceID, gone)

	res := env.pressSlack(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionApprove), false)
	if res.Settled || !strings.Contains(res.Reply, "no longer a member") {
		t.Fatalf("removed member press = %+v, want a refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a removed member's press changed the state to %s", got)
	}
}

// A Telegram binding does not decide a press that arrives as Slack, even for
// the same workspace and member: the binding must be of the press's chat app.
func TestChannelPressSlack_ATelegramBindingDoesNotDecideASlackPress(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	telegramUser := bindChatSender(t, env.Installs, env.WorkspaceID, owner)

	res := env.pressSlack(t, env.Installs, telegramUser, env.token(t, env.Proposal.ID, service.ProposalDecisionApprove), false)
	if res.Settled || !strings.Contains(res.Reply, "Link your Multica account") {
		t.Fatalf("cross-app press = %+v, want the link prompt", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a cross-app press changed the state to %s", got)
	}
}
