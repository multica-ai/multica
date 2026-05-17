package inbound_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/gateway"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/channel/replyctx"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type fakeChatBinding struct {
	wsID pgtype.UUID
	err  error
}

func (f *fakeChatBinding) LookupWorkspaceID(_ context.Context, _, _ string) (pgtype.UUID, error) {
	return f.wsID, f.err
}

type fakeUserResolver struct {
	user inbound.ResolvedUser
	err  error
}

func (f *fakeUserResolver) Resolve(_ context.Context, _, _ string) (inbound.ResolvedUser, error) {
	return f.user, f.err
}

type fakeProjectValidator struct {
	calls []struct {
		WorkspaceID pgtype.UUID
		ProjectID   pgtype.UUID
	}
	err error
}

func (f *fakeProjectValidator) ValidateProjectInWorkspace(_ context.Context, workspaceID, projectID pgtype.UUID) error {
	f.calls = append(f.calls, struct {
		WorkspaceID pgtype.UUID
		ProjectID   pgtype.UUID
	}{WorkspaceID: workspaceID, ProjectID: projectID})
	return f.err
}

type fakeIssueService struct {
	created []facade.CreateIssueReq
	gotByID []struct {
		WorkspaceID pgtype.UUID
		Identifier  string
	}
	setStatus []struct {
		ID      pgtype.UUID
		ActorID pgtype.UUID
		Status  string
	}
	setAssignee []struct {
		ID                 pgtype.UUID
		ActorID            pgtype.UUID
		AssigneeIdentifier string
	}
	setPriority []struct {
		ID       pgtype.UUID
		ActorID  pgtype.UUID
		Priority string
	}
	addLabel []struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}
	removeLabel []struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}
	listTodos []struct {
		WorkspaceID pgtype.UUID
		UserID      pgtype.UUID
	}
	createReturn       facade.Issue
	getByIdentifierRet facade.Issue
	listTodosReturn    []facade.Issue
	createErr          error
	getByIdentifierErr error
	setStatusErr       error
	setAssigneeErr     error
	setPriorityErr     error
	addLabelErr        error
	removeLabelErr     error
	listTodosErr       error
}

func (f *fakeIssueService) CreateIssue(_ context.Context, req facade.CreateIssueReq) (facade.Issue, error) {
	f.created = append(f.created, req)
	return f.createReturn, f.createErr
}

func (f *fakeIssueService) GetIssue(_ context.Context, _ pgtype.UUID) (facade.Issue, error) {
	return facade.Issue{}, nil
}

func (f *fakeIssueService) GetIssueByIdentifier(_ context.Context, wsID pgtype.UUID, identifier string) (facade.Issue, error) {
	f.gotByID = append(f.gotByID, struct {
		WorkspaceID pgtype.UUID
		Identifier  string
	}{wsID, identifier})
	return f.getByIdentifierRet, f.getByIdentifierErr
}

func (f *fakeIssueService) SetIssueStatus(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, status string, _ facade.ChannelMutationContext) error {
	f.setStatus = append(f.setStatus, struct {
		ID      pgtype.UUID
		ActorID pgtype.UUID
		Status  string
	}{id, actorID, status})
	return f.setStatusErr
}

func (f *fakeIssueService) SetIssueAssignee(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, assigneeIdentifier string, _ facade.ChannelMutationContext) error {
	f.setAssignee = append(f.setAssignee, struct {
		ID                 pgtype.UUID
		ActorID            pgtype.UUID
		AssigneeIdentifier string
	}{id, actorID, assigneeIdentifier})
	return f.setAssigneeErr
}

func (f *fakeIssueService) SetIssuePriority(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, priority string, _ facade.ChannelMutationContext) error {
	f.setPriority = append(f.setPriority, struct {
		ID       pgtype.UUID
		ActorID  pgtype.UUID
		Priority string
	}{id, actorID, priority})
	return f.setPriorityErr
}

func (f *fakeIssueService) AddIssueLabel(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, _ facade.ChannelMutationContext) error {
	f.addLabel = append(f.addLabel, struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}{id, actorID, labelName})
	return f.addLabelErr
}

func (f *fakeIssueService) RemoveIssueLabel(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, _ facade.ChannelMutationContext) error {
	f.removeLabel = append(f.removeLabel, struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}{id, actorID, labelName})
	return f.removeLabelErr
}

func (f *fakeIssueService) ListMyTodos(_ context.Context, wsID, userID pgtype.UUID) ([]facade.Issue, error) {
	f.listTodos = append(f.listTodos, struct {
		WorkspaceID pgtype.UUID
		UserID      pgtype.UUID
	}{wsID, userID})
	return f.listTodosReturn, f.listTodosErr
}

type fakeCommentService struct {
	added     []facade.AddCommentReq
	addReturn facade.Comment
	addErr    error
}

type fakeIssueDigestService struct {
	digest   facade.IssueDigest
	progress facade.IssueProgress
	projects []facade.ProjectProgress
	detail   facade.IssueDetail
	timeline facade.IssueTimelinePage
	logs     facade.IssueLogPage
	err      error
}

func (f *fakeIssueDigestService) GetIssueDigest(_ context.Context, _ pgtype.UUID, _ string) (facade.IssueDigest, error) {
	return f.digest, f.err
}

func (f *fakeIssueDigestService) GetIssueProgress(_ context.Context, _ pgtype.UUID, _ string) (facade.IssueProgress, error) {
	if f.progress.Digest.Issue.Identifier == "" {
		return facade.IssueProgress{Digest: f.digest}, f.err
	}
	return f.progress, f.err
}

func (f *fakeIssueDigestService) ListProjectProgress(_ context.Context, _ pgtype.UUID) ([]facade.ProjectProgress, error) {
	return f.projects, f.err
}

func (f *fakeIssueDigestService) GetIssueDetail(_ context.Context, _ pgtype.UUID, _ string) (facade.IssueDetail, error) {
	return f.detail, f.err
}

func (f *fakeIssueDigestService) GetIssueTimeline(_ context.Context, _ pgtype.UUID, _ string, _, _ int) (facade.IssueTimelinePage, error) {
	return f.timeline, f.err
}

func (f *fakeIssueDigestService) GetIssueLogs(_ context.Context, _ pgtype.UUID, _ string, _, _ int) (facade.IssueLogPage, error) {
	return f.logs, f.err
}

type fakeActionProposalStore struct {
	created []inbound.ActionProposalCreateRequest
	found   inbound.ActionProposal
	findErr error
	marked  []string
}

