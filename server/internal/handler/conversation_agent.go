package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// Agents may answer each other, but a run of this many agent messages with
	// no human in between stops the exchange until a person writes again.
	conversationAgentLoopLimit = 3

	conversationPromptMaxMessages = 20
	conversationPromptMaxRunes    = 8000
)

// conversationReplyTargets decides which agents a new message asks to reply.
// In a channel's main timeline that is exactly the agents it @mentions. In a
// thread, a message without a mention goes to the agent that last replied
// there, or failing that to the agents the thread's root message mentioned.
func (h *Handler) conversationReplyTargets(ctx context.Context, conv db.Conversation, msg db.ConversationMessage) []string {
	ids := mentionedAgentIDs(msg.Content)
	if len(ids) > 0 || conv.Kind != conversationKindThread {
		return ids
	}
	if latest, err := h.Queries.GetLatestAgentAuthor(ctx, conv.ID); err == nil && latest.Valid {
		return []string{uuidToString(latest)}
	}
	root, err := h.Queries.GetConversationMessage(ctx, conv.RootMessageID)
	if err != nil {
		return nil
	}
	if root.AuthorType == "agent" && root.AuthorID.Valid {
		return []string{uuidToString(root.AuthorID)}
	}
	return mentionedAgentIDs(root.Content)
}

