package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// chatCommandAppURL sets the app URL the chat links are built from, for one test.
func chatCommandAppURL(t *testing.T) {
	t.Helper()
	prev := testHandler.cfg.AppURL
	testHandler.cfg.AppURL = "https://app.example"
	t.Cleanup(func() { testHandler.cfg.AppURL = prev })
}

// chatCommand sends a command from a sender bound in installation as userID's
// chat, and returns the reply and whether the server answered it.
func chatCommand(t *testing.T, installationID, channelUserID, name string) (channel.Message, bool) {
	t.Helper()
	msg, handled, err := NewChannelActions(testHandler, service.NewActionSigner([]byte(chatActionTestSecret))).Command(
		context.Background(),
		channel.Command{
			Sender: channel.Sender{ChannelType: "telegram", InstallationID: chatUUID(t, installationID), ChannelUserID: channelUserID},
			Name:   name,
		},
	)
	if err != nil {
		t.Fatalf("Command(%s): %v", name, err)
	}
	return msg, handled
}

func workspaceSlug(t *testing.T, workspaceID string) string {
	t.Helper()
	var slug string
	dbfx.QueryRow(t, `SELECT slug FROM workspace WHERE id = $1`, workspaceID).Scan(&slug)
	return slug
}

func TestChannelCommand_BriefRepliesWithTheHeadlineAndALink(t *testing.T) {
	chatCommandAppURL(t)
	env := newDigestEnv(t)
	var brief BriefResponse
	env.publish(t, "", "", briefRequestBody(nil)).Want(http.StatusCreated).JSON(&brief)

	member := dbfx.User(t, "Brief reader", fmt.Sprintf("chat-brief-%s@example.com", uuid.NewString()))
	dbfx.Member(t, env.WorkspaceID, member, "member")
	installs := uuid.NewString()
	sender := bindChatSender(t, installs, env.WorkspaceID, member)

	msg, handled := chatCommand(t, installs, sender, channel.CommandBrief)
	want := "Nothing needs you today.\n\nBrief: https://app.example/" + workspaceSlug(t, env.WorkspaceID) + "/company?brief=" + brief.ID
	if !handled || msg.Text != want {
		t.Fatalf("/brief = %q (handled %v), want %q", msg.Text, handled, want)
	}
}

func TestChannelCommand_BriefWithNoBriefSaysSo(t *testing.T) {
	chatCommandAppURL(t)
	env := newDigestEnv(t)
	member := dbfx.User(t, "No brief", fmt.Sprintf("chat-nobrief-%s@example.com", uuid.NewString()))
	dbfx.Member(t, env.WorkspaceID, member, "member")
	installs := uuid.NewString()
	sender := bindChatSender(t, installs, env.WorkspaceID, member)

	msg, handled := chatCommand(t, installs, sender, channel.CommandBrief)
	if !handled || msg.Text != chatMsgNoBrief {
		t.Fatalf("/brief with no brief = %q (handled %v)", msg.Text, handled)
	}
}

// A sender with no binding, or whose binding is not a member of the workspace,
// is not answered: the message takes the ordinary path.
func TestChannelCommand_UnlinkedSendersTakeTheOrdinaryPath(t *testing.T) {
	chatCommandAppURL(t)
	installs := uuid.NewString()
	if _, handled := chatCommand(t, installs, "nobody-"+uuid.NewString(), channel.CommandDecisions); handled {
		t.Fatal("an unlinked sender was answered")
	}

	gone := dbfx.User(t, "Not a member", fmt.Sprintf("chat-nonmember-%s@example.com", uuid.NewString()))
	sender := bindChatSender(t, installs, testWorkspaceID, gone)
	if _, handled := chatCommand(t, installs, sender, channel.CommandDecisions); handled {
		t.Fatal("a binding that is not a member was answered")
	}
}