func (f *fakeActionProposalStore) CreateActionProposal(_ context.Context, req inbound.ActionProposalCreateRequest) (inbound.ActionProposal, error) {
	f.created = append(f.created, req)
	expiresAt := req.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(10 * time.Minute)
	}
	return inbound.ActionProposal{
		ID:             uuid(0x90),
		Code:           "ABCD1234",
		ConnectionID:   req.ConnectionID,
		ChatID:         req.ChatID,
		SenderID:       req.SenderID,
		WorkspaceID:    req.WorkspaceID,
		InboundEventID: req.InboundEventID,
		Intent:         req.Intent,
		Status:         "pending",
		ExpiresAt:      expiresAt,
	}, nil
}

func (f *fakeActionProposalStore) FindActionProposal(_ context.Context, _, _, _, _ string) (inbound.ActionProposal, error) {
	return f.found, f.findErr
}

func (f *fakeActionProposalStore) MarkActionProposalStatus(_ context.Context, _ pgtype.UUID, status string) error {
	f.marked = append(f.marked, status)
	return nil
}

type fakeReplyContextStore struct {
	ctx     inboundReplyContext
	ok      bool
	err     error
	upserts []replyctx.Context
}

type inboundReplyContext = struct {
	WorkspaceID     pgtype.UUID
	IssueID         pgtype.UUID
	IssueIdentifier string
	IssueTitle      string
	ExpiresAt       time.Time
}

func (f *fakeReplyContextStore) Lookup(_ context.Context, _, _, _ string, _ time.Time) (replyctx.Context, bool, error) {
	return replyctx.Context{
		WorkspaceID:     f.ctx.WorkspaceID,
		IssueID:         f.ctx.IssueID,
		IssueIdentifier: f.ctx.IssueIdentifier,
		IssueTitle:      f.ctx.IssueTitle,
		ExpiresAt:       f.ctx.ExpiresAt,
	}, f.ok, f.err
}

func (f *fakeReplyContextStore) Upsert(_ context.Context, item replyctx.Context) error {
	f.upserts = append(f.upserts, item)
	return nil
}

func (f *fakeReplyContextStore) Clear(_ context.Context, _, _, _ string) error {
	return nil
}

func (f *fakeCommentService) AddComment(_ context.Context, req facade.AddCommentReq) (facade.Comment, error) {
	f.added = append(f.added, req)
	return f.addReturn, f.addErr
}

type recordingChannel struct {
	name    string
	sends   []port.OutboundMessage
	cards   []port.OutboundCardMessage
	sendErr error
}

func (r *recordingChannel) Name() string                       { return r.name }
func (r *recordingChannel) Connect(_ context.Context) error    { return nil }
func (r *recordingChannel) Disconnect(_ context.Context) error { return nil }
func (r *recordingChannel) Events() <-chan port.InboundEvent   { return nil }
func (r *recordingChannel) GetChatInfo(_ context.Context, _ string) (port.ChatInfo, error) {
	return port.ChatInfo{}, nil
}
func (r *recordingChannel) GetUserInfo(_ context.Context, _ string) (port.UserInfo, error) {
	return port.UserInfo{}, nil
}
func (r *recordingChannel) Send(_ context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	r.sends = append(r.sends, msg)
	return port.SendResult{PlatformMessageID: "msg-1"}, r.sendErr
}
func (r *recordingChannel) SendCard(_ context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	r.cards = append(r.cards, msg)
	return port.SendResult{PlatformMessageID: "card-1"}, r.sendErr
}

func uuid(tag byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = tag
	}
	u.Valid = true
	return u
}

func buildDispatchConfig() (inbound.DispatchConfig, *fakeIssueService, *fakeCommentService, *recordingChannel) {
	issueSvc := &fakeIssueService{}
	commentSvc := &fakeCommentService{}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.DispatchConfig{
		IssueFacade:   facade.NewIssueFacade(issueSvc),
		CommentFacade: facade.NewCommentFacade(commentSvc),
		ReplySink:     inbound.NewGatewayReplySink(gw),
		ChatBinding:   &fakeChatBinding{wsID: uuid(0x01)},
		UserResolver:  &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: uuid(0x02), DisplayName: "测试用户"}},
	}
	return cfg, issueSvc, commentSvc, recCh
}

func makeEvt(intentKind port.IntentKind, params map[string]string) port.InboundEvent {
	return port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-1",
		ChatID:      "chat-1",
		SenderID:    "ou_sender1",
		Text:        "some text",
		Intent: port.InboundIntent{
			Kind:       intentKind,
			Confidence: 1,
			Params:     params,
			Source:     port.SourceRule,
		},
	}
}

// ---------------------------------------------------------------------------
// TC-intent-2: Unsupported ops return UNSUPPORTED_OP template
// ---------------------------------------------------------------------------

func TestDispatchStep_UnsupportedOp_ReturnsUnsupportedTemplate(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentUnsupported, map[string]string{"issue_key": "STA-2"})
	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if out.EventID != "evt-1" {
		t.Error("event was not returned unchanged")
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "UNSUPPORTED_OP") {
		t.Errorf("reply missing UNSUPPORTED_OP key: %q", recCh.sends[0].Text)
	}
	if !strings.Contains(recCh.sends[0].Text, "Web 端") {
		t.Errorf("reply should mention Web 端: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-int-2: Create issue happy path
// ---------------------------------------------------------------------------

func TestDispatchStep_CreateIssue_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.createReturn = facade.Issue{
		ID:          uuid(0xAA),
		WorkspaceID: uuid(0x01),
		Identifier:  "STA-39",
		Title:       "登录页加载慢",
		Status:      "todo",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "登录页加载慢"})
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}

	if len(issueSvc.created) != 1 {
		t.Fatalf("expected 1 CreateIssue call, got %d", len(issueSvc.created))
	}
	call := issueSvc.created[0]
	if call.Title != "登录页加载慢" {
		t.Errorf("title = %q, want %q", call.Title, "登录页加载慢")
	}
	if call.WorkspaceID != uuid(0x01) {
		t.Error("workspace ID mismatch")
	}
	if call.ActorID != uuid(0x02) {
		t.Error("actor ID mismatch")
	}

	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "ISSUE_CREATED") {
		t.Errorf("reply missing ISSUE_CREATED: %q", recCh.sends[0].Text)
	}
	if !strings.Contains(recCh.sends[0].Text, "STA-39") {
		t.Errorf("reply should contain identifier STA-39: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_CreateIssue_PassesAssignee(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	issueSvc.createReturn = facade.Issue{ID: uuid(0xAB), Identifier: "STA-40", Title: "加远程 agent", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "加远程 agent", "assignee": "张三"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.created) != 1 {
		t.Fatalf("expected 1 CreateIssue call, got %d", len(issueSvc.created))
	}
	if issueSvc.created[0].AssigneeIdentifier != "张三" {
		t.Fatalf("assignee = %q, want 张三", issueSvc.created[0].AssigneeIdentifier)
	}
}

