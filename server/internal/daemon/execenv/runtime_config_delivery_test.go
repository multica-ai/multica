package execenv

import (
	"strings"
	"testing"
)

// The MUL-4899 delivery contract. Two orthogonal properties are pinned here and
// must not be collapsed:
//
//   - The invariant ("never link a local path") is ALWAYS-ON — every task kind,
//     no exceptions. It lives outside writeOutput's kind switch so a future kind
//     cannot silently inherit no invariant at all; this test is what keeps that
//     true.
//   - The surface policy ("here is how a file actually gets delivered HERE") is
//     PER-KIND, and the chat kind alone splits three ways: web/mobile renders a
//     card, a channel whose deployment performs the last hop pushes the file
//     into the room as its own message, and everything else cannot deliver one
//     at all.
//
// The third of those is a DEPLOYMENT fact, not a channel-type fact, which is
// why the same channel appears below with both answers. It arrives on the claim
// as ChatChannelDeliversFiles and is used as given.

// deliveryInvariantFixtures covers every task kind. The chat kind appears six
// times because the surface splits on channel type and, within one channel
// type, on whether this deployment can actually carry a file.
func deliveryInvariantFixtures() map[string]TaskContextForEnv {
	return map[string]TaskContextForEnv{
		"comment":     {IssueID: "i-1", TriggerCommentID: "tc-1", AgentName: "Eve", AgentID: "eve-1"},
		"assignment":  {IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1"},
		"autopilot":   {AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1"},
		"quickcreate": {QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1"},
		"chat_direct": {ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1"},
		"chat_slack":  {ChatSessionID: "c-1", ChatChannelType: ChannelTypeSlack, AgentName: "Eve", AgentID: "eve-1"},
		"chat_feishu": {ChatSessionID: "c-1", ChatChannelType: ChannelTypeFeishu, AgentName: "Eve", AgentID: "eve-1"},
		"chat_wecom":  {ChatSessionID: "c-1", ChatChannelType: ChannelTypeWecom, ChatChannelDeliversFiles: true, AgentName: "Eve", AgentID: "eve-1"},
		// The same adapter on a deployment with no object storage: there is
		// nothing to read the bound attachment out of, so the server reports no
		// delivery and the brief must say so.
		"chat_wecom_no_store": {ChatSessionID: "c-1", ChatChannelType: ChannelTypeWecom, AgentName: "Eve", AgentID: "eve-1"},
		// A daemon that knows about file delivery against a server that does
		// not send the field. Identical shape to the row above by construction —
		// an absent field decodes as false — and listed separately because it
		// is a separate way to reach the same wrong answer.
		"chat_wecom_old_server": {ChatSessionID: "c-1", ChatChannelType: ChannelTypeWecom, AgentName: "Eve", AgentID: "eve-1"},
	}
}

func TestBriefDeliveryInvariantIsAlwaysOn(t *testing.T) {
	t.Parallel()

	// Phrases every kind must carry, whatever its surface can or cannot deliver.
	wantAll := []string{
		"Runtime-local paths are never deliverables",
		"NEVER write an absolute path or a `file://` URL as a clickable link",
		"`path/to/file.ts:42`",
	}

	for name, ctx := range deliveryInvariantFixtures() {
		out := buildMetaSkillContent("claude", ctx)
		for _, want := range wantAll {
			if !strings.Contains(out, want) {
				t.Errorf("kind=%s: brief is missing always-on delivery invariant %q", name, want)
			}
		}
	}
}

func TestBriefSurfaceDeliveryPolicy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mustHave []string
		mustNot  []string
	}{
		// Issue surfaces: files ride the comment.
		"comment": {
			mustHave: []string{"`--attachment <path>` to `multica issue comment add`"},
			mustNot:  []string{"multica attachment upload"},
		},
		"assignment": {
			mustHave: []string{"`--attachment <path>` to `multica issue comment add`"},
			mustNot:  []string{"multica attachment upload"},
		},
		// Direct chat: the upload binds to the reply and the browser renders a
		// card, so the file can sit inline where the agent puts it.
		"chat_direct": {
			mustHave: []string{"`multica attachment upload <local-path>`"},
			mustNot:  []string{"text-only", "separate message"},
		},
		// WeCom on a deployment that can deliver: the upload works, but the
		// adapter delivers the file as its own message. Saying only "files work
		// here" would have the agent write "see the chart below" with nothing
		// below it.
		"chat_wecom": {
			mustHave: []string{
				"`multica attachment upload <local-path>`",
				"WeCom conversation as a separate message",
				"not inline",
			},
			mustNot: []string{"text-only", "does NOT apply"},
		},
		// The same channel where the server reported no delivery. This is the
		// row that fails if the capability is ever re-derived from the channel
		// type: the brief would promise a hop the deployment has not got, and
		// the agent would write "the file is attached" into a room where
		// nothing is.
		"chat_wecom_no_store": {
			mustHave: []string{"WeCom conversation is text-only", "does NOT apply"},
			mustNot:  []string{"run `multica attachment upload", "separate message"},
		},
		"chat_wecom_old_server": {
			mustHave: []string{"WeCom conversation is text-only", "does NOT apply"},
			mustNot:  []string{"run `multica attachment upload", "separate message"},
		},
		// Slack and Lark are text-only. The upload command must not appear as an
		// instruction: it binds to a Multica chat reply, which an IM reply on
		// those platforms is not, so suggesting it would have the agent upload a
		// file and report it as delivered.
		"chat_slack": {
			mustHave: []string{"Slack conversation is text-only", "does NOT apply"},
			mustNot:  []string{"run `multica attachment upload"},
		},
		"chat_feishu": {
			mustHave: []string{"Feishu/Lark conversation is text-only", "does NOT apply"},
			mustNot:  []string{"run `multica attachment upload"},
		},
		"autopilot": {
			mustHave: []string{"this surface is text-only"},
			mustNot:  []string{"multica attachment upload"},
		},
		"quickcreate": {
			mustHave: []string{"your stdout is text-only", "`multica issue create` call itself via `--attachment <path>`"},
			mustNot:  []string{"multica attachment upload"},
		},
	}

	fixtures := deliveryInvariantFixtures()
	for name, want := range cases {
		ctx, ok := fixtures[name]
		if !ok {
			t.Fatalf("no fixture for surface %q", name)
		}
		out := buildMetaSkillContent("claude", ctx)
		for _, phrase := range want.mustHave {
			if !strings.Contains(out, phrase) {
				t.Errorf("surface=%s: brief missing surface policy %q\n--- Output section ---\n%s",
					name, phrase, outputSection(out))
			}
		}
		for _, phrase := range want.mustNot {
			if strings.Contains(out, phrase) {
				t.Errorf("surface=%s: brief must NOT carry %q (wrong surface's delivery mechanism)\n--- Output section ---\n%s",
					name, phrase, outputSection(out))
			}
		}
	}
}

// TestBriefInboundAttachmentIsNotADeliverable locks the inbound half: a
// downloaded attachment's local path is a private working copy, and the most
// tempting one to echo back because it arrived from the conversation.
//
// The Attachments section owns that framing — it is what `## Output` cannot
// express, because Output does not know an attachment felt shared. The
// no-clickable-local-path rule itself belongs to Output and used to be restated
// here verbatim; MUL-5442 replaced the restatement with a pointer, so this test
// pins the framing plus the pointer and lets the delivery tests above own the
// rule.
func TestBriefInboundAttachmentIsNotADeliverable(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "i-1", TriggerCommentID: "tc-1", AgentName: "Eve", AgentID: "eve-1",
	})
	for _, want := range []string{
		"private working copy",
		"not something the reader can open",
		"the link rules in `## Output` apply to it too",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Attachments section missing %q\n---\n%s", want, out)
		}
	}
	// The rule the pointer defers to must actually be present in the brief.
	if !strings.Contains(out, "NEVER write an absolute path or a `file://` URL as a clickable link") {
		t.Errorf("Attachments points at ## Output but the rule is missing\n---\n%s", out)
	}
}