// /decisions names at most five pending proposals, each with its link, and
// leaves out one that is snoozed until later.
func TestChannelCommand_DecisionsListsFiveAndLeavesOutSnoozed(t *testing.T) {
	chatCommandAppURL(t)
	// Each proposal comes from its own squad: a squad may hold three pending.
	// Seven are filed and the newest is snoozed, so six wait and one overflows.
	titles := make([]string, 7)
	ids := make([]string, 7)
	for i := range titles {
		fx := newProposalTestFixture(t)
		titles[i] = fx.title(fmt.Sprintf("chat decision %d", i+1))
		ids[i] = fx.fileProposal(t, fmt.Sprintf("chat decision %d", i+1), nil).ID
		time.Sleep(2 * time.Millisecond) // distinct created_at, so the newest order is exact
	}
	// The newest is snoozed for a week, so it is not waiting for the boss today.
	until := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(ids[6], map[string]any{"decision": "snooze", "until": until})).Want(http.StatusOK)

	member := newWorkspaceMemberUser(t, "member")
	installs := uuid.NewString()
	sender := bindChatSender(t, installs, testWorkspaceID, member)
	msg, handled := chatCommand(t, installs, sender, channel.CommandDecisions)
	if !handled {
		t.Fatal("/decisions was not answered for a member")
	}

	lines := decisionLines(msg.Text)
	if len(lines) != chatDecisionsLimit {
		t.Fatalf("named %d decisions, want %d:\n%s", len(lines), chatDecisionsLimit, msg.Text)
	}
	if strings.Contains(msg.Text, titles[6]) {
		t.Fatalf("a snoozed decision was named:\n%s", msg.Text)
	}
	if !strings.Contains(lines[0], titles[5]) || !strings.Contains(lines[0], "decision="+ids[5]) {
		t.Fatalf("the newest pending decision should lead, with its link:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "...and 1 more in the Company") {
		t.Fatalf("the overflow was not counted:\n%s", msg.Text)
	}
}

// decisionLines returns the decision lines of a /decisions reply.
func decisionLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "- ") {
			out = append(out, line)
		}
	}
	return out
}

// A bot or agent sender is consumed without a reply, even when its binding is
// a member's: an agent never reads the brief or the decisions through a chat.
func TestChannelCommand_BotSenderIsConsumedWithoutAReply(t *testing.T) {
	chatCommandAppURL(t)
	env := newDigestEnv(t)
	var brief BriefResponse
	env.publish(t, "", "", briefRequestBody(nil)).Want(http.StatusCreated).JSON(&brief)

	member := dbfx.User(t, "Bot caller", fmt.Sprintf("chat-bot-%s@example.com", uuid.NewString()))
	dbfx.Member(t, env.WorkspaceID, member, "member")
	installs := uuid.NewString()
	sender := bindChatSender(t, installs, env.WorkspaceID, member)

	msg, handled, err := NewChannelActions(testHandler, service.NewActionSigner([]byte(chatActionTestSecret))).Command(
		context.Background(),
		channel.Command{
			Sender: channel.Sender{ChannelType: "telegram", InstallationID: chatUUID(t, installs), ChannelUserID: sender, IsBot: true},
			Name:   channel.CommandBrief,
		},
	)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !handled || msg.Text != "" {
		t.Fatalf("a bot's /brief = %q (handled %v), want consumed with no reply", msg.Text, handled)
	}
}

// A Lark member's /brief is answered with the same text as the other platforms:
// the binding is a feishu binding, and the sender is that member.
func TestChannelCommandLark_BriefRepliesToABoundLarkMember(t *testing.T) {
	chatCommandAppURL(t)
	env := newDigestEnv(t)
	var brief BriefResponse
	env.publish(t, "", "", briefRequestBody(nil)).Want(http.StatusCreated).JSON(&brief)

	member := dbfx.User(t, "Lark reader", fmt.Sprintf("chat-lark-%s@example.com", uuid.NewString()))
	dbfx.Member(t, env.WorkspaceID, member, "member")
	installs := uuid.NewString()
	openID := bindLarkSender(t, installs, env.WorkspaceID, member)

	msg, handled, err := NewChannelActions(testHandler, service.NewActionSigner([]byte(chatActionTestSecret))).Command(
		context.Background(),
		channel.Command{
			Sender: channel.Sender{ChannelType: channel.TypeFeishu, InstallationID: chatUUID(t, installs), ChannelUserID: openID},
			Name:   channel.CommandBrief,
		},
	)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	want := "Nothing needs you today.\n\nBrief: https://app.example/" + workspaceSlug(t, env.WorkspaceID) + "/company?brief=" + brief.ID
	if !handled || msg.Text != want {
		t.Fatalf("Lark /brief = %q (handled %v), want %q", msg.Text, handled, want)
	}
}
