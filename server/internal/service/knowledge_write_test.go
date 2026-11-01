package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/knowledge"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	memberActor = func(e *knowledgeEnv) KnowledgeActor {
		return KnowledgeActor{Type: KnowledgeActorMember, ID: e.user}
	}
	agentActor = func(e *knowledgeEnv) KnowledgeActor {
		return KnowledgeActor{Type: KnowledgeActorAgent, ID: mustUUID(e.t, e.liveAgent)}
	}
)

func (e *knowledgeEnv) enabled() *knowledgeEnv {
	e.t.Helper()
	if _, err := e.enable(); err != nil {
		e.t.Fatalf("enable: %v", err)
	}
	return e
}

func (e *knowledgeEnv) create(actor KnowledgeActor, in KnowledgeDocumentInput) (KnowledgeDocumentView, bool, error) {
	return e.svc.CreateDocument(context.Background(), e.ws, actor, in)
}

func (e *knowledgeEnv) mustCreate(actor KnowledgeActor, in KnowledgeDocumentInput) KnowledgeDocumentView {
	e.t.Helper()
	v, _, err := e.create(actor, in)
	if err != nil {
		e.t.Fatalf("create: %v", err)
	}
	return v
}

func (e *knowledgeEnv) search(q string) KnowledgeSearchResult {
	e.t.Helper()
	r, err := e.svc.Search(context.Background(), e.ws, q, "", 0)
	if err != nil {
		e.t.Fatalf("search: %v", err)
	}
	return r
}

