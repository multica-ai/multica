package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// policyBody carries every heading the policy kind requires.
const policyBody = "# Old policy\n\n## Scope\n\nx\n\n## Rule\n\nx\n\n## Owner\n\nx\n"

func (e *knowledgeEnv) reviewIssueCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title LIKE 'Review request:%'`, e.wsID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// K1: a due document owned by a squad gets one review request from that squad,
// and a second run does not repeat it. A document without an owning squad is
// requested from the Executive Company, or reported when there is none.
func TestFreshnessReviewRequestsOnceFromTheOwningSquad(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	squad := testutil.New(e.pool, e.wsID, e.userID).Squad(t, "Operations", e.liveAgent)
	past := pgtype.Date{Time: time.Now().AddDate(0, 0, -3), Valid: true}

	owned := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Owned policy", Kind: "policy",
		Publish: true, Content: policyBody, ValidUntil: past})
	if _, err := e.pool.Exec(ctx, `UPDATE knowledge_link SET squad_id = $1 WHERE id = $2`, mustUUID(t, squad), mustUUID(t, owned.ID)); err != nil {
		t.Fatal(err)
	}
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Unowned policy", Kind: "policy",
		Publish: true, Content: policyBody, ValidUntil: past})

	report, err := e.svc.FreshnessReview(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["review_due"] != 2 || report.Counts["review_requested"] != 1 || report.Counts["review_no_squad"] != 1 {
		t.Fatalf("first run counts = %v", report.Counts)
	}
	if n := e.reviewIssueCount(t); n != 1 {
		t.Fatalf("review requests after the first run = %d, want 1", n)
	}
	var assigneeType, assigneeID string
	if err := e.pool.QueryRow(ctx, `SELECT assignee_type, assignee_id::text FROM issue
		WHERE workspace_id = $1 AND title = 'Review request: Owned policy'`, e.wsID).Scan(&assigneeType, &assigneeID); err != nil {
		t.Fatal(err)
	}
	if assigneeType != "squad" || assigneeID != squad {
		t.Fatalf("request assigned to %s %s, want squad %s", assigneeType, assigneeID, squad)
	}
	var linked *string
	if err := e.pool.QueryRow(ctx, `SELECT review_issue_id::text FROM knowledge_link WHERE id = $1`, owned.ID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked == nil {
		t.Fatal("the document does not point at its review request")
	}

	again, err := e.svc.FreshnessReview(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if again.Counts["review_already_requested"] != 1 || again.Counts["review_requested"] != 0 {
		t.Fatalf("second run counts = %v, want the open request kept", again.Counts)
	}
	if n := e.reviewIssueCount(t); n != 1 {
		t.Fatalf("a second run duplicated the request: %d", n)
	}
}

// K1: a request is not repeated inside the window after it closed, and is
// repeated once the window has passed.
func TestFreshnessReviewWaitsOutTheWindowAfterAClosedRequest(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	squad := testutil.New(e.pool, e.wsID, e.userID).Squad(t, "Operations", e.liveAgent)
	past := pgtype.Date{Time: time.Now().AddDate(0, 0, -3), Valid: true}
	doc := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Closed policy", Kind: "policy",
		Publish: true, Content: policyBody, ValidUntil: past})
	if _, err := e.pool.Exec(ctx, `UPDATE knowledge_link SET squad_id = $1 WHERE id = $2`, mustUUID(t, squad), mustUUID(t, doc.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.FreshnessReview(ctx, e.ws, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE workspace_id = $1 AND title = 'Review request: Closed policy'`, e.wsID); err != nil {
		t.Fatal(err)
	}

	soon, err := e.svc.FreshnessReview(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if soon.Counts["review_requested"] != 0 {
		t.Fatalf("a closed request was repeated inside the window: %v", soon.Counts)
	}
	later, err := e.svc.FreshnessReview(ctx, e.ws, time.Now().AddDate(0, 0, 31))
	if err != nil {
		t.Fatal(err)
	}
	if later.Counts["review_requested"] != 1 || e.reviewIssueCount(t) != 2 {
		t.Fatalf("after the window counts = %v, requests = %d", later.Counts, e.reviewIssueCount(t))
	}
}

