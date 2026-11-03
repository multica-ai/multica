package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const taxonomyADR = "# Storage\n\n## Context\n\nqueue latency grew.\n\n## Decision\n\nuse partitioned tables.\n\n## Consequences\n\nmigrations get longer.\n"

func TestKnowledgeDraftWithoutItsHeadingsAnswersWithTheMissingList(t *testing.T) {
	fx := newKnowledgeFixture(t)
	var body struct {
		Error           string   `json:"error"`
		Kind            string   `json:"knowledge_kind"`
		MissingSections []string `json:"missing_sections"`
	}
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kind": "adr", "title": "Storage", "content": "# Storage\n\n## Context\n\nx\n",
	}))).Want(http.StatusBadRequest).JSON(&body)
	if body.Kind != "adr" || strings.Join(body.MissingSections, ",") != "Decision,Consequences" || body.Error == "" {
		t.Fatalf("body = %+v", body)
	}

	var draft service.KnowledgeDocumentView
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kind": "adr", "title": "Storage", "content": taxonomyADR,
	}))).Want(http.StatusCreated).JSON(&draft)
	if draft.KnowledgeKind != "adr" || draft.KnowledgeBase != "engineering" || draft.State != "draft" {
		t.Fatalf("draft = %+v", draft)
	}
}

func TestKnowledgeKindsAreReadByAllAndEditedByAdmins(t *testing.T) {
	fx := newKnowledgeFixture(t)

	var list struct {
		Kinds []service.KnowledgeKindView `json:"kinds"`
	}
	testutil.Call(t, testHandler.ListKnowledgeKinds, newRequest("GET", "/api/knowledge/kinds", nil)).Want(http.StatusOK).JSON(&list)
	var adr *service.KnowledgeKindView
	for i := range list.Kinds {
		if list.Kinds[i].Key == "adr" {
			adr = &list.Kinds[i]
		}
	}
	if adr == nil || !strings.Contains(adr.Template, "## Consequences") {
		t.Fatalf("adr kind = %+v", adr)
	}

	// A member may not edit a kind.
	memberReq := withURLParam(newRequestAs(fx.member, "PATCH", "/api/knowledge/kinds/adr", map[string]any{"review_days": 30}), "key", "adr")
	testutil.Call(t, testHandler.UpdateKnowledgeKind, memberReq).Want(http.StatusForbidden)

	// An admin's template must keep the required headings.
	bad := withURLParam(newRequest("PATCH", "/api/knowledge/kinds/adr", map[string]any{"template": "# Only a title\n"}), "key", "adr")
	testutil.Call(t, testHandler.UpdateKnowledgeKind, bad).Want(http.StatusBadRequest)

	ok := withURLParam(newRequest("PATCH", "/api/knowledge/kinds/adr", map[string]any{"review_days": 365}), "key", "adr")
	var updated service.KnowledgeKindView
	testutil.Call(t, testHandler.UpdateKnowledgeKind, ok).Want(http.StatusOK).JSON(&updated)
	if updated.ReviewDays != 365 {
		t.Fatalf("review_days = %d", updated.ReviewDays)
	}

	// The default base must be a knowledge base of the workspace.
	noBase := withURLParam(newRequest("PATCH", "/api/knowledge/kinds/adr", map[string]any{"default_base_key": "nowhere"}), "key", "adr")
	testutil.Call(t, testHandler.UpdateKnowledgeKind, noBase).Want(http.StatusBadRequest)
	toProduct := withURLParam(newRequest("PATCH", "/api/knowledge/kinds/adr", map[string]any{"default_base_key": "product"}), "key", "adr")
	var rebased service.KnowledgeKindView
	testutil.Call(t, testHandler.UpdateKnowledgeKind, toProduct).Want(http.StatusOK).JSON(&rebased)
	if rebased.DefaultBaseKey != "product" {
		t.Fatalf("default_base_key = %q, want product", rebased.DefaultBaseKey)
	}
}

