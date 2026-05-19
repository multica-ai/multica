package facade_test

// Red-phase tests for the channel facade.
//
// These tests intentionally reference symbols that do not yet exist in the
// `facade` package. They will fail to compile until the Green phase wires up
// the production code. The shape of the tests pins the public contract from
// DESIGN §4.2 (IssueFacade / CommentFacade) and TestCase §2 (TC-facade-1 /
// TC-facade-2).

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/facade"
)

// ---------------------------------------------------------------------------
// Test fixtures (mocks live in _test.go so they cannot leak into the binary)
// ---------------------------------------------------------------------------

// mockIssueService records the arguments a facade pass through and returns
// caller-controlled fixtures so we can assert pass-through semantics without
// touching the real service implementation.
type mockIssueService struct {
	createCalls          []facade.CreateIssueReq
	getCalls             []pgtype.UUID
	getByIdentifierCalls []struct {
		WorkspaceID pgtype.UUID
		Identifier  string
	}
	setStatusCalls []struct {
		ID      pgtype.UUID
		ActorID pgtype.UUID
		Status  string
	}
	setAssigneeCalls []struct {
		ID                 pgtype.UUID
		ActorID            pgtype.UUID
		AssigneeIdentifier string
	}
	setPriorityCalls []struct {
		ID       pgtype.UUID
		ActorID  pgtype.UUID
		Priority string
	}
	addLabelCalls []struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}
	removeLabelCalls []struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}
	listMyTodosCalls []struct {
		WorkspaceID pgtype.UUID
		UserID      pgtype.UUID
	}

	createReturn          facade.Issue
	createErr             error
	getReturn             facade.Issue
	getErr                error
	getByIdentifierReturn facade.Issue
	getByIdentifierErr    error
	setStatusErr          error
	setAssigneeErr        error
	setPriorityErr        error
	addLabelErr           error
	removeLabelErr        error
	listMyTodosReturn     []facade.Issue
	listMyTodosErr        error
}

func (m *mockIssueService) CreateIssue(_ context.Context, req facade.CreateIssueReq) (facade.Issue, error) {
	m.createCalls = append(m.createCalls, req)
	return m.createReturn, m.createErr
}

func (m *mockIssueService) GetIssue(_ context.Context, id pgtype.UUID) (facade.Issue, error) {
	m.getCalls = append(m.getCalls, id)
	return m.getReturn, m.getErr
}

func (m *mockIssueService) GetIssueByIdentifier(_ context.Context, workspaceID pgtype.UUID, identifier string) (facade.Issue, error) {
	m.getByIdentifierCalls = append(m.getByIdentifierCalls, struct {
		WorkspaceID pgtype.UUID
		Identifier  string
	}{workspaceID, identifier})
	return m.getByIdentifierReturn, m.getByIdentifierErr
}

func (m *mockIssueService) SetIssueStatus(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, status string, _ facade.ChannelMutationContext) error {
	m.setStatusCalls = append(m.setStatusCalls, struct {
		ID      pgtype.UUID
		ActorID pgtype.UUID
		Status  string
	}{id, actorID, status})
	return m.setStatusErr
}

func (m *mockIssueService) SetIssueAssignee(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, assigneeIdentifier string, _ facade.ChannelMutationContext) error {
	m.setAssigneeCalls = append(m.setAssigneeCalls, struct {
		ID                 pgtype.UUID
		ActorID            pgtype.UUID
		AssigneeIdentifier string
	}{id, actorID, assigneeIdentifier})
	return m.setAssigneeErr
}

func (m *mockIssueService) SetIssuePriority(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, priority string, _ facade.ChannelMutationContext) error {
	m.setPriorityCalls = append(m.setPriorityCalls, struct {
		ID       pgtype.UUID
		ActorID  pgtype.UUID
		Priority string
	}{id, actorID, priority})
	return m.setPriorityErr
}

func (m *mockIssueService) AddIssueLabel(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, _ facade.ChannelMutationContext) error {
	m.addLabelCalls = append(m.addLabelCalls, struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}{id, actorID, labelName})
	return m.addLabelErr
}

func (m *mockIssueService) RemoveIssueLabel(_ context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, _ facade.ChannelMutationContext) error {
	m.removeLabelCalls = append(m.removeLabelCalls, struct {
		ID        pgtype.UUID
		ActorID   pgtype.UUID
		LabelName string
	}{id, actorID, labelName})
	return m.removeLabelErr
}

