package persona

import (
	"context"
	"errors"
	"testing"
	"time"
)

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }
func timep(t time.Time) *time.Time {
	return &t
}

func newTestBackend(t *testing.T) *MockBackend {
	t.Helper()
	b := NewMockBackend("ws-test")
	b.SetClock(func() time.Time { return time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC) })
	return b
}

func actorCtx() AuditContext {
	return AuditContext{
		Actor:     "user-sara",
		ActorType: SubjectMember,
		Surface:   "mcp",
		SessionID: "mcp-session-1",
	}
}

func TestCreateGrantHappyPath(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	g, err := b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("agent-1"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, actorCtx())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.ID == "" {
		t.Fatalf("expected generated id")
	}
	if g.Effect != EffectAllow {
		t.Errorf("default effect should be allow, got %q", g.Effect)
	}
	if g.Status != StatusActive {
		t.Errorf("default status should be active, got %q", g.Status)
	}
	if g.CreatedBy != "user-sara" {
		t.Errorf("created_by should be set from audit context")
	}
}

func TestCreateGrantValidation(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	cases := []struct {
		name string
		in   GrantInput
	}{
		{"missing subject_type", GrantInput{ResourcePattern: strp("x"), Capability: strp("y")}},
		{"missing resource_pattern", GrantInput{SubjectType: strp(SubjectAgent), SubjectID: strp("a"), Capability: strp("y")}},
		{"missing capability", GrantInput{SubjectType: strp(SubjectAgent), SubjectID: strp("a"), ResourcePattern: strp("x")}},
		{"missing subject_id for member", GrantInput{SubjectType: strp(SubjectMember), ResourcePattern: strp("x"), Capability: strp("y")}},
		{"bad effect", GrantInput{SubjectType: strp(SubjectAgent), SubjectID: strp("a"), ResourcePattern: strp("x"), Capability: strp("y"), Effect: strp("maybe")}},
		{"bad classification", GrantInput{SubjectType: strp(SubjectAgent), SubjectID: strp("a"), ResourcePattern: strp("x"), Capability: strp("y"), ClassificationCeiling: strp("nuclear")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := b.Create(ctx, c.in, actorCtx())
			var inv *ErrInvalidGrant
			if !errors.As(err, &inv) {
				t.Fatalf("expected ErrInvalidGrant, got %v", err)
			}
		})
	}
}

func TestWorkspaceDefaultGrantNeedsNoSubjectID(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.Create(context.Background(), GrantInput{
		SubjectType:     strp(SubjectWorkspaceDefault),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, actorCtx())
	if err != nil {
		t.Fatalf("workspace_default should not require subject_id: %v", err)
	}
}

func TestGetMissing(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.Get(context.Background(), "nope", actorCtx())
	if !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("expected ErrGrantNotFound, got %v", err)
	}
}

func TestUpdateChangesAndAudits(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	g, _ := b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("agent-1"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, actorCtx())

	updated, err := b.Update(ctx, g.ID, GrantInput{
		Capability: strp("issue.write"),
		Note:       strp("escalated for migration"),
	}, actorCtx())
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Capability != "issue.write" {
		t.Errorf("capability not applied")
	}
	if updated.Note != "escalated for migration" {
		t.Errorf("note not applied")
	}

	audit, _ := b.ListAudit(ctx, g.ID, 0)
	if len(audit) < 2 {
		t.Fatalf("expected at least 2 audit entries, got %d", len(audit))
	}
	// Newest first.
	if audit[0].Action != "grant.update" {
		t.Errorf("expected newest action grant.update, got %q", audit[0].Action)
	}
	if audit[0].Before == nil || audit[0].After == nil {
		t.Errorf("update audit must capture before/after")
	}
	if audit[0].Before.Capability != "issue.read" {
		t.Errorf("audit before capability wrong: %q", audit[0].Before.Capability)
	}
	if audit[0].After.Capability != "issue.write" {
		t.Errorf("audit after capability wrong: %q", audit[0].After.Capability)
	}
	if audit[0].SessionID != "mcp-session-1" {
		t.Errorf("audit session_id missing")
	}
}

