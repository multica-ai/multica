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
//     PER-KIND, and there are five surfaces, not four — web/mobile chat and IM
//     chat are the same taskKind but opposite answers.

// deliveryInvariantFixtures covers all five task kinds. The chat kind appears
// twice because ChatChannelType splits it into two surfaces.
func deliveryInvariantFixtures() map[string]TaskContextForEnv {
	return map[string]TaskContextForEnv{
		"comment":     {IssueID: "i-1", TriggerCommentID: "tc-1", AgentName: "Eve", AgentID: "eve-1"},
		"assignment":  {IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1"},
		"autopilot":   {AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1"},
		"quickcreate": {QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1"},
		"chat_direct": {ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1"},
		"chat_slack":  {ChatSessionID: "c-1", ChatChannelType: ChannelTypeSlack, AgentName: "Eve", AgentID: "eve-1"},
		"chat_feishu": {ChatSessionID: "c-1", ChatChannelType: ChannelTypeFeishu, AgentName: "Eve", AgentID: "eve-1"},
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
		// Direct chat is the ONLY surface where `attachment upload` works.
		"chat_direct": {
			mustHave: []string{"`multica attachment upload <local-path>`"},
			mustNot:  []string{"text-only"},
		},
		// IM surfaces are text-only. The upload command must not appear: it binds
		// to a Multica chat reply, which an IM reply is not, so suggesting it
		// would have the agent upload a file and report it as delivered.
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
func TestBriefInboundAttachmentIsNotADeliverable(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "i-1", TriggerCommentID: "tc-1", AgentName: "Eve", AgentID: "eve-1",
	})
	for _, want := range []string{
		"private working copy",
		"Never echo it back into a deliverable as a link",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Attachments section missing %q\n---\n%s", want, out)
		}
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

// outputSection extracts the brief's `## Output` section for readable failures.
func outputSection(brief string) string {
	idx := strings.Index(brief, "\n## Output\n")
	if idx < 0 {
		return "<no ## Output section>"
	}
	return brief[idx:]
}