func TestChannelDisplayName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		ChannelTypeSlack:  "Slack",
		ChannelTypeFeishu: "Feishu/Lark",
		"":                "",
		// An unmapped channel names itself rather than reading as "unknown".
		"discord": "discord",
	}
	for in, want := range cases {
		if got := ChannelDisplayName(in); got != want {
			t.Errorf("ChannelDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Quick actions moved to the daemon's dedicated post-completion suggestion
// pass; the brief must no longer teach the in-band footer syntax to anyone —
// a taught agent would emit footers the transcript then has to strip.
func TestQuickActionsInstructionsAbsentFromAllChatBriefs(t *testing.T) {
	t.Parallel()
	contexts := []TaskContextForEnv{
		{ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1"},
		{ChatSessionID: "c-1", ChatChannelType: ChannelTypeSlack, AgentName: "Eve", AgentID: "eve-1"},
		{ChatSessionID: "c-1", ChatChannelType: ChannelTypeFeishu, AgentName: "Eve", AgentID: "eve-1"},
	}
	for _, ctx := range contexts {
		brief := buildMetaSkillContent("claude", ctx)
		if strings.Contains(brief, "```quick-actions") || strings.Contains(brief, "### Quick Actions") {
			t.Fatalf("brief (channel=%q) must not teach the in-band quick-actions syntax", ctx.ChatChannelType)
		}
	}
}

// outputSection extracts the brief's `## Output` section for readable failures.
func outputSection(brief string) string {
	idx := strings.Index(brief, "\n## Output\n")
	if idx < 0 {
		return "<no ## Output section>"
	}
	return brief[idx:]
}
