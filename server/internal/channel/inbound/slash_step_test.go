package inbound_test

import (
	"context"
	"strings"
	"testing"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	chcommand "github.com/multica-ai/multica/server/internal/channel/command"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ruleIntentStep runs the production deterministic command matcher.
type ruleIntentStep struct{}

func (ruleIntentStep) Name() string { return "command-recog" }

func (ruleIntentStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	m := chcommand.NewRuleMatcher()
	if in, ok := m.Match(evt.Text); ok {
		evt.Intent = port.InboundIntent{
			Kind:       port.IntentKind(in.Kind),
			Confidence: in.Confidence,
			Params:     in.Params,
			Source:     port.SourceRule,
		}
		return evt, inbound.DecisionContinue, nil
	}
	evt.Intent = port.InboundIntent{Kind: port.IntentUnknown, Source: port.SourceRule}
	return evt, inbound.DecisionContinue, nil
}

func assertRuleHit(t *testing.T, expanded string) {
	t.Helper()
	m := chcommand.NewRuleMatcher()
	in, ok := m.Match(expanded)
	if !ok {
		t.Fatalf("rule matcher missed expanded text %q", expanded)
	}
	if in.Confidence != 1 {
		t.Fatalf("confidence = %f, want 1", in.Confidence)
	}
	if in.Source != chaction.SourceRule {
		t.Fatalf("source = %s, want rule", in.Source)
	}
}

func runSlash(t *testing.T, text string, aliases map[string]string) port.InboundEvent {
	t.Helper()
	step := inbound.NewSlashStep(inbound.SlashConfig{Aliases: aliases})
	evt := port.InboundEvent{Text: text}
	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Fatalf("Decision = %v, want Continue", d)
	}
	return out
}

func TestSlashStep_BuiltinExpansionsHitRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"/done STA-68", "STA-68 完成了"},
		{"/status STA-68 in_review", "STA-68 改成 in_review"},
		{"/comment STA-68 修复了", "STA-68 评论：修复了"},
		{"/assign STA-68 @张三", "STA-68 指派给 @张三"},
		{"/assign STA-68 李四", "STA-68 指派给 @李四"},
		{"/priority STA-68 high", "STA-68 改优先级 high"},
		{"/label STA-68 +bug", "STA-68 加标签 bug"},
		{"/label STA-68 -bug", "STA-68 去掉标签 bug"},
		{"/query STA-68", "STA-68 现在状态"},
		{"/detail STA-68", "查看详情 STA-68"},
		{"/timeline STA-68", "查看动态 STA-68 1"},
		{"/timeline STA-68 2", "查看动态 STA-68 2"},
		{"/logs STA-68", "查看日志 STA-68 1"},
		{"/logs STA-68 3", "查看日志 STA-68 3"},
		{"/todo", "我的待办"},
		{"/create 登录优化", "帮我记一个 登录优化"},
	}
	for _, tc := range cases {
		evt := runSlash(t, tc.in, nil)
		if evt.Text != tc.want {
			t.Fatalf("%q → got %q, want %q", tc.in, evt.Text, tc.want)
		}
		if evt.Intent.Source != port.SourceCommand {
			t.Fatalf("%q source = %q, want command", tc.in, evt.Intent.Source)
		}
		assertRuleHit(t, evt.Text)
	}
}

func TestSlashStep_Help_SkipAndReply(t *testing.T) {
	t.Parallel()
	cfg, _, _, recCh := buildDispatchConfig()
	step := inbound.NewSlashStep(inbound.SlashConfig{
		ReplySink:   cfg.ReplySink,
		SendReplies: true,
	})
	for _, text := range []string{"/", "/help", "/HELP"} {
		t.Run(text, func(t *testing.T) {
			evt := port.InboundEvent{
				ChannelName: "feishu",
				Text:        text,
				ChatID:      "c1",
			}
			_, d, err := step.Run(context.Background(), evt)
			if err != nil {
				t.Fatal(err)
			}
			if d != inbound.DecisionSkip {
				t.Fatalf("Decision = %v, want Skip", d)
			}
		})
	}
	if len(recCh.sends) != 3 {
		t.Fatalf("expected 3 help replies, got %d", len(recCh.sends))
	}
	for _, msg := range recCh.sends {
		if !strings.HasPrefix(msg.Text, "[SLASH_HELP]") {
			t.Fatalf("help reply missing tag: %q", msg.Text)
		}
		if !strings.Contains(msg.Text, "/done") {
			t.Fatalf("help body missing /done: %q", msg.Text)
		}
	}
}

func TestSlashStep_NonSlashBypass(t *testing.T) {
	t.Parallel()
	step := inbound.NewSlashStep(inbound.SlashConfig{})
	evt := port.InboundEvent{Text: "STA-68 完成了"}
	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if d != inbound.DecisionContinue {
		t.Fatalf("Decision = %v", d)
	}
	if out.Text != evt.Text {
		t.Fatalf("text mutated: %q", out.Text)
	}
}

