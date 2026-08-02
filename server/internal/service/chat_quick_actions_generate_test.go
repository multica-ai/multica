package service

import (
	"encoding/json"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

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