func mentionedAgentIDs(content string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, m := range util.ParseMentions(content) {
		if m.Type == "agent" && !seen[m.ID] {
			seen[m.ID] = true
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// triggerConversationAgents asks each targeted agent to reply to msg. Only
// channels (and their threads) have agents; a direct conversation is between
// two people. When an agent cannot reply, a system line says why, so a
// mention never vanishes silently.
func (h *Handler) triggerConversationAgents(ctx context.Context, conv, root db.Conversation, msg db.ConversationMessage, actorType, actorID, originatorUserID string) {
	if root.Kind != conversationKindChannel || root.ArchivedAt.Valid || originatorUserID == "" {
		return
	}
	targets := h.conversationReplyTargets(ctx, conv, msg)
	if actorType == "agent" {
		filtered := targets[:0]
		for _, id := range targets {
			if id != actorID {
				filtered = append(filtered, id)
			}
		}
		targets = filtered
		if len(targets) > 0 && h.conversationAgentLoopReached(ctx, conv) {
			h.postConversationEvent(ctx, conv, root, fmt.Sprintf(
				"Agents paused after %d replies in a row. Write a message to continue.", conversationAgentLoopLimit))
			return
		}
	}
	triggered := false
	for _, agentID := range targets {
		agentUUID, err := parseUUIDStrict(agentID)
		if err != nil {
			continue
		}
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: root.WorkspaceID})
		if err != nil {
			continue
		}
		if notice := h.conversationAgentBlocked(ctx, root, agent, actorType, actorID, originatorUserID); notice != "" {
			h.postConversationEvent(ctx, conv, root, notice)
			continue
		}
		if err := h.sendConversationAgentTurn(ctx, conv, root, agent, msg, originatorUserID); err != nil {
			slog.Warn("conversation agent trigger failed",
				"conversation_id", uuidToString(conv.ID), "agent_id", agentID, "error", err)
			h.postConversationEvent(ctx, conv, root, fmt.Sprintf("%s couldn't start a reply.", agent.Name))
			continue
		}
		triggered = true
	}
	if triggered {
		h.publishConversationEvent(ctx, protocol.EventConversationUpdated, conv, root, actorType, actorID, nil)
	}
}

func (h *Handler) conversationAgentLoopReached(ctx context.Context, conv db.Conversation) bool {
	tail, err := h.Queries.ListConversationTailAuthorTypes(ctx, db.ListConversationTailAuthorTypesParams{
		ConversationID: conv.ID, Limit: conversationAgentLoopLimit,
	})
	if err != nil || len(tail) < conversationAgentLoopLimit {
		return false
	}
	for _, authorType := range tail {
		if authorType != "agent" {
			return false
		}
	}
	return true
}

// conversationAgentBlocked returns why the agent cannot reply here, or "".
func (h *Handler) conversationAgentBlocked(ctx context.Context, root db.Conversation, agent db.Agent, actorType, actorID, originatorUserID string) string {
	if _, err := h.Queries.GetConversationMember(ctx, db.GetConversationMemberParams{
		ConversationID: root.ID, MemberType: "agent", MemberID: agent.ID,
	}); err != nil {
		return fmt.Sprintf("%s isn't in this channel. Add them to the channel to get a reply.", agent.Name)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Sprintf("%s is archived and can't reply.", agent.Name)
	}
	if verdict, err := service.AgentReadiness(ctx, h.runtimeLookup(obsmetrics.RuntimeLookupSourceChat), agent); err == nil && verdict.Blocked() {
		return fmt.Sprintf("%s can't reply right now: %s.", agent.Name, verdict.Detail)
	}
	if !h.canInvokeAgent(ctx, agent, actorType, actorID, originatorUserID, uuidToString(root.WorkspaceID)) {
		return fmt.Sprintf("%s isn't available to you.", agent.Name)
	}
	return ""
}

// conversationAgentSession is the hidden chat session that carries one
// agent's replies in one conversation. Chat events stream to the session's
// creator only, so when the creator can no longer read the conversation a
// fresh session owned by the person asking now takes over.
func (h *Handler) conversationAgentSession(ctx context.Context, conv, root db.Conversation, agent db.Agent, originatorUserID string) (db.ChatSession, error) {
	if link, err := h.Queries.GetConversationAgentSession(ctx, db.GetConversationAgentSessionParams{
		ConversationID: conv.ID, AgentID: agent.ID,
	}); err == nil {
		if session, err := h.Queries.GetChatSession(ctx, link.ChatSessionID); err == nil && session.Status == "active" {
			if _, _, ok := h.canAccessConversation(ctx, conv, session.CreatorID); ok {
				return session, nil
			}
		}
	}
	title := "#" + root.Name
	if conv.Kind == conversationKindThread {
		title += " thread"
	}
	session, err := h.Queries.CreateConversationChatSession(ctx, db.CreateConversationChatSessionParams{
		WorkspaceID: conv.WorkspaceID, AgentID: agent.ID, CreatorID: parseUUID(originatorUserID),
		Title: title, ConversationID: conv.ID,
	})
	if err != nil {
		return db.ChatSession{}, err
	}
	if _, err := h.Queries.UpsertConversationAgentSession(ctx, db.UpsertConversationAgentSessionParams{
		ConversationID: conv.ID, AgentID: agent.ID, WorkspaceID: conv.WorkspaceID, ChatSessionID: session.ID,
	}); err != nil {
		return db.ChatSession{}, err
	}
	return session, nil
}

func (h *Handler) sendConversationAgentTurn(ctx context.Context, conv, root db.Conversation, agent db.Agent, msg db.ConversationMessage, originatorUserID string) error {
	session, err := h.conversationAgentSession(ctx, conv, root, agent, originatorUserID)
	if err != nil {
		return err
	}
	prompt := h.buildConversationAgentPrompt(ctx, conv, root, agent, msg)
	originator := parseUUID(originatorUserID)
	_, err = h.TaskService.SendDirectChatMessage(ctx, session, agent, originator, prompt, nil, "member", originator)
	return err
}

type conversationNames struct {
	h      *Handler
	ctx    context.Context
	people map[string]string
	agents map[string]string
}

func (n *conversationNames) author(authorType string, authorID pgtype.UUID) string {
	id := uuidToString(authorID)
	switch authorType {
	case "member":
		if name, ok := n.people[id]; ok {
			return name
		}
		name := "Someone"
		if u, err := n.h.Queries.GetUser(n.ctx, authorID); err == nil && u.Name != "" {
			name = u.Name
		}
		n.people[id] = name
		return name
	case "agent":
		if name, ok := n.agents[id]; ok {
			return name
		}
		name := "An agent"
		if a, err := n.h.Queries.GetAgent(n.ctx, authorID); err == nil {
			name = a.Name
		}
		n.agents[id] = name
		return name + " (agent)"
	}
	return "Multica"
}

// buildConversationAgentPrompt hands the agent what it needs to reply in the
// conversation: where it is, who else it can bring in, everything said since
// its own last reply there, and the message it is answering.
func (h *Handler) buildConversationAgentPrompt(ctx context.Context, conv, root db.Conversation, agent db.Agent, msg db.ConversationMessage) string {
	names := &conversationNames{h: h, ctx: ctx, people: map[string]string{}, agents: map[string]string{}}
	var b strings.Builder
	where := fmt.Sprintf("the #%s channel", root.Name)
	if conv.Kind == conversationKindThread {
		where = fmt.Sprintf("a thread in the #%s channel", root.Name)
	}
	fmt.Fprintf(&b, "You were asked to reply in %s on the Multica Team page. Your reply is posted there as you, for everyone in the channel to read, so write it for them.\n", where)
	if root.About != "" {
		fmt.Fprintf(&b, "Channel topic: %s\n", root.About)
	}
	if members, err := h.Queries.ListConversationMembers(ctx, root.ID); err == nil {
		var others []string
		for _, m := range members {
			if m.MemberType != "agent" || m.MemberID == agent.ID {
				continue
			}
			if a, err := h.Queries.GetAgent(ctx, m.MemberID); err == nil && !a.ArchivedAt.Valid {
				others = append(others, fmt.Sprintf("[@%s](mention://agent/%s)", a.Name, uuidToString(a.ID)))
			}
		}
		if len(others) > 0 {
			fmt.Fprintf(&b, "Other agents in this channel, who reply when you mention them like this: %s. Mention one only when you need their help.\n", strings.Join(others, ", "))
		}
	}

	if conv.Kind == conversationKindThread {
		if rootMsg, err := h.Queries.GetConversationMessage(ctx, conv.RootMessageID); err == nil {
			fmt.Fprintf(&b, "\nThe thread replies to this message from %s:\n%s\n", names.author(rootMsg.AuthorType, rootMsg.AuthorID), quoteBlock(truncateRunes(rootMsg.Content, 2000)))
		}
	}

	lastReply, _ := h.Queries.GetLastAgentReplySeq(ctx, db.GetLastAgentReplySeqParams{ConversationID: conv.ID, AuthorID: agent.ID})
	recent, _ := h.Queries.ListConversationMessagesAfter(ctx, db.ListConversationMessagesAfterParams{
		ConversationID: conv.ID, Seq: lastReply, Limit: conversationPromptMaxMessages + 1,
	})
	var lines []string
	budget := conversationPromptMaxRunes
	for _, m := range recent {
		if m.ID == msg.ID || m.Kind != "message" {
			continue
		}
		if len(lines) == conversationPromptMaxMessages {
			break
		}
		line := fmt.Sprintf("%s: %s", names.author(m.AuthorType, m.AuthorID), truncateRunes(m.Content, 1500))
		budget -= utf8.RuneCountInString(line)
		if budget < 0 {
			break
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		b.WriteString("\nEarlier messages since your last reply here, oldest first:\n")
		for i := len(lines) - 1; i >= 0; i-- {
			b.WriteString(lines[i])
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\nReply to this latest message from %s:\n%s\n", names.author(msg.AuthorType, msg.AuthorID), quoteBlock(msg.Content))
	return b.String()
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func quoteBlock(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

// RegisterConversationListeners mirrors agent replies from the hidden chat
// sessions back into their conversations.
func (h *Handler) RegisterConversationListeners(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, func(e events.Event) {
		payload, ok := e.Payload.(protocol.ChatDonePayload)
		if !ok || payload.ChatSessionID == "" {
			return
		}
		h.mirrorConversationReply(context.Background(), payload)
	})
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		sessionID, _ := payload["chat_session_id"].(string)
		if retry, _ := payload["retry_pending"].(bool); retry || sessionID == "" {
			return
		}
		h.noteConversationReplyFailed(context.Background(), sessionID)
	})
}

func (h *Handler) conversationForChatSession(ctx context.Context, chatSessionID string) (db.ConversationAgentSession, db.Conversation, db.Conversation, bool) {
	id, err := parseUUIDStrict(chatSessionID)
	if err != nil {
		return db.ConversationAgentSession{}, db.Conversation{}, db.Conversation{}, false
	}
	link, err := h.Queries.GetConversationAgentSessionByChat(ctx, id)
	if err != nil {
		return db.ConversationAgentSession{}, db.Conversation{}, db.Conversation{}, false
	}
	conv, err := h.Queries.GetConversation(ctx, link.ConversationID)
	if err != nil {
		return db.ConversationAgentSession{}, db.Conversation{}, db.Conversation{}, false
	}
	root, err := h.conversationAccessRoot(ctx, conv)
	if err != nil {
		return db.ConversationAgentSession{}, db.Conversation{}, db.Conversation{}, false
	}
	return link, conv, root, true
}

func (h *Handler) mirrorConversationReply(ctx context.Context, payload protocol.ChatDonePayload) {
	link, conv, root, ok := h.conversationForChatSession(ctx, payload.ChatSessionID)
	if !ok {
		return
	}
	content := strings.TrimSpace(payload.Content)
	if content == "" || payload.MessageKind == protocol.ChatMessageKindNoResponse {
		h.publishConversationEvent(ctx, protocol.EventConversationUpdated, conv, root, "system", "", nil)
		return
	}
	taskID, _ := parseUUIDStrict(payload.TaskID)
	agentID := uuidToString(link.AgentID)
	msg, err := h.postConversationMessage(ctx, conv, root, "agent", agentID, "message", content, taskID, "")
	if err != nil {
		slog.Warn("mirror conversation reply failed", "conversation_id", uuidToString(conv.ID), "error", err)
		return
	}
	h.publishConversationEvent(ctx, protocol.EventConversationUpdated, conv, root, "agent", agentID, nil)

	// The agent may have asked another agent in. That run is on behalf of the
	// person who started this exchange.
	originator := ""
	if taskID.Valid {
		if task, err := h.Queries.GetAgentTask(ctx, taskID); err == nil && task.OriginatorUserID.Valid {
			originator = uuidToString(task.OriginatorUserID)
		}
	}
	if originator == "" {
		if latest, err := h.Queries.GetLatestMemberAuthor(ctx, conv.ID); err == nil && latest.Valid {
			originator = uuidToString(latest)
		}
	}
	h.triggerConversationAgents(ctx, conv, root, msg, "agent", agentID, originator)
}

func (h *Handler) noteConversationReplyFailed(ctx context.Context, chatSessionID string) {
	link, conv, root, ok := h.conversationForChatSession(ctx, chatSessionID)
	if !ok {
		return
	}
	name := "The agent"
	if agent, err := h.Queries.GetAgent(ctx, link.AgentID); err == nil {
		name = agent.Name
	}
	h.postConversationEvent(ctx, conv, root, fmt.Sprintf("%s couldn't finish a reply.", name))
	h.publishConversationEvent(ctx, protocol.EventConversationUpdated, conv, root, "system", "", nil)
}
