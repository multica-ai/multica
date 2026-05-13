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

	// Phase-2 ext (JEH-1114, route_by_domain).
	Labels        []db.IssueLabel
	ListLabelsErr error

	// Phase-2 ext (JEH-1114, validate_evidence).
	Comments              []db.Comment
	ListCommentsErr       error
	IssueAttachments      []db.Attachment
	ListAttachmentsErr    error
	AttachmentURLs        []string
	ListAttachmentURLsErr error

	// Phase-3 (JEH-1108) — reassign_issue capture and canned error.
	UpdatedAssignee    db.UpdateIssueAssigneeParams
	UpdateAssigneeErr  error
	UpdateAssigneeUsed bool
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
func (f *fakeIssueActions) ListLabels(_ context.Context, _ pgtype.UUID) ([]db.IssueLabel, error) {
	if f.ListLabelsErr != nil {
		return nil, f.ListLabelsErr
	}
	return f.Labels, nil
}
func (f *fakeIssueActions) ListCommentsForIssue(_ context.Context, _ db.ListCommentsForIssueParams) ([]db.Comment, error) {
	if f.ListCommentsErr != nil {
		return nil, f.ListCommentsErr
	}
	return f.Comments, nil
}
func (f *fakeIssueActions) ListAttachmentsByIssue(_ context.Context, _ db.ListAttachmentsByIssueParams) ([]db.Attachment, error) {
	if f.ListAttachmentsErr != nil {
		return nil, f.ListAttachmentsErr
	}
	return f.IssueAttachments, nil
}
func (f *fakeIssueActions) ListAttachmentURLsByIssueOrComments(_ context.Context, _ pgtype.UUID) ([]string, error) {
	if f.ListAttachmentURLsErr != nil {
		return nil, f.ListAttachmentURLsErr
	}
	return f.AttachmentURLs, nil
}
func (f *fakeIssueActions) UpdateIssueAssignee(_ context.Context, p db.UpdateIssueAssigneeParams) (db.Issue, error) {
	f.UpdateAssigneeUsed = true
	f.UpdatedAssignee = p
	if f.UpdateAssigneeErr != nil {
		return db.Issue{}, f.UpdateAssigneeErr
	}
	return db.Issue{ID: p.ID, AssigneeType: p.AssigneeType, AssigneeID: p.AssigneeID}, nil
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

func TestClassifyDomain(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		desc    string
		want    string
	}{
		{"empty falls back to default", "", "", DomainBusiness},
		{"file extension wins", "Tweak handler", "fix in actions.go and types.ts", DomainCode},
		{"github url is code", "Review PR", "https://github.com/firtal-group/foo", DomainCode},
		{"figma url is design", "New onboarding", "see figma.com/file/abc", DomainDesign},
		{"copy keyword routes to content", "Q3 newsletter", "draft the headline copy", DomainContent},
		{"design keyword wins over generic word", "Update icon palette", "spacing tweak", DomainDesign},
		{"word boundary respected for short words", "fluid runtime", "team updates", DomainBusiness},
		{"PR is matched as a whole word, not inside other words", "Predict pricing", "improve pricing model", DomainBusiness},
		{"refactor keyword routes to code", "Cleanup", "refactor the inbox view", DomainCode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyDomain(c.title, c.desc, DomainBusiness)
			if got != c.want {
				t.Errorf("classifyDomain(%q,%q) = %q, want %q", c.title, c.desc, got, c.want)
			}
		})
	}
}

