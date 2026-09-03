package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type workflowAPIState struct {
	Status       string `json:"status"`
	CurrentStage int32  `json:"current_stage"`
}

type workflowAPIMutation struct {
	Run     workflowAPIState `json:"run"`
	Outcome string           `json:"outcome"`
}

type workflowAPITransition struct {
	Kind      string `json:"kind"`
	FromStage *int32 `json:"from_stage"`
	ToStage   *int32 `json:"to_stage"`
}

func workflowCreateDefinitionAPI(t *testing.T, token string, stages ...string) string {
	t.Helper()
	rows := make([]map[string]string, 0, len(stages))
	for i, name := range stages {
		rows = append(rows, map[string]string{"key": fmt.Sprintf("stage_%d", i+1), "name": name})
	}
	resp := workflowAuthRequest(t, token, http.MethodPost, "/api/workflow-definitions", map[string]any{
		"name":       fmt.Sprintf("Integration Flow %d", time.Now().UnixNano()),
		"definition": map[string]any{"schema_version": 1, "stages": rows},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create workflow definition status = %d", resp.StatusCode)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func workflowSeedIssueTree(t *testing.T, creatorID string, stageCount int) (string, []string) {
	t.Helper()
	ctx := context.Background()
	var base int32
	if err := testPool.QueryRow(ctx, `SELECT COALESCE(max(number),0)+100 FROM issue WHERE workspace_id=$1`, testWorkspaceID).Scan(&base); err != nil {
		t.Fatal(err)
	}
	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number)
		VALUES ($1,'Workflow Integration Parent','in_progress','none','member',$2,0,$3)
		RETURNING id::text
	`, testWorkspaceID, creatorID, base).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	children := make([]string, 0, stageCount)
	for stage := 1; stage <= stageCount; stage++ {
		var childID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,parent_issue_id,stage,position,number)
			VALUES ($1,$2,'backlog','none','member',$3,$4,$5,0,$6)
			RETURNING id::text
		`, testWorkspaceID, fmt.Sprintf("Workflow Stage %d", stage), creatorID, parentID, stage, base+int32(stage)).Scan(&childID); err != nil {
			t.Fatal(err)
		}
		children = append(children, childID)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, parentID) })
	return parentID, children
}

func workflowUpdateIssueStatusAPI(t *testing.T, token, issueID, status string) {
	t.Helper()
	resp := workflowAuthRequest(t, token, http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": status})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update issue %s status=%s: status %d", issueID, status, resp.StatusCode)
	}
}
func TestWorkflowLifecycleThroughRouter(t *testing.T) {
	ownerID, token := workflowTestUser(t, "owner")
	definitionID := workflowCreateDefinitionAPI(t, token, "Build", "Test")
	parentID, children := workflowSeedIssueTree(t, ownerID, 2)

	resp := workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start workflow status = %d", resp.StatusCode)
	}
	var started workflowAPIMutation
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.Run.Status != "running" || started.Run.CurrentStage != 1 {
		t.Fatalf("start run = %+v", started.Run)
	}

	workflowUpdateIssueStatusAPI(t, token, children[0], "done")
	var stage2Status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, children[1]).Scan(&stage2Status); err != nil {
		t.Fatal(err)
	}
	if stage2Status != "todo" {
		t.Fatalf("stage2 status = %q, want todo", stage2Status)
	}

	workflowUpdateIssueStatusAPI(t, token, children[1], "done")
	var parentStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, parentID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "in_review" {
		t.Fatalf("parent status = %q, want in_review", parentStatus)
	}
	resp = workflowAuthRequest(t, token, http.MethodGet, "/api/issues/"+parentID+"/workflow", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get workflow status = %d", resp.StatusCode)
	}
	var run workflowAPIState
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed_pending_review" || run.CurrentStage != 2 {
		t.Fatalf("final run = %+v", run)
	}

	resp = workflowAuthRequest(t, token, http.MethodGet, "/api/issues/"+parentID+"/workflow/transitions", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transitions status = %d", resp.StatusCode)
	}
	var transitions []workflowAPITransition
	if err := json.NewDecoder(resp.Body).Decode(&transitions); err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(transitions))
	for _, tr := range transitions {
		kinds = append(kinds, tr.Kind)
	}
	want := []string{"started", "stage_advanced", "completed_pending_review"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("transition kinds = %v, want %v", kinds, want)
	}
}