func (m *mockIssueService) ListMyTodos(_ context.Context, workspaceID, userID pgtype.UUID) ([]facade.Issue, error) {
	m.listMyTodosCalls = append(m.listMyTodosCalls, struct {
		WorkspaceID pgtype.UUID
		UserID      pgtype.UUID
	}{workspaceID, userID})
	return m.listMyTodosReturn, m.listMyTodosErr
}

type mockCommentService struct {
	addCalls  []facade.AddCommentReq
	addReturn facade.Comment
	addErr    error
}

func (m *mockCommentService) AddComment(_ context.Context, req facade.AddCommentReq) (facade.Comment, error) {
	m.addCalls = append(m.addCalls, req)
	return m.addReturn, m.addErr
}

// uuid is a small helper that returns a deterministic pgtype.UUID for the
// given byte tag — keeps the test source compact without pulling a UUID
// generator into the facade package's test deps.
func uuid(tag byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = tag
	}
	u.Valid = true
	return u
}

// ---------------------------------------------------------------------------
// TC-facade-1: IssueFacade calls the service, passes actorID through, and
// does not cleanse / mutate inputs on the way down. Mirrors TestCase §2
// "调用透传 actorID（来自 channel_user_binding.user_id）；mock service 收到的
// req.WorkspaceID 与 actorID 与入参一致".
// ---------------------------------------------------------------------------

func TestIssueFacade_CreateIssue_PassesActorIDThrough(t *testing.T) {
	t.Parallel()

	wsID := uuid(0x01)
	actorID := uuid(0x02)
	wantIssue := facade.Issue{
		ID:          uuid(0x03),
		WorkspaceID: wsID,
		Title:       "from facade",
		Status:      "todo",
	}

	svc := &mockIssueService{createReturn: wantIssue}
	f := facade.NewIssueFacade(svc)

	got, err := f.CreateIssue(context.Background(), facade.CreateIssueReq{
		WorkspaceID: wsID,
		ActorID:     actorID,
		Title:       "from facade",
		Description: "desc",
	})
	if err != nil {
		t.Fatalf("CreateIssue: unexpected error: %v", err)
	}
	if got.ID != wantIssue.ID {
		t.Fatalf("CreateIssue returned ID = %v, want %v", got.ID, wantIssue.ID)
	}

	if len(svc.createCalls) != 1 {
		t.Fatalf("service CreateIssue called %d times, want 1", len(svc.createCalls))
	}
	call := svc.createCalls[0]
	if call.WorkspaceID != wsID {
		t.Errorf("service received WorkspaceID = %v, want %v", call.WorkspaceID, wsID)
	}
	if call.ActorID != actorID {
		t.Errorf("service received ActorID = %v, want %v", call.ActorID, actorID)
	}
	if call.Title != "from facade" || call.Description != "desc" {
		t.Errorf("service received req = %+v, want title/desc preserved", call)
	}
}

func TestIssueFacade_GetIssue_PassesIDThrough(t *testing.T) {
	t.Parallel()

	id := uuid(0x10)
	want := facade.Issue{ID: id, Title: "x"}
	svc := &mockIssueService{getReturn: want}
	f := facade.NewIssueFacade(svc)

	got, err := f.GetIssue(context.Background(), id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.ID != id {
		t.Fatalf("got.ID = %v, want %v", got.ID, id)
	}
	if len(svc.getCalls) != 1 || svc.getCalls[0] != id {
		t.Fatalf("service.GetIssue calls = %v, want [%v]", svc.getCalls, id)
	}
}

func TestIssueFacade_GetIssueByIdentifier_PassesArgsThrough(t *testing.T) {
	t.Parallel()

	wsID := uuid(0x20)
	svc := &mockIssueService{getByIdentifierReturn: facade.Issue{ID: uuid(0x21)}}
	f := facade.NewIssueFacade(svc)

	if _, err := f.GetIssueByIdentifier(context.Background(), wsID, "STA-2"); err != nil {
		t.Fatalf("GetIssueByIdentifier: %v", err)
	}
	if len(svc.getByIdentifierCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.getByIdentifierCalls))
	}
	got := svc.getByIdentifierCalls[0]
	if got.WorkspaceID != wsID || got.Identifier != "STA-2" {
		t.Errorf("service received (%v,%q), want (%v,%q)", got.WorkspaceID, got.Identifier, wsID, "STA-2")
	}
}

