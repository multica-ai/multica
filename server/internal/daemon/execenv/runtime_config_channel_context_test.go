package execenv

import (
	"strings"
	"testing"
)

// The `## Conversation Channel` contract.
//
// A chat_session behind Feishu, Slack or WeCom may be a GROUP room shared by
// many people, and the brief used to describe every chat run as "A user is
// messaging you directly in a chat window". An agent told it is in a private
// 1:1 has no reason to weigh who else is reading, so the one input an operator
// needs in order to write "do not repeat customer detail in a shared room"
// was missing from the runtime.
//
// What the block does and does not do is the point of these tests: it states
// the room's shape and stops. Deciding what may be said in front of that
// audience belongs to the operator's agent instructions, so no rule of that
// kind may appear here.

// chatCtx builds a chat-kind context for a given channel and room shape.
func chatCtx(channelType, chatType string) TaskContextForEnv {
	return TaskContextForEnv{
		ChatSessionID:   "c-1",
		ChatChannelType: channelType,
		ChatType:        chatType,
		AgentID:         "eve-1",
		AgentName:       "Eve",
	}
}

// everyChannel is every IM the brief can name. The block is channel-agnostic
// by construction — it reads chat_type, which every adapter persists on the
// same channel_chat_session_binding column — and this table is what keeps a
// future channel from having to be special-cased in.
func everyChannel() map[string]string {
	return map[string]string{
		ChannelTypeSlack:  "Slack",
		ChannelTypeFeishu: "Feishu/Lark",
		ChannelTypeWecom:  "WeCom",
	}
}

func TestBriefConversationChannel_GroupChatNamesTheAudience(t *testing.T) {
	t.Parallel()

	want := []string{
		"## Conversation Channel",
		// The fact the agent cannot infer: more than one reader.
		"everyone in the room receives every reply you send",
		"did not write the message you are answering",
		"Nothing you send here is private to the person asking",
		// In a shared room the asker changes per message, so the agent must
		// not read the runtime owner as the person it is answering.
		"`## Task Initiator`",
		"`## Requesting User`",
	}

	for channelType, display := range everyChannel() {
		out := buildMetaSkillContent("claude", chatCtx(channelType, ChatTypeGroup))
		for _, phrase := range want {
			if !strings.Contains(out, phrase) {
				t.Errorf("channel=%s: group brief is missing %q\n---\n%s", channelType, phrase, out)
			}
		}
		if !strings.Contains(out, display) {
			t.Errorf("channel=%s: brief must name the platform as %q", channelType, display)
		}
	}
}

func TestBriefConversationChannel_DirectChatSaysSoAndClaimsNoAudience(t *testing.T) {
	t.Parallel()

	for channelType := range everyChannel() {
		out := buildMetaSkillContent("claude", chatCtx(channelType, ChatTypeP2P))
		if !strings.Contains(out, "## Conversation Channel") {
			t.Errorf("channel=%s: direct chat still needs the channel block — it names the platform", channelType)
		}
		if !strings.Contains(out, "only the person you are replying to") {
			t.Errorf("channel=%s: direct brief must say the reply reaches one person\n---\n%s", channelType, out)
		}
		if strings.Contains(out, "everyone in the room") {
			t.Errorf("channel=%s: direct brief must not describe a shared room", channelType)
		}
	}
}

// Degradation, two steps. No channel at all (web chat) renders nothing. A
// channel with no known room shape — an older server sends chat_channel_type
// but no chat_type — names the platform and asserts no audience: guessing
// "direct" is the wrong-picture failure this block exists to end.
func TestBriefConversationChannel_DegradesWhenFactsAreMissing(t *testing.T) {
	t.Parallel()

	web := buildMetaSkillContent("claude", chatCtx("", ""))
	if strings.Contains(web, "## Conversation Channel") {
		t.Errorf("a web chat has no channel facts and must render no channel block\n---\n%s", web)
	}

	shapeless := buildMetaSkillContent("claude", chatCtx(ChannelTypeFeishu, ""))
	if !strings.Contains(shapeless, "## Conversation Channel") {
		t.Error("a known channel with unknown room shape must still name the platform")
	}
	// Including the chat-mode line, which is read BEFORE this block and used to
	// restate the claim the block had just declined to make.
	for _, phrase := range []string{"everyone in the room", "only the person you are replying to", "messaging you directly"} {
		if strings.Contains(shapeless, phrase) {
			t.Errorf("unknown room shape must claim no audience, found %q\n---\n%s", phrase, shapeless)
		}
	}
}

