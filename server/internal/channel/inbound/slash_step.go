package inbound

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

const (
	// slashReplyTag prefixes the /help reply so adapters can spot test hooks.
	slashReplyTag = "[SLASH_HELP]"
	slashStepName = "slash_expand"
)

var indirectBuiltin = regexp.MustCompile(`(?i)^(done|status|comment|assign|priority|label|query|todo|create|confirm|cancel|detail|timeline|logs)\s+(.*)$`)

// slashLabelValueRe matches label tokens allowed by intent rules (patterns.go).
var slashLabelValueRe = regexp.MustCompile(`^[\w-]+$`)

// SlashConfig configures slash command expansion and optional direct replies.
type SlashConfig struct {
	ReplySink   ChannelReplySink
	SendReplies bool
	// Aliases maps slash subcommand (no leading /, case-insensitive) to an
	// expansion template. Templates may be natural-language (applied after
	// placeholder substitution) or indirect builtins like "done {issue_key}".
	// Custom aliases override built-ins with the same name.
	Aliases map[string]string
}

type slashStep struct {
	cfg     SlashConfig
	aliases map[string]string
}

// NewSlashStep returns a Step that expands /commands into natural language
// understood by the rule engine (STA-75). It runs before intent recognition.
func NewSlashStep(cfg SlashConfig) Step {
	merged := make(map[string]string)
	for k, v := range normalizeAliasKeys(cfg.Aliases) {
		merged[k] = v
	}
	return &slashStep{cfg: cfg, aliases: merged}
}

func (slashStep) Name() string { return slashStepName }

func (s *slashStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	text := strings.TrimSpace(evt.Text)
	if !strings.HasPrefix(text, "/") {
		return evt, DecisionContinue, nil
	}

	body := strings.TrimSpace(strings.TrimPrefix(text, "/"))
	cmd, rest := splitCmd(body)

	if cmd == "" || strings.EqualFold(cmd, "help") {
		s.maybeSendReply(ctx, evt, slashHelpText())
		return evt, DecisionSkip, nil
	}

	lcmd := strings.ToLower(cmd)
	if tmpl, ok := s.aliases[lcmd]; ok {
		expanded, ok := expandAliasTemplate(tmpl, rest)
		if !ok {
			return evt, DecisionContinue, nil
		}
		evt.Text = expanded
		evt.Intent.Source = port.SourceCommand
		return evt, DecisionContinue, nil
	}

	expanded, ok := expandBuiltin(lcmd, rest)
	if ok {
		evt.Text = expanded
		evt.Intent.Source = port.SourceCommand
		return evt, DecisionContinue, nil
	}

	return evt, DecisionContinue, nil
}

func splitCmd(body string) (cmd, rest string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ""
	}
	i := strings.IndexByte(body, ' ')
	if i < 0 {
		return body, ""
	}
	return body[:i], strings.TrimSpace(body[i+1:])
}

func splitFirst(s string) (first, rest string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i+1:])
}

func normalizeAliasKeys(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(k, "/")))
		if key == "" {
			continue
		}
		out[key] = v
	}
	return out
}

func expandAliasTemplate(tmpl, rest string) (string, bool) {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return "", false
	}
	sub := substituteSlashPlaceholders(tmpl, rest)
	final, _ := maybeResolveIndirectBuiltin(sub)
	return final, true
}

func substituteSlashPlaceholders(tmpl, rest string) string {
	issueKey, afterKey := splitFirst(rest)
	second, afterSecond := splitFirst(afterKey)
	_ = afterSecond // reserved for future multi-arg templates
	labelVal, labelOK := parseLabelArg(afterKey)

	s := tmpl
	s = strings.ReplaceAll(s, "{issue_key}", issueKey)
	s = strings.ReplaceAll(s, "{user}", strings.TrimSpace(afterKey))
	s = strings.ReplaceAll(s, "{text}", strings.TrimSpace(afterKey))
	s = strings.ReplaceAll(s, "{status}", second)
	s = strings.ReplaceAll(s, "{priority}", second)
	if labelOK {
		s = strings.ReplaceAll(s, "{label}", labelVal)
	} else {
		s = strings.ReplaceAll(s, "{label}", "")
	}
	return s
}

func parseLabelArg(afterIssue string) (label string, ok bool) {
	afterIssue = strings.TrimSpace(afterIssue)
	if afterIssue == "" {
		return "", false
	}
	if strings.HasPrefix(afterIssue, "+") {
		return strings.TrimSpace(afterIssue[1:]), true
	}
	if strings.HasPrefix(afterIssue, "-") {
		return strings.TrimSpace(afterIssue[1:]), true
	}
	return "", false
}

func maybeResolveIndirectBuiltin(s string) (string, bool) {
	s = strings.TrimSpace(s)
	m := indirectBuiltin.FindStringSubmatch(s)
	if m == nil {
		return s, true
	}
	subCmd := strings.ToLower(m[1])
	rest := strings.TrimSpace(m[2])
	return expandBuiltin(subCmd, rest)
}

