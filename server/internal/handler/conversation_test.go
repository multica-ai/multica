package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func conversationRequestAs(t *testing.T, userID, method, path string, body any, params map[string]string) *http.Request {
	t.Helper()
	req := newRequestAs(userID, method, path, body)
	for k, v := range params {
		req = withURLParams(req, k, v)
	}
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: util.MustParseUUID(userID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member row for %s: %v", userID, err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}

func callConversation(t *testing.T, fn http.HandlerFunc, userID, method, path, convID string, body any, out any) int {
	t.Helper()
	params := map[string]string{}
	if convID != "" {
		params["conversationId"] = convID
	}
	w := httptest.NewRecorder()
	fn(w, conversationRequestAs(t, userID, method, path, body, params))
	if out != nil && w.Code < 300 {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s: %v (%s)", method, path, err, w.Body.String())
		}
	}
	return w.Code
}

func createChannelForTest(t *testing.T, name, visibility string, members ...conversationMemberInput) ConversationResponse {
	t.Helper()
	var conv ConversationResponse
	code := callConversation(t, testHandler.CreateConversation, testUserID, http.MethodPost, "/api/conversations", "",
		map[string]any{"name": name, "visibility": visibility, "members": members}, &conv)
	if code != http.StatusCreated {
		t.Fatalf("create channel %q: %d", name, code)
	}
	t.Cleanup(func() { deleteConversationForTest(conv.ID) })
	return conv
}

func deleteConversationForTest(id string) {
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM chat_session WHERE conversation_id IN (SELECT id FROM conversation WHERE id = $1 OR parent_conversation_id = $1)`, id)
	testPool.Exec(ctx, `DELETE FROM conversation_agent_session WHERE conversation_id IN (SELECT id FROM conversation WHERE id = $1 OR parent_conversation_id = $1)`, id)
	testPool.Exec(ctx, `DELETE FROM conversation_message WHERE conversation_id IN (SELECT id FROM conversation WHERE id = $1 OR parent_conversation_id = $1)`, id)
	testPool.Exec(ctx, `DELETE FROM conversation_member WHERE conversation_id IN (SELECT id FROM conversation WHERE id = $1 OR parent_conversation_id = $1)`, id)
	testPool.Exec(ctx, `DELETE FROM conversation WHERE id = $1 OR parent_conversation_id = $1`, id)
}

func sendConversationMessageForTest(t *testing.T, userID, convID, content, clientID string) (ConversationMessageResponse, int) {
	t.Helper()
	var msg ConversationMessageResponse
	code := callConversation(t, testHandler.SendConversationMessage, userID, http.MethodPost, "/api/conversations/"+convID+"/messages", convID,
		map[string]any{"content": content, "client_message_id": clientID}, &msg)
	return msg, code
}

func listConversationMessagesForTest(t *testing.T, userID, convID string) []ConversationMessageResponse {
	t.Helper()
	var out struct {
		Messages []ConversationMessageResponse `json:"messages"`
	}
	if code := callConversation(t, testHandler.ListConversationMessages, userID, http.MethodGet, "/api/conversations/"+convID+"/messages", convID, nil, &out); code != http.StatusOK {
		t.Fatalf("list messages: %d", code)
	}
	return out.Messages
}

func listConversationsForTest(t *testing.T, userID string) []ConversationResponse {
	t.Helper()
	var out struct {
		Conversations []ConversationResponse `json:"conversations"`
	}
	if code := callConversation(t, testHandler.ListConversations, userID, http.MethodGet, "/api/conversations", "", nil, &out); code != http.StatusOK {
		t.Fatalf("list conversations: %d", code)
	}
	return out.Conversations
}

func findConversation(list []ConversationResponse, id string) *ConversationResponse {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

func convAgentMention(agentID, name string) string {
	return "[@" + name + "](mention://agent/" + agentID + ")"
}

func TestConversations_NotFoundWhileTeamPageOff(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, false)
	if code := callConversation(t, testHandler.ListConversations, testUserID, http.MethodGet, "/api/conversations", "", nil, nil); code != http.StatusNotFound {
		t.Fatalf("list with flag off: expected 404, got %d", code)
	}
}

func TestConversations_ChannelVisibility(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	other := seedSecurityTestOwner(t, "conv-visibility-other")

	public := createChannelForTest(t, "General Chat", "public")
	if public.Name != "general-chat" || !public.IsMember {
		t.Fatalf("public channel = %+v, want normalized name and creator membership", public)
	}
	private := createChannelForTest(t, "secret-plans", "private")

	if code := callConversation(t, testHandler.CreateConversation, testUserID, http.MethodPost, "/api/conversations", "",
		map[string]any{"name": "#General-Chat"}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate channel name: expected 409, got %d", code)
	}

	list := listConversationsForTest(t, other)
	if got := findConversation(list, public.ID); got == nil || got.IsMember {
		t.Fatalf("other member must see the public channel without membership: %+v", got)
	}
	if findConversation(list, private.ID) != nil {
		t.Fatal("other member must not see a private channel they are not in")
	}
	if code := callConversation(t, testHandler.GetConversation, other, http.MethodGet, "/api/conversations/"+private.ID, private.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("private channel read by outsider: expected 404, got %d", code)
	}
	if _, code := sendConversationMessageForTest(t, other, private.ID, "let me in", ""); code != http.StatusNotFound {
		t.Fatalf("private channel post by outsider: expected 404, got %d", code)
	}

	// Posting in a public channel joins it.
	if _, code := sendConversationMessageForTest(t, other, public.ID, "hello", ""); code != http.StatusCreated {
		t.Fatalf("post in public channel: %d", code)
	}
	if got := findConversation(listConversationsForTest(t, other), public.ID); got == nil || !got.IsMember {
		t.Fatalf("posting must join the public channel: %+v", got)
	}
}

func TestConversations_DirectIsOnePerPair(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	other := seedSecurityTestOwner(t, "conv-direct-other")
	outsider := seedSecurityTestOwner(t, "conv-direct-outsider")

	var first, again, reverse ConversationResponse
	if code := callConversation(t, testHandler.OpenDirectConversation, testUserID, http.MethodPost, "/api/conversations/direct", "",
		map[string]any{"user_id": other}, &first); code != http.StatusCreated {
		t.Fatalf("open direct: %d", code)
	}
	t.Cleanup(func() { deleteConversationForTest(first.ID) })
	if first.Kind != "direct" || len(first.Members) != 2 {
		t.Fatalf("direct conversation = %+v, want two members", first)
	}
	if code := callConversation(t, testHandler.OpenDirectConversation, testUserID, http.MethodPost, "/api/conversations/direct", "",
		map[string]any{"user_id": other}, &again); code != http.StatusOK || again.ID != first.ID {
		t.Fatalf("reopen direct: %d %s, want 200 %s", code, again.ID, first.ID)
	}
	if code := callConversation(t, testHandler.OpenDirectConversation, other, http.MethodPost, "/api/conversations/direct", "",
		map[string]any{"user_id": testUserID}, &reverse); code != http.StatusOK || reverse.ID != first.ID {
		t.Fatalf("open from the other side: %d %s, want 200 %s", code, reverse.ID, first.ID)
	}
	if code := callConversation(t, testHandler.GetConversation, outsider, http.MethodGet, "/api/conversations/"+first.ID, first.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("direct conversation read by a third member: expected 404, got %d", code)
	}
	if code := callConversation(t, testHandler.AddConversationMembers, testUserID, http.MethodPost, "/api/conversations/"+first.ID+"/members", first.ID,
		map[string]any{"members": []conversationMemberInput{{MemberType: "member", MemberID: outsider}}}, nil); code != http.StatusBadRequest {
		t.Fatalf("adding a third person to a direct conversation: expected 400, got %d", code)
	}
}

func TestConversations_SeqIdempotencyAndUnread(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	other := seedSecurityTestOwner(t, "conv-unread-other")
	conv := createChannelForTest(t, "unread-check", "private", conversationMemberInput{MemberType: "member", MemberID: other})

	first, code := sendConversationMessageForTest(t, testUserID, conv.ID, "one", "client-1")
	if code != http.StatusCreated || first.Seq != 1 {
		t.Fatalf("first send: %d seq %d", code, first.Seq)
	}
	retry, code := sendConversationMessageForTest(t, testUserID, conv.ID, "one", "client-1")
	if code != http.StatusOK || retry.ID != first.ID {
		t.Fatalf("retry with the same client id: %d %s, want 200 %s", code, retry.ID, first.ID)
	}
	second, _ := sendConversationMessageForTest(t, testUserID, conv.ID, "two", "client-2")
	if second.Seq != 2 {
		t.Fatalf("second seq = %d, want 2", second.Seq)
	}
	if msgs := listConversationMessagesForTest(t, other, conv.ID); len(msgs) != 2 || msgs[0].Content != "one" || msgs[1].Content != "two" {
		t.Fatalf("messages = %+v, want one, two oldest first", msgs)
	}

	if got := findConversation(listConversationsForTest(t, other), conv.ID); got == nil || got.UnreadCount != 2 {
		t.Fatalf("other member unread = %+v, want 2", got)
	}
	if got := findConversation(listConversationsForTest(t, testUserID), conv.ID); got == nil || got.UnreadCount != 0 {
		t.Fatalf("sender unread = %+v, want 0", got)
	}
	if code := callConversation(t, testHandler.MarkConversationRead, other, http.MethodPost, "/api/conversations/"+conv.ID+"/read", conv.ID,
		map[string]any{"seq": 2}, nil); code != http.StatusNoContent {
		t.Fatalf("mark read: %d", code)
	}
	if got := findConversation(listConversationsForTest(t, other), conv.ID); got == nil || got.UnreadCount != 0 {
		t.Fatalf("unread after mark read = %+v, want 0", got)
	}
}

func conversationAgentTasks(t *testing.T, convID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue atq
		JOIN chat_session cs ON cs.id = atq.chat_session_id
		WHERE cs.conversation_id = $1 AND atq.agent_id = $2
	`, convID, agentID).Scan(&n); err != nil {
		t.Fatalf("count conversation tasks: %v", err)
	}
	return n
}