func TestIssueFacade_SetIssueStatus_PassesActorIDThrough(t *testing.T) {
	t.Parallel()

	id := uuid(0x30)
	actorID := uuid(0x31)
	svc := &mockIssueService{}
	f := facade.NewIssueFacade(svc)

	if err := f.SetIssueStatus(context.Background(), id, actorID, "in_progress", facade.ChannelMutationContext{}); err != nil {
		t.Fatalf("SetIssueStatus: %v", err)
	}
	if len(svc.setStatusCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.setStatusCalls))
	}
	call := svc.setStatusCalls[0]
	if call.ID != id || call.ActorID != actorID || call.Status != "in_progress" {
		t.Errorf("service received %+v, want id=%v actor=%v status=in_progress", call, id, actorID)
	}
}

func TestIssueFacade_SetIssueAssignee_PassesArgsThrough(t *testing.T) {
	t.Parallel()

	id := uuid(0x32)
	actorID := uuid(0x33)
	svc := &mockIssueService{}
	f := facade.NewIssueFacade(svc)

	if err := f.SetIssueAssignee(context.Background(), id, actorID, "@张三", facade.ChannelMutationContext{}); err != nil {
		t.Fatalf("SetIssueAssignee: %v", err)
	}
	if len(svc.setAssigneeCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.setAssigneeCalls))
	}
	call := svc.setAssigneeCalls[0]
	if call.ID != id || call.ActorID != actorID || call.AssigneeIdentifier != "@张三" {
		t.Errorf("service received %+v, want id=%v actor=%v assignee=@张三", call, id, actorID)
	}
}

func TestIssueFacade_SetIssuePriority_PassesArgsThrough(t *testing.T) {
	t.Parallel()

	id := uuid(0x34)
	actorID := uuid(0x35)
	svc := &mockIssueService{}
	f := facade.NewIssueFacade(svc)

	if err := f.SetIssuePriority(context.Background(), id, actorID, "high", facade.ChannelMutationContext{}); err != nil {
		t.Fatalf("SetIssuePriority: %v", err)
	}
	if len(svc.setPriorityCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.setPriorityCalls))
	}
	call := svc.setPriorityCalls[0]
	if call.ID != id || call.ActorID != actorID || call.Priority != "high" {
		t.Errorf("service received %+v, want id=%v actor=%v priority=high", call, id, actorID)
	}
}

func TestIssueFacade_AddIssueLabel_PassesArgsThrough(t *testing.T) {
	t.Parallel()

	id := uuid(0x36)
	actorID := uuid(0x37)
	svc := &mockIssueService{}
	f := facade.NewIssueFacade(svc)

	if err := f.AddIssueLabel(context.Background(), id, actorID, "bug", facade.ChannelMutationContext{}); err != nil {
		t.Fatalf("AddIssueLabel: %v", err)
	}
	if len(svc.addLabelCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.addLabelCalls))
	}
	call := svc.addLabelCalls[0]
	if call.ID != id || call.ActorID != actorID || call.LabelName != "bug" {
		t.Errorf("service received %+v, want id=%v actor=%v label=bug", call, id, actorID)
	}
}

func TestIssueFacade_RemoveIssueLabel_PassesArgsThrough(t *testing.T) {
	t.Parallel()

	id := uuid(0x38)
	actorID := uuid(0x39)
	svc := &mockIssueService{}
	f := facade.NewIssueFacade(svc)

	if err := f.RemoveIssueLabel(context.Background(), id, actorID, "bug", facade.ChannelMutationContext{}); err != nil {
		t.Fatalf("RemoveIssueLabel: %v", err)
	}
	if len(svc.removeLabelCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.removeLabelCalls))
	}
	call := svc.removeLabelCalls[0]
	if call.ID != id || call.ActorID != actorID || call.LabelName != "bug" {
		t.Errorf("service received %+v, want id=%v actor=%v label=bug", call, id, actorID)
	}
}

