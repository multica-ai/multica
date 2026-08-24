package execenv

import (
	"strings"
	"testing"
)

// The MUL-4899 delivery contract as the BRIEF states it. Two orthogonal
// properties are pinned here and must not be collapsed:
//
//   - The invariant ("never link a local path") is ALWAYS-ON — every task kind,
//     no exceptions. It lives outside writeOutput's kind switch so a future kind
//     cannot silently inherit no invariant at all; this test is what keeps that
//     true.
//   - The surface policy ("here is how a file actually gets delivered HERE") is
//     PER-KIND, and the chat kind splits two ways: web/mobile names the upload
//     command, because a browser renders the bound file as a card and that never
//     changes; a channel-backed chat points at the per-turn user message.
//
// The brief stops there on purpose. Whether the last hop into a room happens is
// a DEPLOYMENT fact — object storage, and a server new enough to report it —
// that flips under a session already running, and the brief is the prompt-cache
// prefix (MUL-5377), so a brief that answered it would render twice for one
// resumed chat. The verdict is stated by the per-turn chat prompt, and both of
// its branches are pinned by TestBuildChatPromptTwoLayerChannelPolicy in
// daemon/prompt_test.go — that is where the "upload works here" / "describe it
// in words" split is covered now.
//
// So the same channel still appears below with both verdicts, but what the rows
// assert has changed: they must render the SAME brief, and
// TestBriefChannelDeliveryCopyIgnoresServerVerdict is the row-level statement
// of it.

// deliveryInvariantFixtures covers every task kind. The chat kind appears six
// times because the surface splits on channel type, and because the three WeCom
// rows carry the verdicts whose briefs must not differ.
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
		// delivery. Turning that storage on or off is an operator action, which
		// is precisely why the brief may not encode the answer.
		"chat_wecom_no_store": {ChatSessionID: "c-1", ChatChannelType: ChannelTypeWecom, AgentName: "Eve", AgentID: "eve-1"},
		// A daemon that knows about file delivery against a server that does
		// not send the field. Identical shape to the row above by construction —
		// an absent field decodes as false — and listed separately because it
		// is a separate way to reach the same state, one an upgrade leaves.
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
		// A channel-backed chat names its platform, defers the verdict to the
		// per-turn message, and states neither answer. Both verdicts' old copy
		// is denied on every such row: "separate message" and the bare upload
		// imperative are the promise, "conversation is text-only" and "does NOT
		// apply" are the denial, and the brief may carry neither — the same
		// session sees both verdicts, so either one is wrong half the time.
		"chat_wecom": {
			mustHave: []string{
				"WeCom conversation depends on how this deployment is configured",
				"the per-turn user message tells you",
				"never report a file as delivered",
			},
			mustNot: []string{
				"run `multica attachment upload",
				"separate message",
				"conversation is text-only",
				"does NOT apply",
			},
		},
		"chat_wecom_no_store": {
			mustHave: []string{
				"WeCom conversation depends on how this deployment is configured",
				"the per-turn user message tells you",
			},
			mustNot: []string{
				"run `multica attachment upload",
				"separate message",
				"conversation is text-only",
				"does NOT apply",
			},
		},
		"chat_wecom_old_server": {
			mustHave: []string{
				"WeCom conversation depends on how this deployment is configured",
				"the per-turn user message tells you",
			},
			mustNot: []string{
				"run `multica attachment upload",
				"separate message",
				"conversation is text-only",
				"does NOT apply",
			},
		},
		// No deployment carries files into Slack or Lark today, but the brief
		// must not say so: wiring that hop is a server-side change the daemon
		// never sees, and a brief holding the denial would keep denying it
		// afterwards. Naming the platform still matters — the deferral has to
		// be about THIS conversation.
		//
		// These two carry the SAME four denials the WeCom rows do, and that is
		// what makes the paragraph above a rule rather than a note. The
		// byte-identity invariant cannot enforce it on its own: a denial
		// written into the brief unconditionally is the same on both values of
		// the verdict, so it renders one brief and passes.
		//
		// A phrase list cannot be the only guard either — it denies the
		// wordings someone thought to name, and the next one will be worded
		// differently. TestBriefChannelDeliveryCopyIsPlatformNeutral holds the
		// same rule with no wording in it, by requiring every channel-backed
		// brief to differ only in the platform it names. Keep both: that one
		// catches any wording, these fail beside the copy being edited.
		"chat_slack": {
			mustHave: []string{
				"Slack conversation depends on how this deployment is configured",
				"the per-turn user message tells you",
				"never report a file as delivered",
			},
			mustNot: []string{
				"run `multica attachment upload",
				"separate message",
				"conversation is text-only",
				"does NOT apply",
			},
		},
		"chat_feishu": {
			mustHave: []string{
				"Feishu/Lark conversation depends on how this deployment is configured",
				"the per-turn user message tells you",
				"never report a file as delivered",
			},
			mustNot: []string{
				"run `multica attachment upload",
				"separate message",
				"conversation is text-only",
				"does NOT apply",
			},
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
		// mustNot is checked against the WHOLE brief, not just `## Output`: a
		// denial is wrong wherever it is written, and scoping the search to
		// the section it usually lands in would let the next one land
		// somewhere else. The phrases are ordinary English, though, so a
		// future section could match one legitimately — quote the text around
		// the actual hit and say which section it was in, or the failure
		// points a reader at an Output section the phrase may not be in.
		for _, phrase := range want.mustNot {
			at := strings.Index(out, phrase)
			if at < 0 {
				continue
			}
			where := "in the `## Output` section"
			if !strings.Contains(outputSection(out), phrase) {
				where = "OUTSIDE the `## Output` section"
			}
			t.Errorf("surface=%s: brief must NOT carry %q — found %s\n--- context ---\n%s",
				name, phrase, where, briefContext(out, at, len(phrase)))
		}
	}
}