func TestConversations_MentionTriggersAgentOnHiddenSession(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentID := createHandlerTestAgent(t, "ConvMentionAgent", []byte("[]"))
	outsiderAgent := createHandlerTestAgent(t, "ConvOutsiderAgent", []byte("[]"))
	conv := createChannelForTest(t, "agent-room", "public", conversationMemberInput{MemberType: "agent", MemberID: agentID})

	if _, code := sendConversationMessageForTest(t, testUserID, conv.ID, "no mention, no reply", ""); code != http.StatusCreated {
		t.Fatalf("send: %d", code)
	}
	if n := conversationAgentTasks(t, conv.ID, agentID); n != 0 {
		t.Fatalf("unmentioned agent got %d tasks, want 0", n)
	}

	if _, code := sendConversationMessageForTest(t, testUserID, conv.ID, convAgentMention(agentID, "ConvMentionAgent")+" what's up?", ""); code != http.StatusCreated {
		t.Fatalf("send mention: %d", code)
	}
	if n := conversationAgentTasks(t, conv.ID, agentID); n != 1 {
		t.Fatalf("mentioned agent got %d tasks, want 1", n)
	}
	var detail ConversationResponse
	callConversation(t, testHandler.GetConversation, testUserID, http.MethodGet, "/api/conversations/"+conv.ID, conv.ID, nil, &detail)
	if len(detail.WorkingAgents) != 1 || detail.WorkingAgents[0].AgentID != agentID {
		t.Fatalf("working agents = %+v, want the mentioned agent", detail.WorkingAgents)
	}

	var sessionID, prompt string
	if err := testPool.QueryRow(context.Background(), `
		SELECT cs.id, cm.content FROM chat_session cs
		JOIN chat_message cm ON cm.chat_session_id = cs.id AND cm.role = 'user'
		WHERE cs.conversation_id = $1
	`, conv.ID).Scan(&sessionID, &prompt); err != nil {
		t.Fatalf("load hidden session: %v", err)
	}
	if !strings.Contains(prompt, "#agent-room") || !strings.Contains(prompt, "what's up?") || !strings.Contains(prompt, "no mention, no reply") {
		t.Fatalf("prompt lacks channel context:\n%s", prompt)
	}
	if findChatSession(listChatSessionsForTest(t, "?status=all&include_threads=true"), sessionID) != nil {
		t.Fatal("the hidden session must not appear in the chat session list")
	}

	if _, code := sendConversationMessageForTest(t, testUserID, conv.ID, convAgentMention(outsiderAgent, "ConvOutsiderAgent")+" hi", ""); code != http.StatusCreated {
		t.Fatalf("send outsider mention: %d", code)
	}
	if n := conversationAgentTasks(t, conv.ID, outsiderAgent); n != 0 {
		t.Fatalf("agent outside the channel got %d tasks, want 0", n)
	}
	msgs := listConversationMessagesForTest(t, testUserID, conv.ID)
	last := msgs[len(msgs)-1]
	if last.AuthorType != "system" || !strings.Contains(last.Content, "isn't in this channel") {
		t.Fatalf("last message = %+v, want a notice that the agent is not in the channel", last)
	}
}

