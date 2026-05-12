package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeIssueActions captures the calls the action runner makes so tests can
// assert on params and return canned errors without spinning up a DB.
type fakeIssueActions struct {
	UpdateStatusCalled bool
	CreatedIssue       db.CreateIssueParams
	AttachedLabels     []db.AttachLabelToIssueParams

	Skills      []db.ListSkillSummariesByWorkspaceRow
	AgentSkills []db.Skill
	Agent       db.Agent
	GetAgentErr error

	TaskQueued       db.CreateQuickCreateTaskParams
	TaskQueueErr     error
	CreatedComment   db.CreateCommentParams
	CreateCommentErr error
	ParentIssue      db.Issue
	GetIssueErr      error
}

func (f *fakeIssueActions) UpdateIssueStatus(_ context.Context, _ db.UpdateIssueStatusParams) (db.Issue, error) {
	f.UpdateStatusCalled = true
	return db.Issue{}, nil
}
func (f *fakeIssueActions) CreateIssue(_ context.Context, p db.CreateIssueParams) (db.Issue, error) {
	f.CreatedIssue = p
	// Return an issue with a fixed ID so AttachLabelToIssue gets a non-zero IssueID.
	return db.Issue{ID: mustUUID("11111111-1111-1111-1111-111111111111")}, nil
}
func (f *fakeIssueActions) IncrementIssueCounter(_ context.Context, _ pgtype.UUID) (int32, error) {
	return 42, nil
}
func (f *fakeIssueActions) GetIssue(_ context.Context, _ pgtype.UUID) (db.Issue, error) {
	if f.GetIssueErr != nil {
		return db.Issue{}, f.GetIssueErr
	}
	return f.ParentIssue, nil
}
func (f *fakeIssueActions) CreateInboxItem(_ context.Context, _ db.CreateInboxItemParams) (db.InboxItem, error) {
	return db.InboxItem{}, nil
}
func (f *fakeIssueActions) CreateComment(_ context.Context, p db.CreateCommentParams) (db.Comment, error) {
	f.CreatedComment = p
	if f.CreateCommentErr != nil {
		return db.Comment{}, f.CreateCommentErr
	}
	return db.Comment{ID: mustUUID("22222222-2222-2222-2222-222222222222")}, nil
}
func (f *fakeIssueActions) AttachLabelToIssue(_ context.Context, p db.AttachLabelToIssueParams) error {
	f.AttachedLabels = append(f.AttachedLabels, p)
	return nil
}
func (f *fakeIssueActions) ListSkillSummariesByWorkspace(_ context.Context, _ pgtype.UUID) ([]db.ListSkillSummariesByWorkspaceRow, error) {
	return f.Skills, nil
}
func (f *fakeIssueActions) ListAgentSkills(_ context.Context, _ pgtype.UUID) ([]db.Skill, error) {
	return f.AgentSkills, nil
}
func (f *fakeIssueActions) GetAgent(_ context.Context, _ pgtype.UUID) (db.Agent, error) {
	if f.GetAgentErr != nil {
		return db.Agent{}, f.GetAgentErr
	}
	return f.Agent, nil
}
func (f *fakeIssueActions) CreateQuickCreateTask(_ context.Context, p db.CreateQuickCreateTaskParams) (db.AgentTaskQueue, error) {
	f.TaskQueued = p
	if f.TaskQueueErr != nil {
		return db.AgentTaskQueue{}, f.TaskQueueErr
	}
	return db.AgentTaskQueue{ID: mustUUID("33333333-3333-3333-3333-333333333333")}, nil
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func newServiceWithFake(fake *fakeIssueActions) *Service {
	return &Service{issues: fake, enabled: true}
}

func testTriggerEvent() TriggerEvent {
	return TriggerEvent{
		EventID:     "evt-1",
		WorkspaceID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		IssueID:     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		Type:        TriggerStatusChanged,
		FromStatus:  "todo",
		ToStatus:    "in_review",
		Raw: map[string]any{
			"issue": map[string]any{
				"title":  "Login bug",
				"status": "in_review",
			},
		},
	}
}

func testWorkflow(actionType string, cfg any) workflow {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return workflow{
		id:            mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		workspaceID:   mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		createdByID:   mustUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"),
		createdByType: "agent",
		actionType:    actionType,
		actionConfig:  configJSON,
	}
}

func TestActionCreateSubIssue_AttachesLabels(t *testing.T) {
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{ID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionCreateSubIssue, ActionConfigCreateSubIssue{
		Title:       "QA: {{title}}",
		Description: "auto",
		LabelIDs: []string{
			"99999999-9999-9999-9999-999999999999",
			"88888888-8888-8888-8888-888888888888",
		},
	})

	if _, err := svc.actionCreateSubIssue(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionCreateSubIssue returned %v", err)
	}
	if fake.CreatedIssue.Title != "QA: Login bug" {
		t.Errorf("expected rendered title, got %q", fake.CreatedIssue.Title)
	}
	if len(fake.AttachedLabels) != 2 {
		t.Errorf("expected 2 labels attached, got %d", len(fake.AttachedLabels))
	}
}

func TestActionCreateSubIssue_IgnoresMalformedLabelIDs(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionCreateSubIssue, ActionConfigCreateSubIssue{
		Title:    "x",
		LabelIDs: []string{"not-a-uuid", "99999999-9999-9999-9999-999999999999"},
	})

	if _, err := svc.actionCreateSubIssue(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionCreateSubIssue returned %v", err)
	}
	if len(fake.AttachedLabels) != 1 {
		t.Errorf("expected 1 valid label attached, got %d", len(fake.AttachedLabels))
	}
}