func TestActionRouteByDomain_HappyPath(t *testing.T) {
	codeLabelID := mustUUID("44444444-4444-4444-4444-444444444444")
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{
			ID:          mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			Title:       "Fix login bug in auth.ts",
			Description: pgtype.Text{String: "stack trace + repro", Valid: true},
		},
		Labels: []db.IssueLabel{
			{ID: codeLabelID, Name: "domain:code"},
			{ID: mustUUID("55555555-5555-5555-5555-555555555555"), Name: "domain:business"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRouteByDomain, ActionConfigRouteByDomain{})

	if err := svc.actionRouteByDomain(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionRouteByDomain returned %v", err)
	}
	if len(fake.AttachedLabels) != 1 {
		t.Fatalf("expected 1 label attached, got %d", len(fake.AttachedLabels))
	}
	if fake.AttachedLabels[0].LabelID != codeLabelID {
		t.Errorf("attached wrong label: got %v, want code label", fake.AttachedLabels[0].LabelID)
	}
}

func TestActionRouteByDomain_RespectsCustomPrefix(t *testing.T) {
	customLabelID := mustUUID("66666666-6666-6666-6666-666666666666")
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{
			ID:    mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			Title: "Hero section copy needs polish",
		},
		Labels: []db.IssueLabel{
			{ID: customLabelID, Name: "track::content"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRouteByDomain, ActionConfigRouteByDomain{
		LabelPrefix: "track::",
	})

	if err := svc.actionRouteByDomain(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionRouteByDomain returned %v", err)
	}
	if len(fake.AttachedLabels) != 1 || fake.AttachedLabels[0].LabelID != customLabelID {
		t.Errorf("expected custom-prefixed content label attached, got %+v", fake.AttachedLabels)
	}
}

func TestActionRouteByDomain_FallsBackToDefaultDomain(t *testing.T) {
	defaultLabelID := mustUUID("77777777-7777-7777-7777-777777777777")
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{
			ID:    mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			Title: "Quarterly planning",
		},
		Labels: []db.IssueLabel{
			{ID: defaultLabelID, Name: "domain:business"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRouteByDomain, ActionConfigRouteByDomain{
		DefaultDomain: DomainBusiness,
	})

	if err := svc.actionRouteByDomain(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionRouteByDomain returned %v", err)
	}
	if len(fake.AttachedLabels) != 1 || fake.AttachedLabels[0].LabelID != defaultLabelID {
		t.Errorf("expected business label attached, got %+v", fake.AttachedLabels)
	}
}

func TestActionRouteByDomain_RejectsMissingLabel(t *testing.T) {
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{
			ID:    mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			Title: "Untriaged ticket",
		},
		Labels: []db.IssueLabel{}, // workspace has no domain:* labels
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRouteByDomain, ActionConfigRouteByDomain{})

	err := svc.actionRouteByDomain(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "no label") {
		t.Fatalf("expected no-label error, got %v", err)
	}
	if len(fake.AttachedLabels) != 0 {
		t.Errorf("expected no labels attached on failure, got %d", len(fake.AttachedLabels))
	}
}

func TestActionRouteByDomain_RejectsUnknownDefaultDomain(t *testing.T) {
	fake := &fakeIssueActions{
		ParentIssue: db.Issue{ID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")},
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRouteByDomain, ActionConfigRouteByDomain{
		DefaultDomain: "growth",
	})

	err := svc.actionRouteByDomain(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "default_domain") {
		t.Fatalf("expected default_domain error, got %v", err)
	}
}

func TestActionRouteByDomain_PropagatesIssueLookupError(t *testing.T) {
	fake := &fakeIssueActions{
		GetIssueErr: errors.New("issue gone"),
	}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionRouteByDomain, ActionConfigRouteByDomain{})

	err := svc.actionRouteByDomain(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "load issue") {
		t.Fatalf("expected load-issue error, got %v", err)
	}
}

// --- Phase-3 action tests (JEH-1108) ---

func TestActionReassignIssue_HappyPath(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionReassignIssue, ActionConfigReassignIssue{
		AssigneeID:   "77777777-7777-7777-7777-777777777777",
		AssigneeType: AssigneeTypeAgent,
	})

	if err := svc.actionReassignIssue(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("actionReassignIssue returned %v", err)
	}
	if !fake.UpdateAssigneeUsed {
		t.Fatal("expected UpdateIssueAssignee to be called")
	}
	if fake.UpdatedAssignee.AssigneeType.String != AssigneeTypeAgent {
		t.Errorf("assignee_type = %q, want %q", fake.UpdatedAssignee.AssigneeType.String, AssigneeTypeAgent)
	}
	if !fake.UpdatedAssignee.AssigneeType.Valid {
		t.Fatal("assignee_type must be a valid pgtype.Text")
	}
}

func TestActionReassignIssue_RejectsUnsupportedAssigneeType(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionReassignIssue, ActionConfigReassignIssue{
		AssigneeID:   "77777777-7777-7777-7777-777777777777",
		AssigneeType: "external_uuid",
	})
	err := svc.actionReassignIssue(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "unsupported assignee_type") {
		t.Fatalf("expected unsupported assignee_type error, got %v", err)
	}
	if fake.UpdateAssigneeUsed {
		t.Fatal("UpdateIssueAssignee must not be called on invalid type")
	}
}

