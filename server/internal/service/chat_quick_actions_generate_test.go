package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestSelectChatQuickActionsContextExcludesFutureTurnAfterItCompletes(t *testing.T) {
	previousID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	targetID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	futureID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	rows := []db.ChatMessage{
		{Role: "user", Content: "future queued prompt", TaskID: futureID, MessageKind: protocol.ChatMessageKindMessage},
		{Role: "user", Content: "target prompt", TaskID: targetID, MessageKind: protocol.ChatMessageKindMessage},
		{Role: "assistant", Content: "previous reply", TaskID: previousID, MessageKind: protocol.ChatMessageKindMessage},
		{Role: "user", Content: "previous prompt", TaskID: previousID, MessageKind: protocol.ChatMessageKindMessage},
	}
	target := db.ChatMessage{
		Role:        "assistant",
		Content:     "target reply",
		TaskID:      targetID,
		MessageKind: protocol.ChatMessageKindMessage,
	}

	selected := selectChatQuickActionsContext(rows, target, targetID)
	if len(selected) != 4 {
		t.Fatalf("selected %d messages, want previous turn + target turn", len(selected))
	}
	for _, msg := range selected {
		if msg.Content == "future queued prompt" {
			t.Fatal("a later turn must stay out even if its task completed before generation")
		}
	}
}

func TestSelectChatQuickActionsContextIncludesAutoRetryInputOwner(t *testing.T) {
	rootID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	retryID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	rows := []db.ChatMessage{
		{Role: "user", Content: "root prompt", TaskID: rootID, MessageKind: protocol.ChatMessageKindMessage},
	}
	target := db.ChatMessage{
		Role:        "assistant",
		Content:     "retry reply",
		TaskID:      retryID,
		MessageKind: protocol.ChatMessageKindMessage,
	}

	selected := selectChatQuickActionsContext(rows, target, rootID)
	if len(selected) != 2 || selected[0].Content != "root prompt" {
		t.Fatalf("selected messages = %+v, want retry input followed by its reply", selected)
	}
}

func chatMsg(role, content string, actions ...protocol.ChatQuickAction) db.ChatMessage {
	msg := db.ChatMessage{
		Role:        role,
		Content:     content,
		MessageKind: protocol.ChatMessageKindMessage,
	}
	if len(actions) > 0 {
		encoded, err := json.Marshal(actions)
		if err != nil {
			panic(err)
		}
		msg.QuickActions = encoded
	}
	return msg
}

func TestRenderChatQuickActionsContextLabelsSpeakersAndClosesWithTheTask(t *testing.T) {
	out := renderChatQuickActionsContext([]db.ChatMessage{
		chatMsg("user", "why is this slow"),
		chatMsg("assistant", "because it spawns a CLI per turn"),
	}, nil)

	if !strings.Contains(out, "[user]: why is this slow") {
		t.Fatalf("user turn missing or mislabeled:\n%s", out)
	}
	// The role label must be "agent", not the DB's "assistant": the prompt
	// talks about the agent's reply throughout.
	if !strings.Contains(out, "[agent]: because it spawns a CLI per turn") {
		t.Fatalf("assistant turn must render as [agent]:\n%s", out)
	}
	if !strings.Contains(out, "ALREADY SUGGESTED (do not repeat or paraphrase):\n(none)") {
		t.Fatalf("empty previous-suggestion section must read (none):\n%s", out)
	}
	if !strings.HasSuffix(out, "Produce the follow-up suggestions for the latest agent reply.") {
		t.Fatalf("prompt must end with the instruction:\n%s", out)
	}
}

func TestRenderChatQuickActionsContextListsPreviousLabels(t *testing.T) {
	out := renderChatQuickActionsContext(
		[]db.ChatMessage{chatMsg("assistant", "done")},
		[]string{"看下 diff", "老 daemon 的影响"},
	)
	if !strings.Contains(out, "- 看下 diff\n- 老 daemon 的影响") {
		t.Fatalf("previous labels must be listed verbatim:\n%s", out)
	}
	if strings.Contains(out, "(none)") {
		t.Fatalf("(none) must not appear when labels exist:\n%s", out)
	}
}

// The anchor reply keeps BOTH ends when it is too long: the tail carries the
// conclusion and proposed next steps, which is exactly the material the
// suggestions are built from, so a head-only cut would strip the most useful
// part of the input.
func TestRenderChatQuickActionsContextKeepsHeadAndTailOfLongLatestReply(t *testing.T) {
	head := strings.Repeat("H", 500)
	middle := strings.Repeat("M", chatQuickActionsLatestBudget)
	tail := strings.Repeat("T", 500)
	out := renderChatQuickActionsContext([]db.ChatMessage{
		chatMsg("assistant", head+middle+tail),
	}, nil)

	if !strings.Contains(out, head) {
		t.Fatal("truncated latest reply must keep its head")
	}
	if !strings.Contains(out, tail) {
		t.Fatal("truncated latest reply must keep its tail")
	}
	if !strings.Contains(out, "…[truncated]…") {
		t.Fatal("truncation must be marked so the model does not read across the cut")
	}
	if strings.Contains(out, middle) {
		t.Fatal("the middle of an over-long reply must be dropped")
	}
}

func TestRenderChatQuickActionsContextLeavesShortLatestReplyIntact(t *testing.T) {
	reply := strings.Repeat("x", chatQuickActionsLatestBudget)
	out := renderChatQuickActionsContext([]db.ChatMessage{chatMsg("assistant", reply)}, nil)
	if strings.Contains(out, "…[truncated]…") {
		t.Fatal("a reply exactly at the budget must not be truncated")
	}
}