// K2: the audit repairs only orphaned link rows. A kind that is missing is
// reported and left alone.
func TestTaxonomyAuditRepairsOnlyOrphansNotMissingKinds(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	gone := e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	untyped := e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Untyped", Content: "no kind here", Publish: true})
	if err := e.weknoraWorkspace().Delete(ctx, gone.DocumentID); err != nil {
		t.Fatal(err)
	}
	report, err := e.svc.TaxonomyAudit(ctx, e.ws, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["repaired"] != 1 || report.Counts["missing_kind"] != 1 {
		t.Fatalf("counts = %v", report.Counts)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_link WHERE id = $1`, untyped.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the document with no kind was changed by the repair: %d, %v", n, err)
	}
}

// K2: a listing that stops at the page cap cannot tell a gone document from one
// past the cap, so the audit reports the orphan and repairs nothing.
func TestTaxonomyAuditRepairsNothingOnATruncatedListing(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	gone := e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	if err := e.weknoraWorkspace().Delete(ctx, gone.DocumentID); err != nil {
		t.Fatal(err)
	}
	// More than one page of documents in the market base, with the cap at one page.
	market, err := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e, "market"))
	if err != nil {
		t.Fatal(err)
	}
	ws := e.weknoraWorkspace()
	for i := 0; i < librarianListPageSize+1; i++ {
		if _, err := ws.CreateManual(ctx, market.WeknoraKbID, knowledge.ManualDocument{Title: fmt.Sprintf("Filler %d", i), Content: "x", Publish: true}); err != nil {
			t.Fatal(err)
		}
	}
	saved := librarianListMaxPages
	librarianListMaxPages = 1
	t.Cleanup(func() { librarianListMaxPages = saved })

	report, err := e.svc.TaxonomyAudit(ctx, e.ws, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Truncated || report.Counts["orphaned"] != 1 || report.Counts["repaired"] != 0 {
		t.Fatalf("truncated audit: truncated=%v counts=%v", report.Truncated, report.Counts)
	}
	if n := e.linkCount(); n != 1 {
		t.Fatalf("a partial audit repaired the registry: %d links, want 1", n)
	}
}

// Spec 3.3: a due document with no owning squad gets its request from the
// Executive Company, the Librarian's squad, so a draft update can follow. Without
// that squad the document is only reported (the test above).
func TestFreshnessReviewSendsAnUnownedDocumentToTheExecutiveCompany(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	exec := testutil.New(e.pool, e.wsID, e.userID).Squad(t, "Executive Office", e.liveAgent)
	past := pgtype.Date{Time: time.Now().AddDate(0, 0, -3), Valid: true}
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "company", Title: "Orphan policy", Kind: "policy",
		Publish: true, Content: policyBody, ValidUntil: past})

	report, err := e.svc.FreshnessReview(ctx, e.ws, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["review_requested"] != 1 || report.Counts["review_no_squad"] != 0 {
		t.Fatalf("counts = %v, want the unowned document requested", report.Counts)
	}
	var assigneeType, assigneeID, body string
	if err := e.pool.QueryRow(ctx, `SELECT assignee_type, COALESCE(assignee_id::text, ''), description FROM issue
		WHERE workspace_id = $1 AND title = 'Review request: Orphan policy'`, e.wsID).Scan(&assigneeType, &assigneeID, &body); err != nil {
		t.Fatal(err)
	}
	if assigneeType != "squad" || assigneeID != exec {
		t.Fatalf("request assigned to %s %s, want the Executive Office squad %s", assigneeType, assigneeID, exec)
	}
	if !strings.Contains(body, "No squad owns this document") {
		t.Fatalf("request body does not say the document is unowned: %q", body)
	}
}
