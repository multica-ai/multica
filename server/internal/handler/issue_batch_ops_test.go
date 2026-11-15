package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type batchUpdateTestResponse struct {
	Updated int                `json:"updated"`
	DryRun  bool               `json:"dry_run"`
	Results []batchIssueResult `json:"results"`
}

func batchUpdate(t *testing.T, body map[string]any) (int, batchUpdateTestResponse, string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest("POST", "/api/issues/batch-update", body))
	var resp batchUpdateTestResponse
	raw := w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode batch response: %v (%s)", err, raw)
		}
	}
	return w.Code, resp, raw
}

func resultFor(t *testing.T, resp batchUpdateTestResponse, issueID string) batchIssueResult {
	t.Helper()
	for _, r := range resp.Results {
		if r.IssueID == issueID {
			return r
		}
	}
	t.Fatalf("no result for issue %s in %+v", issueID, resp.Results)
	return batchIssueResult{}
}

func issueLabelSet(t *testing.T, issueID string) map[string]bool {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT label_id::text FROM issue_to_label WHERE issue_id = $1`, issueID)
	if err != nil {
		t.Fatalf("list issue labels: %v", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan label: %v", err)
		}
		set[id] = true
	}
	return set
}

func issuePropertyValue(t *testing.T, issueID, propertyID string) (string, bool) {
	t.Helper()
	var value *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT properties ->> $2 FROM issue WHERE id = $1`, issueID, propertyID,
	).Scan(&value); err != nil {
		t.Fatalf("read property: %v", err)
	}
	if value == nil {
		return "", false
	}
	return *value, true
}

func setIssueParent(t *testing.T, issueID, parentID string) {
	t.Helper()
	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids": []string{issueID},
		"updates":   map[string]any{"parent_issue_id": parentID},
	})
	if code != http.StatusOK || resp.Updated != 1 {
		t.Fatalf("set parent: %d %s", code, raw)
	}
}

// A batch that cannot apply to every issue reports, per issue, which ones it
// skipped and why, instead of only a short count. (MUL-8882)
func TestBatchUpdateReportsSkippedIssues(t *testing.T) {
	root := createTestIssue(t, "BU-skip root", "todo", "low")
	child := createTestIssue(t, "BU-skip child", "todo", "low")
	other := createTestIssue(t, "BU-skip other", "todo", "low")
	for _, id := range []string{child, other, root} {
		id := id
		t.Cleanup(func() { deleteTestIssue(t, id) })
	}
	setIssueParent(t, child, root)

	missing := uuid.NewString()
	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids": []string{root, child, other, "not-a-uuid", missing, other},
		"updates":   map[string]any{"parent_issue_id": child},
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if resp.Updated != 1 {
		t.Fatalf("expected updated=1, got %d: %s", resp.Updated, raw)
	}
	// The repeated id is reported once.
	if len(resp.Results) != 5 {
		t.Fatalf("expected 5 results, got %d: %s", len(resp.Results), raw)
	}
	want := map[string]string{root: "parent_cycle", child: "parent_self", "not-a-uuid": "invalid_id", missing: "not_found"}
	for id, code := range want {
		got := resultFor(t, resp, id)
		if got.Outcome != batchOutcomeSkipped || got.Code != code || got.Message == "" {
			t.Errorf("issue %s: want skipped/%s with a message, got %+v", id, code, got)
		}
	}
	if got := resultFor(t, resp, other); got.Outcome != batchOutcomeUpdated || got.Identifier == "" {
		t.Errorf("other: want updated with identifier, got %+v", got)
	}
}

