package workflows

import (
	"context"
	"errors"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEvaluate_EmptyConditionsAlwaysMatch(t *testing.T) {
	if !evaluate(nil, map[string]any{}) {
		t.Fatal("empty conditions must match")
	}
}

func TestEvaluate_DottedFieldLookup(t *testing.T) {
	ctx := map[string]any{
		"issue": map[string]any{
			"priority": "high",
			"project_id": "p-1",
		},
	}
	conds := []Condition{{Field: "issue.priority", Op: "eq", Value: "high"}}
	if !evaluate(conds, ctx) {
		t.Fatal("eq high should match")
	}
	conds[0].Value = "low"
	if evaluate(conds, ctx) {
		t.Fatal("eq low should not match")
	}
}

func TestEvaluate_InOperator(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{"priority": "urgent"}}
	conds := []Condition{{
		Field:  "issue.priority",
		Op:     "in",
		Values: []any{"urgent", "high"},
	}}
	if !evaluate(conds, ctx) {
		t.Fatal("'urgent' should be in [urgent, high]")
	}
	conds[0].Values = []any{"low", "medium"}
	if evaluate(conds, ctx) {
		t.Fatal("'urgent' should not be in [low, medium]")
	}
}

func TestEvaluate_NullChecks(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{"assignee_id": nil}}
	isNull := []Condition{{Field: "issue.assignee_id", Op: "is_null"}}
	if !evaluate(isNull, ctx) {
		t.Fatal("explicit nil should satisfy is_null")
	}
	isNotNull := []Condition{{Field: "issue.assignee_id", Op: "is_not_null"}}
	if evaluate(isNotNull, ctx) {
		t.Fatal("explicit nil should fail is_not_null")
	}
}

func TestEvaluate_UnknownOperatorFailsClosed(t *testing.T) {
	ctx := map[string]any{"x": "y"}
	conds := []Condition{{Field: "x", Op: "regex", Value: ".*"}}
	if evaluate(conds, ctx) {
		t.Fatal("unknown operator must fail closed")
	}
}

func TestEvaluate_AllMustHold(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{"priority": "high", "status": "todo"}}
	conds := []Condition{
		{Field: "issue.priority", Op: "eq", Value: "high"},
		{Field: "issue.status", Op: "eq", Value: "in_progress"},
	}
	if evaluate(conds, ctx) {
		t.Fatal("AND semantics: second condition must fail the whole match")
	}
}

func TestEvaluate_MissingFieldFailsEquality(t *testing.T) {
	ctx := map[string]any{"issue": map[string]any{}}
	conds := []Condition{{Field: "issue.priority", Op: "eq", Value: "high"}}
	if evaluate(conds, ctx) {
		t.Fatal("missing field must not satisfy eq")
	}
}

func evidenceCondition(value any) Condition {
	return Condition{Op: OpEvidencePresent, Value: value}
}

func TestEvaluateEvidence_DefaultsToPRURLAnyOf(t *testing.T) {
	fake := &fakeIssueActions{
		Comments: []db.Comment{
			{Content: "still working on it"},
			{Content: "PR up: https://github.com/firtal-group/foo/pull/42"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(nil))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("expected PR URL in comments to satisfy default require=[pr_url]")
	}
}

func TestEvaluateEvidence_PRURLFromIssueDescription(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	te := testTriggerEvent()
	if issue, ok := te.Raw["issue"].(map[string]any); ok {
		issue["description"] = "see https://github.com/example/repo/pull/7 for the change"
	}

	ok, err := svc.evaluateEvidence(context.Background(), wf, te, evidenceCondition(EvidenceConfig{}))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("expected PR URL in description to satisfy default require")
	}
}

func TestEvaluateEvidence_NoEvidenceReturnsFalse(t *testing.T) {
	fake := &fakeIssueActions{
		Comments: []db.Comment{{Content: "just a status update"}},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(nil))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if ok {
		t.Fatal("expected no PR URL → false (not error)")
	}
}