func TestWorkflowBlockedMaterializationResumeThroughRouter(t *testing.T) {
	ownerID, token := workflowTestUser(t, "owner")
	definitionID := workflowCreateDefinitionAPI(t, token, "Build", "Test")
	parentID, children := workflowSeedIssueTree(t, ownerID, 1)
	resp := workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	workflowUpdateIssueStatusAPI(t, token, children[0], "done")
	resp = workflowAuthRequest(t, token, http.MethodGet, "/api/issues/"+parentID+"/workflow", nil)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("get blocked workflow status = %d", resp.StatusCode)
	}
	var blocked workflowAPIState
	if err := json.NewDecoder(resp.Body).Decode(&blocked); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if blocked.Status != "blocked_materialization" || blocked.CurrentStage != 2 {
		t.Fatalf("blocked run = %+v", blocked)
	}

	var base int32
	if err := testPool.QueryRow(context.Background(), `SELECT COALESCE(max(number),0)+1 FROM issue WHERE workspace_id=$1`, testWorkspaceID).Scan(&base); err != nil {
		t.Fatal(err)
	}
	var stage2ID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,parent_issue_id,stage,position,number)
		VALUES ($1,'Materialized Stage 2','backlog','none','member',$2,$3,2,0,$4) RETURNING id::text
	`, testWorkspaceID, ownerID, parentID, base).Scan(&stage2ID); err != nil {
		t.Fatal(err)
	}

	resp = workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/resume", nil)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("resume status = %d", resp.StatusCode)
	}
	var resumed workflowAPIMutation
	if err := json.NewDecoder(resp.Body).Decode(&resumed); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resumed.Run.Status != "running" || resumed.Run.CurrentStage != 2 || resumed.Outcome != "materialized" {
		t.Fatalf("resumed = %+v", resumed)
	}

	var stage2Status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, stage2ID).Scan(&stage2Status); err != nil {
		t.Fatal(err)
	}
	if stage2Status != "todo" {
		t.Fatalf("materialized stage status = %q, want todo", stage2Status)
	}

	resp = workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/resume", nil)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("second resume status = %d", resp.StatusCode)
	}
	var again workflowAPIMutation
	if err := json.NewDecoder(resp.Body).Decode(&again); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if again.Outcome != "noop" {
		t.Fatalf("second resume outcome = %q, want noop", again.Outcome)
	}
}
func workflowIsolatedOwner(t *testing.T) (workspaceID, userID, token string) {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("workflow-isolated-%d@multica.test", time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('Workflow Isolated',$1) RETURNING id::text`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	slug := fmt.Sprintf("workflow-isolated-%d", time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name,slug,description) VALUES ('Workflow Isolated',$1,'workflow test') RETURNING id::text`, slug).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatal(err)
	}
	var err error
	token, err = generateTestJWT(userID, email, "Workflow Isolated")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id=$1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, userID)
	})
	return workspaceID, userID, token
}

func workflowAuthRequestWorkspace(t *testing.T, token, workspaceID, method, path string, body any) *http.Response {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, testServer.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", workspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func TestWorkflowPromotionPreservesIssueStatusInvariants(t *testing.T) {
	workspaceID, userID, token := workflowIsolatedOwner(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_status (workspace_id,key,name,category,color,position)
		VALUES ($1,'queued_custom','Queued Custom','backlog','#123456',10)
	`, workspaceID); err != nil {
		t.Fatal(err)
	}

	var definitionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workflow_definition (workspace_id,name,version,definition,created_by)
		VALUES ($1,'Invariant Flow',1,$2::jsonb,$3) RETURNING id::text
	`, workspaceID, `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`, userID).Scan(&definitionID); err != nil {
		t.Fatal(err)
	}

	for i, pos := range []float64{10, 20} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number)
			VALUES ($1,$2,'todo','none','member',$3,$4,$5)
		`, workspaceID, fmt.Sprintf("Todo reference %d", i), userID, pos, i+1); err != nil {
			t.Fatal(err)
		}
	}
	var parentID, stage1ID, stage2ID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number)
		VALUES ($1,'Invariant Parent','in_progress','none','member',$2,0,100) RETURNING id::text
	`, workspaceID, userID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,parent_issue_id,stage,position,number)
		VALUES ($1,'Invariant Stage 1','backlog','none','member',$2,$3,1,0,101) RETURNING id::text
	`, workspaceID, userID, parentID).Scan(&stage1ID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,parent_issue_id,stage,position,number,last_activity_at)
		VALUES ($1,'Invariant Stage 2','queued_custom','none','member',$2,$3,2,50,102,now()) RETURNING id::text
	`, workspaceID, userID, parentID).Scan(&stage2ID); err != nil {
		t.Fatal(err)
	}

	resp := workflowAuthRequestWorkspace(t, token, workspaceID, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start invariant workflow status = %d", resp.StatusCode)
	}

	var beforeStatus string
	var beforePosition float64
	var beforeRevision int64
	var beforeActivity, beforeUpdated time.Time
	if err := testPool.QueryRow(ctx, `SELECT status,position,revision,last_activity_at,updated_at FROM issue WHERE id=$1`, stage2ID).Scan(&beforeStatus, &beforePosition, &beforeRevision, &beforeActivity, &beforeUpdated); err != nil {
		t.Fatal(err)
	}
	if beforeStatus != "queued_custom" {
		t.Fatalf("before status = %q", beforeStatus)
	}
	time.Sleep(15 * time.Millisecond)
	resp = workflowAuthRequestWorkspace(t, token, workspaceID, http.MethodPut, "/api/issues/"+stage1ID, map[string]any{"status": "done"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("finish invariant stage1 status = %d", resp.StatusCode)
	}

	var afterStatus string
	var afterPosition float64
	var afterRevision int64
	var afterActivity, afterUpdated time.Time
	if err := testPool.QueryRow(ctx, `SELECT status,position,revision,last_activity_at,updated_at FROM issue WHERE id=$1`, stage2ID).Scan(&afterStatus, &afterPosition, &afterRevision, &afterActivity, &afterUpdated); err != nil {
		t.Fatal(err)
	}
	if afterStatus != "todo" {
		t.Fatalf("after status = %q, want todo", afterStatus)
	}
	if afterPosition != 9 {
		t.Fatalf("after position = %v, want 9", afterPosition)
	}
	if afterRevision != beforeRevision+1 {
		t.Fatalf("revision %d -> %d, want +1", beforeRevision, afterRevision)
	}
	if !afterActivity.After(beforeActivity) {
		t.Fatalf("last_activity_at did not advance: %s -> %s", beforeActivity, afterActivity)
	}
	if !afterUpdated.After(beforeUpdated) {
		t.Fatalf("updated_at did not advance: %s -> %s", beforeUpdated, afterUpdated)
	}
}
func TestWorkflowHTTPWorkspaceIsolation(t *testing.T) {
	wsA, userA, tokenA := workflowIsolatedOwner(t)
	wsB, _, tokenB := workflowIsolatedOwner(t)
	ctx := context.Background()
	var definitionID, parentID, childID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workflow_definition (workspace_id,name,version,definition,created_by)
		VALUES ($1,'Isolation Flow',1,$2::jsonb,$3) RETURNING id::text
	`, wsA, `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`, userA).Scan(&definitionID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number)
		VALUES ($1,'Isolation Parent','in_progress','none','member',$2,0,1) RETURNING id::text
	`, wsA, userA).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,parent_issue_id,stage,position,number)
		VALUES ($1,'Isolation Child','backlog','none','member',$2,$3,1,0,2) RETURNING id::text
	`, wsA, userA, parentID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	_ = childID

	resp := workflowAuthRequestWorkspace(t, tokenA, wsA, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workspace A start status = %d", resp.StatusCode)
	}
	checks := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/workflow-definitions/" + definitionID, nil},
		{http.MethodGet, "/api/issues/" + parentID + "/workflow", nil},
		{http.MethodGet, "/api/issues/" + parentID + "/workflow/transitions", nil},
		{http.MethodPost, "/api/issues/" + parentID + "/workflow/start", map[string]any{"workflow_definition_id": definitionID}},
		{http.MethodPost, "/api/issues/" + parentID + "/workflow/resume", nil},
		{http.MethodPost, "/api/issues/" + parentID + "/workflow/cancel", nil},
	}
	for _, tc := range checks {
		resp := workflowAuthRequestWorkspace(t, tokenB, wsB, tc.method, tc.path, tc.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("foreign workspace %s %s status = %d, want 404", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestWorkflowOrderGuardReturnsConflictThroughIssueUpdate(t *testing.T) {
	ownerID, token := workflowTestUser(t, "owner")
	definitionID := workflowCreateDefinitionAPI(t, token, "Build", "Test")
	parentID, children := workflowSeedIssueTree(t, ownerID, 2)

	resp := workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start workflow status = %d", resp.StatusCode)
	}

	resp = workflowAuthRequest(t, token, http.MethodPut, "/api/issues/"+children[1], map[string]any{"status": "todo"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("future stage update status = %d, want 409", resp.StatusCode)
	}
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, children[1]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "backlog" {
		t.Fatalf("future stage status = %q, want backlog", status)
	}
}

func TestWorkflowOrderGuardReturnsConflictThroughIssueCreate(t *testing.T) {
	ownerID, token := workflowTestUser(t, "owner")
	definitionID := workflowCreateDefinitionAPI(t, token, "Build", "Test")
	parentID, _ := workflowSeedIssueTree(t, ownerID, 2)
	resp := workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start workflow status = %d", resp.StatusCode)
	}

	resp = workflowAuthRequest(t, token, http.MethodPost, "/api/issues", map[string]any{
		"title": "illegal future work", "status": "todo", "parent_issue_id": parentID, "stage": 2,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("future stage create status = %d, want 409", resp.StatusCode)
	}
}

func TestWorkflowOrderGuardReturnsConflictThroughBatchUpdate(t *testing.T) {
	ownerID, token := workflowTestUser(t, "owner")
	definitionID := workflowCreateDefinitionAPI(t, token, "Build", "Test")
	parentID, children := workflowSeedIssueTree(t, ownerID, 2)
	resp := workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start workflow status = %d", resp.StatusCode)
	}

	resp = workflowAuthRequest(t, token, http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{children[1]}, "updates": map[string]any{"status": "todo"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("future stage batch update status = %d, want 409", resp.StatusCode)
	}
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, children[1]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "backlog" {
		t.Fatalf("future stage status = %q, want backlog", status)
	}
}

func TestWorkflowOrderGuardBatchPreflightPreventsPartialMutation(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
			ownerID, token := workflowTestUser(t, "owner")
			definitionID := workflowCreateDefinitionAPI(t, token, "Build", "Test")
			parentID, children := workflowSeedIssueTree(t, ownerID, 2)
			resp := workflowAuthRequest(t, token, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("start workflow status = %d", resp.StatusCode)
			}

			ids := []string{children[0], children[1]}
			if reverse {
				ids[0], ids[1] = ids[1], ids[0]
			}
			resp = workflowAuthRequest(t, token, http.MethodPost, "/api/issues/batch-update", map[string]any{
				"issue_ids": ids, "updates": map[string]any{"status": "done"},
			})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("cross-stage batch status = %d, want 409", resp.StatusCode)
			}

			var stage1, stage2 string
			if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, children[0]).Scan(&stage1); err != nil {
				t.Fatal(err)
			}
			if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, children[1]).Scan(&stage2); err != nil {
				t.Fatal(err)
			}
			if stage1 != "todo" || stage2 != "backlog" {
				t.Fatalf("batch partially mutated workflow: stage1=%q stage2=%q", stage1, stage2)
			}
		})
	}
}