func TestBatchUpdateLabelOperations(t *testing.T) {
	a := createTestIssue(t, "BU-labels A", "todo", "low")
	b := createTestIssue(t, "BU-labels B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })
	suffix := uuid.NewString()[:8]
	keep := createTestIssueLabel(t, "bu-keep-"+suffix)
	drop := createTestIssueLabel(t, "bu-drop-"+suffix)
	add := createTestIssueLabel(t, "bu-add-"+suffix)

	code, _, raw := batchUpdate(t, map[string]any{
		"issue_ids": []string{a},
		"labels":    map[string]any{"add": []string{keep, drop}},
	})
	if code != http.StatusOK {
		t.Fatalf("seed labels: %d %s", code, raw)
	}

	// Add and remove leave every other label alone.
	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids": []string{a, b},
		"labels":    map[string]any{"add": []string{add}, "remove": []string{drop}},
	})
	if code != http.StatusOK || resp.Updated != 2 {
		t.Fatalf("add/remove: %d %s", code, raw)
	}
	if got := issueLabelSet(t, a); !got[keep] || !got[add] || got[drop] || len(got) != 2 {
		t.Errorf("issue A labels after add/remove: %v", got)
	}
	if got := issueLabelSet(t, b); !got[add] || len(got) != 1 {
		t.Errorf("issue B labels after add/remove: %v", got)
	}

	// Repeating the same operation changes nothing and says so.
	_, resp, raw = batchUpdate(t, map[string]any{
		"issue_ids": []string{a},
		"labels":    map[string]any{"add": []string{add}},
	})
	if got := resultFor(t, resp, a); got.Outcome != batchOutcomeUnchanged || resp.Updated != 1 {
		t.Errorf("repeat add: want unchanged and counted, got %+v (%s)", got, raw)
	}

	// clear + add replaces the set.
	code, _, raw = batchUpdate(t, map[string]any{
		"issue_ids": []string{a, b},
		"labels":    map[string]any{"clear": true, "add": []string{drop}},
	})
	if code != http.StatusOK {
		t.Fatalf("clear+add: %d %s", code, raw)
	}
	for _, id := range []string{a, b} {
		if got := issueLabelSet(t, id); !got[drop] || len(got) != 1 {
			t.Errorf("issue %s labels after clear+add: %v", id, got)
		}
	}
}

func TestBatchUpdatePropertyOperations(t *testing.T) {
	a := createTestIssue(t, "BU-props A", "todo", "low")
	b := createTestIssue(t, "BU-props B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })
	prop := createTestProperty(t, map[string]any{"name": "BU Notes " + uuid.NewString()[:8], "type": "text"})

	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids":  []string{a, b},
		"properties": map[string]any{"set": map[string]any{prop.ID: "needs repro"}},
	})
	if code != http.StatusOK || resp.Updated != 2 {
		t.Fatalf("set: %d %s", code, raw)
	}
	for _, id := range []string{a, b} {
		if got, ok := issuePropertyValue(t, id, prop.ID); !ok || got != "needs repro" {
			t.Errorf("issue %s: property = %q, %v", id, got, ok)
		}
	}

	code, _, raw = batchUpdate(t, map[string]any{
		"issue_ids":  []string{a},
		"properties": map[string]any{"unset": []string{prop.ID}},
	})
	if code != http.StatusOK {
		t.Fatalf("unset: %d %s", code, raw)
	}
	if _, ok := issuePropertyValue(t, a, prop.ID); ok {
		t.Errorf("issue A: property still set after unset")
	}
	if _, ok := issuePropertyValue(t, b, prop.ID); !ok {
		t.Errorf("issue B: property cleared although it was not selected")
	}
}

// Fields, labels and properties of one issue land together in one write.
func TestBatchUpdateFieldsLabelsAndPropertiesTogether(t *testing.T) {
	a := createTestIssue(t, "BU-combined A", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	label := createTestIssueLabel(t, "bu-combined-"+uuid.NewString()[:8])
	prop := createTestProperty(t, map[string]any{"name": "BU Combined " + uuid.NewString()[:8], "type": "text"})

	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids":  []string{a},
		"updates":    map[string]any{"priority": "high"},
		"labels":     map[string]any{"add": []string{label}},
		"properties": map[string]any{"set": map[string]any{prop.ID: "x"}},
	})
	if code != http.StatusOK || resultFor(t, resp, a).Outcome != batchOutcomeUpdated {
		t.Fatalf("combined: %d %s", code, raw)
	}
	var priority string
	testPool.QueryRow(context.Background(), `SELECT priority FROM issue WHERE id = $1`, a).Scan(&priority)
	if priority != "high" {
		t.Errorf("priority = %q", priority)
	}
	if !issueLabelSet(t, a)[label] {
		t.Errorf("label not attached")
	}
	if v, ok := issuePropertyValue(t, a, prop.ID); !ok || v != "x" {
		t.Errorf("property = %q, %v", v, ok)
	}
}