// The brief opens with the chat-mode line and only later reaches `##
// Conversation Channel`. Whatever the second says about the audience, the
// first must not have already contradicted it — it is what the agent reads
// first, so a wrong claim there survives a correct one below.
func TestBriefChatModeLineAgreesWithTheChannelBlock(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		ctx      TaskContextForEnv
		mustNot  []string
		mustHave string
	}{
		"group room": {
			ctx:      chatCtx(ChannelTypeFeishu, ChatTypeGroup),
			mustNot:  []string{"messaging you directly"},
			mustHave: "one participant in a group conversation",
		},
		"known 1:1": {
			ctx:      chatCtx(ChannelTypeSlack, ChatTypeP2P),
			mustHave: "messaging you directly",
		},
		// The case the two-step degradation is for: a daemon claiming from a
		// server that does not send chat_type yet, or a binding deleted between
		// enqueue and claim. Neither privacy nor an audience may be asserted.
		"channel with no reported shape": {
			ctx:     chatCtx(ChannelTypeWecom, ""),
			mustNot: []string{"messaging you directly", "everyone in the room"},
		},
		// A web chat has no binding row by construction, so absent shape here
		// really does mean one reader. It keeps the original wording.
		"web chat": {
			ctx:      chatCtx("", ""),
			mustHave: "messaging you directly",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)
			for _, phrase := range tc.mustNot {
				if strings.Contains(out, phrase) {
					t.Errorf("%s: brief must not say %q\n---\n%s", name, phrase, out)
				}
			}
			if tc.mustHave != "" && !strings.Contains(out, tc.mustHave) {
				t.Errorf("%s: brief should say %q\n---\n%s", name, tc.mustHave, out)
			}
		})
	}
}

func TestBriefConversationChannel_ChatKindOnly(t *testing.T) {
	t.Parallel()

	// Every non-chat kind, each carrying channel fields it has no business
	// rendering — the kind gate, not the data, is what must hold.
	others := map[string]TaskContextForEnv{
		"comment":     {IssueID: "i-1", TriggerCommentID: "tc-1", ChatChannelType: ChannelTypeFeishu, ChatType: ChatTypeGroup},
		"assignment":  {IssueID: "i-1", ChatChannelType: ChannelTypeFeishu, ChatType: ChatTypeGroup},
		"autopilot":   {AutopilotRunID: "r-1", ChatChannelType: ChannelTypeFeishu, ChatType: ChatTypeGroup},
		"quickcreate": {QuickCreatePrompt: "p", ChatChannelType: ChannelTypeFeishu, ChatType: ChatTypeGroup},
	}
	for name, ctx := range others {
		ctx.AgentID, ctx.AgentName = "eve-1", "Eve"
		if out := buildMetaSkillContent("claude", ctx); strings.Contains(out, "## Conversation Channel") {
			t.Errorf("kind=%s: the channel block is chat-only", name)
		}
	}
}

// The block reports the room; it does not police what is said in it. An
// operator writes that policy in agent instructions, and a rule baked in here
// would fire on every workspace whether or not its owner wanted it.
func TestBriefConversationChannel_StatesFactsAndSetsNoPolicy(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", chatCtx(ChannelTypeWecom, ChatTypeGroup))
	for _, banned := range []string{"do not share", "Do NOT share", "never mention", "avoid discussing"} {
		if strings.Contains(out, banned) {
			t.Errorf("the channel block must not decide what may be said; found %q", banned)
		}
	}
	if !strings.Contains(out, "for your own instructions to decide") {
		t.Errorf("the block must hand the judgement back to the agent's instructions\n---\n%s", out)
	}
}

// The chat workflow's opening line asserted a private 1:1 for every chat run.
// It must stop doing that in a room it knows is shared, and must keep saying
// it where it is true (direct chat, and a web chat which is 1:1 by
// construction).
func TestBriefChatWorkflow_DoesNotCallAGroupChatDirect(t *testing.T) {
	t.Parallel()

	group := buildMetaSkillContent("claude", chatCtx(ChannelTypeFeishu, ChatTypeGroup))
	if strings.Contains(group, "messaging you directly") {
		t.Errorf("a group chat brief must not claim the user is messaging the agent directly\n---\n%s", group)
	}
	if !strings.Contains(group, "**You are in chat mode.**") {
		t.Error("the chat workflow must still open by declaring chat mode")
	}

	for name, ctx := range map[string]TaskContextForEnv{
		"direct": chatCtx(ChannelTypeSlack, ChatTypeP2P),
		"web":    chatCtx("", ""),
	} {
		if out := buildMetaSkillContent("claude", ctx); !strings.Contains(out, "messaging you directly") {
			t.Errorf("kind=chat/%s: a 1:1 conversation should still read as one\n---\n%s", name, out)
		}
	}
}
