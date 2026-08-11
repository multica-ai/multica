package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Claim must report the chat session's real channel type for EVERY registered
// channel, not just Slack (MUL-4899).
//
// The binding lookup used to hardcode channel_type='slack', so a Feishu session
// — which writes the same channel_chat_session_binding row under
// channel_type='feishu' (lark/channel_store.go) — came back with an empty
// chat_channel_type, i.e. indistinguishable from a web chat. Downstream that
// mis-flag made the daemon inject `multica attachment upload` guidance into a
// conversation that cannot carry attachments at all.
//
// chat_in_thread stays Slack-only and is asserted as such: it selects between
// `multica chat history` and `multica chat thread`, and both endpoints are
// hardwired to h.SlackHistory (chat_history.go). There is no Feishu reader, so
// the flag has nothing to select between and must not imply one exists.

// seedChannelBinding binds sessionID to a group room on an IM channel of
// channelType, creating the installation the binding requires. This is the row
// shape both the Slack binder (slack/binding.go) and the Feishu store
// (lark/channel_store.go) write — identical but for channel_type, which is
// exactly why the claim lookup must not hardcode one value.
func seedChannelBinding(t *testing.T, ctx context.Context, agentID, sessionID, channelType, lastMessageID, lastThreadID string) {
	t.Helper()
	seedChannelBindingOfChatType(t, ctx, agentID, sessionID, channelType, "group", lastMessageID, lastThreadID)
}

// seedChannelBindingOfChatType is seedChannelBinding with the room shape
// (p2p / group) chosen by the caller — the column the claim must report so the
// per-turn prompt can tell the agent whether anyone else is reading.
func seedChannelBindingOfChatType(t *testing.T, ctx context.Context, agentID, sessionID, channelType, chatType, lastMessageID, lastThreadID string) {
	t.Helper()
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, installer_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, channelType, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("seed %s installation: %v", channelType, err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_chat_session_binding
			(chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, last_message_id, last_thread_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, sessionID, installationID, channelType, "C-TEST-"+channelType, chatType, lastMessageID, lastThreadID); err != nil {
		t.Fatalf("seed %s binding: %v", channelType, err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
}

// claimedChatChannel is the channel-awareness slice of a claim response.
type claimedChatChannel struct {
	ChannelType   string `json:"chat_channel_type"`
	ChatType      string `json:"chat_type"`
	InThread      bool   `json:"chat_in_thread"`
	DeliversFiles bool   `json:"chat_channel_delivers_files"`
}

// claimChatChannelFields claims the queued task for runtimeID and returns the
// channel-awareness fields the daemon reads off the claim response.
func claimChatChannelFields(t *testing.T, runtimeID string) claimedChatChannel {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-channel-type")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *claimedChatChannel `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a claimed task")
	}
	return *resp.Task
}

func TestClaim_FeishuBoundSessionReportsChannelType(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "feishu-backed chat")
	// last_thread_id != last_message_id: the shape that would set ChatInThread on
	// Slack. Feishu must still report false — there is no reader to thread into.
	seedChannelBinding(t, ctx, agentID, sessionID, "feishu", "msg-1", "thread-9")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	claimed := claimChatChannelFields(t, runtimeID)
	if claimed.ChannelType != "feishu" {
		t.Errorf("chat_channel_type = %q, want %q — a Feishu session must not look like a web chat", claimed.ChannelType, "feishu")
	}
	if claimed.InThread {
		t.Error("chat_in_thread must stay false on Feishu: it selects between two Slack-only read commands")
	}
}

func TestClaim_SlackBoundSessionStillReportsThreadState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "slack-backed chat")
	seedChannelBinding(t, ctx, agentID, sessionID, "slack", "msg-1", "thread-9")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	claimed := claimChatChannelFields(t, runtimeID)
	if claimed.ChannelType != "slack" {
		t.Errorf("chat_channel_type = %q, want %q", claimed.ChannelType, "slack")
	}
	if !claimed.InThread {
		t.Error("chat_in_thread should be true when last_thread_id differs from last_message_id")
	}
}