func (e *knowledgeEnv) linkCount() int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_link WHERE workspace_id = $1`, e.wsID).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestKnowledgeAgentWritesADraftThatSearchCannotSeeUntilPublished(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()

	// An agent asking to publish is still a draft.
	v := e.mustCreate(agentActor(e), KnowledgeDocumentInput{
		KB: "market", Title: "Acme profile", Content: "Acme sells anvils to coyotes.", Publish: true,
	})
	if v.State != "draft" || v.KnowledgeBase != "market" || v.CreatedByType != "agent" || v.PublishedAt != nil || v.Kind != "manual" {
		t.Fatalf("draft view = %+v", v)
	}
	if len(e.search("anvils").Hits) != 0 {
		t.Fatal("a draft must not be searchable")
	}

	pub, err := e.svc.PublishDocument(ctx, e.ws, mustUUID(t, v.ID))
	if err != nil {
		t.Fatal(err)
	}
	if pub.State != "published" || pub.PublishedAt == nil || pub.ID != v.ID {
		t.Fatalf("published view = %+v", pub)
	}
	hits := e.search("anvils").Hits
	if len(hits) != 1 || hits[0].KnowledgeBase != "market" || hits[0].Title != "Acme profile" {
		t.Fatalf("hits = %+v", hits)
	}

	// Publishing again changes nothing and calls nothing.
	before := e.fake.Calls(knowledge.OpPublish)
	if _, err := e.svc.PublishDocument(ctx, e.ws, mustUUID(t, v.ID)); err != nil {
		t.Fatal(err)
	}
	if e.fake.Calls(knowledge.OpPublish) != before {
		t.Fatal("publishing a published document must not call the sidecar")
	}
}

func TestKnowledgeMemberPublishesAtOnceAgentsNever(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	m := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Charter", Content: "We ship weekly.", Publish: true})
	if m.State != "published" || m.PublishedAt == nil {
		t.Fatalf("member publish = %+v", m)
	}
	if len(e.search("weekly").Hits) != 1 {
		t.Fatal("a member's published document must be searchable")
	}
	a := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "company", Title: "Note", Content: "Quarterly planning.", Publish: true})
	if a.State != "draft" {
		t.Fatalf("agent publish must be ignored, got %q", a.State)
	}
	if _, _, err := e.create(agentActor(e), KnowledgeDocumentInput{KB: "decisions", Title: "Fake decision", Content: "x"}); !errors.Is(err, ErrKnowledgeForbidden) {
		t.Fatalf("agent write to decisions = %v, want ErrKnowledgeForbidden", err)
	}
}

func TestKnowledgeIdempotencyKeyWritesOnce(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	in := KnowledgeDocumentInput{KB: "market", Title: "Once", Content: "Only once.", IdempotencyKey: "issue:42:finding"}
	first, created, err := e.create(agentActor(e), in)
	if err != nil || !created {
		t.Fatalf("first = %v created=%v", err, created)
	}
	second, created, err := e.create(agentActor(e), in)
	if err != nil || created {
		t.Fatalf("retry = %v created=%v, want the first write back", err, created)
	}
	if second.ID != first.ID || e.fake.Calls(knowledge.OpCreateManual) != 1 || e.linkCount() != 1 {
		t.Fatalf("retry duplicated: ids %s/%s, %d CreateManual calls, %d links", first.ID, second.ID, e.fake.Calls(knowledge.OpCreateManual), e.linkCount())
	}
	// The key is per workspace, not per base.
	other := in
	other.KB = "product"
	if v, created, err := e.create(agentActor(e), other); err != nil || created || v.ID != first.ID {
		t.Fatalf("same key in another base = %+v created=%v err=%v", v, created, err)
	}
}

func TestKnowledgeURLIsFetchedOnlyAfterPublish(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	v := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Competitor site", URL: "https://example.test/pricing"})
	if v.State != "draft" || v.Kind != "url" || v.URL != "https://example.test/pricing" || v.SourceType != "url" || v.DocumentID != "" {
		t.Fatalf("url draft = %+v", v)
	}
	if e.fake.Calls(knowledge.OpCreateURL) != 0 || e.fake.Calls(knowledge.OpCreateManual) != 0 {
		t.Fatal("a URL draft must not reach the sidecar before publish")
	}
	pub, err := e.svc.PublishDocument(ctx, e.ws, mustUUID(t, v.ID))
	if err != nil {
		t.Fatal(err)
	}
	if pub.State != "published" || pub.DocumentID == "" || e.fake.Calls(knowledge.OpCreateURL) != 1 {
		t.Fatalf("published url = %+v, %d fetches", pub, e.fake.Calls(knowledge.OpCreateURL))
	}

	// Discarding a URL draft needs no sidecar at all.
	d := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Other", URL: "https://example.test/other"})
	e.fake.SetUnavailable(true)
	if err := e.svc.DiscardDocument(ctx, e.ws, mustUUID(t, d.ID)); err != nil {
		t.Fatalf("discard url draft while offline: %v", err)
	}
}

func TestKnowledgeDiscardAndDelete(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	draft := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Wrong", Content: "A wrong claim."})
	if err := e.svc.DiscardDocument(ctx, e.ws, mustUUID(t, draft.ID)); err != nil {
		t.Fatal(err)
	}
	if e.linkCount() != 0 || e.fake.Calls(knowledge.OpDelete) != 1 {
		t.Fatalf("discard left %d links, %d deletes", e.linkCount(), e.fake.Calls(knowledge.OpDelete))
	}
	if err := e.svc.DiscardDocument(ctx, e.ws, mustUUID(t, draft.ID)); !errors.Is(err, ErrKnowledgeDocumentNotFound) {
		t.Fatalf("discard twice = %v", err)
	}

	live := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Right", Content: "A right claim.", Publish: true})
	var conflict *KnowledgeConflictError
	if err := e.svc.DiscardDocument(ctx, e.ws, mustUUID(t, live.ID)); !errors.As(err, &conflict) {
		t.Fatalf("discard published = %v, want a conflict", err)
	}
	if err := e.svc.DeleteDocument(ctx, e.ws, mustUUID(t, live.ID)); err != nil {
		t.Fatal(err)
	}
	if len(e.search("right").Hits) != 0 || e.linkCount() != 0 {
		t.Fatal("delete must remove the document everywhere")
	}
}

func TestKnowledgeOfflineWritesAndRetry(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	e.fake.SetUnavailable(true)

	// A person's write is not recorded: they retry it themselves.
	_, _, err := e.create(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Lost", Content: "never stored", IdempotencyKey: "k-agent"})
	if !errors.Is(err, knowledge.ErrUnavailable) || e.linkCount() != 0 {
		t.Fatalf("agent write offline = %v, links=%d", err, e.linkCount())
	}

	// The system's write is kept as a failed row with its content.
	rec := DecisionRecord{ProposalID: mustUUID(t, e.wsID), Title: "Go to Asia", Squad: "Research", Recommendation: "Go.", Decision: "approved", Note: "Yes.", DecidedAt: testNow()}
	if err := e.svc.WriteDecisionRecord(ctx, e.ws, rec); !errors.Is(err, knowledge.ErrUnavailable) {
		t.Fatalf("write decision offline = %v", err)
	}
	var state, pending string
	if err := e.pool.QueryRow(ctx, `SELECT state, pending_content FROM knowledge_link WHERE workspace_id = $1`, e.wsID).Scan(&state, &pending); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || !strings.Contains(pending, "Go to Asia") {
		t.Fatalf("failed row = %q %q", state, pending)
	}

	// Back online, the next write for the workspace finishes it.
	e.fake.ClearFailures()
	e.svc.RetryFailed(ctx, e.ws)
	hits := e.search("Asia").Hits
	if len(hits) != 1 {
		t.Fatalf("retried decision not searchable: %+v", hits)
	}
	if err := e.pool.QueryRow(ctx, `SELECT state, pending_content FROM knowledge_link WHERE workspace_id = $1`, e.wsID).Scan(&state, &pending); err != nil {
		t.Fatal(err)
	}
	if state != "published" || pending != "" {
		t.Fatalf("after retry = %q %q", state, pending)
	}
	if err := e.svc.WriteDecisionRecord(ctx, e.ws, rec); err != nil || e.fake.Calls(knowledge.OpCreateManual) != 3 {
		// 1 failed attempt, 1 retry, and the repeat must not add another.
		t.Fatalf("repeat after retry: err=%v calls=%d", err, e.fake.Calls(knowledge.OpCreateManual))
	}
}

func TestKnowledgeDecisionAndBriefWritersAreIdempotent(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	proposal := mustUUID(t, e.liveAgent) // any uuid
	rec := DecisionRecord{ProposalID: proposal, Title: "Hire a researcher", Squad: "Research", Recommendation: "Hire one.", Decision: "rejected", Note: "Not this quarter.", DecidedAt: testNow(), Link: "https://app.test/acme/issues/ACM-7"}
	for i := 0; i < 3; i++ {
		if err := e.svc.WriteDecisionRecord(ctx, e.ws, rec); err != nil {
			t.Fatal(err)
		}
	}
	brief := mustUUID(t, e.oldAgent)
	for i := 0; i < 2; i++ {
		if err := e.svc.WriteBriefArchive(ctx, e.ws, brief, "Daily brief 2026-11-01", "Three things happened."); err != nil {
			t.Fatal(err)
		}
	}
	if e.fake.Calls(knowledge.OpCreateManual) != 2 || e.linkCount() != 2 {
		t.Fatalf("expected one decision and one brief, got %d creates, %d links", e.fake.Calls(knowledge.OpCreateManual), e.linkCount())
	}
	rows, err := e.pool.Query(ctx, `SELECT idempotency_key, state, source_type, created_by_type FROM knowledge_link WHERE workspace_id = $1 ORDER BY idempotency_key`, e.wsID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var k, st, src, by string
		if err := rows.Scan(&k, &st, &src, &by); err != nil {
			t.Fatal(err)
		}
		got = append(got, k+"|"+st+"|"+src+"|"+by)
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "brief:") || !strings.HasSuffix(got[0], "|published|brief|system") ||
		!strings.HasPrefix(got[1], "proposal:") || !strings.HasSuffix(got[1], ":decision|published|proposal|system") {
		t.Fatalf("links = %v", got)
	}

	md := rec.Markdown()
	for _, s := range []string{"# Decision: Hire a researcher", "Squad: Research", "Decision: rejected", "Date: 2026-11-01", "ACM-7", "Hire one.", "Not this quarter."} {
		if !strings.Contains(md, s) {
			t.Errorf("decision markdown lacks %q:\n%s", s, md)
		}
	}
	if hits := e.search("quarter").Hits; len(hits) != 1 || hits[0].KnowledgeBase != "decisions" {
		t.Fatalf("decision not retrievable from decisions: %+v", hits)
	}
}

func TestKnowledgeWritersSkipAWorkspaceWithoutKnowledge(t *testing.T) {
	e := newKnowledgeEnv(t) // not enabled
	ctx := context.Background()
	if err := e.svc.WriteDecisionRecord(ctx, e.ws, DecisionRecord{ProposalID: e.user, Title: "x", Decision: "approved", DecidedAt: testNow()}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.WriteBriefArchive(ctx, e.ws, e.user, "b", "body"); err != nil {
		t.Fatal(err)
	}
	if e.linkCount() != 0 || e.fake.Calls(knowledge.OpCreateManual) != 0 {
		t.Fatal("nothing may be written for a workspace that did not enable knowledge")
	}
	var nilSvc *KnowledgeService
	if err := nilSvc.WriteBriefArchive(ctx, e.ws, e.user, "b", "body"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.create(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "t", Content: "c"}); err == nil {
		t.Fatal("create before enable must fail")
	} else {
		var nr *KnowledgeNotReadyError
		if !errors.As(err, &nr) || nr.State != KnowledgeStateDisabled {
			t.Fatalf("create before enable = %v", err)
		}
	}
}

func TestKnowledgeListAndSearchStates(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	draft := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Draft one", Content: "draft text"})
	pub := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Published one", Content: "published text", Publish: true})

	// A document that exists only in WeKnora (added there) is listed too.
	base, err := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e, "market"))
	if err != nil {
		t.Fatal(err)
	}
	ws := e.fake.Workspace(e.openKey())
	if _, err := ws.CreateManual(ctx, base.WeknoraKbID, knowledge.ManualDocument{Title: "Added in WeKnora", Content: "outside text", Publish: true}); err != nil {
		t.Fatal(err)
	}

	list, err := e.svc.ListDocuments(ctx, e.ws, "market", "")
	if err != nil || list.State != "ready" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	titles := map[string]string{}
	for _, d := range list.Documents {
		titles[d.Title] = d.State + "/" + d.ID
	}
	if len(list.Documents) != 3 || titles["Draft one"] != "draft/"+draft.ID || titles["Published one"] != "published/"+pub.ID || !strings.HasPrefix(titles["Added in WeKnora"], "published/") {
		t.Fatalf("documents = %+v", list.Documents)
	}
	if drafts, _ := e.svc.ListDocuments(ctx, e.ws, "", "draft"); len(drafts.Documents) != 1 || drafts.Documents[0].Title != "Draft one" {
		t.Fatalf("drafts = %+v", drafts.Documents)
	}
	if _, err := e.svc.ListDocuments(ctx, e.ws, "nope", ""); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("unknown kb = %v", err)
	}
	var invalid *KnowledgeInvalidError
	if _, err := e.svc.ListDocuments(ctx, e.ws, "", "weird"); !errors.As(err, &invalid) {
		t.Fatalf("bad state = %v", err)
	}

	if r, err := e.svc.Search(ctx, e.ws, "published", "product", 0); err != nil || len(r.Hits) != 0 {
		t.Fatalf("search scoped to another base = %+v %v", r, err)
	}
	if _, err := e.svc.Search(ctx, e.ws, "  ", "", 0); !errors.As(err, &invalid) {
		t.Fatalf("empty query = %v", err)
	}

	// Offline: reads degrade to the state with Multica's own records.
	e.fake.SetUnavailable(true)
	r, err := e.svc.Search(ctx, e.ws, "published", "", 0)
	if err != nil || r.State != "offline" || len(r.Hits) != 0 {
		t.Fatalf("offline search = %+v %v", r, err)
	}
	list, err = e.svc.ListDocuments(ctx, e.ws, "", "")
	if err != nil || list.State != "offline" || len(list.Documents) != 2 {
		t.Fatalf("offline list = %+v %v", list, err)
	}
}

func TestKnowledgeCreateValidation(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	cases := map[string]KnowledgeDocumentInput{
		"no kb":         {Title: "t", Content: "c"},
		"no title":      {KB: "market", Content: "c"},
		"neither":       {KB: "market", Title: "t"},
		"both":          {KB: "market", Title: "t", Content: "c", URL: "https://example.test"},
		"bad scheme":    {KB: "market", Title: "t", URL: "file:///etc/passwd"},
		"bad source":    {KB: "market", Title: "t", Content: "c", SourceType: "email"},
		"long title":    {KB: "market", Title: strings.Repeat("a", 201), Content: "c"},
		"huge content":  {KB: "market", Title: "t", Content: strings.Repeat("a", knowledgeMaxContent+1)},
		"url no host":   {KB: "market", Title: "t", URL: "https://"},
		"javascript":    {KB: "market", Title: "t", URL: "javascript:alert(1)"},
		"whitespace":    {KB: "market", Title: "  ", Content: "c"},
		"blank content": {KB: "market", Title: "t", Content: "   "},
	}
	for name, in := range cases {
		var invalid *KnowledgeInvalidError
		if _, _, err := e.create(memberActor(e), in); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want KnowledgeInvalidError", name, err)
		}
	}
	if _, _, err := e.create(memberActor(e), KnowledgeDocumentInput{KB: "nope", Title: "t", Content: "c"}); !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Errorf("unknown kb = %v", err)
	}
	if e.linkCount() != 0 {
		t.Errorf("rejected writes left %d links", e.linkCount())
	}
}

func testNow() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }

func dbBaseKey(e *knowledgeEnv, key string) db.GetKnowledgeBaseByKeyParams {
	return db.GetKnowledgeBaseByKeyParams{WorkspaceID: e.ws, Key: key}
}