func TestBatchUpdateDryRunWritesNothing(t *testing.T) {
	a := createTestIssue(t, "BU-dry A", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	label := createTestIssueLabel(t, "bu-dry-"+uuid.NewString()[:8])

	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids": []string{a},
		"updates":   map[string]any{"status": "in_progress", "parent_issue_id": a},
		"dry_run":   true,
	})
	if code != http.StatusOK || !resp.DryRun || resp.Updated != 0 {
		t.Fatalf("dry run: %d %s", code, raw)
	}
	if got := resultFor(t, resp, a); got.Code != "parent_self" {
		t.Errorf("dry run should still report the skip, got %+v", got)
	}

	_, resp, raw = batchUpdate(t, map[string]any{
		"issue_ids": []string{a},
		"updates":   map[string]any{"status": "in_progress"},
		"labels":    map[string]any{"add": []string{label}},
		"dry_run":   true,
	})
	if got := resultFor(t, resp, a); got.Outcome != batchOutcomeWouldUpdate {
		t.Errorf("dry run outcome: %+v (%s)", got, raw)
	}
	var status string
	testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, a).Scan(&status)
	if status != "todo" || len(issueLabelSet(t, a)) != 0 {
		t.Errorf("dry run wrote: status=%q labels=%v", status, issueLabelSet(t, a))
	}
}

func TestBatchDeleteReportsResultsAndDryRun(t *testing.T) {
	a := createTestIssue(t, "BD-results A", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	missing := uuid.NewString()

	run := func(dryRun bool) (int, batchUpdateTestResponse, string) {
		w := httptest.NewRecorder()
		testHandler.BatchDeleteIssues(w, newRequest("POST", "/api/issues/batch-delete", map[string]any{
			"issue_ids": []string{a, missing}, "dry_run": dryRun,
		}))
		var resp batchUpdateTestResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp, w.Body.String()
	}

	code, resp, raw := run(true)
	if code != http.StatusOK || !resp.DryRun {
		t.Fatalf("dry run: %d %s", code, raw)
	}
	if got := resultFor(t, resp, a); got.Outcome != batchOutcomeWouldDelete {
		t.Errorf("dry run outcome: %+v", got)
	}
	if got := resultFor(t, resp, missing); got.Code != "not_found" {
		t.Errorf("missing issue: %+v", got)
	}
	var n int
	testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE id = $1`, a).Scan(&n)
	if n != 1 {
		t.Fatalf("dry run deleted the issue")
	}

	code, resp, raw = run(false)
	if code != http.StatusOK {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if got := resultFor(t, resp, a); got.Outcome != batchOutcomeDeleted {
		t.Errorf("delete outcome: %+v", got)
	}
	testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE id = $1`, a).Scan(&n)
	if n != 0 {
		t.Errorf("issue still exists after delete")
	}
}

// Problems shared by every issue reject the whole request up front.
func TestBatchUpdateRejectsInvalidLabelAndPropertyOps(t *testing.T) {
	a := createTestIssue(t, "BU-invalid A", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	label := createTestIssueLabel(t, "bu-invalid-"+uuid.NewString()[:8])
	prop := createTestProperty(t, map[string]any{"name": "BU Number " + uuid.NewString()[:8], "type": "number"})

	cases := []struct {
		name string
		body map[string]any
	}{
		{"unknown label", map[string]any{"labels": map[string]any{"add": []string{uuid.NewString()}}}},
		{"label added and removed", map[string]any{"labels": map[string]any{"add": []string{label}, "remove": []string{label}}}},
		{"unknown property", map[string]any{"properties": map[string]any{"set": map[string]any{uuid.NewString(): "x"}}}},
		{"invalid property value", map[string]any{"properties": map[string]any{"set": map[string]any{prop.ID: "not a number"}}}},
		{"property set and unset", map[string]any{"properties": map[string]any{"set": map[string]any{prop.ID: 3}, "unset": []string{prop.ID}}}},
		{"description with labels", map[string]any{"updates": map[string]any{"description": "x"}, "labels": map[string]any{"add": []string{label}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.body["issue_ids"] = []string{a}
			if code, _, raw := batchUpdate(t, tc.body); code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", code, raw)
			}
			if got := issueLabelSet(t, a); len(got) != 0 {
				t.Errorf("rejected request wrote labels: %v", got)
			}
		})
	}
}