func TestDeleteSoftRevokes(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	g, _ := b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("agent-1"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, actorCtx())

	if err := b.Delete(ctx, g.ID, actorCtx()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := b.Get(ctx, g.ID, actorCtx())
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got.Status != StatusRevoked {
		t.Errorf("expected status revoked, got %q", got.Status)
	}
}

func TestListFilters(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	_, _ = b.Create(ctx, GrantInput{SubjectType: strp(SubjectAgent), SubjectID: strp("a1"), ResourcePattern: strp("issue:*"), Capability: strp("issue.read")}, actorCtx())
	_, _ = b.Create(ctx, GrantInput{SubjectType: strp(SubjectMember), SubjectID: strp("m1"), ResourcePattern: strp("agent:*"), Capability: strp("agent.run")}, actorCtx())

	all, _ := b.List(ctx, ListFilter{}, actorCtx())
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	onlyAgents, _ := b.List(ctx, ListFilter{SubjectType: SubjectAgent}, actorCtx())
	if len(onlyAgents) != 1 || onlyAgents[0].SubjectType != SubjectAgent {
		t.Errorf("subject_type filter broken: %+v", onlyAgents)
	}
	byCapability, _ := b.List(ctx, ListFilter{Capability: "agent.run"}, actorCtx())
	if len(byCapability) != 1 {
		t.Errorf("capability filter broken")
	}
}

func TestEvaluateAllowExplicit(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	_, _ = b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, actorCtx())

	res, err := b.Evaluate(ctx, EvaluationQuery{
		ActorType:  SubjectAgent,
		ActorID:    "rasp",
		Resource:   "issue:JEH-1181",
		Capability: "issue.read",
	}, actorCtx())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != EffectAllow {
		t.Errorf("expected allow, got %q (reason: %s)", res.Decision, res.Reason)
	}
	if res.WinningGrant == nil {
		t.Errorf("expected winning_grant")
	}
}

func TestEvaluateDefaultDeny(t *testing.T) {
	b := newTestBackend(t)
	res, err := b.Evaluate(context.Background(), EvaluationQuery{
		ActorType:  SubjectAgent,
		ActorID:    "rasp",
		Resource:   "issue:JEH-1181",
		Capability: "issue.read",
	}, actorCtx())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != EffectDeny {
		t.Errorf("expected deny on empty grants, got %q", res.Decision)
	}
}

func TestEvaluateDenyOverridesAllow(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	_, _ = b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
		Effect:          strp(EffectAllow),
	}, actorCtx())
	_, _ = b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
		Effect:          strp(EffectDeny),
	}, actorCtx())

	res, _ := b.Evaluate(ctx, EvaluationQuery{
		ActorType:  SubjectAgent,
		ActorID:    "rasp",
		Resource:   "issue:JEH-1181",
		Capability: "issue.read",
	}, actorCtx())
	if res.Decision != EffectDeny {
		t.Errorf("deny should override allow, got %q", res.Decision)
	}
}

func TestEvaluateClassificationCeiling(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	_, _ = b.Create(ctx, GrantInput{
		SubjectType:           strp(SubjectAgent),
		SubjectID:             strp("rasp"),
		ResourcePattern:       strp("issue:*"),
		Capability:            strp("issue.read"),
		ClassificationCeiling: strp(ClassificationInternal),
	}, actorCtx())

	resOK, _ := b.Evaluate(ctx, EvaluationQuery{
		ActorType:             SubjectAgent,
		ActorID:               "rasp",
		Resource:              "issue:JEH-1181",
		Capability:            "issue.read",
		ResourceClassification: ClassificationInternal,
	}, actorCtx())
	if resOK.Decision != EffectAllow {
		t.Errorf("internal access should be allowed: %s", resOK.Reason)
	}

	resBlocked, _ := b.Evaluate(ctx, EvaluationQuery{
		ActorType:              SubjectAgent,
		ActorID:                "rasp",
		Resource:               "issue:JEH-1181",
		Capability:             "issue.read",
		ResourceClassification: ClassificationConfidential,
	}, actorCtx())
	if resBlocked.Decision != EffectDeny {
		t.Errorf("confidential should be blocked by internal ceiling")
	}
}

func TestEvaluateValidityWindow(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	at := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	b.SetClock(func() time.Time { return at })

	_, _ = b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
		StartsAt:        timep(at.Add(time.Hour)),
	}, actorCtx())

	res, _ := b.Evaluate(ctx, EvaluationQuery{
		ActorType:  SubjectAgent,
		ActorID:    "rasp",
		Resource:   "issue:JEH-1181",
		Capability: "issue.read",
	}, actorCtx())
	if res.Decision != EffectDeny {
		t.Errorf("grant not yet active should not allow")
	}
}

func TestAuditLogIncludesActorAndSession(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	_, _ = b.Create(ctx, GrantInput{
		SubjectType:     strp(SubjectAgent),
		SubjectID:       strp("rasp"),
		ResourcePattern: strp("issue:*"),
		Capability:      strp("issue.read"),
	}, actorCtx())

	entries, _ := b.ListAudit(ctx, "", 0)
	if len(entries) == 0 {
		t.Fatalf("expected audit entries")
	}
	for _, e := range entries {
		if e.Actor != "user-sara" {
			t.Errorf("audit actor missing: %+v", e)
		}
		if e.SessionID != "mcp-session-1" {
			t.Errorf("audit session_id missing: %+v", e)
		}
		if e.Surface != "mcp" {
			t.Errorf("audit surface should default to mcp: %+v", e)
		}
	}
}