func TestRenderChatQuickActionsContextTruncatesOlderMessagesToTheSmallerBudget(t *testing.T) {
	long := strings.Repeat("y", chatQuickActionsOlderBudget+200)
	out := renderChatQuickActionsContext([]db.ChatMessage{
		chatMsg("user", long),
		chatMsg("assistant", "short reply"),
	}, nil)

	if strings.Contains(out, long) {
		t.Fatal("an older message over budget must be cut")
	}
	if !strings.Contains(out, strings.Repeat("y", chatQuickActionsOlderBudget)+"…") {
		t.Fatalf("older messages are head-truncated with an ellipsis:\n%s", out)
	}
}

func TestCollectPreviousChatQuickActionsPrefersNewestAndDedupes(t *testing.T) {
	msgs := []db.ChatMessage{
		chatMsg("assistant", "older", protocol.ChatQuickAction{Label: "Old one"}),
		chatMsg("user", "next"),
		chatMsg("assistant", "newer",
			protocol.ChatQuickAction{Label: "New one"},
			// Case-insensitive duplicate of the older label: one entry only.
			protocol.ChatQuickAction{Label: "OLD ONE"},
		),
	}

	got := collectPreviousChatQuickActions(msgs)
	want := []string{"New one", "OLD ONE"}
	if len(got) != len(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %v, want %v (newest first, deduped)", got, want)
		}
	}
}

func TestCollectPreviousChatQuickActionsCapsTheList(t *testing.T) {
	var msgs []db.ChatMessage
	for i := 0; i < chatQuickActionsPreviousMax+3; i++ {
		msgs = append(msgs, chatMsg("assistant", "reply", protocol.ChatQuickAction{
			Label: string(rune('a'+i)) + " label",
		}))
	}
	if got := collectPreviousChatQuickActions(msgs); len(got) != chatQuickActionsPreviousMax {
		t.Fatalf("collected %d labels, want the cap of %d", len(got), chatQuickActionsPreviousMax)
	}
}

func TestCollectPreviousChatQuickActionsSkipsUnparseableAndBlank(t *testing.T) {
	msgs := []db.ChatMessage{
		{Role: "assistant", QuickActions: []byte("not json")},
		chatMsg("assistant", "reply", protocol.ChatQuickAction{Label: "   "}),
		chatMsg("assistant", "reply", protocol.ChatQuickAction{Label: "Real"}),
	}
	got := collectPreviousChatQuickActions(msgs)
	if len(got) != 1 || got[0] != "Real" {
		t.Fatalf("labels = %v, want just [Real]", got)
	}
}

// The pass is asked for {"actions":[...]} because response_format=json_object
// rejects a top-level array.
func TestParseChatQuickActionsOutputAcceptsObjectShape(t *testing.T) {
	actions := parseChatQuickActionsOutput(`{"actions":[{"label":"Ship it","prompt":"open the PR","primary":true}]}`)
	if len(actions) != 1 || actions[0].Label != "Ship it" || !actions[0].Primary {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestParseChatQuickActionsOutputAcceptsEmptyObjectShape(t *testing.T) {
	if actions := parseChatQuickActionsOutput(`{"actions":[]}`); len(actions) != 0 {
		t.Fatalf("actions = %+v, want none", actions)
	}
}

// The bare array stays supported: it is what the bracket fallback extracts out
// of prose, and a model that drops the wrapper should not cost the user pills.
func TestParseChatQuickActionsOutputStillAcceptsBareArray(t *testing.T) {
	actions := parseChatQuickActionsOutput(`[{"label":"Bare","prompt":"still parsed"}]`)
	if len(actions) != 1 || actions[0].Label != "Bare" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestParseChatQuickActionsOutputAcceptsFencedObject(t *testing.T) {
	raw := "Here you go:\n```json\n" + `{"actions":[{"label":"Fenced","prompt":"p"}]}` + "\n```"
	actions := parseChatQuickActionsOutput(raw)
	if len(actions) != 1 || actions[0].Label != "Fenced" {
		t.Fatalf("actions = %+v", actions)
	}
}

// The MUL-5689 shape, with every pull toward the wrong language present at
// once: an older Chinese turn, a Chinese agent reply, Chinese labels replayed
// under ALREADY SUGGESTED — and the user's newest turn in English. The rendered
// prompt must close by pointing at that newest [user] turn and disowning the
// rest.
func TestRenderChatQuickActionsContextClosesWithTheLanguageRule(t *testing.T) {
	out := renderChatQuickActionsContext([]db.ChatMessage{
		chatMsg("user", "帮我看下这个 PR"),
		chatMsg("user", "review this PR for simplifications"),
		chatMsg("assistant", "已创建工单 EFF-359，并分配给了 Claude Engineer。"),
	}, []string{"查看工单详情", "补充审查范围"})

	// Last thing before the task line: the conversation above it is Chinese, so
	// anything earlier would be read through that.
	if !strings.Contains(out, chatQuickActionsLanguageRule+"\n\nProduce the follow-up") {
		t.Fatalf("language rule must be the final constraint:\n%s", out)
	}
	// Anchored on the newest user turn — not the window, not the agent.
	if !strings.Contains(out, "same language as the most recent [user] message") {
		t.Fatalf("rule must anchor on the most recent user turn:\n%s", out)
	}
	for _, disowned := range []string{"agent's reply", "older messages", "the system instructions", "ALREADY SUGGESTED"} {
		if !strings.Contains(chatQuickActionsLanguageRule, disowned) {
			t.Fatalf("rule must explicitly exclude %q: %s", disowned, chatQuickActionsLanguageRule)
		}
	}
	// The Chinese context is still delivered verbatim; the rule governs output,
	// it does not scrub the input.
	if !strings.Contains(out, "已创建工单 EFF-359") || !strings.Contains(out, "- 查看工单详情") {
		t.Fatalf("conversation and previous labels must survive intact:\n%s", out)
	}
}