func TestIssueFacade_ListMyTodos_PassesArgsThrough(t *testing.T) {
	t.Parallel()

	wsID := uuid(0x40)
	userID := uuid(0x41)
	want := []facade.Issue{{ID: uuid(0x42)}, {ID: uuid(0x43)}}
	svc := &mockIssueService{listMyTodosReturn: want}
	f := facade.NewIssueFacade(svc)

	got, err := f.ListMyTodos(context.Background(), wsID, userID)
	if err != nil {
		t.Fatalf("ListMyTodos: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	if len(svc.listMyTodosCalls) != 1 {
		t.Fatalf("expected 1 service call, got %d", len(svc.listMyTodosCalls))
	}
	call := svc.listMyTodosCalls[0]
	if call.WorkspaceID != wsID || call.UserID != userID {
		t.Errorf("service received (%v,%v), want (%v,%v)", call.WorkspaceID, call.UserID, wsID, userID)
	}
}

func TestIssueFacade_PropagatesServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("permission denied")
	svc := &mockIssueService{createErr: wantErr}
	f := facade.NewIssueFacade(svc)

	_, err := f.CreateIssue(context.Background(), facade.CreateIssueReq{
		WorkspaceID: uuid(0x50),
		ActorID:     uuid(0x51),
		Title:       "t",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v (errors.Is)", err, wantErr)
	}
}

// ---------------------------------------------------------------------------
// TC-facade-2: SQL-injection / quote payloads pass through verbatim. The
// channel layer must not sanitise; existing service-level validation is the
// single source of truth (PRD E9 / TestCase §2 TC-facade-2).
// ---------------------------------------------------------------------------

func TestIssueFacade_CreateIssue_DoesNotSanitiseInjectionPayload(t *testing.T) {
	t.Parallel()

	const malicious = `'); DROP TABLE issue; --`
	svc := &mockIssueService{createReturn: facade.Issue{ID: uuid(0x60)}}
	f := facade.NewIssueFacade(svc)

	_, err := f.CreateIssue(context.Background(), facade.CreateIssueReq{
		WorkspaceID: uuid(0x61),
		ActorID:     uuid(0x62),
		Title:       malicious,
		Description: malicious,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if len(svc.createCalls) != 1 {
		t.Fatalf("expected 1 service call, got %d", len(svc.createCalls))
	}
	got := svc.createCalls[0]
	if got.Title != malicious {
		t.Errorf("title was mutated: got %q, want verbatim %q", got.Title, malicious)
	}
	if got.Description != malicious {
		t.Errorf("description was mutated: got %q, want verbatim %q", got.Description, malicious)
	}
}

func TestCommentFacade_AddComment_DoesNotSanitiseInjectionPayload(t *testing.T) {
	t.Parallel()

	const malicious = `"); DROP TABLE comment; --`
	svc := &mockCommentService{addReturn: facade.Comment{ID: uuid(0x70)}}
	f := facade.NewCommentFacade(svc)

	_, err := f.AddComment(context.Background(), facade.AddCommentReq{
		IssueID: uuid(0x71),
		ActorID: uuid(0x72),
		Content: malicious,
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if len(svc.addCalls) != 1 {
		t.Fatalf("expected 1 service call, got %d", len(svc.addCalls))
	}
	got := svc.addCalls[0]
	if got.Content != malicious {
		t.Errorf("comment content was mutated: got %q, want verbatim %q", got.Content, malicious)
	}
}

// ---------------------------------------------------------------------------
// CommentFacade smoke: pass-through + actor pass-through + error propagation.
// ---------------------------------------------------------------------------

func TestCommentFacade_AddComment_PassesActorIDThrough(t *testing.T) {
	t.Parallel()

	issueID := uuid(0x80)
	actorID := uuid(0x81)
	want := facade.Comment{ID: uuid(0x82), IssueID: issueID, Content: "hello"}
	svc := &mockCommentService{addReturn: want}
	f := facade.NewCommentFacade(svc)

	got, err := f.AddComment(context.Background(), facade.AddCommentReq{
		IssueID: issueID,
		ActorID: actorID,
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("got.ID = %v, want %v", got.ID, want.ID)
	}
	if len(svc.addCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(svc.addCalls))
	}
	call := svc.addCalls[0]
	if call.IssueID != issueID || call.ActorID != actorID || call.Content != "hello" {
		t.Errorf("service received %+v, want issue=%v actor=%v content=hello", call, issueID, actorID)
	}
}

func TestCommentFacade_PropagatesServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("issue not found")
	svc := &mockCommentService{addErr: wantErr}
	f := facade.NewCommentFacade(svc)

	_, err := f.AddComment(context.Background(), facade.AddCommentReq{
		IssueID: uuid(0x90),
		ActorID: uuid(0x91),
		Content: "x",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v (errors.Is)", err, wantErr)
	}
}

// ---------------------------------------------------------------------------
// Compile-time interface conformance: NewIssueFacade / NewCommentFacade must
// return values that satisfy the public facade.IssueFacade / facade.CommentFacade
// interfaces. This is enforced at the package level in facade.go via
// `var _ IssueFacade = (*issueFacade)(nil)`; the test below just ensures the
// constructor's declared return type is the interface, not a concrete struct
// (so callers wire by interface, not implementation).
// ---------------------------------------------------------------------------

func TestFacadeConstructors_ReturnInterfaceTypes(t *testing.T) {
	t.Parallel()

	var _ facade.IssueFacade = facade.NewIssueFacade(&mockIssueService{})
	var _ facade.CommentFacade = facade.NewCommentFacade(&mockCommentService{})
}
