package inbound

import (
	"context"
	"errors"
	"testing"
	"time"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestConversationKey_GroupIgnoresSender(t *testing.T) {
	a := port.InboundEvent{ChannelName: "feishu", ChatType: port.ChatTypeGroup, ChatID: "oc_1", SenderID: "ou_a"}
	b := port.InboundEvent{ChannelName: "feishu", ChatType: port.ChatTypeGroup, ChatID: "oc_1", SenderID: "ou_b"}
	if ConversationKey(a) != ConversationKey(b) {
		t.Fatalf("group conversation key should be chat-scoped: %q != %q", ConversationKey(a), ConversationKey(b))
	}
	if ProcessingKey(a) == ProcessingKey(b) {
		t.Fatalf("group processing key must include sender: %q", ProcessingKey(a))
	}
}

func TestConversationKey_DirectIgnoresChatID(t *testing.T) {
	a := port.InboundEvent{ChannelName: "feishu", ChatType: port.ChatTypeDirect, ChatID: "oc_1", SenderID: "ou_a"}
	b := port.InboundEvent{ChannelName: "feishu", ChatType: port.ChatTypeDirect, ChatID: "oc_2", SenderID: "ou_a"}
	if ConversationKey(a) != ConversationKey(b) {
		t.Fatalf("direct conversation key should be user-scoped: %q != %q", ConversationKey(a), ConversationKey(b))
	}
}

func TestConversationKey_ThreadUsesThreadID(t *testing.T) {
	a := port.InboundEvent{ChannelName: "feishu", ChatType: port.ChatTypeGroup, ChatID: "oc_1", SenderID: "ou_a", ThreadID: "thread_1"}
	b := port.InboundEvent{ChannelName: "feishu", ChatType: port.ChatTypeGroup, ChatID: "oc_1", SenderID: "ou_a", ThreadID: "thread_2"}
	if ConversationKey(a) == ConversationKey(b) {
		t.Fatalf("thread conversation key must include thread id: %q", ConversationKey(a))
	}
}

func TestRuntimeAccept_UserACKs(t *testing.T) {
	cases := []struct {
		name string
		res  AcceptResult
		want string
	}{
		{
			name: "accepted defers ack until pre-pipeline passes",
			res:  AcceptResult{EventID: "row-1", Accepted: true},
			want: "",
		},
		{
			name: "queued ack is deferred until pre-pipeline passes",
			res:  AcceptResult{EventID: "row-1", Accepted: true, QueueDepth: 2},
			want: "",
		},
		{
			name: "backpressure",
			res:  AcceptResult{EventID: "row-1", RejectedBackpressure: true, QueueDepth: 3},
			want: "我现在忙不过来了，当前会话还有 3 条在排队，请稍后再发。",
		},
		{
			name: "duplicate has no ack",
			res:  AcceptResult{EventID: "row-1", Duplicate: true},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeRuntimeStore{accept: tc.res}
			sink := &recordingReplySink{}
			rt := NewRuntime(RuntimeConfig{Store: store, ReplySink: sink})
			_, err := rt.Accept(context.Background(), port.InboundEvent{
				ChannelName: "feishu",
				EventID:     "evt-1",
				ChatID:      "oc_1",
				ChatType:    port.ChatTypeGroup,
				SenderID:    "ou_1",
			}, AcceptOptions{ConversationLimit: 3})
			if err != nil {
				t.Fatalf("Accept: %v", err)
			}
			if got := sink.last(); got != tc.want {
				t.Fatalf("ack = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRuntimeProcessRecord_SendsDeferredAckAfterPreContinue(t *testing.T) {
	store := &fakeRuntimeStore{}
	sink := &recordingReplySink{}
	rt := NewRuntime(RuntimeConfig{
		Store:     store,
		ReplySink: sink,
		PrePipeline: NewPipeline(fnStep{
			name: "pre",
			run: func(_ context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
				return evt, DecisionContinue, nil
			},
		}),
	})
	rt.deferProcessingAck("row-1")

	err := rt.processRecord(context.Background(), &InboundEventRecord{
		ID:    "row-1",
		Phase: InboundPhasePre,
		Event: port.InboundEvent{
			ChannelName: "feishu",
			EventID:     "evt-1",
			Type:        port.EventTypeMessageReceived,
			ChatID:      "oc_1",
			ChatType:    port.ChatTypeGroup,
			SenderID:    "ou_1",
			Text:        "hello",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	if got := sink.last(); got != "好的，开始处理。" {
		t.Fatalf("ack = %q", got)
	}
}

func TestRuntimeProcessRecord_DoesNotSendDeferredAckAfterPreSkip(t *testing.T) {
	store := &fakeRuntimeStore{}
	sink := &recordingReplySink{}
	convStore := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"row-1": {
				ID:             "msg-inbound",
				Provider:       "feishu",
				ConnectionID:   "conn-1",
				ConversationID: "conv-1",
			},
		},
	}
	rt := NewRuntime(RuntimeConfig{
		Store:             store,
		ReplySink:         sink,
		ConversationStore: convStore,
		PrePipeline: NewPipeline(fnStep{
			name: "pre",
			run: func(_ context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
				return evt, DecisionSkip, nil
			},
		}),
	})
	rt.deferProcessingAck("row-1")

	err := rt.processRecord(context.Background(), &InboundEventRecord{
		ID:    "row-1",
		Phase: InboundPhasePre,
		Event: port.InboundEvent{
			ChannelName:         "feishu",
			ChannelConnectionID: "conn-1",
			EventID:             "evt-1",
			Type:                port.EventTypeMessageReceived,
			ChatID:              "oc_1",
			ChatType:            port.ChatTypeGroup,
			SenderID:            "ou_1",
			Text:                "hello",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	if got := sink.last(); got != "" {
		t.Fatalf("ack = %q, want empty", got)
	}
	if _, ok := rt.pendingAckByEvent["row-1"]; ok {
		t.Fatal("deferred ack should be discarded when pre-pipeline skips")
	}
	if len(convStore.upsertedTurns) != 1 {
		t.Fatalf("upserted turns = %d, want 1", len(convStore.upsertedTurns))
	}
	turn := convStore.upsertedTurns[0]
	if turn.Status != channelconversation.TurnStatusSkipped {
		t.Fatalf("turn status = %q, want skipped", turn.Status)
	}
	if turn.IntentKind != string(chintent.IntentUnknown) {
		t.Fatalf("turn intent = %q, want Unknown", turn.IntentKind)
	}
	if turn.CompletedAt.IsZero() {
		t.Fatal("skipped turn should be terminal with completed_at set")
	}
}

func TestRuntimeProcessRecord_RuleIntentDoesNotWaitForAgent(t *testing.T) {
	store := &fakeRuntimeStore{}
	post := NewPipeline(fnStep{
		name: "post",
		run: func(_ context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
			if evt.Intent.Kind != port.IntentCreateIssue {
				t.Fatalf("intent = %q, want CreateIssue", evt.Intent.Kind)
			}
			return evt, DecisionContinue, nil
		},
	})
	rt := NewRuntime(RuntimeConfig{
		Store: store,
		RuleResolvers: []chintent.IntentResolver{fakeResolver{
			result: chintent.IntentResult{
				Matched: true,
				Intent: chintent.Intent{
					Kind:       chintent.IntentCreateIssue,
					Confidence: 1,
					Params:     map[string]string{"title": "from channel"},
					Source:     chintent.SourceRule,
				},
			},
		}},
		PostPipeline: post,
	})

	err := rt.processRecord(context.Background(), &InboundEventRecord{
		ID:    "row-1",
		Phase: InboundPhaseIntent,
		Event: port.InboundEvent{
			ChannelName: "feishu",
			EventID:     "evt-1",
			Type:        port.EventTypeMessageReceived,
			ChatID:      "oc_1",
			ChatType:    port.ChatTypeGroup,
			SenderID:    "ou_1",
			Text:        "create issue",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	if store.waitingAgent {
		t.Fatal("rule intent should not enter waiting_agent")
	}
	if !store.processed {
		t.Fatal("post pipeline completion should mark event processed")
	}
}

func TestRuntimeApplyIntentResult_ClarifyContinuesToPostPipeline(t *testing.T) {
	store := &fakeRuntimeStore{}
	rt := NewRuntime(RuntimeConfig{
		Store: store,
	})
	rec := &InboundEventRecord{
		ID:    "row-1",
		Phase: InboundPhaseIntent,
		Event: port.InboundEvent{ChannelName: "feishu", EventID: "evt-1", ChatID: "oc_1", SenderID: "ou_1"},
	}
	waiting, err := rt.applyIntentResult(context.Background(), rec, chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:   chintent.IntentASKClarify,
			Params: map[string]string{},
			Source: chintent.SourceChat,
		},
	}, ChatBindingContext{}, false)
	if err != nil {
		t.Fatalf("applyIntentResult: %v", err)
	}
	if waiting {
		t.Fatal("ASKClarify should continue through the post pipeline")
	}
	if rec.Phase != InboundPhasePost {
		t.Fatalf("phase = %q, want post", rec.Phase)
	}
}

func TestRuntimeProcessRecord_NaturalLanguageStartsChannelTurnBeforeRules(t *testing.T) {
	store := &fakeRuntimeStore{}
	rt := NewRuntime(RuntimeConfig{
		Store: store,
		RuleResolvers: []chintent.IntentResolver{fakeResolver{
			result: chintent.IntentResult{
				Matched: true,
				Intent:  chintent.Intent{Kind: chintent.IntentQueryProgress, Params: map[string]string{"scope": "projects"}},
			},
		}},
		ChatIntent: fakeAsyncIntentClient{taskID: "550e8400-e29b-41d4-a716-446655440000"},
	})

	err := rt.processRecord(context.Background(), &InboundEventRecord{
		ID:          "row-1",
		Phase:       InboundPhaseIntent,
		WorkspaceID: "550e8400-e29b-41d4-a716-446655440001",
		Event: port.InboundEvent{
			ChannelName: "feishu",
			EventID:     "evt-1",
			Type:        port.EventTypeMessageReceived,
			ChatID:      "oc_1",
			ChatType:    port.ChatTypeGroup,
			SenderID:    "ou_1",
			Text:        "各项目进展怎么样？",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	if !store.waitingAgent {
		t.Fatal("natural-language turn should wait for channel agent")
	}
	if store.waitKind != WaitKindChannelTurn {
		t.Fatalf("wait kind = %q, want %q", store.waitKind, WaitKindChannelTurn)
	}
}

func TestRuntimeProcessRecord_StartsChannelTurnWithConversationContext(t *testing.T) {
	ctx := context.Background()
	store := &fakeRuntimeStore{}
	conversationStore := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"row-1": {ConversationID: "00000000-0000-0000-0000-000000000001"},
		},
		recentRefs: []channelconversation.EntityRef{{
			EntityType: channelconversation.EntityTypeIssue,
			EntityKey:  "STA-12",
			Role:       channelconversation.EntityRoleMentioned,
		}},
	}
	channelTurn := &recordingChannelTurnClient{taskID: "550e8400-e29b-41d4-a716-446655440000"}
	rt := NewRuntime(RuntimeConfig{
		Store:             store,
		ConversationStore: conversationStore,
		ChannelTurn:       channelTurn,
	})

	err := rt.processRecord(ctx, &InboundEventRecord{
		ID:          "row-1",
		Phase:       InboundPhaseIntent,
		WorkspaceID: "550e8400-e29b-41d4-a716-446655440001",
		Event: port.InboundEvent{
			ChannelName:         "feishu",
			ChannelConnectionID: "conn-1",
			EventID:             "evt-1",
			Type:                port.EventTypeMessageReceived,
			ChatID:              "oc_1",
			ChatType:            port.ChatTypeGroup,
			SenderID:            "ou_1",
			ThreadID:            "thread-1",
			Text:                "把它改成 done",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	if !store.waitingAgent {
		t.Fatal("natural-language turn should wait for channel agent")
	}
	if len(channelTurn.startReqs) != 1 {
		t.Fatalf("StartAgentTurn calls = %d, want 1", len(channelTurn.startReqs))
	}
	req := channelTurn.startReqs[0]
	if len(req.ContextEntities) != 1 || req.ContextEntities[0].EntityKey != "STA-12" {
		t.Fatalf("ContextEntities = %+v, want STA-12", req.ContextEntities)
	}
	if req.ContextIssueKey != "STA-12" {
		t.Fatalf("ContextIssueKey = %q, want STA-12", req.ContextIssueKey)
	}
	if req.ThreadID != "thread-1" {
		t.Fatalf("ThreadID = %q, want thread-1", req.ThreadID)
	}
}

func TestRuntimeProcessRecord_FillsRuleIntentIssueKeyBeforePostPhase(t *testing.T) {
	ctx := context.Background()
	store := &fakeRuntimeStore{}
	conversationStore := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"row-1": {ConversationID: "00000000-0000-0000-0000-000000000001"},
		},
		recentRefs: []channelconversation.EntityRef{{
			EntityType: channelconversation.EntityTypeIssue,
			EntityKey:  "STA-12",
			Role:       channelconversation.EntityRoleMentioned,
		}},
	}
	rt := NewRuntime(RuntimeConfig{
		Store:             store,
		ConversationStore: conversationStore,
		RuleResolvers: []chintent.IntentResolver{fakeResolver{
			result: chintent.IntentResult{
				Matched: true,
				Intent: chintent.Intent{
					Kind:       chintent.IntentSetStatus,
					Confidence: 1,
					Params:     map[string]string{"status": "done"},
					Source:     chintent.SourceRule,
				},
			},
		}},
	})

	err := rt.processRecord(ctx, &InboundEventRecord{
		ID:          "row-1",
		Phase:       InboundPhaseIntent,
		WorkspaceID: "550e8400-e29b-41d4-a716-446655440001",
		Event: port.InboundEvent{
			ChannelName:         "feishu",
			ChannelConnectionID: "conn-1",
			EventID:             "evt-1",
			Type:                port.EventTypeMessageReceived,
			ChatID:              "oc_1",
			ChatType:            port.ChatTypeGroup,
			SenderID:            "ou_1",
			Text:                "/done",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	if store.savedEvent.Intent.Params["issue_key"] != "STA-12" {
		t.Fatalf("saved issue_key = %q, want STA-12", store.savedEvent.Intent.Params["issue_key"])
	}
	if store.savedPhase != InboundPhasePost {
		t.Fatalf("saved phase = %q, want %q", store.savedPhase, InboundPhasePost)
	}
}

func TestRuntimeResumeChannelTurnSendsFinalReply(t *testing.T) {
	store := &fakeRuntimeStore{
		load: &InboundEventRecord{
			ID: "row-1",
			Event: port.InboundEvent{
				ChannelName: "feishu",
				EventID:     "evt-1",
				ChatID:      "oc_1",
				ChatType:    port.ChatTypeGroup,
				SenderID:    "ou_1",
			},
		},
	}
	sink := &recordingReplySink{}
	dispatch := &fakeDispatchStore{}
	rt := NewRuntime(RuntimeConfig{
		Store:         store,
		ReplySink:     sink,
		DispatchStore: dispatch,
		ChannelTurn:   fakeAsyncIntentClient{done: true},
	})

	rt.resumeChannelTurn(context.Background(), WaitingAgentEvent{
		ID:         "row-1",
		WaitKind:   WaitKindChannelTurn,
		WaitTaskID: "550e8400-e29b-41d4-a716-446655440000",
	})

	if got := sink.last(); got != "channel reply" {
		t.Fatalf("reply = %q, want channel reply", got)
	}
	if !store.processed {
		t.Fatal("channel turn completion should mark event processed")
	}
	if dispatch.reply != "channel reply" {
		t.Fatalf("persisted reply = %q", dispatch.reply)
	}
}

func TestRuntimeResumeChannelTurnRetriesPersistedReplyAfterSendFailure(t *testing.T) {
	store := &fakeRuntimeStore{
		load: &InboundEventRecord{
			ID: "row-1",
			Event: port.InboundEvent{
				ChannelName: "feishu",
				EventID:     "evt-1",
				ChatID:      "oc_1",
				ChatType:    port.ChatTypeGroup,
				SenderID:    "ou_1",
			},
		},
	}
	sink := &recordingReplySink{sendErr: errors.New("provider unavailable")}
	dispatch := &fakeDispatchStore{}
	rt := NewRuntime(RuntimeConfig{
		Store:         store,
		ReplySink:     sink,
		DispatchStore: dispatch,
		ChannelTurn:   fakeAsyncIntentClient{done: true},
	})
	item := WaitingAgentEvent{
		ID:         "row-1",
		WaitKind:   WaitKindChannelTurn,
		WaitTaskID: "550e8400-e29b-41d4-a716-446655440000",
	}

	rt.resumeChannelTurn(context.Background(), item)

	if store.processed {
		t.Fatal("send failure must not mark channel turn processed")
	}
	if dispatch.reply != "channel reply" {
		t.Fatalf("persisted reply = %q", dispatch.reply)
	}
	if sink.sendCalls != 1 {
		t.Fatalf("send calls after failure = %d, want 1", sink.sendCalls)
	}

	sink.sendErr = nil
	rt.resumeChannelTurn(context.Background(), item)

	if got := sink.last(); got != "channel reply" {
		t.Fatalf("reply = %q, want channel reply", got)
	}
	if !store.processed {
		t.Fatal("successful retry should mark channel turn processed")
	}
	if sink.sendCalls != 2 {
		t.Fatalf("send calls after retry = %d, want 2", sink.sendCalls)
	}
}

func TestRuntimeStartChannelTurnFailureSendsOncePerEvent(t *testing.T) {
	store := &fakeRuntimeStore{}
	sink := &recordingReplySink{}
	dispatch := &fakeDispatchStore{}
	rt := NewRuntime(RuntimeConfig{
		Store:         store,
		ReplySink:     sink,
		DispatchStore: dispatch,
		ChatIntent:    fakeAsyncIntentClient{err: errors.New("no runtime")},
	})
	rec := &InboundEventRecord{
		ID:          "row-1",
		Phase:       InboundPhaseIntent,
		WorkspaceID: "550e8400-e29b-41d4-a716-446655440001",
		Event: port.InboundEvent{
			ChannelName:         "feishu",
			ChannelConnectionID: "conn-1",
			EventID:             "evt-1",
			Type:                port.EventTypeMessageReceived,
			ChatID:              "oc_1",
			ChatType:            port.ChatTypeGroup,
			SenderID:            "ou_1",
			Text:                "各项目进展怎么样？",
		},
	}

	if err := rt.processRecord(context.Background(), rec); err != nil {
		t.Fatalf("processRecord first: %v", err)
	}
	if err := rt.processRecord(context.Background(), rec); err != nil {
		t.Fatalf("processRecord second: %v", err)
	}
	if got := len(sink.replies); got != 1 {
		t.Fatalf("reply count = %d, want 1", got)
	}
}

func TestRuntimeStartChannelTurnFailureUsesUserVisibleMessage(t *testing.T) {
	store := &fakeRuntimeStore{}
	sink := &recordingReplySink{}
	rt := NewRuntime(RuntimeConfig{
		Store:     store,
		ReplySink: sink,
		ChatIntent: fakeAsyncIntentClient{err: &chintent.ChannelAgentUnavailableError{
			Message: "指定智能体当前不可用，或对应运行时不支持群聊语义处理。请换一个智能体，或重启/更新运行时后再试。",
			Reason:  "bound agent runtime does not advertise channel_turn",
		}},
	})

	err := rt.processRecord(context.Background(), &InboundEventRecord{
		ID:          "row-1",
		Phase:       InboundPhaseIntent,
		WorkspaceID: "550e8400-e29b-41d4-a716-446655440001",
		Event: port.InboundEvent{
			ChannelName:         "feishu",
			ChannelConnectionID: "conn-1",
			EventID:             "evt-1",
			Type:                port.EventTypeMessageReceived,
			ChatID:              "oc_1",
			ChatType:            port.ChatTypeGroup,
			SenderID:            "ou_1",
			Text:                "各项目进展怎么样？",
		},
	})
	if err != nil {
		t.Fatalf("processRecord: %v", err)
	}
	want := "指定智能体当前不可用，或对应运行时不支持群聊语义处理。请换一个智能体，或重启/更新运行时后再试。"
	if got := sink.last(); got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestRuntimeStartChannelTurnFailureCooldownSuppressesChatSpam(t *testing.T) {
	store := &fakeRuntimeStore{}
	sink := &recordingReplySink{}
	dispatch := &fakeDispatchStore{}
	rt := NewRuntime(RuntimeConfig{
		Store:         store,
		ReplySink:     sink,
		DispatchStore: dispatch,
		ChatIntent:    fakeAsyncIntentClient{err: errors.New("no runtime")},
	})

	for _, id := range []string{"row-1", "row-2"} {
		err := rt.processRecord(context.Background(), &InboundEventRecord{
			ID:          id,
			Phase:       InboundPhaseIntent,
			WorkspaceID: "550e8400-e29b-41d4-a716-446655440001",
			Event: port.InboundEvent{
				ChannelName:         "feishu",
				ChannelConnectionID: "conn-1",
				EventID:             "evt-" + id,
				Type:                port.EventTypeMessageReceived,
				ChatID:              "oc_1",
				ChatType:            port.ChatTypeGroup,
				SenderID:            "ou_1",
				Text:                "各项目进展怎么样？",
			},
		})
		if err != nil {
			t.Fatalf("processRecord %s: %v", id, err)
		}
	}
	if got := len(sink.replies); got != 1 {
		t.Fatalf("reply count = %d, want 1", got)
	}
	if _, ok := dispatch.completion("row-2"); !ok {
		t.Fatal("suppressed failure should still persist dispatch completion")
	}
}

func TestRuntimeResumeChannelTurnFailureUsesFailureOnce(t *testing.T) {
	store := &fakeRuntimeStore{
		load: &InboundEventRecord{
			ID: "row-1",
			Event: port.InboundEvent{
				ChannelName:         "feishu",
				ChannelConnectionID: "conn-1",
				EventID:             "evt-1",
				ChatID:              "oc_1",
				ChatType:            port.ChatTypeGroup,
				SenderID:            "ou_1",
			},
		},
	}
	sink := &recordingReplySink{}
	dispatch := &fakeDispatchStore{}
	rt := NewRuntime(RuntimeConfig{
		Store:         store,
		ReplySink:     sink,
		DispatchStore: dispatch,
		ChannelTurn:   fakeAsyncIntentClient{done: true, err: errors.New("task failed")},
	})

	item := WaitingAgentEvent{ID: "row-1", WaitKind: WaitKindChannelTurn, WaitTaskID: "550e8400-e29b-41d4-a716-446655440000"}
	rt.resumeChannelTurn(context.Background(), item)
	rt.resumeChannelTurn(context.Background(), item)

	if got := len(sink.replies); got != 1 {
		t.Fatalf("reply count = %d, want 1", got)
	}
	if !store.processed {
		t.Fatal("failed channel turn should mark event processed")
	}
}

func TestRuntimeWorker_DeadRetryNotifiesUser(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeRuntimeStore{
		claim: InboundEventRecord{
			ID:    "row-1",
			Phase: InboundPhasePost,
			Event: port.InboundEvent{ChannelName: "feishu", EventID: "evt-1", ChatID: "oc_1", SenderID: "ou_1"},
		},
		retry:   RetryResult{Dead: true},
		onRetry: cancel,
	}
	sink := &recordingReplySink{}
	rt := NewRuntime(RuntimeConfig{
		Store:     store,
		ReplySink: sink,
		PostPipeline: NewPipeline(fnStep{
			name: "post",
			run: func(context.Context, port.InboundEvent) (port.InboundEvent, Decision, error) {
				return port.InboundEvent{}, DecisionContinue, context.DeadlineExceeded
			},
		}),
	})

	rt.workerLoop(ctx, "worker-1")
	if got := sink.last(); got != "处理失败了，这条消息我先停止处理，请稍后重试。" {
		t.Fatalf("dead retry reply = %q", got)
	}
}

type fakeRuntimeStore struct {
	accept       AcceptResult
	claim        InboundEventRecord
	load         *InboundEventRecord
	savedEvent   port.InboundEvent
	savedPhase   string
	claimed      bool
	waitingAgent bool
	waitKind     string
	processed    bool
	retry        RetryResult
	onRetry      func()
	chatCtx      ChatBindingContext
}

func (s *fakeRuntimeStore) AcceptEvent(context.Context, port.InboundEvent, AcceptOptions) (AcceptResult, error) {
	return s.accept, nil
}

func (s *fakeRuntimeStore) Load(context.Context, string) (*InboundEventRecord, error) {
	if s.load != nil {
		rec := *s.load
		return &rec, nil
	}
	return nil, nil
}

func (s *fakeRuntimeStore) ClaimNext(context.Context, string) (*InboundEventRecord, error) {
	if s.claim.ID != "" && !s.claimed {
		s.claimed = true
		rec := s.claim
		return &rec, nil
	}
	return nil, nil
}

func (s *fakeRuntimeStore) SaveEvent(_ context.Context, _ string, evt port.InboundEvent, phase string, _ ChatBindingContext) error {
	s.savedEvent = evt
	s.savedPhase = phase
	return nil
}

func (s *fakeRuntimeStore) MarkQueued(context.Context, string, port.InboundEvent, string, ChatBindingContext) error {
	return nil
}

func (s *fakeRuntimeStore) MarkWaitingAgent(_ context.Context, _ string, _ port.InboundEvent, _ string, _ ChatBindingContext, waitKind string) error {
	s.waitingAgent = true
	s.waitKind = waitKind
	return nil
}

func (s *fakeRuntimeStore) MarkProcessed(context.Context, string) error {
	s.processed = true
	return nil
}

func (s *fakeRuntimeStore) MarkRetry(context.Context, string, error) (RetryResult, error) {
	if s.onRetry != nil {
		s.onRetry()
	}
	return s.retry, nil
}

func (s *fakeRuntimeStore) MarkDead(context.Context, string, error) error {
	return nil
}

func (s *fakeRuntimeStore) ListWaitingAgent(context.Context, int) ([]WaitingAgentEvent, error) {
	return nil, nil
}

func (s *fakeRuntimeStore) LookupChatContext(context.Context, string, string) (ChatBindingContext, error) {
	return s.chatCtx, nil
}

func (s *fakeRuntimeStore) RequeueStaleProcessing(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

type recordingReplySink struct {
	replies   []string
	sendErr   error
	sendCalls int
}

func (s *recordingReplySink) SendText(_ context.Context, _ port.InboundEvent, msg port.OutboundMessage) error {
	s.sendCalls++
	if s.sendErr != nil {
		return s.sendErr
	}
	s.replies = append(s.replies, msg.Text)
	return nil
}

func (s *recordingReplySink) SendRich(_ context.Context, _ port.InboundEvent, msg port.OutboundRichMessage) error {
	s.sendCalls++
	if s.sendErr != nil {
		return s.sendErr
	}
	s.replies = append(s.replies, msg.Body)
	return nil
}

func (s *recordingReplySink) last() string {
	if len(s.replies) == 0 {
		return ""
	}
	return s.replies[len(s.replies)-1]
}

type fakeDispatchStore struct {
	reply     string
	ok        bool
	replies   map[string]string
	completed map[string]bool
}

func (s *fakeDispatchStore) GetDispatchCompletion(_ context.Context, id string) (string, bool, error) {
	if s.completed != nil {
		if s.completed[id] {
			return s.replies[id], true, nil
		}
		return "", false, nil
	}
	if s.ok || s.reply != "" {
		return s.reply, true, nil
	}
	return "", false, nil
}

func (s *fakeDispatchStore) MarkDispatchCompleted(_ context.Context, id string, reply string) error {
	if s.replies == nil {
		s.replies = map[string]string{}
	}
	if s.completed == nil {
		s.completed = map[string]bool{}
	}
	s.reply = reply
	s.ok = true
	s.replies[id] = reply
	s.completed[id] = true
	return nil
}

func (s *fakeDispatchStore) completion(id string) (string, bool) {
	if s.completed == nil || !s.completed[id] {
		return "", false
	}
	return s.replies[id], true
}

type fakeResolver struct {
	result chintent.IntentResult
	err    error
}

func (r fakeResolver) Name() string { return "fake" }

func (r fakeResolver) Resolve(context.Context, chintent.IntentRequest) (chintent.IntentResult, error) {
	return r.result, r.err
}

type fakeAsyncIntentClient struct {
	taskID string
	result chintent.IntentResult
	done   bool
	err    error
	reply  string
}

func (f fakeAsyncIntentClient) StartIntent(context.Context, chintent.IntentRequest) (string, error) {
	return f.taskID, f.err
}

func (f fakeAsyncIntentClient) ParseIntentResult(context.Context, string) (chintent.IntentResult, bool, error) {
	return f.result, f.done, f.err
}

func (f fakeAsyncIntentClient) StartTurn(ctx context.Context, req chintent.IntentRequest) (string, error) {
	return f.StartIntent(ctx, req)
}

func (f fakeAsyncIntentClient) ParseTurnResult(ctx context.Context, taskID string) (chintent.IntentResult, bool, error) {
	return f.ParseIntentResult(ctx, taskID)
}

func (f fakeAsyncIntentClient) StartAgentTurn(context.Context, chintent.IntentRequest) (string, error) {
	return f.taskID, f.err
}

func (f fakeAsyncIntentClient) ParseAgentTurnResult(context.Context, string) (string, bool, error) {
	reply := f.reply
	if reply == "" {
		reply = "channel reply"
	}
	return reply, f.done, f.err
}

type recordingChannelTurnClient struct {
	taskID    string
	startReqs []chintent.IntentRequest
	reply     string
	done      bool
	err       error
}

func (f *recordingChannelTurnClient) StartAgentTurn(_ context.Context, req chintent.IntentRequest) (string, error) {
	f.startReqs = append(f.startReqs, req)
	return f.taskID, nil
}

func (f *recordingChannelTurnClient) ParseAgentTurnResult(context.Context, string) (string, bool, error) {
	reply := f.reply
	if reply == "" {
		reply = "channel reply"
	}
	done := f.done
	if !done {
		done = true
	}
	return reply, done, f.err
}

type fnStep struct {
	name string
	run  func(context.Context, port.InboundEvent) (port.InboundEvent, Decision, error)
}

func (s fnStep) Name() string { return s.name }

func (s fnStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	return s.run(ctx, evt)
}
