package intent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
)

var (
	legalIssueKeyRe = regexp.MustCompile(`(?i)\b[A-Z]{2,5}-[1-9][0-9]*\b`)
	issueLikeKeyRe  = regexp.MustCompile(`(?i)\b[A-Z]+-\d+\b`)
)

// IntentRequest is the stable input every resolver sees.
type IntentRequest struct {
	WorkspaceID      string
	DefaultProjectID string
	// AgentID, when non-empty, forces channel intent to use that agent only
	// (no fallback to another agent if unavailable).
	AgentID         string
	Text            string
	Channel         string
	ConnectionID    string
	ChatID          string
	ChatType        string
	SenderID        string
	SenderName      string
	InboundEventID  string
	SourceHint      IntentSource
	ContextIssueKey string
	ContextMode     string

	ThreadID         string
	QuotedMessageID  string
	QuotedText       string
	ReplyToMessageID string

	// ContextEntities carries recent entity references from channel messages
	// in this conversation and sender scope.
	ContextEntities []channelconversation.EntityRef
	// ExplicitEntities carries entities derived from explicit platform signals
	// such as quote/reply targets. It has higher priority than temporal
	// conversation context.
	ExplicitEntities []channelconversation.EntityRef
}

// IntentResult is a resolver's answer. Matched=false lets the chain continue.
type IntentResult struct {
	Matched bool
	Intent  Intent
	Reply   string
}

// IntentResolver turns a channel message into one structured Intent.
type IntentResolver interface {
	Name() string
	Resolve(ctx context.Context, req IntentRequest) (IntentResult, error)
}

type RuleResolver struct {
	matcher RuleMatcher
}

func NewRuleResolver(matcher RuleMatcher) *RuleResolver {
	if matcher == nil {
		matcher = NewRuleMatcher()
	}
	return &RuleResolver{matcher: matcher}
}

func (*RuleResolver) Name() string { return "rule" }

func (r *RuleResolver) Resolve(_ context.Context, req IntentRequest) (IntentResult, error) {
	in, ok := r.matcher.Match(req.Text)
	if !ok {
		return IntentResult{}, nil
	}
	if req.SourceHint == SourceCommand {
		in.Source = SourceCommand
	}
	return IntentResult{Matched: true, Intent: in}, nil
}

type ChannelAgentTurnClient interface {
	StartAgentTurn(ctx context.Context, req IntentRequest) (string, error)
	ParseAgentTurnResult(ctx context.Context, taskID string) (string, bool, error)
}

func BuildChannelAgentTurnPrompt(req IntentRequest) string {
	var b strings.Builder
	b.WriteString("You are handling a Multica channel message as a teammate in a work chat.\n")
	b.WriteString("This is NOT an intent-classification task. Use the existing `multica` CLI when you need workspace facts or need to make low-risk changes.\n\n")
	b.WriteString("User-visible reply rules:\n")
	b.WriteString("- Reply naturally and concisely in the user's language.\n")
	b.WriteString("- Never expose internal tags such as [ASK_CLARIFY], UNKNOWN, JSON plans, task IDs, or implementation labels.\n")
	b.WriteString("- If a critical parameter is missing, ask one clear question instead of guessing.\n")
	b.WriteString("- Do not perform delete or irreversible/destructive operations from channel. Explain that this is not supported here.\n\n")
	b.WriteString("Work rules:\n")
	b.WriteString("- For workspace or project progress questions, do not stop at project records. If no explicit projects exist, use `multica issue list --output json` and summarize open, blocked, in_review, and recently active issues.\n")
	b.WriteString("- For issue progress questions, use `multica issue get <id> --output json` and `multica issue comment list <id> --output json`. Include status, assignee if useful, the latest meaningful member/agent reply, and the next step.\n")
	b.WriteString("- For creates and updates, use the existing CLI such as `multica issue create`, `multica issue status`, `multica issue assign`, and `multica issue comment add --content-stdin`.\n")
	b.WriteString("- When the user asks to close, cancel, drop, abandon, stop, park, or no longer do an existing issue in any language, treat it as an issue status update to `cancelled`; do not treat it as deleting the issue or cancelling a confirmation/action code.\n")
	b.WriteString("- Only interpret cancellation as cancelling a pending confirmation/action code when the message explicitly names a confirmation code or command such as `/cancel <code>`.\n")
	b.WriteString("- For comments, if the user named the issue but did not provide comment body, ask what they want to write. Do not invent the comment.\n")
	b.WriteString("- For comments or mutations on an existing issue, the target must be resolvable from ExplicitEntities/ContextEntities or an explicit issue key in the message.\n\n")
	fmt.Fprintf(&b, "Workspace ID: %s\nDefault project ID: %s\nChannel: %s\nConnection ID: %s\nChat ID: %s\nChat type: %s\nSender: %s (%s)\n", req.WorkspaceID, req.DefaultProjectID, req.Channel, req.ConnectionID, req.ChatID, req.ChatType, req.SenderName, req.SenderID)
	appendContextSignals(&b, req)
	b.WriteString("\nUser message:\n")
	b.WriteString(req.Text)
	b.WriteString("\n\nFinal output:\n")
	b.WriteString("Write the exact message that should be sent back to the channel. If you performed a CLI mutation, summarize what changed and mention any relevant issue key.\n")
	return b.String()
}

