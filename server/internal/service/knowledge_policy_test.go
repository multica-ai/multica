package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// recordingGovernance keeps the automatic publishes it is told about.
type recordingGovernance struct{ events []AutoPublishEvent }

func (r *recordingGovernance) RecordAutoPublish(_ context.Context, ev AutoPublishEvent) error {
	r.events = append(r.events, ev)
	return nil
}

func TestResolvePublishModeKindOverBaseOverDefault(t *testing.T) {
	policies := []db.KnowledgePublishPolicy{
		{Scope: PublishScopeBase, ScopeKey: "engineering", Mode: PublishModeAuto},
		{Scope: PublishScopeKind, ScopeKey: "adr", Mode: PublishModeManual},
	}
	cases := []struct{ kind, base, want string }{
		{"adr", "engineering", PublishModeManual},   // the kind wins over its base
		{"runbook", "engineering", PublishModeAuto}, // the base applies to other kinds
		{"", "market", PublishModeAfterReview},      // no row: the fallback
	}
	for _, c := range cases {
		if got := resolvePublishMode(policies, c.kind, c.base, PublishModeAfterReview); got != c.want {
			t.Errorf("resolve(%q, %q) = %q, want %q", c.kind, c.base, got, c.want)
		}
	}
}

// TestSquadMayAutoPublishStub pins the stand-in for the autonomy check. Phase 4
// (11.9) replaces it; until then every squad may publish after review.
func TestSquadMayAutoPublishStub(t *testing.T) {
	if !squadMayAutoPublish(context.Background(), pgtype.UUID{}) {
		t.Fatal("the autonomy stub must return true until phase 4")
	}
	if !squadMayAutoPublish(context.Background(), pgtype.UUID{Valid: true, Bytes: [16]byte{1}}) {
		t.Fatal("the autonomy stub must return true for every squad until phase 4")
	}
}

func TestSetPublishPolicyValidatesScopeAndMode(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "engineering", "sometimes"); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "nowhere", PublishModeAuto); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("unknown base: err = %v", err)
	}
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeKind, "no_such_kind", PublishModeAuto); !errors.Is(err, ErrKnowledgeKindNotFound) {
		t.Fatalf("unknown kind: err = %v", err)
	}
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, "team", "engineering", PublishModeAuto); err == nil {
		t.Fatal("an unknown scope was accepted")
	}
	p, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeKind, "adr", PublishModeAfterReview)
	if err != nil || p.Mode != PublishModeAfterReview {
		t.Fatalf("set: %+v, %v", p, err)
	}
	if err := e.svc.DeletePublishPolicy(ctx, e.ws, PublishScopeKind, "adr"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeletePublishPolicy(ctx, e.ws, PublishScopeKind, "adr"); !errors.Is(err, ErrKnowledgePolicyNotFound) {
		t.Fatalf("second delete: err = %v", err)
	}
}

// policyFixture gives the workspace an author squad (the live agent), a critic
// squad of another agent, and a second agent in the author's squad.
type policyFixture struct {
	e                    *knowledgeEnv
	author, critic, peer string
	authorSquad          string
	criticSquad          string
}

func newPolicyFixture(t *testing.T) *policyFixture {
	e := newKnowledgeEnv(t).enabled()
	fx := testutil.New(e.pool, e.wsID, e.userID)
	critic := fx.Agent(t, "critic", "")
	peer := fx.Agent(t, "peer", "")
	authorSquad := fx.Squad(t, "Authors", e.liveAgent)
	fx.SquadMember(t, authorSquad, "agent", e.liveAgent)
	fx.SquadMember(t, authorSquad, "agent", peer)
	criticSquad := fx.Squad(t, "Critics", critic)
	fx.SquadMember(t, criticSquad, "agent", critic)
	return &policyFixture{e: e, author: e.liveAgent, critic: critic, peer: peer, authorSquad: authorSquad, criticSquad: criticSquad}
}

func (p *policyFixture) agent(id string) KnowledgeActor {
	return KnowledgeActor{Type: KnowledgeActorAgent, ID: mustUUID(p.e.t, id)}
}

func (p *policyFixture) draft(title string) KnowledgeDocumentView {
	p.e.t.Helper()
	return p.e.mustCreate(p.agent(p.author), KnowledgeDocumentInput{Kind: "adr", Title: title, Content: adrBody})
}