func expandBuiltin(cmd, rest string) (string, bool) {
	switch cmd {
	case "done":
		key, _ := splitFirst(rest)
		if key == "" || !looksLikeIssueKey(key) {
			return "", false
		}
		return fmt.Sprintf("%s 完成了", key), true
	case "status":
		key, tail := splitFirst(rest)
		st, _ := splitFirst(tail)
		if key == "" || st == "" || !looksLikeIssueKey(key) {
			return "", false
		}
		return fmt.Sprintf("%s 改成 %s", key, st), true
	case "comment":
		key, msg := splitFirst(rest)
		if key == "" || msg == "" || !looksLikeIssueKey(key) {
			return "", false
		}
		return fmt.Sprintf("%s 评论：%s", key, msg), true
	case "assign":
		key, assignee := splitFirst(rest)
		if key == "" || assignee == "" || !looksLikeIssueKey(key) {
			return "", false
		}
		if !strings.HasPrefix(assignee, "@") {
			assignee = "@" + assignee
		}
		return fmt.Sprintf("%s 指派给 %s", key, assignee), true
	case "priority":
		key, pr := splitFirst(rest)
		if key == "" || pr == "" || !looksLikeIssueKey(key) {
			return "", false
		}
		return fmt.Sprintf("%s 改优先级 %s", key, pr), true
	case "label":
		key, labPart := splitFirst(rest)
		if key == "" || labPart == "" || !looksLikeIssueKey(key) {
			return "", false
		}
		labPart = strings.TrimSpace(labPart)
		if strings.HasPrefix(labPart, "+") {
			lbl := strings.TrimSpace(labPart[1:])
			if lbl == "" || !slashLabelValueRe.MatchString(lbl) {
				return "", false
			}
			return fmt.Sprintf("%s 加标签 %s", key, lbl), true
		}
		if strings.HasPrefix(labPart, "-") {
			lbl := strings.TrimSpace(labPart[1:])
			if lbl == "" || !slashLabelValueRe.MatchString(lbl) {
				return "", false
			}
			return fmt.Sprintf("%s 去掉标签 %s", key, lbl), true
		}
		return "", false
	case "query":
		key, rem := splitFirst(rest)
		if key == "" || rem != "" || !looksLikeIssueKey(key) {
			return "", false
		}
		return fmt.Sprintf("%s 现在状态", key), true
	case "detail":
		key, rem := splitFirst(rest)
		if key == "" || rem != "" || !looksLikeIssueKey(key) {
			return "", false
		}
		return fmt.Sprintf("查看详情 %s", key), true
	case "timeline":
		key, tail := splitFirst(rest)
		page, rem := splitFirst(tail)
		if key == "" || !looksLikeIssueKey(key) || rem != "" {
			return "", false
		}
		if page == "" {
			page = "1"
		}
		return fmt.Sprintf("查看动态 %s %s", key, page), true
	case "logs":
		key, tail := splitFirst(rest)
		page, rem := splitFirst(tail)
		if key == "" || !looksLikeIssueKey(key) || rem != "" {
			return "", false
		}
		if page == "" {
			page = "1"
		}
		return fmt.Sprintf("查看日志 %s %s", key, page), true
	case "todo":
		if strings.TrimSpace(rest) != "" {
			return "", false
		}
		return "我的待办", true
	case "create":
		title := strings.TrimSpace(rest)
		if title == "" {
			return "", false
		}
		return "帮我记一个 " + title, true
	case "confirm":
		code, rem := splitFirst(rest)
		if code == "" || rem != "" {
			return "", false
		}
		return fmt.Sprintf("确认操作 %s", strings.ToUpper(code)), true
	case "cancel":
		code, rem := splitFirst(rest)
		if code == "" || rem != "" {
			return "", false
		}
		return fmt.Sprintf("取消操作 %s", strings.ToUpper(code)), true
	default:
		return "", false
	}
}

// patterns.go uses [A-Z]{2,}-\d+; keep slash arg checks aligned with dispatch.
func looksLikeIssueKey(s string) bool {
	return identifierRe.MatchString(s)
}

func slashHelpText() string {
	return slashReplyTag + ` 可用快捷命令：
/done <issue> — 标记完成
/status <issue> <状态> — 修改状态
/comment <issue> <内容> — 添加评论
/assign <issue> @<用户> — 指派
/priority <issue> <级别> — 改优先级
/label <issue> +<标签> / -<标签> — 加/去标签
/query <issue> — 查状态
/detail <issue> — 查完整详情
/timeline <issue> [页码] — 展开动态
/logs <issue> [页码] — 展开执行日志
/todo — 我的待办
/create <标题> — 创建 Issue
/confirm <code> — 确认待执行动作
/cancel <code> — 取消待执行动作`
}

func (s *slashStep) maybeSendReply(ctx context.Context, evt port.InboundEvent, text string) {
	if !s.cfg.SendReplies || s.cfg.ReplySink == nil {
		return
	}
	if err := s.cfg.ReplySink.SendText(ctx, evt, port.OutboundMessage{
		Target: port.TargetChat(evt.ChatID),
		Text:   text,
	}); err != nil {
		slog.Warn("slash_expand: failed to send reply",
			"event_id", evt.EventID,
			"channel_name", evt.ChannelName,
			"chat_id", evt.ChatID,
			"error", err,
		)
	}
}

// SlashAliasesFromPreferencesConnection extracts channel.<connection_id>.slash_aliases
// from notification preferences JSON (best-effort; never panics).
func SlashAliasesFromPreferencesConnection(prefs map[string]any, connectionID string) map[string]string {
	out := map[string]string{}
	if prefs == nil {
		return out
	}
	ch, ok := prefs["channel"].(map[string]any)
	if !ok {
		return out
	}
	connectionPrefs, ok := ch[connectionID].(map[string]any)
	if !ok {
		return out
	}
	raw, ok := connectionPrefs["slash_aliases"].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok || k == "" {
			continue
		}
		out[k] = s
	}
	return out
}

var _ Step = (*slashStep)(nil)