func TestActionRunSkill_HappyPath(t *testing.T) {
	skillName := "firtal-data-evaluate"
	fake := &fakeIssueActions{
		Skills:      []db.ListSkillSummariesByWorkspaceRow{{Name: skillName}},
		AgentSkills: []db.Skill{{Name: skillName}},
		Agent: db.Agent{
			RuntimeID: mustUUID("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"),
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRunSkill, ActionConfigRunSkill{
		SkillName:  skillName,
		AgentID:    "ffffffff-ffff-ffff-ffff-ffffffffffff",
		SkillInput: map[string]any{"query": "active campaigns"},
	})

	if err := svc.actionRunSkill(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionRunSkill returned %v", err)
	}

	if fake.TaskQueued.AgentID.Bytes == ([16]byte{}) {
		t.Fatal("expected CreateQuickCreateTask to be called with non-zero agent_id")
	}

	var ctx map[string]any
	if err := json.Unmarshal(fake.TaskQueued.Context, &ctx); err != nil {
		t.Fatalf("context not valid JSON: %v", err)
	}
	if ctx["type"] != "quick_create" {
		t.Errorf("context.type = %v, want quick_create", ctx["type"])
	}
	prompt, _ := ctx["prompt"].(string)
	if !strings.Contains(prompt, skillName) {
		t.Errorf("prompt does not mention skill: %q", prompt)
	}
	if ctx["workflow_skill_name"] != skillName {
		t.Errorf("context.workflow_skill_name = %v, want %q", ctx["workflow_skill_name"], skillName)
	}
}

func TestActionRunSkill_RejectsMissingSkill(t *testing.T) {
	fake := &fakeIssueActions{
		// No skills in workspace.
		Agent: db.Agent{RuntimeID: mustUUID("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRunSkill, ActionConfigRunSkill{
		SkillName: "absent",
		AgentID:   "ffffffff-ffff-ffff-ffff-ffffffffffff",
	})

	err := svc.actionRunSkill(context.Background(), wf, testTriggerEvent())
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestActionRunSkill_RejectsAgentMissingSkill(t *testing.T) {
	skillName := "firtal-data-evaluate"
	fake := &fakeIssueActions{
		Skills:      []db.ListSkillSummariesByWorkspaceRow{{Name: skillName}},
		AgentSkills: []db.Skill{}, // agent has no skills attached
		Agent: db.Agent{
			RuntimeID: mustUUID("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"),
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRunSkill, ActionConfigRunSkill{
		SkillName: skillName,
		AgentID:   "ffffffff-ffff-ffff-ffff-ffffffffffff",
	})

	err := svc.actionRunSkill(context.Background(), wf, testTriggerEvent())
	if err == nil {
		t.Fatal("expected error for unattached skill, got nil")
	}
	if !strings.Contains(err.Error(), "does not have skill") {
		t.Errorf("expected 'does not have skill' error, got %v", err)
	}
}

func TestActionRunSkill_RejectsArchivedAgent(t *testing.T) {
	skillName := "firtal-data-evaluate"
	fake := &fakeIssueActions{
		Skills:      []db.ListSkillSummariesByWorkspaceRow{{Name: skillName}},
		AgentSkills: []db.Skill{{Name: skillName}},
		Agent: db.Agent{
			ArchivedAt: pgtype.Timestamptz{Valid: true},
			RuntimeID:  mustUUID("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"),
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRunSkill, ActionConfigRunSkill{
		SkillName: skillName,
		AgentID:   "ffffffff-ffff-ffff-ffff-ffffffffffff",
	})

	err := svc.actionRunSkill(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("expected archived error, got %v", err)
	}
}

func TestActionCommentOnIssue_TargetSelf(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionCommentOnIssue, ActionConfigCommentOnIssue{
		Target:  CommentTargetSelf,
		Content: "Auto-comment on {{title}}",
	})

	if err := svc.actionCommentOnIssue(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionCommentOnIssue returned %v", err)
	}
	if fake.CreatedComment.Content != "Auto-comment on Login bug" {
		t.Errorf("expected template-rendered content, got %q", fake.CreatedComment.Content)
	}
	if fake.CreatedComment.Type != "comment" {
		t.Errorf("comment type = %q, want comment", fake.CreatedComment.Type)
	}
}

func TestActionCommentOnIssue_TargetParent(t *testing.T) {
	parentID := mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1")
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{
			ID:            mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			ParentIssueID: parentID,
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionCommentOnIssue, ActionConfigCommentOnIssue{
		Target:  CommentTargetParent,
		Content: "ping",
	})

	if err := svc.actionCommentOnIssue(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionCommentOnIssue returned %v", err)
	}
	if fake.CreatedComment.IssueID != parentID {
		t.Errorf("comment posted on wrong issue: want parent, got %v", fake.CreatedComment.IssueID)
	}
}

func TestActionCommentOnIssue_NoParentFails(t *testing.T) {
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{ID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionCommentOnIssue, ActionConfigCommentOnIssue{
		Target:  CommentTargetParent,
		Content: "ping",
	})

	err := svc.actionCommentOnIssue(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "no parent") {
		t.Fatalf("expected no-parent error, got %v", err)
	}
}

func TestActionCommentOnIssue_RejectsUnknownTarget(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionCommentOnIssue, ActionConfigCommentOnIssue{
		Target:  "external_uuid",
		Content: "ping",
	})

	err := svc.actionCommentOnIssue(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("expected unsupported target error, got %v", err)
	}
}

func TestRenderTemplate_ExpandedPlaceholders(t *testing.T) {
	raw := map[string]any{
		"issue": map[string]any{
			"title":      "Login bug",
			"status":     "in_review",
			"identifier": "MUL-42",
		},
	}
	cases := []struct {
		tpl  string
		want string
	}{
		{"{{title}}", "Login bug"},
		{"{{status}} / {{issue.identifier}}", "in_review / MUL-42"},
		{"{{issue.title}}", "Login bug"},
		{"unknown {{issue.missing}}", "unknown {{issue.missing}}"},
	}
	for _, c := range cases {
		got := renderTemplate(c.tpl, raw)
		if got != c.want {
			t.Errorf("renderTemplate(%q) = %q, want %q", c.tpl, got, c.want)
		}
	}
}

func TestBuildRunSkillPrompt_DeterministicJSON(t *testing.T) {
	input := map[string]any{"b": 2, "a": 1}
	first := buildRunSkillPrompt("x", input)
	second := buildRunSkillPrompt("x", input)
	if first != second {
		t.Fatalf("expected deterministic prompt, got\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(first, `"a"`) || !strings.Contains(first, `"b"`) {
		t.Errorf("prompt missing keys: %q", first)
	}
}

func TestActionRunSkill_PropagatesTaskQueueError(t *testing.T) {
	skillName := "x"
	fake := &fakeIssueActions{
		Skills:      []db.ListSkillSummariesByWorkspaceRow{{Name: skillName}},
		AgentSkills: []db.Skill{{Name: skillName}},
		Agent: db.Agent{
			RuntimeID: mustUUID("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"),
		},
		TaskQueueErr: errors.New("db down"),
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRunSkill, ActionConfigRunSkill{
		SkillName: skillName,
		AgentID:   "ffffffff-ffff-ffff-ffff-ffffffffffff",
	})

	err := svc.actionRunSkill(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "enqueue task") {
		t.Fatalf("expected enqueue error, got %v", err)
	}
}