func TestKnowledgePoliciesAreSetByAdminsOnly(t *testing.T) {
	fx := newKnowledgeFixture(t)

	memberPut := newRequestAs(fx.member, "PUT", "/api/knowledge/policies", map[string]any{"scope": "base", "key": "engineering", "mode": "auto"})
	testutil.Call(t, testHandler.SetKnowledgePolicy, memberPut).Want(http.StatusForbidden)

	var saved service.PublishPolicyView
	testutil.Call(t, testHandler.SetKnowledgePolicy, newRequest("PUT", "/api/knowledge/policies",
		map[string]any{"scope": "base", "key": "engineering", "mode": "auto_after_review"})).Want(http.StatusOK).JSON(&saved)
	if saved.Mode != "auto_after_review" {
		t.Fatalf("saved = %+v", saved)
	}

	var list struct {
		Policies    []service.PublishPolicyView `json:"policies"`
		DefaultMode string                      `json:"default_mode"`
	}
	testutil.Call(t, testHandler.ListKnowledgePolicies, newRequest("GET", "/api/knowledge/policies", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Policies) != 1 || list.DefaultMode != "manual" {
		t.Fatalf("policies = %+v", list)
	}

	testutil.Call(t, testHandler.DeleteKnowledgePolicy, newRequest("DELETE", "/api/knowledge/policies?scope=base&key=engineering", nil)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.DeleteKnowledgePolicy, newRequest("DELETE", "/api/knowledge/policies?scope=base&key=engineering", nil)).Want(http.StatusNotFound)
}

func TestKnowledgeReviewComesFromAnotherSquad(t *testing.T) {
	fx := newKnowledgeFixture(t)
	var draft service.KnowledgeDocumentView
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kind": "adr", "title": "Storage", "content": taxonomyADR,
	}))).Want(http.StatusCreated).JSON(&draft)

	review := func(userID string) *http.Request {
		req := newRequestAs(userID, "POST", "/api/knowledge/documents/"+draft.ID+"/review", map[string]any{"verdict": "ok"})
		return withURLParam(req, "id", draft.ID)
	}
	testutil.Call(t, testHandler.ReviewKnowledgeDocument, review(fx.member)).Want(http.StatusForbidden)

	bad := withURLParam(newRequest("POST", "/api/knowledge/documents/"+draft.ID+"/review", map[string]any{"verdict": "maybe"}), "id", draft.ID)
	testutil.Call(t, testHandler.ReviewKnowledgeDocument, bad).Want(http.StatusBadRequest)
}

func TestKnowledgeEvalCasesAndRuns(t *testing.T) {
	fx := newKnowledgeFixture(t)

	var created service.EvalCaseView
	testutil.Call(t, testHandler.CreateKnowledgeEvalCase, newRequestAs(fx.member, "POST", "/api/knowledge/eval-cases",
		map[string]any{"question": "how do we store queues?", "expected_kind": "adr"})).Want(http.StatusCreated).JSON(&created)
	if created.ExpectedKind != "adr" || created.CreatedByType != "member" {
		t.Fatalf("case = %+v", created)
	}

	testutil.Call(t, testHandler.CreateKnowledgeEvalCase, newRequestAs(fx.member, "POST", "/api/knowledge/eval-cases",
		map[string]any{"question": "both", "expected_kind": "adr", "expected_document_id": "doc-1"})).Want(http.StatusBadRequest)

	var cases struct {
		Cases []service.EvalCaseView `json:"cases"`
	}
	testutil.Call(t, testHandler.ListKnowledgeEvalCases, newRequest("GET", "/api/knowledge/eval-cases", nil)).Want(http.StatusOK).JSON(&cases)
	if len(cases.Cases) != 1 {
		t.Fatalf("cases = %+v", cases.Cases)
	}

	var run service.EvalRunView
	testutil.Call(t, testHandler.RunKnowledgeEval, newRequest("POST", "/api/knowledge/eval/runs", nil)).Want(http.StatusOK).JSON(&run)
	if run.State != "ready" || run.Total != 1 || run.Hits != 0 {
		t.Fatalf("run = %+v", run)
	}

	memberDelete := withURLParam(newRequestAs(fx.member, "DELETE", "/api/knowledge/eval-cases/"+created.ID, nil), "id", created.ID)
	testutil.Call(t, testHandler.DeleteKnowledgeEvalCase, memberDelete).Want(http.StatusForbidden)
	adminDelete := withURLParam(newRequest("DELETE", "/api/knowledge/eval-cases/"+created.ID, nil), "id", created.ID)
	testutil.Call(t, testHandler.DeleteKnowledgeEvalCase, adminDelete).Want(http.StatusNoContent)
}

func TestKnowledgeHealthIsEmptyBeforeAnyRun(t *testing.T) {
	newKnowledgeFixture(t)
	var health struct {
		Jobs []service.HealthRunView `json:"jobs"`
	}
	testutil.Call(t, testHandler.GetKnowledgeHealth, newRequest("GET", "/api/knowledge/health", nil)).Want(http.StatusOK).JSON(&health)
	if health.Jobs == nil || len(health.Jobs) != 0 {
		t.Fatalf("health = %+v", health.Jobs)
	}
}
