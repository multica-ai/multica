package workflows

import (
	"encoding/json"
	"testing"
)

func TestTriggerMatches_StatusChangedNoFilter(t *testing.T) {
	wf := workflow{triggerType: TriggerStatusChanged, triggerConfig: nil}
	te := TriggerEvent{Type: TriggerStatusChanged, FromStatus: "todo", ToStatus: "in_review"}
	if !triggerMatches(wf, te) {
		t.Fatal("workflow with no trigger_config must match any status transition")
	}
}

func TestTriggerMatches_FromToFilter(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigStatusChanged{FromStatus: "todo", ToStatus: "in_review"})
	wf := workflow{triggerType: TriggerStatusChanged, triggerConfig: cfg}

	cases := []struct {
		from, to string
		want     bool
	}{
		{"todo", "in_review", true},
		{"in_progress", "in_review", false}, // wrong from
		{"todo", "done", false},             // wrong to
		{"in_progress", "done", false},
	}
	for _, c := range cases {
		got := triggerMatches(wf, TriggerEvent{Type: TriggerStatusChanged, FromStatus: c.from, ToStatus: c.to})
		if got != c.want {
			t.Errorf("triggerMatches(%s→%s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestTriggerMatches_OnlyToFilter(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigStatusChanged{ToStatus: "done"})
	wf := workflow{triggerType: TriggerStatusChanged, triggerConfig: cfg}
	if !triggerMatches(wf, TriggerEvent{Type: TriggerStatusChanged, FromStatus: "todo", ToStatus: "done"}) {
		t.Fatal("workflow with only to_status set should match any → done")
	}
	if triggerMatches(wf, TriggerEvent{Type: TriggerStatusChanged, FromStatus: "todo", ToStatus: "blocked"}) {
		t.Fatal("workflow with to_status=done must not match → blocked")
	}
}

func TestRenderTitle_SubstitutesTitle(t *testing.T) {
	raw := map[string]any{
		"issue": map[string]any{"title": "Login bug", "priority": "high"},
	}
	got := renderTitle("Follow-up: {{title}} ({{priority}})", raw)
	want := "Follow-up: Login bug (high)"
	if got != want {
		t.Errorf("renderTitle = %q, want %q", got, want)
	}
}

func TestRenderTitle_HandlesMissingPayload(t *testing.T) {
	if got := renderTitle("static", nil); got != "static" {
		t.Errorf("renderTitle with nil raw = %q, want %q", got, "static")
	}
	if got := renderTitle("static", map[string]any{}); got != "static" {
		t.Errorf("renderTitle with empty raw = %q, want %q", got, "static")
	}
}

func TestRetryBackoffsMatchSpec(t *testing.T) {
	// Pin the spec-locked backoffs (1 / 5 / 15 min). A future tweak to the
	// schedule must update this test alongside the runtime.
	if len(retryBackoffs) != 3 {
		t.Fatalf("retryBackoffs length = %d, want 3", len(retryBackoffs))
	}
	want := []int{1, 5, 15}
	for i, d := range retryBackoffs {
		if int(d.Minutes()) != want[i] {
			t.Errorf("retryBackoffs[%d] = %v, want %d minutes", i, d, want[i])
		}
	}
	if maxAttempts != len(retryBackoffs)+1 {
		t.Fatalf("maxAttempts must be retryBackoffs+1 to leave room for the initial run")
	}
}

// --- Phase-3 trigger match tests (JEH-1108) ---

func TestTriggerMatches_CommentMentionAgent(t *testing.T) {
	target := "11111111-1111-1111-1111-111111111111"
	cfg, _ := json.Marshal(TriggerConfigCommentMention{
		MatchMode: CommentMatchAgent,
		Target:    target,
	})
	wf := workflow{triggerType: TriggerCommentMention, triggerConfig: cfg}

	makeEvent := func(content string) TriggerEvent {
		return TriggerEvent{
			Type: TriggerCommentMention,
			Raw: map[string]any{
				"comment": map[string]any{"content": content},
			},
		}
	}

	yes := makeEvent("hi [@Rasp](mention://agent/" + target + ") fix this")
	if !triggerMatches(wf, yes) {
		t.Fatal("agent-mention with matching target must fire")
	}
	wrong := makeEvent("hi [@Other](mention://agent/22222222-2222-2222-2222-222222222222)")
	if triggerMatches(wf, wrong) {
		t.Fatal("agent-mention with non-matching target must not fire")
	}
	noMention := makeEvent("just a plain comment")
	if triggerMatches(wf, noMention) {
		t.Fatal("comment without mention must not fire")
	}
}

func TestTriggerMatches_CommentMentionMember(t *testing.T) {
	target := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	cfg, _ := json.Marshal(TriggerConfigCommentMention{
		MatchMode: CommentMatchMember,
		Target:    target,
	})
	wf := workflow{triggerType: TriggerCommentMention, triggerConfig: cfg}

	te := TriggerEvent{
		Type: TriggerCommentMention,
		Raw: map[string]any{
			"comment": map[string]any{
				"content": "thanks [@Sara](mention://member/" + target + ")",
			},
		},
	}
	if !triggerMatches(wf, te) {
		t.Fatal("member-mention with matching target must fire")
	}
	// Same UUID but as agent type → no match (mode is member).
	te.Raw["comment"].(map[string]any)["content"] = "[@x](mention://agent/" + target + ")"
	if triggerMatches(wf, te) {
		t.Fatal("agent-typed mention must not match member mode")
	}
}

func TestTriggerMatches_CommentMentionKeyword(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigCommentMention{
		MatchMode: CommentMatchKeyword,
		Target:    "URGENT",
	})
	wf := workflow{triggerType: TriggerCommentMention, triggerConfig: cfg}

	makeEvent := func(content string) TriggerEvent {
		return TriggerEvent{
			Type: TriggerCommentMention,
			Raw: map[string]any{
				"comment": map[string]any{"content": content},
			},
		}
	}

	if !triggerMatches(wf, makeEvent("This is urgent please")) {
		t.Fatal("keyword match must be case-insensitive")
	}
	if !triggerMatches(wf, makeEvent("URGENT!!!")) {
		t.Fatal("exact keyword case must still match")
	}
	if triggerMatches(wf, makeEvent("nothing to see")) {
		t.Fatal("non-matching content must not fire")
	}
}

func TestTriggerMatches_CommentMentionRegex(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigCommentMention{
		MatchMode: CommentMatchRegex,
		Target:    `(?i)deploy(ed)?\b`,
	})
	wf := workflow{triggerType: TriggerCommentMention, triggerConfig: cfg}

	makeEvent := func(content string) TriggerEvent {
		return TriggerEvent{
			Type: TriggerCommentMention,
			Raw: map[string]any{
				"comment": map[string]any{"content": content},
			},
		}
	}

	if !triggerMatches(wf, makeEvent("we deploy at 5")) {
		t.Fatal("regex must match 'deploy'")
	}
	if !triggerMatches(wf, makeEvent("Just deployed!")) {
		t.Fatal("regex must match 'deployed'")
	}
	if triggerMatches(wf, makeEvent("plain text")) {
		t.Fatal("regex must not match unrelated content")
	}

	// Malformed regex must not fire (and must not panic).
	badCfg, _ := json.Marshal(TriggerConfigCommentMention{
		MatchMode: CommentMatchRegex,
		Target:    `[unclosed`,
	})
	badWF := workflow{triggerType: TriggerCommentMention, triggerConfig: badCfg}
	if triggerMatches(badWF, makeEvent("anything")) {
		t.Fatal("malformed regex must fail closed")
	}
}