func TestActionReassignIssue_RejectsBadUUID(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionReassignIssue, ActionConfigReassignIssue{
		AssigneeID:   "not-a-uuid",
		AssigneeType: AssigneeTypeMember,
	})
	err := svc.actionReassignIssue(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "reassign_issue") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestActionReassignIssue_RequiresIssueID(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionReassignIssue, ActionConfigReassignIssue{
		AssigneeID:   "77777777-7777-7777-7777-777777777777",
		AssigneeType: AssigneeTypeAgent,
	})
	te := testTriggerEvent()
	te.IssueID = ""
	err := svc.actionReassignIssue(context.Background(), wf, te)
	if err == nil || !strings.Contains(err.Error(), "no issue_id") {
		t.Fatalf("expected no issue_id error, got %v", err)
	}
}

func TestActionWebhookOutbound_PlaceholderReturnsUnimplemented(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := testWorkflow(ActionWebhookOutbound, ActionConfigWebhookOutbound{
		URL: "https://example.com/hook",
	})
	err := svc.actionWebhookOutbound(context.Background(), wf, testTriggerEvent())
	if err == nil {
		t.Fatal("placeholder must return an error until PR 2 lands")
	}
	if !errors.Is(err, ErrWebhookOutboundUnimplemented) {
		t.Fatalf("expected ErrWebhookOutboundUnimplemented, got %v", err)
	}
}

func TestActionWebhookOutbound_ValidatesURL(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	// Missing URL is a config error, not the unimplemented sentinel.
	wf := testWorkflow(ActionWebhookOutbound, ActionConfigWebhookOutbound{})
	err := svc.actionWebhookOutbound(context.Background(), wf, testTriggerEvent())
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url-required error, got %v", err)
	}
	if errors.Is(err, ErrWebhookOutboundUnimplemented) {
		t.Fatal("url-required must surface before the unimplemented sentinel")
	}
}

func TestRunAction_DispatchesPhase3Actions(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)

	// reassign_issue routes through runAction.
	wf := testWorkflow(ActionReassignIssue, ActionConfigReassignIssue{
		AssigneeID:   "77777777-7777-7777-7777-777777777777",
		AssigneeType: AssigneeTypeMember,
	})
	if err := svc.runAction(context.Background(), wf, testTriggerEvent()); err != nil {
		t.Fatalf("runAction(reassign_issue) returned %v", err)
	}
	if !fake.UpdateAssigneeUsed {
		t.Fatal("runAction must invoke UpdateIssueAssignee for reassign_issue")
	}

	// webhook_outbound routes through runAction and surfaces the sentinel.
	wf = testWorkflow(ActionWebhookOutbound, ActionConfigWebhookOutbound{URL: "https://example.com"})
	err := svc.runAction(context.Background(), wf, testTriggerEvent())
	if !errors.Is(err, ErrWebhookOutboundUnimplemented) {
		t.Fatalf("runAction(webhook_outbound) must surface the unimplemented sentinel, got %v", err)
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