func appendContextSignals(b *strings.Builder, req IntentRequest) {
	if len(req.ExplicitEntities) > 0 {
		b.WriteString("\nExplicit context:\n")
		b.WriteString("User explicitly referenced these entities, highest priority:\n")
		for _, e := range req.ExplicitEntities {
			fmt.Fprintf(b, "- %s (%s)\n", e.EntityKey, e.EntityType)
		}
	}
	if len(req.ContextEntities) > 0 {
		b.WriteString("\nConversation context:\n")
		b.WriteString("Recent entities from this conversation:\n")
		for _, e := range req.ContextEntities {
			fmt.Fprintf(b, "- %s (%s)\n", e.EntityKey, e.EntityType)
		}
	}
	if req.QuotedText != "" {
		fmt.Fprintf(b, "\nThe user quoted this message:\n%s\n", req.QuotedText)
	}
	if req.ReplyToMessageID != "" {
		fmt.Fprintf(b, "The user is replying to message id: %s\n", req.ReplyToMessageID)
	}
}

func refineIssueLookupIntentWithSourceText(in Intent, sourceText string) Intent {
	if in.Kind != IntentQueryIssue && in.Kind != IntentQueryProgress {
		return in
	}
	if in.Params == nil {
		in.Params = map[string]string{}
	}
	if in.Kind == IntentQueryProgress && strings.TrimSpace(in.Params["scope"]) == "" {
		in.Params["scope"] = "issue"
	}
	if in.Kind == IntentQueryProgress {
		scope := strings.TrimSpace(in.Params["scope"])
		if scope == "projects" || scope == "my_todos" {
			return in
		}
	}
	if issueKey := strings.TrimSpace(in.Params["issue_key"]); issueKey != "" {
		in.Params["issue_key"] = keyParam(issueKey)
		return in
	}

	text := strings.TrimSpace(sourceText)
	if text == "" {
		return in
	}
	keys := extractIssueKeys(text)
	issueLikes := extractIssueLikeKeys(text)
	if len(keys) == 1 && len(issueLikes) == 1 {
		in.Params["issue_key"] = keys[0]
		return in
	}
	if len(keys) > 0 || len(issueLikes) > 0 || !isTodoListQuery(text) {
		return fallbackIntent(IntentASKClarify)
	}
	if in.Kind == IntentQueryProgress {
		in.Params["scope"] = "my_todos"
	}
	return in
}

func extractIssueKeys(text string) []string {
	return uniqueNormalizedMatches(legalIssueKeyRe.FindAllString(text, -1))
}

func extractIssueLikeKeys(text string) []string {
	return uniqueNormalizedMatches(issueLikeKeyRe.FindAllString(text, -1))
}

func uniqueNormalizedMatches(matches []string) []string {
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		key := keyParam(match)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func isTodoListQuery(text string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(text), ""))
	compact = strings.Trim(compact, "？?！!.。")
	switch compact {
	case "我的待办", "待办列表", "看一下待办", "我有哪些待办":
		return true
	default:
		return false
	}
}

func fallbackIntent(kind IntentKind) Intent {
	return Intent{Kind: kind, Confidence: 0, Params: map[string]string{}, Source: SourceChat}
}

func isValidIntentKind(k IntentKind) bool {
	switch k {
	case IntentCreateIssue, IntentAddComment, IntentQueryIssue, IntentIssueDetail, IntentIssueTimeline, IntentIssueLogs,
		IntentQueryProgress,
		IntentSetStatus, IntentSetAssignee, IntentSetPriority, IntentSetLabel,
		IntentConfirmAction, IntentCancelAction,
		IntentUnsupported, IntentUnknown, IntentASKClarify:
		return true
	default:
		return false
	}
}

func intentHasRequiredParams(in Intent) bool {
	switch in.Kind {
	case IntentCreateIssue:
		return strings.TrimSpace(in.Params["title"]) != ""
	case IntentAddComment:
		return strings.TrimSpace(in.Params["issue_key"]) != "" && strings.TrimSpace(in.Params["comment"]) != ""
	case IntentQueryProgress:
		scope := strings.TrimSpace(in.Params["scope"])
		if scope == "" {
			scope = "issue"
			in.Params["scope"] = scope
		}
		if scope == "issue" {
			return strings.TrimSpace(in.Params["issue_key"]) != ""
		}
		return scope == "projects" || scope == "my_todos"
	case IntentIssueDetail, IntentIssueTimeline, IntentIssueLogs:
		return strings.TrimSpace(in.Params["issue_key"]) != ""
	case IntentSetStatus:
		return strings.TrimSpace(in.Params["issue_key"]) != "" && strings.TrimSpace(in.Params["status"]) != ""
	case IntentSetAssignee:
		return strings.TrimSpace(in.Params["issue_key"]) != "" && strings.TrimSpace(in.Params["assignee"]) != ""
	case IntentSetPriority:
		return strings.TrimSpace(in.Params["issue_key"]) != "" && strings.TrimSpace(in.Params["priority"]) != ""
	case IntentSetLabel:
		return strings.TrimSpace(in.Params["issue_key"]) != "" &&
			strings.TrimSpace(in.Params["label"]) != "" &&
			(in.Params["op"] == "add" || in.Params["op"] == "remove")
	case IntentConfirmAction, IntentCancelAction:
		return strings.TrimSpace(in.Params["code"]) != ""
	default:
		return true
	}
}