func TestEvaluateEvidence_RecognisesGitLabMR(t *testing.T) {
	fake := &fakeIssueActions{
		Comments: []db.Comment{
			{Content: "MR up: https://gitlab.example.com/team/foo/-/merge_requests/12"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(nil))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("expected GitLab MR URL to count as PR evidence")
	}
}

func TestEvaluateEvidence_PRPatternRejectsLooseMatches(t *testing.T) {
	fake := &fakeIssueActions{
		Comments: []db.Comment{
			// /pull/ but no numeric ID — must not match.
			{Content: "see https://example.com/pull/something"},
			// Word "pull" without URL — must not match.
			{Content: "I'll pull a new branch"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	ok, _ := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(nil))
	if ok {
		t.Fatal("loose 'pull' references must not match the PR URL pattern")
	}
}

func TestEvaluateEvidence_ImageAttachmentByContentType(t *testing.T) {
	fake := &fakeIssueActions{
		IssueAttachments: []db.Attachment{
			{ContentType: "application/pdf"},
			{ContentType: "image/png"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	cfg := EvidenceConfig{Require: []string{EvidenceKindImageAttachment}}
	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(cfg))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("expected image/png attachment to satisfy image_attachment require")
	}
}

func TestEvaluateEvidence_ImageAttachmentByURLSuffix(t *testing.T) {
	fake := &fakeIssueActions{
		// Issue has no direct attachments, but a comment-bound attachment
		// URL ends in .png — still counts as image evidence.
		AttachmentURLs: []string{
			"/uploads/workspaces/x/abc.pdf",
			"/uploads/workspaces/x/screenshot.png",
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	cfg := EvidenceConfig{Require: []string{EvidenceKindImageAttachment}}
	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(cfg))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("expected .png URL suffix to satisfy image_attachment require")
	}
}

func TestEvaluateEvidence_URLRegexRequiresPattern(t *testing.T) {
	svc := newServiceWithFake(&fakeIssueActions{})
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	cfg := EvidenceConfig{Require: []string{EvidenceKindURLRegex}} // url_regex empty
	_, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(cfg))
	if err == nil {
		t.Fatal("expected error when url_regex is required but missing")
	}
}

func TestEvaluateEvidence_URLRegexMatchesComment(t *testing.T) {
	fake := &fakeIssueActions{
		Comments: []db.Comment{
			{Content: "deployed to https://staging.example.com/build/9912"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	cfg := EvidenceConfig{
		Require:  []string{EvidenceKindURLRegex},
		URLRegex: `https://staging\.example\.com/build/\d+`,
	}
	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(cfg))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("expected staging URL to match user regex")
	}
}

func TestEvaluateEvidence_AnyOfSemanticsAcrossKinds(t *testing.T) {
	// require=[pr_url, image_attachment]: image alone is enough.
	fake := &fakeIssueActions{
		Comments:         []db.Comment{{Content: "no PR yet, see image"}},
		IssueAttachments: []db.Attachment{{ContentType: "image/jpeg"}},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	cfg := EvidenceConfig{Require: []string{EvidenceKindPRURL, EvidenceKindImageAttachment}}
	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(cfg))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if !ok {
		t.Fatal("any-of semantics: an image alone should satisfy [pr_url|image_attachment]")
	}
}

func TestEvaluateEvidence_UnknownRequireValuesIgnored(t *testing.T) {
	fake := &fakeIssueActions{}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	cfg := EvidenceConfig{Require: []string{"ci_run_url"}} // not implemented yet
	ok, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(cfg))
	if err != nil {
		t.Fatalf("evaluateEvidence returned %v", err)
	}
	if ok {
		t.Fatal("unknown require values should not silently match")
	}
}

func TestEvaluateEvidence_ListCommentsErrorPropagates(t *testing.T) {
	fake := &fakeIssueActions{ListCommentsErr: errors.New("db down")}
	svc := newServiceWithFake(fake)
	wf := workflow{id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}

	_, err := svc.evaluateEvidence(context.Background(), wf, testTriggerEvent(), evidenceCondition(nil))
	if err == nil {
		t.Fatal("expected DB error to propagate so conditionsHold fails closed")
	}
}

func TestConditionsHold_EvidencePresentShortCircuitsActionRun(t *testing.T) {
	fake := &fakeIssueActions{Comments: []db.Comment{{Content: "no signal"}}}
	svc := newServiceWithFake(fake)
	wf := workflow{
		id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		conditions: []byte(`[
			{"op": "evidence_present", "value": {"require": ["pr_url"]}}
		]`),
	}
	if svc.conditionsHold(context.Background(), wf, testTriggerEvent()) {
		t.Fatal("expected evidence-missing condition to skip the action")
	}
}

func TestConditionsHold_MixedEvidenceAndPureConditions(t *testing.T) {
	fake := &fakeIssueActions{
		Comments: []db.Comment{
			{Content: "PR up: https://github.com/x/y/pull/1"},
		},
	}
	svc := newServiceWithFake(fake)
	wf := workflow{
		id: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		conditions: []byte(`[
			{"op": "evidence_present"},
			{"field": "issue.status", "op": "eq", "value": "in_review"}
		]`),
	}
	if !svc.conditionsHold(context.Background(), wf, testTriggerEvent()) {
		t.Fatal("expected both evidence and pure condition to hold")
	}
}