// ---------------------------------------------------------------------------
// TC-intent-4: Missing param
// ---------------------------------------------------------------------------

func TestDispatchStep_CreateIssue_MissingTitle_ReturnsMissingParam(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.created) != 0 {
		t.Errorf("CreateIssue called %d times, want 0", len(issueSvc.created))
	}

	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_CreateIssue_ProjectOutsideWorkspaceRejected(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	cfg.ProjectValidator = &fakeProjectValidator{err: pgx.ErrNoRows}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{
		"title":      "登录页加载慢",
		"project_id": "11111111-1111-1111-1111-111111111111",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.created) != 0 {
		t.Fatalf("CreateIssue called %d times, want 0", len(issueSvc.created))
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_CreateIssue_ProjectIDRequiresValidator(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{
		"title":      "登录页加载慢",
		"project_id": "11111111-1111-1111-1111-111111111111",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if !strings.Contains(err.Error(), "project validator is not configured") {
		t.Fatalf("error = %q, want missing project validator", err.Error())
	}
	if len(issueSvc.created) != 0 {
		t.Fatalf("CreateIssue called %d times, want 0", len(issueSvc.created))
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

func TestDispatchStep_CreateIssue_TypedNilProjectValidatorReturnsInfrastructureError(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	var validator *inbound.DBProjectWorkspaceValidator
	cfg.ProjectValidator = validator
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{
		"title":      "登录页加载慢",
		"project_id": "11111111-1111-1111-1111-111111111111",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if !strings.Contains(err.Error(), "project validator is not configured") {
		t.Fatalf("error = %q, want missing project validator", err.Error())
	}
	if len(issueSvc.created) != 0 {
		t.Fatalf("CreateIssue called %d times, want 0", len(issueSvc.created))
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

func TestDispatchStep_CreateIssue_ProjectIDValidatedAndPassedThrough(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	validator := &fakeProjectValidator{}
	cfg.ProjectValidator = validator
	issueSvc.createReturn = facade.Issue{
		ID:         uuid(0xBC),
		Identifier: "STA-41",
		Title:      "登录页加载慢",
		Status:     "todo",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{
		"title":      "登录页加载慢",
		"project_id": "11111111-1111-1111-1111-111111111111",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.created) != 1 {
		t.Fatalf("CreateIssue called %d times, want 1", len(issueSvc.created))
	}
	if len(validator.calls) != 1 {
		t.Fatalf("ValidateProjectInWorkspace called %d times, want 1", len(validator.calls))
	}
	if got := validator.calls[0].WorkspaceID; got != uuid(0x01) {
		t.Fatalf("validated workspace ID = %v, want chat workspace id", got)
	}
	if got := validator.calls[0].ProjectID; got != uuid(0x11) {
		t.Fatalf("validated project ID = %v, want parsed project id", got)
	}
	if got := issueSvc.created[0].ProjectID; got != uuid(0x11) {
		t.Fatalf("ProjectID = %v, want parsed project id", got)
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
}

// ---------------------------------------------------------------------------
// TC-intent-4: Multi-intent IGNORED_SUFFIX
// ---------------------------------------------------------------------------

func TestDispatchStep_IgnoredSuffix_AppendsIgnoredTemplate(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.createReturn = facade.Issue{
		ID:         uuid(0xBB),
		Identifier: "STA-40",
		Title:      "登录页慢",
		Status:     "todo",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{
		"title":           "登录页慢",
		"_ignored_suffix": "@ 老王",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "IGNORED_SUFFIX") {
		t.Errorf("reply missing IGNORED_SUFFIX: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// AddComment happy path
// ---------------------------------------------------------------------------

func TestDispatchStep_AddComment_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, commentSvc, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x30), Identifier: "STA-2", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentAddComment, map[string]string{
		"issue_key": "STA-2",
		"comment":   "已找产品确认",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.gotByID) != 1 {
		t.Fatalf("expected 1 GetIssueByIdentifier, got %d", len(issueSvc.gotByID))
	}
	if issueSvc.gotByID[0].Identifier != "STA-2" {
		t.Errorf("identifier = %q, want STA-2", issueSvc.gotByID[0].Identifier)
	}

	if len(commentSvc.added) != 1 {
		t.Fatalf("expected 1 AddComment, got %d", len(commentSvc.added))
	}
	if commentSvc.added[0].Content != "已找产品确认" {
		t.Errorf("content = %q", commentSvc.added[0].Content)
	}

	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "COMMENT_ADDED") {
		t.Errorf("reply missing COMMENT_ADDED: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// AddComment missing params
// ---------------------------------------------------------------------------

func TestDispatchStep_AddComment_MissingParams(t *testing.T) {
	t.Parallel()

	cfg, _, commentSvc, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentAddComment, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(commentSvc.added) != 0 {
		t.Error("AddComment should not be called with missing params")
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// AddComment: issue not found
// ---------------------------------------------------------------------------

func TestDispatchStep_AddComment_IssueNotFound(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierErr = errors.New("not found")
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentAddComment, map[string]string{
		"issue_key": "STA-999",
		"comment":   "test",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(recCh.sends[0].Text, "ISSUE_NOT_FOUND") {
		t.Errorf("reply missing ISSUE_NOT_FOUND: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// Rec-1: AddComment with invalid identifier format
// ---------------------------------------------------------------------------

func TestDispatchStep_AddComment_InvalidIdentifier_ReturnsNotFound(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, commentSvc, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	for _, badKey := range []string{"abc-def", "!!-??", "STA-0", "STA-", "-39", "中文-哈哈"} {
		evt := makeEvt(port.IntentAddComment, map[string]string{
			"issue_key": badKey,
			"comment":   "test",
		})
		_, _, err := step.Run(context.Background(), evt)
		if err != nil {
			t.Fatalf("Run(%q): %v", badKey, err)
		}
		if len(issueSvc.gotByID) != 0 {
			t.Errorf("GetIssueByIdentifier should not be called for %q", badKey)
		}
		if len(commentSvc.added) != 0 {
			t.Errorf("AddComment should not be called for %q", badKey)
		}
		if !strings.Contains(recCh.sends[len(recCh.sends)-1].Text, "ISSUE_NOT_FOUND") {
			t.Errorf("reply missing ISSUE_NOT_FOUND for %q: %q", badKey, recCh.sends[len(recCh.sends)-1].Text)
		}
	}
}

// ---------------------------------------------------------------------------
// SetStatus happy path
// ---------------------------------------------------------------------------

func TestDispatchStep_SetStatus_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x40), Identifier: "STA-7", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetStatus, map[string]string{
		"issue_key": "STA-7",
		"status":    "done",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.setStatus) != 1 {
		t.Fatalf("expected 1 SetIssueStatus, got %d", len(issueSvc.setStatus))
	}
	call := issueSvc.setStatus[0]
	if call.Status != "done" {
		t.Errorf("status = %q, want done", call.Status)
	}
	if call.ActorID != uuid(0x02) {
		t.Error("actor ID mismatch")
	}

	if !strings.Contains(recCh.sends[0].Text, "STATUS_CHANGED") {
		t.Errorf("reply missing STATUS_CHANGED: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_SetStatus_WithProposalStore_DoesNotMutateBeforeConfirm(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	store := &fakeActionProposalStore{}
	cfg.ProposalStore = store
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		digest: facade.IssueDigest{
			Issue: facade.IssueDigestIssue{Identifier: "STA-7", Title: "t", Status: "todo", Priority: "medium"},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetStatus, map[string]string{
		"issue_key": "STA-7",
		"status":    "done",
	})
	evt.RuntimeEventID = "11111111-1111-1111-1111-111111111111"
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.setStatus) != 0 {
		t.Fatalf("SetIssueStatus called %d times before confirm, want 0", len(issueSvc.setStatus))
	}
	if len(store.created) != 1 {
		t.Fatalf("CreateActionProposal called %d times, want 1", len(store.created))
	}
	if !strings.Contains(recCh.sends[0].Text, "ACTION_PROPOSED") || !strings.Contains(recCh.sends[0].Text, "/confirm ABCD1234") {
		t.Fatalf("proposal reply = %q", recCh.sends[0].Text)
	}
	if !strings.Contains(recCh.sends[0].Text, "状态从 todo 改为 done") {
		t.Fatalf("proposal reply should show current -> target: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_ConfirmProposal_ExecutesOriginalMutationOnce(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x40), Identifier: "STA-7", Title: "t", Status: "todo"}
	store := &fakeActionProposalStore{
		found: inbound.ActionProposal{
			ID:             uuid(0x91),
			Code:           "ABCD1234",
			ConnectionID:   "feishu",
			ChatID:         "chat-1",
			SenderID:       "ou_sender1",
			WorkspaceID:    uuid(0x01),
			InboundEventID: uuid(0x92),
			Intent: port.InboundIntent{
				Kind:   port.IntentSetStatus,
				Params: map[string]string{"issue_key": "STA-7", "status": "done"},
				Source: port.SourceRule,
			},
			Status:    "pending",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		},
	}
	cfg.ProposalStore = store
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentConfirmAction, map[string]string{"code": "ABCD1234"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.setStatus) != 1 {
		t.Fatalf("SetIssueStatus called %d times, want 1", len(issueSvc.setStatus))
	}
	if got := issueSvc.setStatus[0].Status; got != "done" {
		t.Fatalf("status = %q, want done", got)
	}
	if len(store.marked) != 1 || store.marked[0] != "confirmed" {
		t.Fatalf("marked statuses = %#v, want confirmed", store.marked)
	}
	if !strings.Contains(recCh.sends[0].Text, "ACTION_CONFIRMED") {
		t.Fatalf("confirm reply = %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_CancelProposal_DoesNotMutate(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	store := &fakeActionProposalStore{
		found: inbound.ActionProposal{
			ID:             uuid(0x91),
			Code:           "ABCD1234",
			ConnectionID:   "feishu",
			ChatID:         "chat-1",
			SenderID:       "ou_sender1",
			WorkspaceID:    uuid(0x01),
			InboundEventID: uuid(0x92),
			Intent: port.InboundIntent{
				Kind:   port.IntentSetStatus,
				Params: map[string]string{"issue_key": "STA-7", "status": "done"},
				Source: port.SourceRule,
			},
			Status:    "pending",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		},
	}
	cfg.ProposalStore = store
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCancelAction, map[string]string{"code": "ABCD1234"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.setStatus) != 0 {
		t.Fatalf("SetIssueStatus called %d times, want 0", len(issueSvc.setStatus))
	}
	if len(store.marked) != 1 || store.marked[0] != "cancelled" {
		t.Fatalf("marked statuses = %#v, want cancelled", store.marked)
	}
	if !strings.Contains(recCh.sends[0].Text, "ACTION_CANCELLED") {
		t.Fatalf("cancel reply = %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// SetStatus missing params
// ---------------------------------------------------------------------------

func TestDispatchStep_SetStatus_MissingParams(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetStatus, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.setStatus) != 0 {
		t.Error("SetIssueStatus should not be called")
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// SetAssignee happy path
// ---------------------------------------------------------------------------

func TestDispatchStep_SetAssignee_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x40), Identifier: "STA-7", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetAssignee, map[string]string{
		"issue_key": "STA-7",
		"assignee":  "@张三",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.setAssignee) != 1 {
		t.Fatalf("expected 1 SetIssueAssignee, got %d", len(issueSvc.setAssignee))
	}
	call := issueSvc.setAssignee[0]
	if call.AssigneeIdentifier != "@张三" {
		t.Errorf("assignee = %q, want @张三", call.AssigneeIdentifier)
	}
	if call.ActorID != uuid(0x02) {
		t.Error("actor ID mismatch")
	}

	if !strings.Contains(recCh.sends[0].Text, "ASSIGNEE_CHANGED") {
		t.Errorf("reply missing ASSIGNEE_CHANGED: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_SetAssignee_MissingParams(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetAssignee, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.setAssignee) != 0 {
		t.Error("SetIssueAssignee should not be called")
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// SetPriority happy path
// ---------------------------------------------------------------------------

func TestDispatchStep_SetPriority_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x41), Identifier: "STA-8", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetPriority, map[string]string{
		"issue_key": "STA-8",
		"priority":  "high",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.setPriority) != 1 {
		t.Fatalf("expected 1 SetIssuePriority, got %d", len(issueSvc.setPriority))
	}
	call := issueSvc.setPriority[0]
	if call.Priority != "high" {
		t.Errorf("priority = %q, want high", call.Priority)
	}
	if call.ActorID != uuid(0x02) {
		t.Error("actor ID mismatch")
	}

	if !strings.Contains(recCh.sends[0].Text, "PRIORITY_CHANGED") {
		t.Errorf("reply missing PRIORITY_CHANGED: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_SetPriority_MissingParams(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetPriority, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.setPriority) != 0 {
		t.Error("SetIssuePriority should not be called")
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// SetLabel happy path (add)
// ---------------------------------------------------------------------------

func TestDispatchStep_SetLabel_Add_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x42), Identifier: "STA-9", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetLabel, map[string]string{
		"issue_key": "STA-9",
		"label":     "bug",
		"op":        "add",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.addLabel) != 1 {
		t.Fatalf("expected 1 AddIssueLabel, got %d", len(issueSvc.addLabel))
	}
	call := issueSvc.addLabel[0]
	if call.LabelName != "bug" {
		t.Errorf("label = %q, want bug", call.LabelName)
	}
	if call.ActorID != uuid(0x02) {
		t.Error("actor ID mismatch")
	}

	if !strings.Contains(recCh.sends[0].Text, "LABEL_ADDED") {
		t.Errorf("reply missing LABEL_ADDED: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// SetLabel happy path (remove)
// ---------------------------------------------------------------------------

func TestDispatchStep_SetLabel_Remove_HappyPath(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x43), Identifier: "STA-10", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetLabel, map[string]string{
		"issue_key": "STA-10",
		"label":     "bug",
		"op":        "remove",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(issueSvc.removeLabel) != 1 {
		t.Fatalf("expected 1 RemoveIssueLabel, got %d", len(issueSvc.removeLabel))
	}
	call := issueSvc.removeLabel[0]
	if call.LabelName != "bug" {
		t.Errorf("label = %q, want bug", call.LabelName)
	}
	if call.ActorID != uuid(0x02) {
		t.Error("actor ID mismatch")
	}

	if !strings.Contains(recCh.sends[0].Text, "LABEL_REMOVED") {
		t.Errorf("reply missing LABEL_REMOVED: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_SetLabel_MissingParams(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetLabel, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.addLabel) != 0 {
		t.Error("AddIssueLabel should not be called")
	}
	if !strings.Contains(recCh.sends[0].Text, "MISSING_PARAM") {
		t.Errorf("reply missing MISSING_PARAM: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// Business error propagation: facade returns error → INTERNAL_ERROR reply
// ---------------------------------------------------------------------------

func TestDispatchStep_SetAssignee_FacadeError_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x40), Identifier: "STA-7", Title: "t", Status: "todo"}
	issueSvc.setAssigneeErr = errors.New("用户 @张三 不在此 workspace")
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetAssignee, map[string]string{
		"issue_key": "STA-7",
		"assignee":  "@张三",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

func TestDispatchStep_SetPriority_FacadeError_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x41), Identifier: "STA-8", Title: "t", Status: "todo"}
	issueSvc.setPriorityErr = errors.New("优先级仅支持 urgent/high/medium/low/none")
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetPriority, map[string]string{
		"issue_key": "STA-8",
		"priority":  "invalid",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

func TestDispatchStep_SetLabel_FacadeError_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x42), Identifier: "STA-9", Title: "t", Status: "todo"}
	issueSvc.addLabelErr = errors.New("标签 bug 不存在，请先在 Web 端创建")
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetLabel, map[string]string{
		"issue_key": "STA-9",
		"label":     "bug",
		"op":        "add",
	})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

// ---------------------------------------------------------------------------
// QueryIssue — specific issue
// ---------------------------------------------------------------------------

func TestDispatchStep_QueryIssue_SpecificIssue(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:         uuid(0x50),
		Identifier: "STA-2",
		Title:      "登录页加载慢",
		Status:     "in_progress",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	text := recCh.sends[0].Text
	if !strings.Contains(text, "in_progress") {
		t.Errorf("reply should contain status: %q", text)
	}
	if !strings.Contains(text, "登录页加载慢") {
		t.Errorf("reply should contain title: %q", text)
	}
	if !strings.Contains(text, "测试用户") {
		t.Errorf("reply should contain display name: %q", text)
	}
}

func TestDispatchStep_QueryIssue_WithDigestFacade_ReturnsWorkSummaryCard(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		digest: facade.IssueDigest{
			Issue: facade.IssueDigestIssue{
				ID:         uuid(0x50),
				Identifier: "STA-2",
				Title:      "登录页加载慢",
				Status:     "in_review",
				Priority:   "high",
				UpdatedAt:  time.Date(2026, 5, 12, 17, 19, 0, 0, time.UTC),
			},
			ProjectName:  "Station",
			AssigneeName: "张三",
			RecentEvents: []facade.IssueDigestEvent{
				{Kind: "comment", ActorName: "李四", Summary: "已补充复现步骤"},
				{Kind: "activity", ActorName: "张三", Summary: "status_changed"},
			},
			AgentSummary: &facade.IssueAgentSummary{
				AgentName:     "Clawdbot",
				Status:        "completed",
				ResultSummary: "PR 已创建",
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recCh.cards) != 1 {
		t.Fatalf("expected 1 rich card, got %d", len(recCh.cards))
	}
	body := recCh.cards[0].Body
	for _, want := range []string{"STA-2", "登录页加载慢", "最近动态", "Agent", "下一步", "reviewer"} {
		if !strings.Contains(body, want) {
			t.Fatalf("digest body missing %q: %q", want, body)
		}
	}
}

func TestDispatchStep_QueryIssue_LocalAppURLDoesNotRenderActions(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "http://localhost:3000")

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		digest: facade.IssueDigest{
			Issue: facade.IssueDigestIssue{
				ID:         uuid(0x50),
				Identifier: "STA-2",
				Title:      "登录页加载慢",
				Status:     "in_progress",
				Priority:   "high",
			},
			WorkspaceSlug: "test-ws",
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{"issue_key": "STA-2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recCh.cards) != 1 {
		t.Fatalf("expected 1 rich card, got %d", len(recCh.cards))
	}
	if len(recCh.cards[0].Actions) != 0 {
		t.Fatalf("local app url should not render actions: %#v", recCh.cards[0].Actions)
	}
	if !strings.Contains(recCh.cards[0].Body, "/detail STA-2") {
		t.Fatalf("body should include chat-native expansion command: %q", recCh.cards[0].Body)
	}
}

func TestDispatchStep_QueryProgress_InReviewIncludesLatestReplyFullText(t *testing.T) {
	t.Parallel()

	fullReply := "方案如下：\n1. 新建远程 Agent\n2. 绑定 Runtime\n3. 在 Issue 里 @Agent 触发执行\n\n这段内容不应该被摘要替换。"
	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		progress: facade.IssueProgress{
			Digest: facade.IssueDigest{
				Issue: facade.IssueDigestIssue{
					ID:         uuid(0x60),
					Identifier: "STA-60",
					Title:      "远程 agent 接入",
					Status:     "in_review",
				},
				AssigneeName: "张三",
			},
			LatestReply: &facade.IssueProgressReply{
				AuthorType: "agent",
				AuthorName: "Orion",
				Content:    fullReply,
				CreatedAt:  time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC),
			},
			RecommendedNext: "看最新回复并决定通过。",
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryProgress, map[string]string{"scope": "issue", "issue_key": "STA-60"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recCh.cards) != 1 {
		t.Fatalf("expected 1 rich card, got %d", len(recCh.cards))
	}
	body := recCh.cards[0].Body
	for _, want := range []string{"STA-60", "in_review", "最新回复", "Orion", fullReply, "下一步"} {
		if !strings.Contains(body, want) {
			t.Fatalf("progress body missing %q: %q", want, body)
		}
	}
}

func TestDispatchStep_QueryProgress_Projects(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		projects: []facade.ProjectProgress{
			{
				ProjectName: "Station",
				Total:       8,
				Open:        3,
				InProgress:  1,
				InReview:    1,
				Blocked:     1,
				Done:        5,
				FocusIssues: []facade.ProjectProgressIssue{
					{Identifier: "STA-9", Title: "登录态刷新", Status: "blocked", Assignee: "张三"},
				},
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryProgress, map[string]string{"scope": "projects"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	body := recCh.cards[0].Body
	for _, want := range []string{"项目进展", "Station", "开放 3", "Review 1", "阻塞 1", "STA-9"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project progress body missing %q: %q", want, body)
		}
	}
}

func TestDispatchStep_IssueDetail_ReturnsChatNativeBody(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		detail: facade.IssueDetail{
			Digest: facade.IssueDigest{
				Issue: facade.IssueDigestIssue{
					ID:          uuid(0x51),
					Identifier:  "STA-3",
					Title:       "远程 agent 接入",
					Description: "补充远程 agent 的设计和实现路径",
					Status:      "todo",
					Priority:    "medium",
				},
				AssigneeName: "张三",
				Labels:       []string{"agent", "backend"},
			},
			StatusHistory: []facade.IssueDigestEvent{{ActorName: "李四", Summary: "状态变更：backlog -> todo"}},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentIssueDetail, map[string]string{"issue_key": "STA-3"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(recCh.cards) != 1 {
		t.Fatalf("expected 1 rich card, got %d", len(recCh.cards))
	}
	body := recCh.cards[0].Body
	for _, want := range []string{"STA-3 详情", "描述", "补充远程 agent", "标签", "下一步", "/timeline STA-3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail body missing %q: %q", want, body)
		}
	}
}

func TestDispatchStep_IssueTimeline_ReturnsChatNativeBody(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		timeline: facade.IssueTimelinePage{
			Issue:    facade.IssueDigestIssue{Identifier: "STA-4", Title: "timeline test", Status: "in_progress"},
			Page:     2,
			PageSize: 5,
			HasMore:  true,
			Events: []facade.IssueDigestEvent{
				{ActorName: "张三", Summary: "评论：已更新 PR"},
				{ActorName: "系统", Summary: "状态变更：todo -> in_progress"},
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentIssueTimeline, map[string]string{"issue_key": "STA-4", "page": "2"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	body := recCh.cards[0].Body
	for _, want := range []string{"STA-4 动态 第 2 页", "已更新 PR", "状态变更", "/timeline STA-4 3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("timeline body missing %q: %q", want, body)
		}
	}
}

func TestDispatchStep_IssueLogs_ReturnsChatNativeBody(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		logs: facade.IssueLogPage{
			Issue:      facade.IssueDigestIssue{Identifier: "STA-5", Title: "logs test", Status: "in_progress"},
			TaskID:     "task-1",
			AgentName:  "Clawdbot",
			TaskStatus: "running",
			Page:       1,
			PageSize:   8,
			HasMore:    true,
			Messages: []facade.IssueTaskLogEvent{
				{Seq: 12, Type: "progress", Content: "正在分析失败测试"},
				{Seq: 11, Type: "tool", Tool: "go test", Content: "1 failing package"},
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentIssueLogs, map[string]string{"issue_key": "STA-5"})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	body := recCh.cards[0].Body
	for _, want := range []string{"STA-5 执行日志", "Clawdbot", "正在分析失败测试", "go test", "/logs STA-5 2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("logs body missing %q: %q", want, body)
		}
	}
}

// ---------------------------------------------------------------------------
// QueryIssue — "我的待办"
// ---------------------------------------------------------------------------

func TestDispatchStep_QueryIssue_MyTodos(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.listTodosReturn = []facade.Issue{
		{ID: uuid(0x60), Identifier: "STA-10", Title: "Issue A", Status: "todo"},
		{ID: uuid(0x61), Identifier: "STA-11", Title: "Issue B", Status: "in_progress"},
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}
	text := recCh.sends[0].Text
	if !strings.Contains(text, "待办") {
		t.Errorf("reply should mention 待办: %q", text)
	}
}

// ---------------------------------------------------------------------------
// QueryIssue — empty todos
// ---------------------------------------------------------------------------

func TestDispatchStep_QueryIssue_NoTodos(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(recCh.sends[0].Text, "没有待办") {
		t.Errorf("reply should say no todos: %q", recCh.sends[0].Text)
	}
}

func TestDispatchStep_DirectUnknown_UsesReplyContextAsComment(t *testing.T) {
	t.Parallel()

	cfg, _, commentSvc, recCh := buildDispatchConfig()
	cfg.ReplyContext = &fakeReplyContextStore{
		ok: true,
		ctx: inboundReplyContext{
			WorkspaceID:     uuid(0x01),
			IssueID:         uuid(0x71),
			IssueIdentifier: "STA-71",
			IssueTitle:      "远程 agent",
			ExpiresAt:       time.Now().Add(time.Hour),
		},
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentUnknown, map[string]string{})
	evt.ChatType = port.ChatTypeDirect
	evt.Text = "我看过了，按方案 2 做"
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(commentSvc.added) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(commentSvc.added))
	}
	if commentSvc.added[0].IssueID != uuid(0x71) || commentSvc.added[0].Content != evt.Text {
		t.Fatalf("comment = %#v", commentSvc.added[0])
	}
	if len(recCh.sends) != 1 || !strings.Contains(recCh.sends[0].Text, "STA-71") {
		t.Fatalf("reply should mention target issue, got %#v", recCh.sends)
	}
}

func TestDispatchStep_DirectMutation_UsesReplyContextIssue(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	cfg.ChatBinding = &fakeChatBinding{err: errors.New("direct chat has no workspace binding")}
	cfg.ReplyContext = &fakeReplyContextStore{
		ok: true,
		ctx: inboundReplyContext{
			WorkspaceID:     uuid(0x01),
			IssueID:         uuid(0x72),
			IssueIdentifier: "STA-72",
			IssueTitle:      "远程 agent",
			ExpiresAt:       time.Now().Add(time.Hour),
		},
	}
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x72), Identifier: "STA-72", Title: "远程 agent", Status: "in_review"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetStatus, map[string]string{"status": "done"})
	evt.ChatType = port.ChatTypeDirect
	evt.Text = "改成 done"
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.gotByID) != 1 || issueSvc.gotByID[0].Identifier != "STA-72" {
		t.Fatalf("gotByID = %#v", issueSvc.gotByID)
	}
	if len(issueSvc.setStatus) != 1 || issueSvc.setStatus[0].Status != "done" {
		t.Fatalf("setStatus = %#v", issueSvc.setStatus)
	}
	if len(recCh.sends) != 1 || !strings.Contains(recCh.sends[0].Text, "STATUS_CHANGED") {
		t.Fatalf("reply = %#v", recCh.sends)
	}
}

// ---------------------------------------------------------------------------
// Unknown intent
// ---------------------------------------------------------------------------

func TestDispatchStep_UnknownIntent_ReturnsUnknownTemplate(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentUnknown, map[string]string{})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(recCh.sends[0].Text, "UNKNOWN") {
		t.Errorf("reply should not expose UNKNOWN tag: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// ASK_CLARIFY intent
// ---------------------------------------------------------------------------

func TestDispatchStep_ASKClarify_ReturnsAskClarifyTemplate(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentASKClarify, map[string]string{})
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(recCh.sends[0].Text, "ASK_CLARIFY") {
		t.Errorf("reply should not expose ASK_CLARIFY tag: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// Facade error → INTERNAL_ERROR
// ---------------------------------------------------------------------------

func TestDispatchStep_CreateIssue_FacadeError_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	issueSvc.createErr = errors.New("db down")
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "test"})
	_, d, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

// ---------------------------------------------------------------------------
// Send failure does not abort pipeline
// ---------------------------------------------------------------------------

func TestDispatchStep_SendFailure_DoesNotAbortPipeline(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	recCh.sendErr = errors.New("network timeout")
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentUnknown, map[string]string{})
	_, d, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return send error")
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
}

// ---------------------------------------------------------------------------
// Channel not in registry — does not abort
// ---------------------------------------------------------------------------

func TestDispatchStep_ChannelNotInRegistry_DoesNotAbort(t *testing.T) {
	t.Parallel()
	gw := gateway.NewRegistryGateway(channel.NewRegistry())

	cfg := inbound.DispatchConfig{
		IssueFacade:   facade.NewIssueFacade(&fakeIssueService{}),
		CommentFacade: facade.NewCommentFacade(&fakeCommentService{}),
		ReplySink:     inbound.NewGatewayReplySink(gw),
		ChatBinding:   &fakeChatBinding{wsID: uuid(0x01)},
		UserResolver:  &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: uuid(0x02)}},
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentUnknown, map[string]string{})
	_, d, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return missing channel error")
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
}

// ---------------------------------------------------------------------------
// ChatBindingLookup error → INTERNAL_ERROR
// ---------------------------------------------------------------------------

func TestDispatchStep_ChatBindingError_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.ChatBinding = &fakeChatBinding{err: errors.New("db error")}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "test"})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

// ---------------------------------------------------------------------------
// UserResolver error → INTERNAL_ERROR
// ---------------------------------------------------------------------------

func TestDispatchStep_UserResolverError_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cfg, _, _, recCh := buildDispatchConfig()
	cfg.UserResolver = &fakeUserResolver{err: errors.New("binding not found")}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "test"})
	_, _, err := step.Run(context.Background(), evt)
	if err == nil {
		t.Fatal("Run should return infrastructure error")
	}
	if len(recCh.sends) != 0 {
		t.Fatalf("expected no send on retryable error, got %d", len(recCh.sends))
	}
}

// ---------------------------------------------------------------------------
// Pipeline integration
// ---------------------------------------------------------------------------

func TestDispatchStep_InPipeline_DispatchIsTerminalStep(t *testing.T) {
	t.Parallel()

	store := &fakeDedupStore{responses: []dedupResp{{Inserted: true}}}
	cfg, _, _, recCh := buildDispatchConfig()

	p := inbound.NewPipeline(
		inbound.NewNormalizeStep(),
		inbound.NewDedupStep(store),
		inbound.NewDispatchStep(cfg),
	)
	out, err := p.Run(context.Background(), port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-pipeline",
		ChatID:      "chat-1",
		SenderID:    "sender-1",
		Type:        port.EventTypeMessageReceived,
		Text:        "hello",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Terminal != "dispatch" {
		t.Errorf("Terminal = %q, want dispatch", out.Terminal)
	}
	if out.Decision != inbound.DecisionContinue {
		t.Errorf("Decision = %v, want Continue", out.Decision)
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send from dispatch, got %d", len(recCh.sends))
	}
	if strings.Contains(recCh.sends[0].Text, "UNKNOWN") {
		t.Errorf("pipeline reply should not expose UNKNOWN tag: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// Rec-2: validIdentifierFormat unit tests
// ---------------------------------------------------------------------------

func TestValidIdentifierFormat(t *testing.T) {
	t.Parallel()

	valid := []string{"STA-2", "STA-39", "MUL-123", "ABCDE-1", "AB-999999"}
	for _, s := range valid {
		if !inbound.ValidIdentifierFormat(s) {
			t.Errorf("ValidIdentifierFormat(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"abc-def",  // lowercase
		"!!-??",    // special chars
		"STA-0",    // leading zero
		"STA-",     // missing number
		"-39",      // missing prefix
		"中文-哈哈",    // non-ASCII
		"A-1",      // prefix too short
		"ABCDEF-1", // prefix too long
		"",         // empty
		"STA",      // no hyphen
	}
	for _, s := range invalid {
		if inbound.ValidIdentifierFormat(s) {
			t.Errorf("ValidIdentifierFormat(%q) = true, want false", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Name()
// ---------------------------------------------------------------------------

func TestDispatchStep_Name(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)
	if got := step.Name(); got != "dispatch" {
		t.Errorf("Name = %q, want %q", got, "dispatch")
	}
}

// ---------------------------------------------------------------------------
// Reply context write path
// ---------------------------------------------------------------------------

func TestDispatchStep_CreateIssue_SavesReplyContextInDirectChat(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, recCh := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.createReturn = facade.Issue{
		ID:          uuid(0xAA),
		WorkspaceID: uuid(0x01),
		Identifier:  "STA-39",
		Title:       "登录页加载慢",
		Status:      "todo",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "登录页加载慢"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(recCh.sends))
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-39" {
		t.Errorf("IssueIdentifier = %q, want STA-39", got.IssueIdentifier)
	}
	if got.IssueTitle != "登录页加载慢" {
		t.Errorf("IssueTitle = %q, want 登录页加载慢", got.IssueTitle)
	}
}

func TestDispatchStep_CreateIssue_DoesNotSaveReplyContextInGroupChat(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.createReturn = facade.Issue{
		ID:         uuid(0xAA),
		Identifier: "STA-39",
		Title:      "登录页加载慢",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "登录页加载慢"})
	evt.ChatType = port.ChatTypeGroup
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	_, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Error("expected no reply context in group chat")
	}
}

func TestDispatchStep_AddComment_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x30), Identifier: "STA-2", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentAddComment, map[string]string{
		"issue_key": "STA-2",
		"comment":   "已找产品确认",
	})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-2" {
		t.Errorf("IssueIdentifier = %q, want STA-2", got.IssueIdentifier)
	}
}

func TestDispatchStep_SetStatus_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x40), Identifier: "STA-7", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetStatus, map[string]string{
		"issue_key": "STA-7",
		"status":    "done",
	})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-7" {
		t.Errorf("IssueIdentifier = %q, want STA-7", got.IssueIdentifier)
	}
}

func TestDispatchStep_SetAssignee_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x40), Identifier: "STA-7", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetAssignee, map[string]string{
		"issue_key": "STA-7",
		"assignee":  "@张三",
	})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-7" {
		t.Errorf("IssueIdentifier = %q, want STA-7", got.IssueIdentifier)
	}
}

func TestDispatchStep_SetPriority_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x41), Identifier: "STA-8", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetPriority, map[string]string{
		"issue_key": "STA-8",
		"priority":  "high",
	})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-8" {
		t.Errorf("IssueIdentifier = %q, want STA-8", got.IssueIdentifier)
	}
}

func TestDispatchStep_SetLabel_Add_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x42), Identifier: "STA-9", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetLabel, map[string]string{
		"issue_key": "STA-9",
		"label":     "bug",
		"op":        "add",
	})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-9" {
		t.Errorf("IssueIdentifier = %q, want STA-9", got.IssueIdentifier)
	}
}

func TestDispatchStep_SetLabel_Remove_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{ID: uuid(0x43), Identifier: "STA-10", Title: "t", Status: "todo"}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentSetLabel, map[string]string{
		"issue_key": "STA-10",
		"label":     "bug",
		"op":        "remove",
	})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-10" {
		t.Errorf("IssueIdentifier = %q, want STA-10", got.IssueIdentifier)
	}
}

func TestDispatchStep_QueryIssue_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:         uuid(0x50),
		Identifier: "STA-2",
		Title:      "登录页加载慢",
		Status:     "in_progress",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{"issue_key": "STA-2"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-2" {
		t.Errorf("IssueIdentifier = %q, want STA-2", got.IssueIdentifier)
	}
}

func TestDispatchStep_QueryIssue_WithDigestFacade_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		digest: facade.IssueDigest{
			Issue: facade.IssueDigestIssue{
				ID:         uuid(0x50),
				Identifier: "STA-2",
				Title:      "登录页加载慢",
				Status:     "in_review",
				Priority:   "high",
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryIssue, map[string]string{"issue_key": "STA-2"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-2" {
		t.Errorf("IssueIdentifier = %q, want STA-2", got.IssueIdentifier)
	}
}

func TestDispatchStep_QueryProgress_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		progress: facade.IssueProgress{
			Digest: facade.IssueDigest{
				Issue: facade.IssueDigestIssue{
					ID:         uuid(0x60),
					Identifier: "STA-60",
					Title:      "远程 agent 接入",
					Status:     "in_review",
				},
			},
			LatestReply: &facade.IssueProgressReply{
				AuthorType: "agent",
				AuthorName: "Orion",
				Content:    "方案如下",
				CreatedAt:  time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC),
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentQueryProgress, map[string]string{"scope": "issue", "issue_key": "STA-60"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-60" {
		t.Errorf("IssueIdentifier = %q, want STA-60", got.IssueIdentifier)
	}
}

func TestDispatchStep_IssueDetail_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		detail: facade.IssueDetail{
			Digest: facade.IssueDigest{
				Issue: facade.IssueDigestIssue{
					ID:         uuid(0x51),
					Identifier: "STA-3",
					Title:      "远程 agent 接入",
					Status:     "todo",
					Priority:   "medium",
				},
			},
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentIssueDetail, map[string]string{"issue_key": "STA-3"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-3" {
		t.Errorf("IssueIdentifier = %q, want STA-3", got.IssueIdentifier)
	}
}

func TestDispatchStep_IssueTimeline_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		timeline: facade.IssueTimelinePage{
			Issue: facade.IssueDigestIssue{ID: uuid(0x54), Identifier: "STA-4", Title: "timeline test", Status: "in_progress"},
			Page:  1, PageSize: 5, HasMore: false,
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentIssueTimeline, map[string]string{"issue_key": "STA-4"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-4" {
		t.Errorf("IssueIdentifier = %q, want STA-4", got.IssueIdentifier)
	}
}

func TestDispatchStep_IssueLogs_SavesReplyContext(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	cfg.IssueDigestFacade = facade.NewIssueDigestFacade(&fakeIssueDigestService{
		logs: facade.IssueLogPage{
			Issue: facade.IssueDigestIssue{ID: uuid(0x55), Identifier: "STA-5", Title: "logs test", Status: "in_progress"},
			Page:  1, PageSize: 8, HasMore: false,
		},
	})
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentIssueLogs, map[string]string{"issue_key": "STA-5"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected reply context to be saved")
	}
	if got.IssueIdentifier != "STA-5" {
		t.Errorf("IssueIdentifier = %q, want STA-5", got.IssueIdentifier)
	}
}

func TestDispatchStep_UnknownIntent_DoesNotSaveReplyContext(t *testing.T) {
	t.Parallel()

	cfg, _, _, _ := buildDispatchConfig()
	store := replyctx.NewInMemoryStore()
	cfg.ReplyContext = store
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentUnknown, map[string]string{})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	_, ok, err := store.Lookup(context.Background(), "feishu", "ou_sender1", "chat-1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Error("expected no reply context for unknown intent")
	}
}

func TestDispatchStep_CreateIssue_NilReplyContext_DoesNotPanic(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	cfg.ReplyContext = nil
	issueSvc.createReturn = facade.Issue{
		ID:         uuid(0xAA),
		Identifier: "STA-39",
		Title:      "登录页加载慢",
		Status:     "todo",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := makeEvt(port.IntentCreateIssue, map[string]string{"title": "登录页加载慢"})
	evt.ChatType = port.ChatTypeDirect
	_, _, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}