func TestSlashStep_UnknownSlashBypass(t *testing.T) {
	t.Parallel()
	step := inbound.NewSlashStep(inbound.SlashConfig{})
	evt := port.InboundEvent{Text: "/not-a-real-command STA-68"}
	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if d != inbound.DecisionContinue {
		t.Fatalf("Decision = %v", d)
	}
	if out.Text != evt.Text {
		t.Fatalf("text should stay unchanged for unknown slash, got %q", out.Text)
	}
}

func TestSlashStep_InvalidLabelTokenPassesThrough(t *testing.T) {
	t.Parallel()
	step := inbound.NewSlashStep(inbound.SlashConfig{})
	for _, text := range []string{
		`/label STA-68 +bad label`,
		`/label STA-68 +bad/name`,
	} {
		evt := port.InboundEvent{Text: text}
		out, d, err := step.Run(context.Background(), evt)
		if err != nil {
			t.Fatal(err)
		}
		if d != inbound.DecisionContinue {
			t.Fatalf("%q: Decision = %v", text, d)
		}
		if out.Text != evt.Text {
			t.Fatalf("%q: expected unchanged text, got %q", text, out.Text)
		}
	}
}

func TestSlashStep_CustomAliasOverridesBuiltin(t *testing.T) {
	t.Parallel()
	evt := runSlash(t, "/done STA-68", map[string]string{
		"done": "{issue_key} 改成 in_review",
	})
	if evt.Text != "STA-68 改成 in_review" {
		t.Fatalf("got %q", evt.Text)
	}
	assertRuleHit(t, evt.Text)
}

func TestSlashStep_CustomAliasIndirectBuiltin(t *testing.T) {
	t.Parallel()
	evt := runSlash(t, "/finish STA-68", map[string]string{
		"finish": "done {issue_key}",
	})
	if evt.Text != "STA-68 完成了" {
		t.Fatalf("got %q", evt.Text)
	}
	assertRuleHit(t, evt.Text)
}

func TestSlashStep_CustomAliasPlaceholders(t *testing.T) {
	t.Parallel()
	evt := runSlash(t, "/note STA-39 hello world", map[string]string{
		"note": "{issue_key} 评论：{text}",
	})
	if evt.Text != "STA-39 评论：hello world" {
		t.Fatalf("got %q", evt.Text)
	}
	assertRuleHit(t, evt.Text)
}

func TestSlashAliasesFromPreferencesConnection(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"channel": map[string]any{
			"conn-a": map[string]any{
				"issues":        true,
				"slash_aliases": map[string]any{"finish": "done {issue_key}"},
			},
		},
	}
	got := inbound.SlashAliasesFromPreferencesConnection(raw, "conn-a")
	if got["finish"] != "done {issue_key}" {
		t.Fatalf("got %#v", got)
	}
}

func TestPipeline_SlashDoneEndToEnd(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:         uuid(0xAA),
		Identifier: "STA-68",
		Title:      "test",
		Status:     "in_progress",
	}

	store := &fakeDedupStore{responses: []dedupResp{{Inserted: true}}}
	p := inbound.NewPipeline(
		inbound.NewNormalizeStep(),
		inbound.NewDedupStep(store),
		inbound.NewSlashStep(inbound.SlashConfig{}),
		ruleIntentStep{},
		inbound.NewDispatchStep(cfg),
	)

	out, err := p.Run(context.Background(), port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-slash-e2e",
		ChatID:      "chat-1",
		SenderID:    "ou_sender1",
		Type:        port.EventTypeMessageReceived,
		Text:        "/done STA-68",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Terminal != "dispatch" {
		t.Fatalf("terminal = %q", out.Terminal)
	}
	if len(issueSvc.setStatus) != 1 {
		t.Fatalf("SetIssueStatus calls = %d", len(issueSvc.setStatus))
	}
	if issueSvc.setStatus[0].Status != "done" {
		t.Fatalf("status = %q", issueSvc.setStatus[0].Status)
	}
	if len(recCh.sends) != 1 || !strings.Contains(recCh.sends[0].Text, "STATUS_CHANGED") {
		t.Fatalf("unexpected reply: %+v", recCh.sends)
	}
}

func TestPipeline_SlashHelp_StopsBeforeIntent(t *testing.T) {
	t.Parallel()
	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0xAA), Identifier: "STA-68"}

	store := &fakeDedupStore{responses: []dedupResp{{Inserted: true}}}
	p := inbound.NewPipeline(
		inbound.NewNormalizeStep(),
		inbound.NewDedupStep(store),
		inbound.NewSlashStep(inbound.SlashConfig{
			ReplySink:   cfg.ReplySink,
			SendReplies: true,
		}),
		ruleIntentStep{},
		inbound.NewDispatchStep(cfg),
	)
	out, err := p.Run(context.Background(), port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-slash-help",
		ChatID:      "chat-1",
		SenderID:    "ou_sender1",
		Type:        port.EventTypeMessageReceived,
		Text:        "/help",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Terminal != "slash_expand" || out.Decision != inbound.DecisionSkip {
		t.Fatalf("outcome = %+v", out)
	}
	if len(issueSvc.setStatus) != 0 {
		t.Fatal("dispatch should not run")
	}
	if len(recCh.sends) != 1 || !strings.HasPrefix(recCh.sends[0].Text, "[SLASH_HELP]") {
		t.Fatalf("got sends: %+v", recCh.sends)
	}
}
