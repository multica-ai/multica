package handler

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// The Lark press runs through the same ChannelActions as Telegram and Slack. These
// tests check a Lark open id reaches the same rules: the binding is a feishu
// binding, the presser must be an owner or admin, an agent is refused, and a token
// is verified before anything changes.

// bindLarkSender links a Lark open id to a member of ws through the installation.
// It returns the open id the adapter would report.
func bindLarkSender(t *testing.T, installationID, ws, userID string) string {
	t.Helper()
	openID := "ou_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": userID,
		"installation_id": installationID,
		"channel_type":    "feishu",
		"channel_user_id": openID,
	})
	return openID
}

// pressLark is one press from a Lark user through the installation.
func (e chatActionEnv) pressLark(t *testing.T, installationID, openID, data string, isBot bool) channel.PressResult {
	t.Helper()
	res, err := e.Actions.Press(context.Background(), channel.Press{
		Sender: channel.Sender{
			ChannelType:    channel.TypeFeishu,
			InstallationID: chatUUID(t, installationID),
			ChannelUserID:  openID,
			IsBot:          isBot,
		},
		Data: data,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	return res
}

func TestChannelPressLark_OwnerDecidesAndAReplayChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindLarkSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, env.Proposal.ID, service.ProposalDecisionApprove)

	first := env.pressLark(t, env.Installs, sender, raw, false)
	if !first.Settled || !strings.HasPrefix(first.Reply, "Approved:") {
		t.Fatalf("owner press = %+v, want an approval that settles the decision", first)
	}
	if got := proposalState(t, env.Proposal.ID); got != "approved" {
		t.Fatalf("state = %s, want approved", got)
	}
	decidedAt := proposalDecidedAt(t, env.Proposal.ID)

	second := env.pressLark(t, env.Installs, sender, raw, false)
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

func TestChannelPressLark_AdminDecides(t *testing.T) {
	env := newChatActionEnv(t)
	admin := newWorkspaceMemberUser(t, "admin")
	sender := bindLarkSender(t, env.Installs, env.WorkspaceID, admin)

	res := env.pressLark(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionReject), false)
	if !res.Settled || !strings.HasPrefix(res.Reply, "Rejected:") {
		t.Fatalf("admin press = %+v, want the rejection", res)
	}
}

func TestChannelPressLark_MemberIsRefusedWithTheAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	member := newWorkspaceMemberUser(t, "member")
	sender := bindLarkSender(t, env.Installs, env.WorkspaceID, member)

	res := env.pressLark(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionReject), false)
	if res.Settled || !strings.Contains(res.Reply, "owner or admin") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("member press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a member's press changed the state to %s", got)
	}
}

func TestChannelPressLark_AgentIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindLarkSender(t, env.Installs, env.WorkspaceID, owner)

	res := env.pressLark(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionApprove), true)
	if res.Settled || !strings.Contains(res.Reply, "Agents cannot") {
		t.Fatalf("bot press = %+v, want an agent refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("an agent's press changed the state to %s", got)
	}
}

func TestChannelPressLark_ExpiredTokenIsRefusedWithTheAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindLarkSender(t, env.Installs, env.WorkspaceID, owner)

	// Issued 25 hours ago: the 24-hour window has closed.
	old := service.NewActionSigner([]byte(chatActionTestSecret)).WithClock(func() time.Time { return time.Now().Add(-25 * time.Hour) })
	raw, err := old.Sign(chatUUID(t, env.Proposal.ID), service.ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}

	res := env.pressLark(t, env.Installs, sender, raw, false)
	if res.Settled || !strings.Contains(res.Reply, "expired") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("expired press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("an expired press changed the state to %s", got)
	}
}

func TestChannelPressLark_TamperedTokenChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindLarkSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, env.Proposal.ID, service.ProposalDecisionApprove)

	// The action byte becomes reject and the signature stays: the MAC no longer
	// matches the body, so the press is refused.
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	data[1] = 2
	forged := base64.RawURLEncoding.EncodeToString(data)

	res := env.pressLark(t, env.Installs, sender, forged, false)
	if res.Settled || !strings.Contains(res.Toast, "not valid") {
		t.Fatalf("tampered press = %+v, want a refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a tampered press changed the state to %s", got)
	}
}
