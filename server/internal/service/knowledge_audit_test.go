package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

// weknoraWorkspace is the tenant's API as the service sees it, for changes made
// behind Multica's back.
func (e *knowledgeEnv) weknoraWorkspace() knowledge.Workspace {
	e.t.Helper()
	row, err := e.q.GetWorkspaceKnowledge(context.Background(), e.ws)
	if err != nil {
		e.t.Fatal(err)
	}
	key, err := e.svc.Box.Open(row.ApiKeyCiphertext)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.fake.Workspace(string(key))
}

func TestDraftTriageFindsDuplicateTitles(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	first := e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Acme profile", Content: "acme sells anvils"})
	e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "  acme   PROFILE ", Content: "acme sells anvils again"})
	e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Other", Content: "unrelated"})

	report, err := e.svc.DraftTriage(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["new_drafts"] != 3 {
		t.Fatalf("new drafts = %d, want 3", report.Counts["new_drafts"])
	}
	if report.Counts["duplicate"] != 1 {
		t.Fatalf("duplicates = %d, want 1 (the older of the two)", report.Counts["duplicate"])
	}
	var dup LibrarianFinding
	for _, f := range report.Findings {
		if f.Reason == "duplicate" {
			dup = f
		}
	}
	if dup.LinkID != first.ID || dup.OtherLinkID == "" {
		t.Fatalf("duplicate finding = %+v, want the older draft pointing at its twin", dup)
	}
}

func TestFreshnessReviewFlagsDocumentsPastTheirInterval(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	// market_report reviews every 90 days.
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Q3 market", Kind: "market_report", Publish: true,
		Content: "# Q3 market\n\n## Summary\n\nx\n\n## Market size\n\nx\n\n## Trends\n\nx\n\n## Sources\n\nx\n"})
	// A document with a valid_until in the past.
	// Three days back, so no time zone makes it today.
	past := time.Now().AddDate(0, 0, -3)
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Old policy", Kind: "policy", Publish: true,
		Content:    "# Old policy\n\n## Scope\n\nx\n\n## Rule\n\nx\n\n## Owner\n\nx\n",
		ValidUntil: pgtype.Date{Time: past, Valid: true}})

	now, err := e.svc.FreshnessReview(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if now.Counts["review_due"] != 1 {
		t.Fatalf("today: review_due = %d, want 1 (the valid_until one)", now.Counts["review_due"])
	}

	later, err := e.svc.FreshnessReview(ctx, e.ws, time.Now().AddDate(0, 0, 91))
	if err != nil {
		t.Fatal(err)
	}
	if later.Counts["review_due"] != 2 {
		t.Fatalf("in 91 days: review_due = %d, want 2", later.Counts["review_due"])
	}
}

func TestFreshnessReviewSkipsSupersededDocuments(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	old := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Pricing", Kind: "reference", Publish: true,
		Content: "# Pricing\n\n## Summary\n\nold\n", ValidUntil: pgtype.Date{Time: time.Now().AddDate(0, 0, -3), Valid: true}})
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Pricing v2", Kind: "reference", Publish: true,
		Content: "# Pricing v2\n\n## Summary\n\nnew\n", SupersedesLinkID: mustUUID(t, old.ID)})
	report, err := e.svc.FreshnessReview(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["review_due"] != 0 {
		t.Fatalf("a superseded document was listed for review: %+v", report.Findings)
	}
}

func TestTaxonomyAuditFindsOrphansAndRepairsThemOnlyWhenAsked(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	gone := e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Untyped", Content: "no kind here", Publish: true})

	if err := e.weknoraWorkspace().Delete(ctx, gone.DocumentID); err != nil {
		t.Fatal(err)
	}

	report, err := e.svc.TaxonomyAudit(ctx, e.ws, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["orphaned"] != 1 || report.Counts["missing_kind"] != 1 {
		t.Fatalf("audit counts = %v", report.Counts)
	}
	if n := e.linkCount(); n != 2 {
		t.Fatalf("a dry audit changed the registry: %d links", n)
	}

	report, err = e.svc.TaxonomyAudit(ctx, e.ws, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["repaired"] != 1 {
		t.Fatalf("repaired = %d, want 1", report.Counts["repaired"])
	}
	if n := e.linkCount(); n != 1 {
		t.Fatalf("after repair %d links remain, want 1", n)
	}
}

func TestTaxonomyAuditRepairsNothingWhenWeKnoraIsDown(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	gone := e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	if err := e.weknoraWorkspace().Delete(context.Background(), gone.DocumentID); err != nil {
		t.Fatal(err)
	}
	e.fake.SetUnavailable(true)
	if _, err := e.svc.TaxonomyAudit(context.Background(), e.ws, time.Now(), true); err == nil {
		t.Fatal("an audit on a partial view did not fail")
	}
	e.fake.ClearFailures()
	if n := e.linkCount(); n != 1 {
		t.Fatalf("a failed audit changed the registry: %d links", n)
	}
}

func TestLibrarianBackfillRestoresKindsAndTheVenturesBase(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	// A workspace provisioned before phase 3 has neither kinds nor ventures.
	if _, err := e.pool.Exec(ctx, `DELETE FROM knowledge_kind WHERE workspace_id = $1`, e.wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM knowledge_base WHERE workspace_id = $1 AND key = 'ventures'`, e.wsID); err != nil {
		t.Fatal(err)
	}

	done, err := e.svc.RunLibrarian(ctx, LibrarianJobTaxonomyBackfill, time.Now())
	if err != nil || done != 1 {
		t.Fatalf("backfill: done %d, err %v", done, err)
	}
	kinds, _ := e.svc.ListKnowledgeKinds(ctx, e.ws)
	if len(kinds) != len(defaultKnowledgeKinds) {
		t.Fatalf("kinds after backfill = %d", len(kinds))
	}
	status, err := e.svc.Status(ctx, e.ws)
	if err != nil {
		t.Fatal(err)
	}
	var ventures bool
	for _, b := range status.KnowledgeBases {
		ventures = ventures || b.Key == KnowledgeVentureBaseKey
	}
	if !ventures {
		t.Fatal("the ventures base was not restored")
	}

	// A second pass changes nothing and reports the workspace done again.
	if done, err := e.svc.RunLibrarian(ctx, LibrarianJobTaxonomyBackfill, time.Now()); err != nil || done != 1 {
		t.Fatalf("second pass: done %d, err %v", done, err)
	}
}

func TestLibrarianReportsAreStoredAsTheLatestRun(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	if _, err := e.svc.RunLibrarian(ctx, LibrarianJobTriage, time.Now()); err != nil {
		t.Fatal(err)
	}
	jobs, err := e.svc.LatestHealth(ctx, e.ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Job != LibrarianJobTriage {
		t.Fatalf("health = %+v", jobs)
	}
}