func TestAgentDraftFollowsItsPolicyMode(t *testing.T) {
	p := newPolicyFixture(t)
	e := p.e
	ctx := context.Background()
	gov := &recordingGovernance{}
	e.svc.Governance = gov

	// manual (the default): the draft waits for a person.
	if v := p.draft("Manual"); v.State != "draft" || len(gov.events) != 0 {
		t.Fatalf("manual: state %q, %d automatic publishes", v.State, len(gov.events))
	}

	// auto: the draft is published at once and the publish is recorded.
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "engineering", PublishModeAuto); err != nil {
		t.Fatal(err)
	}
	auto := p.draft("Auto")
	if auto.State != "published" {
		t.Fatalf("auto: state %q", auto.State)
	}
	if len(gov.events) != 1 || gov.events[0].Mode != PublishModeAuto || gov.events[0].KnowledgeBase != "engineering" {
		t.Fatalf("auto: governance events = %+v", gov.events)
	}
	if len(e.search("partitioned").Hits) == 0 {
		t.Fatal("an automatically published document is not searchable")
	}

	// auto_after_review: the draft waits for the critic.
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "engineering", PublishModeAfterReview); err != nil {
		t.Fatal(err)
	}
	if v := p.draft("Reviewed"); v.State != "draft" {
		t.Fatalf("auto_after_review: state %q, want draft", v.State)
	}
}

func TestKindPolicyOverridesTheBasePolicyForAgents(t *testing.T) {
	p := newPolicyFixture(t)
	e := p.e
	ctx := context.Background()
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "engineering", PublishModeAuto); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeKind, "adr", PublishModeManual); err != nil {
		t.Fatal(err)
	}
	if v := p.draft("Kind wins"); v.State != "draft" {
		t.Fatalf("the adr kind is manual but the document was %q", v.State)
	}
	// A kind with no row of its own follows the base.
	v := e.mustCreate(p.agent(p.author), KnowledgeDocumentInput{Kind: "runbook", Title: "Deploy", Content: "## Purpose\n\nx\n\n## Steps\n\ny\n\n## Rollback\n\nz\n\n## Owners\n\nw\n"})
	if v.State != "published" {
		t.Fatalf("runbook follows the auto base but was %q", v.State)
	}
}

func TestCriticFromAnotherSquadIsRequired(t *testing.T) {
	p := newPolicyFixture(t)
	e := p.e
	ctx := context.Background()
	gov := &recordingGovernance{}
	e.svc.Governance = gov
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "engineering", PublishModeAfterReview); err != nil {
		t.Fatal(err)
	}
	draft := p.draft("Needs a critic")
	linkID := mustUUID(t, draft.ID)

	// The author's own squad may not attest its work.
	if _, err := e.svc.ReviewDocument(ctx, e.ws, linkID, p.agent(p.peer), KnowledgeVerdictOK, ""); !errors.Is(err, ErrKnowledgeNotCritic) {
		t.Fatalf("a peer in the author's squad: err = %v, want ErrKnowledgeNotCritic", err)
	}
	// A member's approval is not a critic's attestation.
	if _, err := e.svc.ReviewDocument(ctx, e.ws, linkID, memberActor(e), KnowledgeVerdictOK, ""); !errors.Is(err, ErrKnowledgeNotCritic) {
		t.Fatalf("a member: err = %v, want ErrKnowledgeNotCritic", err)
	}
	if n := len(gov.events); n != 0 {
		t.Fatalf("refused reviews published %d documents", n)
	}

	// "changes" is recorded and does not publish.
	v, err := e.svc.ReviewDocument(ctx, e.ws, linkID, p.agent(p.critic), KnowledgeVerdictChanges, "cite the latency numbers")
	if err != nil || v.State != "draft" || v.ReviewVerdict != KnowledgeVerdictChanges {
		t.Fatalf("changes: %+v, %v", v, err)
	}

	// An "ok" from the other squad publishes it, and the governance log hears of it.
	v, err = e.svc.ReviewDocument(ctx, e.ws, linkID, p.agent(p.critic), KnowledgeVerdictOK, "")
	if err != nil || v.State != "published" {
		t.Fatalf("ok: %+v, %v", v, err)
	}
	if len(gov.events) != 1 || gov.events[0].Mode != PublishModeAfterReview ||
		gov.events[0].CriticSquadID != mustUUID(t, p.criticSquad) || gov.events[0].AuthorSquadID != mustUUID(t, p.authorSquad) {
		t.Fatalf("governance events = %+v", gov.events)
	}
}

func TestOkReviewWithoutAfterReviewPolicyOnlyRecordsTheVerdict(t *testing.T) {
	p := newPolicyFixture(t)
	draft := p.draft("Manual review")
	v, err := p.e.svc.ReviewDocument(context.Background(), p.e.ws, mustUUID(t, draft.ID), p.agent(p.critic), KnowledgeVerdictOK, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.State != "draft" || v.ReviewVerdict != KnowledgeVerdictOK {
		t.Fatalf("a manual workspace published on a review: %+v", v)
	}
}

func TestNoopGovernanceAcceptsEverything(t *testing.T) {
	if err := (NoopGovernance{}).RecordAutoPublish(context.Background(), AutoPublishEvent{}); err != nil {
		t.Fatal(err)
	}
}
