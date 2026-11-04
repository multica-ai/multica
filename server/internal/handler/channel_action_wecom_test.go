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

// A WeCom card press runs through the same ChannelActions as Telegram and Slack.
// These tests check the WeCom sender reaches the same rules: the binding is a
// wecom binding, the presser must be an owner or admin, and a token is verified
// before anything changes, and a replayed press changes nothing.

// bindWecomSender links a WeCom user id to a member of ws through the
// installation. It returns the WeCom user id the adapter would report.
func bindWecomSender(t *testing.T, installationID, ws, userID string) string {
	t.Helper()
	wecomUserID := "wx-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": userID,
		"installation_id": installationID,
		"channel_type":    "wecom",
		"channel_user_id": wecomUserID,
	})
	return wecomUserID
}

// pressWecom is one card press from a WeCom user through the installation.
func (e chatActionEnv) pressWecom(t *testing.T, installationID, wecomUserID, data string) channel.PressResult {
	t.Helper()
	res, err := e.Actions.Press(context.Background(), channel.Press{
		Sender: channel.Sender{
			ChannelType:    "wecom",
			InstallationID: chatUUID(t, installationID),
			ChannelUserID:  wecomUserID,
		},
		Data: data,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	return res
}

func TestChannelPressWecom_OwnerDecidesAndAReplayChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindWecomSender(t, env.Installs, env.WorkspaceID, owner)
	raw := env.token(t, env.Proposal.ID, service.ProposalDecisionApprove)

	first := env.pressWecom(t, env.Installs, sender, raw)
	if !first.Settled || !strings.HasPrefix(first.Reply, "Approved:") {
		t.Fatalf("owner press = %+v, want an approval that settles the decision", first)
	}
	if got := proposalState(t, env.Proposal.ID); got != "approved" {
		t.Fatalf("state = %s, want approved", got)
	}
	decidedAt := proposalDecidedAt(t, env.Proposal.ID)

	second := env.pressWecom(t, env.Installs, sender, raw)
	if !second.Settled || !strings.Contains(second.Reply, "Already decided") {
		t.Fatalf("replay = %+v, want already decided", second)
	}
	if again := proposalDecidedAt(t, env.Proposal.ID); !again.Time.Equal(decidedAt.Time) {
		t.Fatalf("replay moved decided_at from %v to %v", decidedAt.Time, again.Time)
	}
}

func TestChannelPressWecom_MemberIsRefusedWithTheAppLink(t *testing.T) {
	env := newChatActionEnv(t)
	member := newWorkspaceMemberUser(t, "member")
	sender := bindWecomSender(t, env.Installs, env.WorkspaceID, member)

	res := env.pressWecom(t, env.Installs, sender, env.token(t, env.Proposal.ID, service.ProposalDecisionReject))
	if res.Settled || !strings.Contains(res.Reply, "owner or admin") || !strings.Contains(res.Reply, "https://app.example/") {
		t.Fatalf("member press = %+v, want a refusal with the app link", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a member's press changed the state to %s", got)
	}
}

func TestChannelPressWecom_ExpiredTokenIsRefused(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindWecomSender(t, env.Installs, env.WorkspaceID, owner)

	// Issued 25 hours ago: the 24-hour window has closed.
	old := service.NewActionSigner([]byte(chatActionTestSecret)).WithClock(func() time.Time { return time.Now().Add(-25 * time.Hour) })
	raw, err := old.Sign(chatUUID(t, env.Proposal.ID), service.ProposalDecisionApprove)
	if err != nil {
		t.Fatal(err)
	}

	res := env.pressWecom(t, env.Installs, sender, raw)
	if res.Settled || !strings.Contains(res.Reply, "expired") {
		t.Fatalf("expired press = %+v, want a refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("an expired press changed the state to %s", got)
	}
}

func TestChannelPressWecom_TamperedTokenChangesNothing(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	sender := bindWecomSender(t, env.Installs, env.WorkspaceID, owner)

	// The action byte becomes reject and the signature stays: the MAC no longer
	// matches the body, so the press is refused.
	data, err := base64.RawURLEncoding.DecodeString(env.token(t, env.Proposal.ID, service.ProposalDecisionApprove))
	if err != nil {
		t.Fatal(err)
	}
	data[1] = 2
	forged := base64.RawURLEncoding.EncodeToString(data)

	res := env.pressWecom(t, env.Installs, sender, forged)
	if res.Settled || !strings.Contains(res.Toast, "not valid") {
		t.Fatalf("tampered press = %+v, want a refusal", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a tampered press changed the state to %s", got)
	}
}

// A Telegram binding does not decide a press that arrives from WeCom: the
// binding must be of the press's chat app.
func TestChannelPressWecom_ATelegramBindingDoesNotDecideAWecomPress(t *testing.T) {
	env := newChatActionEnv(t)
	owner := newWorkspaceMemberUser(t, "owner")
	telegramUser := bindChatSender(t, env.Installs, env.WorkspaceID, owner)

	res := env.pressWecom(t, env.Installs, telegramUser, env.token(t, env.Proposal.ID, service.ProposalDecisionApprove))
	if res.Settled || !strings.Contains(res.Reply, "Link your Multica account") {
		t.Fatalf("cross-app press = %+v, want the link prompt", res)
	}
	if got := proposalState(t, env.Proposal.ID); got != "pending" {
		t.Fatalf("a cross-app press changed the state to %s", got)
	}
}
