package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Round-trip: set scalars and structured values, list and issue-get them back,
// delete one key, and confirm the unrelated values remain intact.
func TestIssueMetadataSetGetDelete(t *testing.T) {
	issueID := createMetadataTestIssue(t, "Metadata round-trip")

	cases := []struct {
		key   string
		value string // raw JSON value
	}{
		{"pipeline_status", `"waiting"`},
		{"pr_number", `482`},
		{"is_blocked", `true`},
		{"is_done", `false`},
		{"workstreams", `["frontend",{"release":{"ready":true}}]`},
		{"qa_evidence", `{"checks":[],"summary":{"passed":true}}`},
		{"empty_array", `[]`},
		{"empty_object", `{}`},
	}

	for _, c := range cases {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/"+c.key, json.RawMessage(`{"value":`+c.value+`}`))
		req = withURLParams(req, "id", issueID, "key", c.key)
		testHandler.SetIssueMetadataKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Set %s=%s: expected 200, got %d: %s", c.key, c.value, w.Code, w.Body.String())
		}
	}

	// List returns every key with the right value type.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/metadata", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueMetadata(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("List metadata: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if got := resp.Metadata["pipeline_status"]; got != "waiting" {
		t.Errorf("pipeline_status: expected \"waiting\", got %T %v", got, got)
	}
	if got := resp.Metadata["pr_number"]; got != float64(482) {
		t.Errorf("pr_number: expected number 482, got %T %v", got, got)
	}
	if got := resp.Metadata["is_blocked"]; got != true {
		t.Errorf("is_blocked: expected true, got %T %v", got, got)
	}
	if got := resp.Metadata["is_done"]; got != false {
		t.Errorf("is_done: expected false, got %T %v", got, got)
	}
	workstreams, ok := resp.Metadata["workstreams"].([]any)
	if !ok || len(workstreams) != 2 {
		t.Fatalf("workstreams: expected native two-element array, got %T %v", resp.Metadata["workstreams"], resp.Metadata["workstreams"])
	}
	workstreamObject, ok := workstreams[1].(map[string]any)
	if !ok {
		t.Fatalf("workstreams nested value: expected object, got %T %v", workstreams[1], workstreams[1])
	}
	release, ok := workstreamObject["release"].(map[string]any)
	if !ok || release["ready"] != true {
		t.Errorf("workstreams nested object: got %T %v", workstreams[1], workstreams[1])
	}
	qa, ok := resp.Metadata["qa_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("qa_evidence: expected native object with empty checks array, got %T %v", resp.Metadata["qa_evidence"], resp.Metadata["qa_evidence"])
	}
	if checks, ok := qa["checks"].([]any); !ok || len(checks) != 0 {
		t.Errorf("qa_evidence checks: expected native empty array, got %T %v", qa["checks"], qa["checks"])
	}
	if got, ok := resp.Metadata["empty_array"].([]any); !ok || len(got) != 0 {
		t.Errorf("empty_array: expected native empty array, got %T %v", resp.Metadata["empty_array"], resp.Metadata["empty_array"])
	}
	if got, ok := resp.Metadata["empty_object"].(map[string]any); !ok || len(got) != 0 {
		t.Errorf("empty_object: expected native empty object, got %T %v", resp.Metadata["empty_object"], resp.Metadata["empty_object"])
	}

	// The ordinary issue response must expose the same native shapes as the
	// dedicated metadata endpoint.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var issueResp IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issueResp); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	if _, ok := issueResp.Metadata["workstreams"].([]any); !ok {
		t.Errorf("issue metadata workstreams: expected native array, got %T %v", issueResp.Metadata["workstreams"], issueResp.Metadata["workstreams"])
	}
	if _, ok := issueResp.Metadata["qa_evidence"].(map[string]any); !ok {
		t.Errorf("issue metadata qa_evidence: expected native object, got %T %v", issueResp.Metadata["qa_evidence"], issueResp.Metadata["qa_evidence"])
	}

	// Delete a key — refresh confirms it is gone, others remain.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issueID+"/metadata/pipeline_status", nil)
	req = withURLParams(req, "id", issueID, "key", "pipeline_status")
	testHandler.DeleteIssueMetadataKey(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Delete pipeline_status: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/metadata", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueMetadata(w, req)
	// Decode into a fresh struct — json.Decode into a non-nil map merges,
	// it does not replace, so reusing `resp` would keep deleted keys around.
	var afterDelete struct {
		Metadata map[string]any `json:"metadata"`
	}
	json.NewDecoder(w.Body).Decode(&afterDelete)
	if _, present := afterDelete.Metadata["pipeline_status"]; present {
		t.Errorf("after delete, pipeline_status should be gone; got %+v", afterDelete.Metadata)
	}
	if _, present := afterDelete.Metadata["pr_number"]; !present {
		t.Errorf("delete removed unrelated key; got %+v", afterDelete.Metadata)
	}
}

