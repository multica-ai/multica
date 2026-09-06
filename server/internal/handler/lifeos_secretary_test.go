package handler

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func secretaryFixture(t *testing.T) (secretaryProjectionRequest, string) {
	t.Helper()
	id := createMetadataTestIssue(t, "secretary original")
	var updated time.Time
	if err := testPool.QueryRow(t.Context(), `SELECT updated_at FROM issue WHERE id=$1`, id).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "state_version": 85, "source_as_of": "2026-09-06T08:00:00+08:00",
		"cases": []any{map[string]any{"id": "recruitment", "title": "招聘", "area": "团队", "summary": "三个岗位"}},
		"items": []any{map[string]any{"key": "a", "issue_id": id, "case_id": "recruitment", "title": "确认岗位", "situation": "画像已准备", "next_step": "确定范围", "recommendation": "按已定画像启动", "why_now": "进入本周流程", "completion": "范围已确认", "kind": "action", "stage": "ready", "owner": "chairman"}}})
	var p map[string]any
	_ = json.Unmarshal(raw, &p)
	rows, err := testPool.Query(t.Context(), `SELECT id::text,title FROM issue WHERE workspace_id=$1 AND id<>$2`, testWorkspaceID, id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var other, title string
		if err = rows.Scan(&other, &title); err != nil {
			t.Fatal(err)
		}
		p["items"] = append(p["items"].([]any), map[string]any{"key": other, "issue_id": other, "case_id": "recruitment", "title": title, "situation": "保留历史", "next_step": "参考", "kind": "reference", "stage": "history", "owner": "secretary"})
	}
	rows.Close()
	raw, _ = json.Marshal(p)
	return secretaryProjectionRequest{ExpectedRevision: 0, Projection: raw, Updates: []secretaryIssueUpdate{{ID: id, ExpectedUpdatedAt: updated, Title: "确认岗位", Description: "准备后的说明", Metadata: map[string]any{"lifeos_case_id": "recruitment"}}}}, id
}
func TestSecretaryProjectionAtomicAndMemberInstructions(t *testing.T) {
	old := testHandler.cfg
	testHandler.cfg.LocalMode = true
	testHandler.cfg.LocalAutomationToken = "test-secretary-only"
	t.Cleanup(func() {
		testHandler.cfg = old
		_, _ = testPool.Exec(context.Background(), `DELETE FROM lifeos_secretary_instruction WHERE workspace_id=$1`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM lifeos_secretary_projection WHERE workspace_id=$1`, testWorkspaceID)
	})
	req, id := secretaryFixture(t)
	publish := func(p secretaryProjectionRequest) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest("PUT", "/api/lifeos/secretary", p)
		r.Header.Set("X-LifeOS-Automation-Token", "test-secretary-only")
		testHandler.PutLifeOSSecretary(w, r)
		return w
	}
	bad := req
	bad.Updates = append([]secretaryIssueUpdate{}, req.Updates...)
	bad.Updates[0].ExpectedUpdatedAt = time.Now().Add(-time.Hour)
	if w := publish(bad); w.Code != 409 {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
	var title string
	_ = testPool.QueryRow(t.Context(), `SELECT title FROM issue WHERE id=$1`, id).Scan(&title)
	if title != "secretary original" {
		t.Fatal("partial update escaped transaction")
	}
	if w := publish(req); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	var stalePayload map[string]any
	_ = json.Unmarshal(req.Projection, &stalePayload)
	stalePayload["state_version"] = 84
	stale := req
	stale.ExpectedRevision, stale.Updates = 1, nil
	stale.Projection, _ = json.Marshal(stalePayload)
	if w := publish(stale); w.Code != 409 {
		t.Fatal("a stale canonical version replaced newer history")
	}
	var precise time.Time
	if err := testPool.QueryRow(t.Context(), `SELECT updated_at FROM issue WHERE id=$1`, id).Scan(&precise); err != nil {
		t.Fatal(err)
	}
	req.ExpectedRevision = 1
	req.Updates[0].ExpectedUpdatedAt = precise
	req.Updates[0].Description = "更新后的第二次说明"
	if w := publish(req); w.Code != 200 {
		t.Fatalf("second material publication: %d %s", w.Code, w.Body.String())
	}
	var originalTitle string
	if err := testPool.QueryRow(t.Context(), `SELECT metadata->>'lifeos_original_title' FROM issue WHERE id=$1`, id).Scan(&originalTitle); err != nil {
		t.Fatal(err)
	}
	if originalTitle != "secretary original" {
		t.Fatal("rewriting the summary lost or replaced the original source")
	}
	command := secretaryInstructionRequest{RequestID: uuid.NewString(), ItemKey: "a", Kind: "complete", ExpectedRevision: 1, Note: "已提交，等接收"}
	send := func(c secretaryInstructionRequest) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.CreateLifeOSSecretaryInstruction(w, newRequest("POST", "/api/lifeos/secretary/instructions", c))
		return w
	}
	first := send(command)
	if first.Code != 200 {
		t.Fatalf("instruction: %d %s", first.Code, first.Body.String())
	}
	retry := send(command)
	if retry.Code != 200 || retry.Body.String() != first.Body.String() {
		t.Fatal("retry not idempotent")
	}
	var status string
	_ = testPool.QueryRow(t.Context(), `SELECT status FROM issue WHERE id=$1`, id).Scan(&status)
	if status == "done" {
		t.Fatal("report fabricated business completion")
	}
	req.ExpectedRevision = 1
	req.Updates = nil
	if w := publish(req); w.Code != 200 {
		t.Fatalf("idempotent publish: %d %s", w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	testHandler.GetLifeOSSecretary(w, newRequest("GET", "/api/lifeos/secretary", nil))
	var data struct {
		Revision     int64                        `json:"revision"`
		Instructions []secretaryStoredInstruction `json:"instructions"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	if w.Code != 200 || data.Revision != 1 || len(data.Instructions) != 1 {
		t.Fatalf("refresh lost instructions: %d %s", w.Code, w.Body.String())
	}
}
func TestSecretaryAuthorizationAndPreparation(t *testing.T) {
	h := &Handler{cfg: Config{LocalMode: true, LocalAutomationToken: "local-only"}}
	w := httptest.NewRecorder()
	h.PutLifeOSSecretary(w, httptest.NewRequest("PUT", "/api/lifeos/secretary", nil))
	if w.Code != 403 {
		t.Fatal("machine credential not required")
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/lifeos/secretary/instructions", nil)
	r.Header.Set("X-Actor-Source", "task_token")
	RequireHumanActor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("agent forged a member instruction")
	}
	if validateSecretaryInstruction(secretaryInstructionRequest{Kind: "complete", Note: ""}) == nil {
		t.Fatal("empty completion report accepted")
	}
	raw := json.RawMessage(`{"version":1,"state_version":1,"source_as_of":"2026-09-06T00:00:00Z","cases":[{"id":"a","title":"招聘"}],"items":[{"key":"x","case_id":"a","kind":"action","stage":"ready","owner":"external","title":"等回复","situation":"等待","next_step":"等待"}]}`)
	if _, err := validateSecretaryProjection(raw); err == nil {
		t.Fatal("external waiting became personal action")
	}
}

func TestSecretaryResolvedViewHonorsWaitingDateAndLatestInstruction(t *testing.T) {
	raw := json.RawMessage(`{"items":[{"key":"a","stage":"ready","owner":"chairman","follow_up_on":"2026-09-06","next_step":"旧安排"}]}`)
	at, _ := time.Parse(time.RFC3339, "2026-09-06T09:00:00+08:00")
	instructions := []secretaryStoredInstruction{
		{Sequence: 1, ItemKey: "a", Kind: "cancel", CreatedAt: at, Payload: json.RawMessage(`{"note":"先停止"}`)},
		{Sequence: 2, ItemKey: "a", Kind: "wait", CreatedAt: at, Payload: json.RawMessage(`{"note":"等负责方回复","scheduled_on":"2026-09-08"}`)},
	}
	view, err := resolveSecretaryView(raw, instructions)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(view, &p)
	item := p.Items[0]
	if item["stage"] != "waiting" || item["owner"] != "external" || item["follow_up_on"] != "2026-09-08" || item["next_step"] != "等负责方回复" {
		t.Fatalf("view disagrees with user instruction: %s", view)
	}
	raw = json.RawMessage(`{"items":[{"key":"a","stage":"history","owner":"secretary"}]}`)
	view, err = resolveSecretaryView(raw, instructions)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(view, &p)
	if p.Items[0]["stage"] != "history" {
		t.Fatal("terminal source resurrected")
	}
}

func TestSecretaryRoutineCompletionNeedsCanonicalReceiptBeforeUpdatingSource(t *testing.T) {
	old := testHandler.cfg
	testHandler.cfg.LocalMode, testHandler.cfg.LocalAutomationToken = true, "test-secretary-only"
	t.Cleanup(func() {
		testHandler.cfg = old
		_, _ = testPool.Exec(context.Background(), `DELETE FROM lifeos_secretary_instruction WHERE workspace_id=$1`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM lifeos_secretary_projection WHERE workspace_id=$1`, testWorkspaceID)
	})
	req, id := secretaryFixture(t)
	var p map[string]any
	_ = json.Unmarshal(req.Projection, &p)
	item := p["items"].([]any)[0].(map[string]any)
	item["action_id"], item["closure_mode"] = "routine-personal", "self_report"
	req.Projection, _ = json.Marshal(p)
	publish := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest("PUT", "/api/lifeos/secretary", req)
		r.Header.Set("X-LifeOS-Automation-Token", "test-secretary-only")
		testHandler.PutLifeOSSecretary(w, r)
		return w
	}
	if w := publish(); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	command := secretaryInstructionRequest{RequestID: uuid.NewString(), ItemKey: "a", Kind: "complete", ExpectedRevision: 1, Note: "本人已阅读并确认"}
	w := httptest.NewRecorder()
	testHandler.CreateLifeOSSecretaryInstruction(w, newRequest("POST", "/api/lifeos/secretary/instructions", command))
	if w.Code != 200 {
		t.Fatalf("instruction: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.GetLifeOSSecretary(w, newRequest("GET", "/api/lifeos/secretary", nil))
	var response struct {
		Projection struct {
			Items []secretaryItem `json:"items"`
		} `json:"projection"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if response.Projection.Items[0].Stage != "history" {
		t.Fatal("personal feedback stayed in the active queue")
	}
	var updated time.Time
	_ = testPool.QueryRow(t.Context(), `SELECT updated_at FROM issue WHERE id=$1`, id).Scan(&updated)
	done := "done"
	req.ExpectedRevision, req.Updates[0].ExpectedUpdatedAt, req.Updates[0].Status = 1, updated, &done
	if w := publish(); w.Code != 400 {
		t.Fatalf("source accepted completion without canonical evidence: %d", w.Code)
	}
	item["stage"], item["business_status"], item["reported_status"] = "history", "completed", "completed"
	req.Projection, _ = json.Marshal(p)
	if w := publish(); w.Code != 200 {
		t.Fatalf("canonical completion: %d %s", w.Code, w.Body.String())
	}
	var status string
	_ = testPool.QueryRow(t.Context(), `SELECT status FROM issue WHERE id=$1`, id).Scan(&status)
	if status != "done" {
		t.Fatal("routine completion not projected")
	}
}