// A channel whose adapter the claim handler does not name must still be
// reported. The lookup enumerated a fixed {slack, feishu} list, so every
// channel added after it — WeCom is the first — claimed as a web chat and
// inherited the web chat's `multica attachment upload` guidance, the exact
// MUL-4899 failure the fixed list was supposed to have ended.
//
// The list is what is under test here, not WeCom: the binding row shape is
// identical for every channel, so a handler that reads the row instead of
// guessing the channel_type cannot regress the next one either.
func TestClaim_UnlistedChannelBoundSessionReportsChannelType(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "wecom-backed chat")
	seedChannelBinding(t, ctx, agentID, sessionID, "wecom", "msg-1", "thread-9")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	claimed := claimChatChannelFields(t, runtimeID)
	if claimed.ChannelType != "wecom" {
		t.Errorf("chat_channel_type = %q, want %q — a channel the handler does not name must not look like a web chat", claimed.ChannelType, "wecom")
	}
	if claimed.InThread {
		t.Error("chat_in_thread must stay false off Slack: it selects between two Slack-only read commands")
	}
}

func TestClaim_UnboundSessionReportsNoChannelType(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "web chat")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	claimed := claimChatChannelFields(t, runtimeID)
	if claimed.ChannelType != "" {
		t.Errorf("chat_channel_type = %q, want empty for a web-only session", claimed.ChannelType)
	}
	if claimed.InThread {
		t.Error("chat_in_thread must be false for a web-only session")
	}
}

// The room shape rides the same binding row as the channel type and must reach
// the daemon with it. Without it the per-turn prompt has no way to know that one
// chat_session is a room shared by many people, and it described every chat run
// as a private 1:1 — the agent then had no input from which to weigh a wider
// audience. Asserted per channel because the column is written by the shared
// session service for all of them (channel/engine/session.go).
func TestClaim_BoundSessionReportsRoomShape(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	for _, tc := range []struct {
		channelType string
		chatType    string
	}{
		{"slack", "group"},
		{"slack", "p2p"},
		{"feishu", "group"},
		{"feishu", "p2p"},
		{"wecom", "group"},
		{"wecom", "p2p"},
	} {
		name := tc.channelType + "/" + tc.chatType
		t.Run(name, func(t *testing.T) {
			agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, name+" chat")
			seedChannelBindingOfChatType(t, ctx, agentID, sessionID, tc.channelType, tc.chatType, "msg-1", "msg-1")
			insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
			requeueTaskForClaim(t, ctx, sessionID)

			claimed := claimChatChannelFields(t, runtimeID)
			if claimed.ChannelType != tc.channelType {
				t.Errorf("chat_channel_type = %q, want %q", claimed.ChannelType, tc.channelType)
			}
			if claimed.ChatType != tc.chatType {
				t.Errorf("chat_type = %q, want %q — the prompt cannot describe a room it is not told about", claimed.ChatType, tc.chatType)
			}
		})
	}
}

// A web chat has no binding row, so there is no room shape to report. It comes
// back empty rather than fabricating "p2p": AudienceOf pairs that empty shape
// with an empty channel to infer web direct, while an external channel whose
// shape is empty remains unknown.
func TestClaim_UnboundSessionReportsNoRoomShape(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "web chat shape")
	insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
	requeueTaskForClaim(t, ctx, sessionID)

	if claimed := claimChatChannelFields(t, runtimeID); claimed.ChatType != "" {
		t.Errorf("chat_type = %q, want empty for a web-only session", claimed.ChatType)
	}
}

