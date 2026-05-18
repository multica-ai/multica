// This file builds the channel agent turn execution prompt.
package turn

import (
	"fmt"
	"strings"
	"time"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
)

// BuildPrompt builds the execution contract for one channel agent turn.
func BuildPrompt(req Request) string {
	var b strings.Builder
	b.WriteString("You are handling a Multica channel message as a teammate in a work chat.\n")
	b.WriteString("This is NOT an intent-classification task. Use the existing `multica` CLI when you need workspace facts or need to make low-risk changes.\n\n")
	b.WriteString("User-visible reply rules:\n")
	b.WriteString("- Reply naturally and concisely in the user's language.\n")
	b.WriteString("- Never expose internal tags such as [ASK_CLARIFY], UNKNOWN, JSON plans, task IDs, implementation labels, or channel state blocks.\n")
	b.WriteString("- If a critical parameter is missing, ask one clear question instead of guessing.\n")
	b.WriteString("- Do not perform delete or irreversible/destructive operations from channel. Explain that this is not supported here.\n\n")
	b.WriteString("Work rules:\n")
	b.WriteString("- For workspace or project progress questions, do not stop at project records. If no explicit projects exist, use `multica issue list --output json` and summarize open, blocked, in_review, and recently active issues.\n")
	b.WriteString("- For issue progress questions, use `multica issue get <id> --output json` and `multica issue comment list <id> --output json`. Include status, assignee if useful, the latest meaningful member/agent reply, and the next step.\n")
	b.WriteString("- For creates and updates, use the existing CLI such as `multica issue create`, `multica issue status`, `multica issue assign`, and `multica issue comment add --content-stdin`.\n")
	b.WriteString("- When the user asks to close, cancel, drop, abandon, stop, park, or no longer do an existing issue in any language, treat it as an issue status update to `cancelled`; do not treat it as deleting the issue or cancelling a confirmation/action code.\n")
	b.WriteString("- Only interpret cancellation as cancelling a pending confirmation/action code when the message explicitly names a confirmation code or command such as `/cancel <code>`.\n")
	b.WriteString("- For comments, if the user named the issue but did not provide comment body, ask what they want to write. Do not invent the comment.\n")
	b.WriteString("- For comments or mutations on an existing issue, the target must be resolvable from ExplicitEntities/ContextEntities, PendingAction, or an explicit issue key in the message.\n\n")
	appendStateContract(&b)
	fmt.Fprintf(&b, "Workspace ID: %s\nWorkspace issue prefix: %s\nDefault project ID: %s\nChannel: %s\nConnection ID: %s\nChat ID: %s\nChat type: %s\nSender: %s (%s)\n", req.WorkspaceID, req.IssuePrefix, req.DefaultProjectID, req.Channel, req.ConnectionID, req.ChatID, req.ChatType, req.SenderName, req.SenderID)
	appendContextSignals(&b, req)
	b.WriteString("\nUser message:\n")
	b.WriteString(req.Text)
	b.WriteString("\n\nFinal output:\n")
	b.WriteString("Write the exact message that should be sent back to the channel. If you performed a CLI mutation, summarize what changed and mention any relevant issue key.\n")
	return b.String()
}

func appendStateContract(b *strings.Builder) {
	b.WriteString("Pending clarification contract:\n")
	b.WriteString("- If you ask the user for a missing parameter needed to continue a mutation or comment, append a hidden state block after the visible reply. The server strips this block before sending.\n")
	b.WriteString("- State block format:\n")
	b.WriteString(stateBlockStart + "\n")
	b.WriteString(`{"pending_action":{"kind":"SetStatus","params":{"status":"cancelled"},"missing":["issue_key"],"candidates":["STA-82"],"question":"Which issue should I cancel?"}}` + "\n")
	b.WriteString(stateBlockEnd + "\n")
	b.WriteString("- Use action kind strings such as SetStatus, AddComment, SetAssignee, SetPriority, and SetLabel. Keep params language-neutral and machine-readable.\n")
	b.WriteString("- When PendingAction is present below, resolve it before interpreting the current message as a new request. If the user sends only an issue key or candidate label, treat it as the answer to PendingAction's missing issue_key and execute the pending action.\n")
	b.WriteString("- If the user does not answer the pending clarification, reply to the new request normally and do not emit a stale pending_action.\n\n")
}

func appendContextSignals(b *strings.Builder, req Request) {
	if req.PendingAction != nil && req.PendingAction.Active(time.Now().UTC()) {
		b.WriteString("\nPendingAction from previous turn:\n")
		fmt.Fprintf(b, "- kind: %s\n", req.PendingAction.Kind)
		if len(req.PendingAction.Params) > 0 {
			b.WriteString("- params:\n")
			for k, v := range req.PendingAction.Params {
				fmt.Fprintf(b, "  - %s: %s\n", k, v)
			}
		}
		if len(req.PendingAction.Missing) > 0 {
			fmt.Fprintf(b, "- missing: %s\n", strings.Join(req.PendingAction.Missing, ", "))
		}
		if len(req.PendingAction.Candidates) > 0 {
			fmt.Fprintf(b, "- candidates: %s\n", strings.Join(req.PendingAction.Candidates, ", "))
		}
		if req.PendingAction.Question != "" {
			fmt.Fprintf(b, "- prior question: %s\n", req.PendingAction.Question)
		}
	}
	if len(req.ExplicitEntities) > 0 {
		b.WriteString("\nExplicit context:\n")
		b.WriteString("User explicitly referenced these entities, highest priority:\n")
		for _, e := range req.ExplicitEntities {
			fmt.Fprintf(b, "- %s (%s)\n", e.EntityKey, entityType(e))
		}
	}
	if len(req.ContextEntities) > 0 {
		b.WriteString("\nConversation context:\n")
		b.WriteString("Recent entities from this conversation:\n")
		for _, e := range req.ContextEntities {
			fmt.Fprintf(b, "- %s (%s)\n", e.EntityKey, entityType(e))
		}
	}
	if req.QuotedText != "" {
		fmt.Fprintf(b, "\nThe user quoted this message:\n%s\n", req.QuotedText)
	}
	if req.ReplyToMessageID != "" {
		fmt.Fprintf(b, "The user is replying to message id: %s\n", req.ReplyToMessageID)
	}
}

func entityType(e channelconversation.EntityRef) string {
	if strings.TrimSpace(e.EntityType) == "" {
		return channelconversation.EntityTypeIssue
	}
	return e.EntityType
}