// TestBriefChannelDeliveryCopyIgnoresServerVerdict is what the two WeCom rows
// assert now that the brief no longer answers the delivery question.
//
// They used to assert two DIFFERENT briefs, one per verdict, which is the shape
// of the defect: the verdict arrives on every claim, an operator toggling object
// storage or upgrading the server flips it under a chat that is already running,
// and the brief is the prompt-cache prefix. Two briefs for one resumed session
// is a cache miss on every turn after the flip (MUL-5377).
//
// So the property is inverted here rather than dropped. What the verdict
// actually changes still has to be pinned somewhere, and it is: the per-turn
// prompt states it, and TestBuildChatPromptTwoLayerChannelPolicy holds both of
// its branches.
//
// TestBriefByteIdenticalAcrossRunsForEveryKind carries the same guarantee for
// every kind and every per-run field. This one is deliberately narrow and lives
// beside the delivery copy, so an edit that reintroduces the branch fails in the
// file that edit would be made in.
func TestBriefChannelDeliveryCopyIgnoresServerVerdict(t *testing.T) {
	t.Parallel()

	fixtures := deliveryInvariantFixtures()
	// chat_wecom carries the verdict true and chat_wecom_no_store carries it
	// false; the pair is the whole test. chat_wecom_old_server has the same
	// shape as no_store by construction and is included so the row cannot drift
	// away from the invariant unnoticed.
	delivering := buildMetaSkillContent("claude", fixtures["chat_wecom"])
	for _, name := range []string{"chat_wecom_no_store", "chat_wecom_old_server"} {
		got := buildMetaSkillContent("claude", fixtures[name])
		if got != delivering {
			t.Errorf("brief for %q differs from chat_wecom — the server's per-turn file-delivery verdict reached the cached prefix (MUL-5377).\n%s",
				name, firstBriefDiff(delivering, got))
		}
	}
}

// TestBriefChannelDeliveryCopyIsPlatformNeutral is the structural form of the
// rule the phrase lists above state one wording at a time.
//
// A phrase list is open-ended by construction: it denies the wordings someone
// thought to name, and a fifth wording passes. The rule underneath has no
// wording in it — one channel-backed brief may differ from another ONLY in
// which platform it names. Anything platform-specific past the name is either a
// promise or a denial about the last hop, and the brief may carry neither,
// because that hop is a deployment fact stated per turn (MUL-4899).
//
// So this substitutes the display name out and requires the briefs to be
// byte-identical. A per-platform position fails here whatever words it is
// written in. The phrase lists stay: they name the wordings, and they fail
// beside the copy rather than on a whole-brief diff.
//
// A channel type added to channel_type.go belongs in the list below — the
// constants are the only enumeration of them that exists.
func TestBriefChannelDeliveryCopyIsPlatformNeutral(t *testing.T) {
	t.Parallel()

	neutralized := func(channelType string, delivers bool) string {
		out := buildMetaSkillContent("claude", TaskContextForEnv{
			ChatSessionID:            "c-1",
			ChatChannelType:          channelType,
			ChatChannelDeliversFiles: delivers,
			AgentName:                "Eve",
			AgentID:                  "eve-1",
		})
		return strings.ReplaceAll(out, ChannelDisplayName(channelType), "<CHANNEL>")
	}

	// Both verdicts, because the tempting place to write a denial is the
	// branch someone believes cannot deliver today.
	for _, delivers := range []bool{false, true} {
		baseline := neutralized(ChannelTypeSlack, delivers)
		for _, channelType := range []string{ChannelTypeFeishu, ChannelTypeWecom} {
			got := neutralized(channelType, delivers)
			if got != baseline {
				t.Errorf("brief for channel %q (delivers=%v) differs from %q beyond the platform name — the brief took a position on one platform's file delivery, which is a per-turn deployment fact (MUL-4899, MUL-5377).\n%s",
					channelType, delivers, ChannelTypeSlack, firstBriefDiff(baseline, got))
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

// briefContext quotes the brief around a match so a mustNot failure shows the
// sentence that actually tripped it rather than a section it may not be in.
func briefContext(brief string, at, length int) string {
	lo := at - 200
	if lo < 0 {
		lo = 0
	}
	hi := at + length + 200
	if hi > len(brief) {
		hi = len(brief)
	}
	return brief[lo:hi]
}

// outputSection extracts the brief's `## Output` section for readable failures.
func outputSection(brief string) string {
	idx := strings.Index(brief, "\n## Output\n")
	if idx < 0 {
		return "<no ## Output section>"
	}
	return brief[idx:]
}