func TestConversations_MirrorReplyAndLoopGuard(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentA := createHandlerTestAgent(t, "ConvLoopAgentA", []byte("[]"))
	agentB := createHandlerTestAgent(t, "ConvLoopAgentB", []byte("[]"))
	conv := createChannelForTest(t, "loop-room", "public",
		conversationMemberInput{MemberType: "agent", MemberID: agentA},
		conversationMemberInput{MemberType: "agent", MemberID: agentB})

	sendConversationMessageForTest(t, testUserID, conv.ID, convAgentMention(agentA, "ConvLoopAgentA")+" start", "")
	var sessionA, taskA string
	if err := testPool.QueryRow(context.Background(), `
		SELECT cs.id, atq.id FROM chat_session cs JOIN agent_task_queue atq ON atq.chat_session_id = cs.id
		WHERE cs.conversation_id = $1 AND cs.agent_id = $2
	`, conv.ID, agentA).Scan(&sessionA, &taskA); err != nil {
		t.Fatalf("load agent A task: %v", err)
	}

	// A's reply asks B in: B gets a task on its own hidden session.
	testHandler.mirrorConversationReply(context.Background(), protocol.ChatDonePayload{
		ChatSessionID: sessionA, TaskID: taskA, Content: "over to " + convAgentMention(agentB, "ConvLoopAgentB"),
	})
	msgs := listConversationMessagesForTest(t, testUserID, conv.ID)
	if last := msgs[len(msgs)-1]; last.AuthorType != "agent" || last.AuthorID == nil || *last.AuthorID != agentA {
		t.Fatalf("mirrored reply = %+v, want a message by agent A", last)
	}
	if n := conversationAgentTasks(t, conv.ID, agentB); n != 1 {
		t.Fatalf("agent B tasks = %d, want 1", n)
	}

	// Two more agent replies make three in a row; the next hand-off is held.
	testHandler.mirrorConversationReply(context.Background(), protocol.ChatDonePayload{ChatSessionID: sessionA, TaskID: taskA, Content: "still A"})
	testHandler.mirrorConversationReply(context.Background(), protocol.ChatDonePayload{
		ChatSessionID: sessionA, TaskID: taskA, Content: "again " + convAgentMention(agentB, "ConvLoopAgentB"),
	})
	if n := conversationAgentTasks(t, conv.ID, agentB); n != 1 {
		t.Fatalf("agent B tasks after the loop limit = %d, want still 1", n)
	}
	msgs = listConversationMessagesForTest(t, testUserID, conv.ID)
	if last := msgs[len(msgs)-1]; last.AuthorType != "system" || !strings.Contains(last.Content, "paused") {
		t.Fatalf("last message = %+v, want the loop-guard notice", last)
	}

	testHandler.noteConversationReplyFailed(context.Background(), sessionA)
	msgs = listConversationMessagesForTest(t, testUserID, conv.ID)
	if last := msgs[len(msgs)-1]; !strings.Contains(last.Content, "ConvLoopAgentA couldn't finish a reply") {
		t.Fatalf("last message = %+v, want the failure notice", last)
	}
}

