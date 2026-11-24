package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestMemberChatSkillMessageTrustBoundary(t *testing.T) {
	messages := []db.ChatMessage{
		{Role: "user", MessageKind: protocol.ChatMessageKindMessage, Content: "member selection"},
		{Role: "assistant", MessageKind: protocol.ChatMessageKindMessage, Content: "agent selection"},
		{Role: "user", MessageKind: "onboarding_kickoff", Content: "system selection"},
		{Role: "user", MessageKind: protocol.ChatMessageKindMessage, ChannelIngested: true, Content: "channel quotation"},
	}
	for _, tc := range []struct {
		source, want string
		known        bool
	}{
		{`{"chat_input_actor_type":"member"}`, "member selection", true},
		{`{"chat_input_actor_type":"agent"}`, "", true},
		{`{"chat_input_actor_type":"system"}`, "", true},
		{`{"originator_source":"direct_human"}`, "", false},
		{`{}`, "", false}, {`bad`, "", false},
	} {
		got := memberChatSkillMessage([]byte(tc.source), messages)
		if tc.known {
			if got == nil || *got != tc.want {
				t.Errorf("source=%s got=%v want=%q", tc.source, got, tc.want)
			}
		} else if got != nil {
			t.Errorf("legacy/unknown source should retain the old hint: %s", tc.source)
		}
	}
}

func TestDirectChatSkillClaimUsesActualSender(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, actor := range []string{"member", "agent"} {
		t.Run(actor, func(t *testing.T) {
			ctx := context.Background()
			agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "skill sender")
			session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
			if err != nil {
				t.Fatal(err)
			}
			message := "[/spoofed label](slash://skill/selected) run this"
			sent, err := testHandler.TaskService.SendDirectChatMessage(ctx, session, agent, parseUUID(testUserID), message, nil, actor, parseUUID(testUserID))
			if err != nil {
				t.Fatal(err)
			}
			// A system kickoff shares role=user and the same input owner. It
			// must remain context without earning a member skill invocation.
			dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "task_id": uuidToString(sent.Task.ID), "role": "user", "message_kind": protocol.ChatMessageKindOnboardingKickoff, "content": "[/system](slash://skill/system)"})
			req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, daemonID)
			req = withURLParam(req, "runtimeId", runtimeID)
			var wire struct {
				Task *struct {
					Message   string `json:"chat_message"`
					Selection string `json:"chat_skill_message"`
				} `json:"task"`
			}
			testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&wire)
			if wire.Task == nil || !strings.Contains(wire.Task.Message, message) || !strings.Contains(wire.Task.Message, "slash://skill/system") {
				t.Fatalf("lost chat input: %+v", wire.Task)
			}
			want := ""
			if actor == "member" {
				want = message
			}
			if wire.Task.Selection != want {
				t.Fatalf("sender %s selection=%q want=%q", actor, wire.Task.Selection, want)
			}
		})
	}
}

func TestDirectChatSkillCapsuleBypassesSteerSupplement(t *testing.T) {
	sessionID, taskID, _ := newChatSupplementFixture(t, true)
	// A member message carrying an explicit skill capsule must not take the
	// text-only supplement channel: the selection only pays off in a real
	// turn, so it queues its own task even when steering would merge an
	// ordinary plain-text message into the running one.
	sent := sendChatGuidance(t, sessionID, map[string]any{"steer": true, "content": "[/review](slash://skill/selected) review this"})
	if sent.Steered || !sent.Queued || sent.TaskID == taskID {
		t.Fatalf("skill capsule steered into the text supplement channel: %+v", sent)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 2 {
		t.Fatalf("skill capsule did not create its own turn: %d", n)
	}
}
