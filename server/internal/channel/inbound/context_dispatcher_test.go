package inbound

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/conversationctx"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// tc-6-1: Agent reply with entity triggers AppendEntities.
func TestDispatchStep_AppendEntities_AfterReply(t *testing.T) {
	store := conversationctx.NewFakeStore()
	issueSvc := &minimalFakeIssueService{
		createReturn: facade.Issue{Identifier: "STA-99"},
	}
	sink := &recordingReplySink{}

	d := NewDispatchStep(DispatchConfig{
		IssueFacade: facade.NewIssueFacade(issueSvc),
		ReplySink:   sink,
		ChatBinding: &fakeChatBindingForDispatch{wsID: pgtype.UUID{Valid: true}},
		UserResolver:  &fakeUserResolverForDispatch{},
		ConversationCtx: store,
		ContextMaxEntities: 5,
		ContextTTL:         30 * time.Minute,
	})

	ctx := context.Background()
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChatID:              "chat-1",
		SenderID:            "user-a",
		Text:                "创建 issue 登录页白屏",
		Intent: port.InboundIntent{
			Kind:   port.IntentCreateIssue,
			Params: map[string]string{"title": "登录页白屏"},
		},
	}

	_, _, err := d.Run(ctx, evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify that the reply was sent and entities were appended.
	if len(sink.replies) == 0 {
		t.Fatal("expected reply to be sent")
	}

	scope := conversationctx.Scope{ConnectionID: "conn-1", WorkspaceID: "00000000-0000-0000-0000-000000000000", ChatID: "chat-1", SenderID: "user-a", ThreadID: ""}
	cc, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected context to be appended after reply")
	}
	if len(cc.Entities) != 1 || cc.Entities[0].Key != "STA-99" {
		t.Fatalf("expected STA-99 in context, got %+v", cc.Entities)
	}
}

// tc-6-2: send error → no AppendEntities.
func TestDispatchStep_NoAppend_OnSendError(t *testing.T) {
	store := conversationctx.NewFakeStore()
	issueSvc := &minimalFakeIssueService{
		createReturn: facade.Issue{Identifier: "STA-99"},
	}
	sink := &errorReplySink{}

	d := NewDispatchStep(DispatchConfig{
		IssueFacade: facade.NewIssueFacade(issueSvc),
		ReplySink:   sink,
		ChatBinding: &fakeChatBindingForDispatch{wsID: pgtype.UUID{Valid: true}},
		UserResolver:  &fakeUserResolverForDispatch{},
		ConversationCtx: store,
		ContextMaxEntities: 5,
		ContextTTL:         30 * time.Minute,
	})

	ctx := context.Background()
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChatID:              "chat-1",
		SenderID:            "user-a",
		Text:                "创建 issue 登录页白屏",
		Intent: port.InboundIntent{
			Kind:   port.IntentCreateIssue,
			Params: map[string]string{"title": "登录页白屏"},
		},
	}

	_, _, err := d.Run(ctx, evt)
	if err == nil {
		t.Fatal("expected error from send failure")
	}

	scope := conversationctx.Scope{ConnectionID: "conn-1", WorkspaceID: "", ChatID: "chat-1", SenderID: "user-a", ThreadID: ""}
	_, ok, _ := store.Get(ctx, scope)
	if ok {
		t.Fatal("expected no context append when send fails")
	}
}

// tc-6-4: reply with no entity key → no AppendEntities.
func TestDispatchStep_NoAppend_WithoutEntity(t *testing.T) {
	store := conversationctx.NewFakeStore()
	// Use ASK_CLARIFY which returns a generic reply without issue keys.
	sink := &recordingReplySink{}

	d := NewDispatchStep(DispatchConfig{
		ReplySink: sink,
		ConversationCtx: store,
		ContextMaxEntities: 5,
		ContextTTL:         30 * time.Minute,
	})

	ctx := context.Background()
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChatID:              "chat-1",
		SenderID:            "user-a",
		Text:                "？",
		Intent: port.InboundIntent{
			Kind:   port.IntentASKClarify,
			Params: map[string]string{},
		},
	}

	_, _, err := d.Run(ctx, evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	scope := conversationctx.Scope{ConnectionID: "conn-1", WorkspaceID: "", ChatID: "chat-1", SenderID: "user-a", ThreadID: ""}
	_, ok, _ := store.Get(ctx, scope)
	if ok {
		t.Fatal("expected no context append when reply has no entity keys")
	}
}

type fakeChatBindingForDispatch struct {
	wsID pgtype.UUID
	err  error
}

func (f *fakeChatBindingForDispatch) LookupWorkspaceID(_ context.Context, _, _ string) (pgtype.UUID, error) {
	return f.wsID, f.err
}

type fakeUserResolverForDispatch struct{}

func (f *fakeUserResolverForDispatch) Resolve(_ context.Context, _, _ string) (ResolvedUser, error) {
	return ResolvedUser{MulticaUserID: pgtype.UUID{Valid: true}, DisplayName: "Test"}, nil
}

// minimalFakeIssueService satisfies facade.IssueFacade for dispatcher tests.
type minimalFakeIssueService struct {
	createReturn facade.Issue
}

func (f *minimalFakeIssueService) CreateIssue(_ context.Context, _ facade.CreateIssueReq) (facade.Issue, error) {
	return f.createReturn, nil
}
func (f *minimalFakeIssueService) GetIssue(_ context.Context, _ pgtype.UUID) (facade.Issue, error) {
	return facade.Issue{}, nil
}
func (f *minimalFakeIssueService) GetIssueByIdentifier(_ context.Context, _ pgtype.UUID, _ string) (facade.Issue, error) {
	return facade.Issue{}, nil
}
func (f *minimalFakeIssueService) SetIssueStatus(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, _ string, _ facade.ChannelMutationContext) error {
	return nil
}
func (f *minimalFakeIssueService) SetIssueAssignee(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, _ string, _ facade.ChannelMutationContext) error {
	return nil
}
func (f *minimalFakeIssueService) SetIssuePriority(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, _ string, _ facade.ChannelMutationContext) error {
	return nil
}
func (f *minimalFakeIssueService) AddIssueLabel(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, _ string, _ facade.ChannelMutationContext) error {
	return nil
}
func (f *minimalFakeIssueService) RemoveIssueLabel(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, _ string, _ facade.ChannelMutationContext) error {
	return nil
}
func (f *minimalFakeIssueService) ListMyTodos(_ context.Context, _, _ pgtype.UUID) ([]facade.Issue, error) {
	return nil, nil
}

type errorReplySink struct{}

func (s *errorReplySink) SendText(_ context.Context, _ port.InboundEvent, _ port.OutboundMessage) error {
	return context.Canceled
}

func (s *errorReplySink) SendRich(_ context.Context, _ port.InboundEvent, _ port.OutboundRichMessage) error {
	return context.Canceled
}

// Helper to create a pgtype.UUID from a byte pattern.
func makeUUID(pattern byte) pgtype.UUID {
	var b [16]byte
	for i := range b {
		b[i] = pattern
	}
	return pgtype.UUID{Bytes: b, Valid: true}
}