func TestConversations_ThreadKeepsTheRepliedAgent(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentID := createHandlerTestAgent(t, "ConvThreadAgent", []byte("[]"))
	conv := createChannelForTest(t, "thread-room", "public", conversationMemberInput{MemberType: "agent", MemberID: agentID})
	root, _ := sendConversationMessageForTest(t, testUserID, conv.ID, "a plain root", "")

	var thread ConversationResponse
	if code := callConversation(t, testHandler.OpenConversationThread, testUserID, http.MethodPost, "/api/conversations/"+conv.ID+"/threads", conv.ID,
		map[string]any{"root_message_id": root.ID}, &thread); code != http.StatusCreated {
		t.Fatalf("open thread: %d", code)
	}
	var again ConversationResponse
	if code := callConversation(t, testHandler.OpenConversationThread, testUserID, http.MethodPost, "/api/conversations/"+conv.ID+"/threads", conv.ID,
		map[string]any{"root_message_id": root.ID}, &again); code != http.StatusOK || again.ID != thread.ID {
		t.Fatalf("reopen thread: %d %s, want 200 %s", code, again.ID, thread.ID)
	}
	if thread.RootMessage == nil || thread.RootMessage.ID != root.ID {
		t.Fatalf("thread root message = %+v", thread.RootMessage)
	}

	sendConversationMessageForTest(t, testUserID, thread.ID, "no agent has spoken here yet", "")
	if n := conversationAgentTasks(t, thread.ID, agentID); n != 0 {
		t.Fatalf("thread without mention or prior agent reply: %d tasks, want 0", n)
	}
	sendConversationMessageForTest(t, testUserID, thread.ID, convAgentMention(agentID, "ConvThreadAgent")+" look", "")
	var sessionID, taskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT cs.id, atq.id FROM chat_session cs JOIN agent_task_queue atq ON atq.chat_session_id = cs.id
		WHERE cs.conversation_id = $1
	`, thread.ID).Scan(&sessionID, &taskID); err != nil {
		t.Fatalf("load thread task: %v", err)
	}
	testHandler.mirrorConversationReply(context.Background(), protocol.ChatDonePayload{ChatSessionID: sessionID, TaskID: taskID, Content: "looked"})

	sendConversationMessageForTest(t, testUserID, thread.ID, "thanks, one more thing", "")
	if n := conversationAgentTasks(t, thread.ID, agentID); n != 2 {
		t.Fatalf("follow-up in the thread: %d tasks, want 2 (the agent keeps the floor)", n)
	}

	msgs := listConversationMessagesForTest(t, testUserID, conv.ID)
	if len(msgs) != 1 || msgs[0].Thread == nil || msgs[0].Thread.ID != thread.ID || msgs[0].Thread.ReplyCount != 4 {
		t.Fatalf("channel root = %+v, want its thread summary with 4 replies", msgs)
	}
}

func TestConversations_ThreadUnderAnAgentMessageGoesToThatAgent(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentID := createHandlerTestAgent(t, "ConvRootAgent", []byte("[]"))
	conv := createChannelForTest(t, "agent-root-room", "public", conversationMemberInput{MemberType: "agent", MemberID: agentID})
	sendConversationMessageForTest(t, testUserID, conv.ID, convAgentMention(agentID, "ConvRootAgent")+" hi", "")
	var sessionID, taskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT cs.id, atq.id FROM chat_session cs JOIN agent_task_queue atq ON atq.chat_session_id = cs.id
		WHERE cs.conversation_id = $1
	`, conv.ID).Scan(&sessionID, &taskID); err != nil {
		t.Fatalf("load channel task: %v", err)
	}
	testHandler.mirrorConversationReply(context.Background(), protocol.ChatDonePayload{ChatSessionID: sessionID, TaskID: taskID, Content: "hello"})

	msgs := listConversationMessagesForTest(t, testUserID, conv.ID)
	reply := msgs[len(msgs)-1]
	if reply.AuthorType != "agent" {
		t.Fatalf("latest channel message = %+v, want the agent reply", reply)
	}
	var thread ConversationResponse
	if code := callConversation(t, testHandler.OpenConversationThread, testUserID, http.MethodPost, "/api/conversations/"+conv.ID+"/threads", conv.ID,
		map[string]any{"root_message_id": reply.ID}, &thread); code != http.StatusCreated {
		t.Fatalf("open thread: %d", code)
	}
	sendConversationMessageForTest(t, testUserID, thread.ID, "and one more", "")
	if n := conversationAgentTasks(t, thread.ID, agentID); n != 1 {
		t.Fatalf("reply under the agent's message: %d tasks, want 1", n)
	}
}