// requeueTaskForClaim puts the session's task back in the queue so the claim
// endpoint can hand it out (insertChannelChatTask creates it already running).
func requeueTaskForClaim(t *testing.T, ctx context.Context, sessionID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'queued', started_at = NULL, dispatched_at = NULL
		WHERE chat_session_id = $1
	`, sessionID); err != nil {
		t.Fatalf("requeue task: %v", err)
	}
}

// declareFileDeliveryForTest turns the deployment's file-delivery capability on
// for one test and puts it back afterwards. Production writes this map once at
// boot (cmd/server/router.go) and never again, so a test that changes it has to
// restore it or leak the capability into every claim that follows.
func declareFileDeliveryForTest(t *testing.T, channelTypes ...string) {
	t.Helper()
	prev := testHandler.channelFileDelivery
	t.Cleanup(func() { testHandler.channelFileDelivery = prev })
	testHandler.channelFileDelivery = map[string]bool{}
	for _, ct := range channelTypes {
		testHandler.DeclareChannelFileDelivery(ct)
	}
}

// Whether a file the agent produces reaches the conversation is the SERVER's
// answer, and this is where it is given.
//
// It cannot be worked out from the channel type, which is the shape the daemon
// used to have. Delivery needs two things — an adapter that goes back for the
// bound attachment, and object storage for it to go back to — and only the
// process that wired both knows whether it has them. A deployment running the
// WeCom adapter with no storage configured has the first and not the second, so
// "channel_type == wecom" answers true where the truth is false, and the agent
// is told to send a file that nothing will carry.
//
// The rows below are the same binding under different deployments, which is
// precisely what a channel-type answer cannot distinguish.
func TestClaim_FileDeliveryIsTheDeploymentsAnswerNotTheChannelTypes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	t.Run("declared: the deployment wired the hop", func(t *testing.T) {
		declareFileDeliveryForTest(t, "wecom")
		agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "wecom chat with storage")
		seedChannelBinding(t, ctx, agentID, sessionID, "wecom", "msg-1", "msg-1")
		insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
		requeueTaskForClaim(t, ctx, sessionID)

		claimed := claimChatChannelFields(t, runtimeID)
		if claimed.ChannelType != "wecom" {
			t.Fatalf("chat_channel_type = %q, want wecom", claimed.ChannelType)
		}
		if !claimed.DeliversFiles {
			t.Error("chat_channel_delivers_files = false on a deployment that declared the hop; the agent would be told to describe a file it could have sent")
		}
	})

	t.Run("not declared: the same channel with no object storage", func(t *testing.T) {
		declareFileDeliveryForTest(t) // nothing declared, as a storage-less deployment
		agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "wecom chat without storage")
		seedChannelBinding(t, ctx, agentID, sessionID, "wecom", "msg-1", "msg-1")
		insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
		requeueTaskForClaim(t, ctx, sessionID)

		claimed := claimChatChannelFields(t, runtimeID)
		if claimed.ChannelType != "wecom" {
			t.Fatalf("chat_channel_type = %q, want wecom", claimed.ChannelType)
		}
		if claimed.DeliversFiles {
			t.Error("chat_channel_delivers_files = true with no storage configured; there is no object to read the attachment out of, and the agent would promise a file that never arrives")
		}
	})

	t.Run("declared for one channel does not answer for another", func(t *testing.T) {
		declareFileDeliveryForTest(t, "wecom")
		agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "slack chat alongside wecom")
		seedChannelBinding(t, ctx, agentID, sessionID, "slack", "msg-1", "msg-1")
		insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
		requeueTaskForClaim(t, ctx, sessionID)

		if claimed := claimChatChannelFields(t, runtimeID); claimed.DeliversFiles {
			t.Error("a Slack session inherited WeCom's delivery capability; the flag is per channel, not per deployment")
		}
	})

	t.Run("a web chat is answered by its own branch, not this flag", func(t *testing.T) {
		declareFileDeliveryForTest(t, "wecom")
		agentID, sessionID, runtimeID, _ := setupDirectChatSession(t, ctx, "web chat alongside wecom")
		insertChannelChatTask(t, ctx, agentID, runtimeID, sessionID)
		requeueTaskForClaim(t, ctx, sessionID)

		claimed := claimChatChannelFields(t, runtimeID)
		if claimed.ChannelType != "" {
			t.Fatalf("chat_channel_type = %q, want empty for a web chat", claimed.ChannelType)
		}
		if claimed.DeliversFiles {
			t.Error("a web chat reported chat_channel_delivers_files; it has no channel and no last hop, and the browser renders its attachment card off the same bind")
		}
	})
}