// Invalid keys, null, and malformed request bodies are rejected with 400.
func TestIssueMetadataValidation(t *testing.T) {
	issueID := createMetadataTestIssue(t, "Metadata validation")

	bad := []struct {
		name    string
		key     string
		rawBody string
	}{
		{"key starts with digit", "1attempts", `{"value":"x"}`},
		{"key has space", "foo bar", `{"value":"x"}`},
		{"value is null", "k", `{"value":null}`},
		{"malformed JSON", "k", `{"value":[}`},
		{"empty body", "k", ``},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			// chi pulls the key from URL params (injected via withURLParams);
			// the raw URL needs to be a valid request line, so PathEscape any
			// chars (spaces, etc.) that would otherwise break httptest.NewRequest.
			req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/"+url.PathEscape(c.key), json.RawMessage(c.rawBody))
			req = withURLParams(req, "id", issueID, "key", c.key)
			testHandler.SetIssueMetadataKey(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// The 8KB DB CHECK kicks in past a few hundred KV pairs of large strings; we
// blow it deliberately with one giant value to confirm the handler surfaces
// a 400 (not a generic 500).
func TestIssueMetadataSizeLimit(t *testing.T) {
	issueID := createMetadataTestIssue(t, "Metadata size limit")

	huge := strings.Repeat("a", 9000)
	values := map[string]any{
		"string":     huge,
		"structured": map[string]any{"nested": []any{huge}},
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"value": value})
			w := httptest.NewRecorder()
			req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/blob", body)
			req = withURLParams(req, "id", issueID, "key", "blob")
			testHandler.SetIssueMetadataKey(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 from size CHECK, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// The 50-key cap is enforced in the handler with a clear 400.
func TestIssueMetadataKeyCountCap(t *testing.T) {
	issueID := createMetadataTestIssue(t, "Metadata key count cap")

	for i := 0; i < maxIssueMetadataKeys; i++ {
		key := fmt.Sprintf("k_%d", i)
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/"+key, json.RawMessage(`{"value":"v"}`))
		req = withURLParams(req, "id", issueID, "key", key)
		testHandler.SetIssueMetadataKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("key #%d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/overflow", json.RawMessage(`{"value":"v"}`))
	req = withURLParams(req, "id", issueID, "key", "overflow")
	testHandler.SetIssueMetadataKey(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("overflow key: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Updating an existing key past the cap is still allowed — only new keys
	// are blocked.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID+"/metadata/k_0", json.RawMessage(`{"value":"v2"}`))
	req = withURLParams(req, "id", issueID, "key", "k_0")
	testHandler.SetIssueMetadataKey(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update existing at cap: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ListIssues with `metadata` query param does JSONB containment filtering and
// returns only matching issues — the killer use case for autopilot.
func TestListIssuesMetadataFilter(t *testing.T) {
	waitingID := createMetadataTestIssue(t, "Waiting issue")
	doneID := createMetadataTestIssue(t, "Done issue")
	structuredID := createMetadataTestIssue(t, "Structured issue")

	for issueID, status := range map[string]string{waitingID: "waiting_review", doneID: "deployed"} {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/pipeline_status",
			json.RawMessage(`{"value":"`+status+`"}`))
		req = withURLParams(req, "id", issueID, "key", "pipeline_status")
		testHandler.SetIssueMetadataKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", issueID, w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+structuredID+"/metadata/workstreams",
		json.RawMessage(`{"value":["frontend",{"release":{"ready":true}}]}`))
	req = withURLParams(req, "id", structuredID, "key", "workstreams")
	testHandler.SetIssueMetadataKey(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("seed structured metadata: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", `/api/issues?metadata={"pipeline_status":"waiting_review"}`, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("List with filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)

	foundWaiting := false
	for _, iss := range listResp.Issues {
		if iss.ID == doneID {
			t.Errorf("filter leaked: deployed issue %s appeared in waiting_review result set", doneID)
		}
		if iss.ID == waitingID {
			foundWaiting = true
			if got, _ := iss.Metadata["pipeline_status"].(string); got != "waiting_review" {
				t.Errorf("waiting issue: pipeline_status not surfaced; got %v", iss.Metadata)
			}
		}
	}
	if !foundWaiting {
		t.Errorf("waiting issue %s missing from filter result; got %d issues", waitingID, len(listResp.Issues))
	}

	// Structured metadata uses the same JSONB containment semantics as scalar
	// values and must remain queryable after the write contract expands.
	w = httptest.NewRecorder()
	filter := `{"workstreams":["frontend",{"release":{"ready":true}}]}`
	req = newRequest("GET", "/api/issues?metadata="+url.QueryEscape(filter), nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("List with structured filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	listResp.Issues = nil
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode structured filter response: %v", err)
	}
	foundStructured := false
	for _, iss := range listResp.Issues {
		if iss.ID == structuredID {
			foundStructured = true
		}
	}
	if !foundStructured {
		t.Errorf("structured issue %s missing from filter result; got %d issues", structuredID, len(listResp.Issues))
	}

	// Malformed filter → 400.
	w = httptest.NewRecorder()
	req = newRequest("GET", `/api/issues?metadata={not-json}`, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed metadata: expected 400, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", `/api/issues?metadata={"workstreams":null}`, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("null metadata filter: expected 400, got %d", w.Code)
	}
}

// New issues default to an empty metadata object — never null — so frontend
// reads like `issue.metadata[key]` never NPE.
func TestNewIssueDefaultsToEmptyMetadata(t *testing.T) {
	issueID := createMetadataTestIssue(t, "Default empty metadata")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue: %d %s", w.Code, w.Body.String())
	}
	var got IssueResponse
	json.NewDecoder(w.Body).Decode(&got)
	if got.Metadata == nil {
		t.Fatalf("Metadata is nil on a fresh issue; expected empty object")
	}
	if len(got.Metadata) != 0 {
		t.Fatalf("Metadata: expected empty, got %v", got.Metadata)
	}
}

func createMetadataTestIssue(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createMetadataTestIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	return issue.ID
}