func TestTriggerMatches_SubIssueCreated_AnyParent(t *testing.T) {
	// Empty ParentIssueID → any sub-issue matches.
	wf := workflow{triggerType: TriggerSubIssueCreated, triggerConfig: nil}
	te := TriggerEvent{
		Type:          TriggerSubIssueCreated,
		ParentIssueID: "anything-uuid",
		Raw:           map[string]any{"issue": map[string]any{"parent_issue_id": "anything-uuid"}},
	}
	if !triggerMatches(wf, te) {
		t.Fatal("empty parent filter must match any sub-issue")
	}
}

func TestTriggerMatches_SubIssueCreated_SpecificParent(t *testing.T) {
	parent := "55555555-5555-5555-5555-555555555555"
	other := "66666666-6666-6666-6666-666666666666"
	cfg, _ := json.Marshal(TriggerConfigSubIssueCreated{ParentIssueID: parent})
	wf := workflow{triggerType: TriggerSubIssueCreated, triggerConfig: cfg}

	yes := TriggerEvent{
		Type:          TriggerSubIssueCreated,
		ParentIssueID: parent,
		Raw:           map[string]any{"issue": map[string]any{"parent_issue_id": parent}},
	}
	if !triggerMatches(wf, yes) {
		t.Fatal("parent match must fire")
	}
	no := TriggerEvent{
		Type:          TriggerSubIssueCreated,
		ParentIssueID: other,
		Raw:           map[string]any{"issue": map[string]any{"parent_issue_id": other}},
	}
	if triggerMatches(wf, no) {
		t.Fatal("parent mismatch must not fire")
	}
}

func TestTriggerMatches_AllChildrenDone_TypeOnly(t *testing.T) {
	// all_children_done has no per-config filter — it matches on type alone
	// (Dispatch already filtered by trigger_type).
	wf := workflow{triggerType: TriggerAllChildrenDone, triggerConfig: nil}
	te := TriggerEvent{Type: TriggerAllChildrenDone}
	if !triggerMatches(wf, te) {
		t.Fatal("all_children_done must match on type alone")
	}
}

func TestTriggerMatches_CronAndWebhookInbound_TypeOnly(t *testing.T) {
	for _, trig := range []string{TriggerCron, TriggerWebhookInbound} {
		wf := workflow{triggerType: trig}
		te := TriggerEvent{Type: trig}
		if !triggerMatches(wf, te) {
			t.Errorf("trigger %q must match on type alone", trig)
		}
	}
}

func TestEnvFlagEnabled(t *testing.T) {
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"yes":   true,
		"on":    true,
		"":      false,
		"0":     false,
		"false": false,
		"maybe": false,
	}
	for in, want := range cases {
		t.Setenv("TEST_FLAG", in)
		if got := envFlagEnabled("TEST_FLAG"); got != want {
			t.Errorf("envFlagEnabled(%q) = %v, want %v", in, got, want)
		}
	}
}
