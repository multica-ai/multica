package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestSelectedSlashSkillIDs(t *testing.T) {
	markdown := make([]string, 0, maxSelectedSlashSkillsPerTask+2)
	markdown = append(markdown, "[/first](slash://skill/a) [/again](slash://skill/a)")
	for i := 0; i < maxSelectedSlashSkillsPerTask+1; i++ {
		markdown = append(markdown, "[/skill](slash://skill/id-"+strings.Repeat("x", i+1)+")")
	}

	ids := selectedSlashSkillIDs(markdown...)
	if len(ids) != maxSelectedSlashSkillsPerTask {
		t.Fatalf("selected ids count = %d, want cap %d", len(ids), maxSelectedSlashSkillsPerTask)
	}
	if ids[0] != "a" {
		t.Fatalf("first-occurrence order lost: %v", ids)
	}
}

func TestSelectedSlashSkillIDsForClaimTrustsOnlyMemberComments(t *testing.T) {
	task := db.AgentTaskQueue{
		TriggerCommentID:    pgtype.UUID{Valid: true},
		CoalescedCommentIds: []pgtype.UUID{{Valid: true}, {Valid: true}},
	}
	resp := AgentTaskResponse{
		TriggerAuthorType:     "agent",
		TriggerCommentContent: "[/agent](slash://skill/agent-skill)",
		CoalescedComments: []CoalescedCommentData{
			{AuthorType: "system", Content: "[/system](slash://skill/system-skill)"},
			{AuthorType: "member", Content: "[/member](slash://skill/member-skill)"},
		},
	}

	ids := selectedSlashSkillIDsForClaim(task, resp)
	if len(ids) != 1 || ids[0] != "member-skill" {
		t.Fatalf("selected ids = %v, want only member-authored marker", ids)
	}
}

func TestSelectedSlashSkillIDsForClaimIncludesQuickCreatePrompt(t *testing.T) {
	task := db.AgentTaskQueue{Context: []byte(`{"type":"quick_create"}`)}
	resp := AgentTaskResponse{
		QuickCreatePrompt: "please [/architecture-sweep](slash://skill/11111111-1111-4111-8111-111111111111)",
	}

	ids := selectedSlashSkillIDsForClaim(task, resp)
	if len(ids) != 1 || ids[0] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("selected ids = %v, want quick-create prompt marker", ids)
	}
}

func TestMergeTaskSkillsDoesNotMutateConfiguredBackingArray(t *testing.T) {
	configured := make([]service.AgentSkillData, 2, 4)
	configured[0] = service.AgentSkillData{ID: "configured-1"}
	configured[1] = service.AgentSkillData{ID: "configured-2"}
	selected := []service.AgentSkillData{{ID: "selected-1"}}

	merged := mergeTaskSkills(configured, selected)
	if len(merged) != 3 {
		t.Fatalf("merged count = %d, want 3", len(merged))
	}
	if configured[:cap(configured)][2].ID != "" {
		t.Fatal("mergeTaskSkills wrote selected data into configured backing array")
	}
}

func TestSelectedSkillMergePreservesAgentBuiltinScope(t *testing.T) {
	svc := &service.TaskService{}
	h := &Handler{TaskService: svc}
	for _, key := range []string{"", service.MikaSystemKey} {
		for _, legacy := range []bool{false, true} {
			for _, refs := range []bool{false, true} {
				t.Run(fmt.Sprintf("system=%s/legacy=%t/refs=%t", key, legacy, refs), func(t *testing.T) {
					builtins := svc.BuiltinSkills(key, legacy)
					agent := &TaskAgentData{}
					if refs {
						_, agent.SkillRefs = service.BuildAgentSkillBundles(builtins)
					} else {
						agent.Skills = builtins
					}
					before := *agent
					resp := AgentTaskResponse{WorkspaceID: "11111111-1111-4111-8111-111111111111", Agent: agent}
					added, failure := h.applyClaimTaskSkills(context.Background(), db.AgentTaskQueue{}, &resp, refs, nil)
					if failure != nil || added != 0 || !reflect.DeepEqual(before, *agent) {
						t.Fatalf("unchosen grant changed scoped built-ins: added=%d failure=%+v before=%+v after=%+v", added, failure, before, *agent)
					}
				})
			}
		}
	}
}

func TestFinalizeClaimDeliveryAllowsNilResponse(t *testing.T) {
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Nil response runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Nil response agent")
	seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)
	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim fixture: task=%v err=%v", task, err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: parseUUID(runtimeID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	_, failure, err := testHandler.finalizeClaimDelivery(ctx, task, runtime, runtimeID, testWorkspaceID, nil, db.CreateTaskTokenParams{
		TokenHash: "nil-response-" + uuidToString(task.ID), TaskID: task.ID, AgentID: task.AgentID,
		WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, nil, false, nil)
	if err != nil || failure != nil {
		t.Fatalf("nil response finalization: failure=%+v err=%v", failure, err)
	}
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id = $1`, task.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("finalized token count = %d, want 1", count)
	}
}

func TestClaimSelectedSkillFromChatAndQuickCreate(t *testing.T) {
	for _, kind := range []string{"chat", "quick-create"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			skillID := dbfx.Insert(t, "skill", testutil.Cols{
				"workspace_id": testWorkspaceID, "name": "selected-" + kind,
				"description": "one run only", "content": "selected content",
				"config": testutil.Raw("'{}'::jsonb"), "created_by": testUserID,
			})
			prompt := "use [/selected](slash://skill/" + skillID + ")"
			var agentID, sessionID, runtimeID, daemonID, taskID string
			if kind == "chat" {
				agentID, sessionID, runtimeID, daemonID = setupDirectChatSession(t, ctx, "selected Skill chat")
				taskID = sendDirectChat(t, ctx, agentID, sessionID, prompt)
			} else {
				agentID, runtimeID, daemonID = createRuntimeGuardAgent(t, ctx)
				payload, err := json.Marshal(map[string]string{"type": "quick_create", "prompt": prompt, "requester_id": testUserID, "workspace_id": testWorkspaceID})
				if err != nil {
					t.Fatal(err)
				}
				taskID = dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "context": payload})
			}
			claim := func() *AgentTaskResponse {
				t.Helper()
				req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
				req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilitySkillBundlesV1)
				req = withURLParam(req, "runtimeId", runtimeID)
				var body struct {
					Task *AgentTaskResponse `json:"task"`
				}
				testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&body)
				if body.Task == nil || body.Task.Agent == nil {
					t.Fatal("missing claimed task")
				}
				return body.Task
			}
			first := claim()
			var selected service.AgentSkillRefData
			for _, ref := range first.Agent.SkillRefs {
				if ref.ID == skillID {
					selected = ref
				}
			}
			if selected.ID == "" {
				t.Fatal("selected Skill missing after loading task input")
			}
			if kind == "chat" {
				markTaskRunning(t, ctx, taskID)
				if _, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), completeResult(t, "done"), "", "", "", false, "", ""); err != nil {
					t.Fatal(err)
				}
				nextID := sendDirectChat(t, ctx, agentID, sessionID, "next task without a Skill selection")
				next := claim()
				for _, ref := range next.Agent.SkillRefs {
					if ref.ID == skillID {
						t.Fatal("later Run inherited previous selection")
					}
				}
				req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/"+nextID+"/skill-bundles/resolve", resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{ID: selected.ID, Source: selected.Source, Hash: selected.Hash}}}, testWorkspaceID, daemonID)
				req = withURLParams(req, "runtimeId", runtimeID, "taskId", nextID)
				testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusNotFound)
			}
		})
	}
}

func TestCreateIssueFreezesSelectedWorkspaceSkillForInitialRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Create Issue Selected Skill runtime")
	agentID := dbfx.Agent(t, "Create Issue Selected Skill agent", runtimeID)
	selectedID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "architecture-sweep",
		"description":  "Selected in the create-task description",
		"content":      "selected content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	unselectedID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "later-description-skill",
		"description":  "Added only after the initial task was created",
		"content":      "must not be selected",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})

	w := testutil.Call(t, testHandler.CreateIssue, newRequest(
		http.MethodPost,
		"/api/issues?workspace_id="+testWorkspaceID,
		map[string]any{
			"title":         "Create task with selected Skill",
			"description":   "please [/architecture-sweep](slash://skill/" + selectedID + ")",
			"status":        "todo",
			"priority":      "none",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		},
	)).Want(http.StatusCreated)

	var created struct {
		ID string `json:"id"`
	}
	w.JSON(&created)
	if created.ID == "" {
		t.Fatalf("missing created issue id: %s", w.Body.String())
	}
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)

	var taskID, persistedContext string
	dbfx.QueryRow(t, `
		SELECT id::text, COALESCE(context::text, 'null')
		FROM agent_task_queue
		WHERE issue_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, created.ID).Scan(&taskID, &persistedContext)
	if !strings.Contains(persistedContext, selectedID) {
		t.Fatalf("initial task %s did not freeze selected Skill %s: %s", taskID, selectedID, persistedContext)
	}

	// The issue body is mutable after creation. Replacing its marker must not
	// revoke the original grant or smuggle a different Skill into this run.
	dbfx.Exec(t, `UPDATE issue SET description = $2 WHERE id = $1`, created.ID,
		"later [/later-description-skill](slash://skill/"+unselectedID+")")
	req := newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+runtimeID+"/tasks/claim",
		nil,
		testWorkspaceID,
		"create-issue-selected-skill-daemon",
	)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilitySkillBundlesV1)
	req = withURLParam(req, "runtimeId", runtimeID)
	w = testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var claim struct {
		Task *AgentTaskResponse `json:"task"`
	}
	w.JSON(&claim)
	if claim.Task == nil || claim.Task.Agent == nil {
		t.Fatalf("missing claimed task Agent: %s", w.Body.String())
	}
	selectedFound := false
	for _, ref := range claim.Task.Agent.SkillRefs {
		if ref.ID == selectedID {
			selectedFound = true
		}
		if ref.ID == unselectedID {
			t.Fatalf("Skill added to mutable issue description leaked into initial run: %+v", ref)
		}
	}
	if !selectedFound {
		t.Fatalf("create-time selected Skill missing from initial claim refs: %+v", claim.Task.Agent.SkillRefs)
	}
}

func TestClaimTaskByRuntime_SelectedWorkspaceSkillGrant(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Selected Skill runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Selected Skill agent")
	selectedID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "selected-workspace-skill",
		"description":  "Selected only for this run",
		"content":      "selected content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	unselectedID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "unselected-workspace-skill",
		"description":  "Must remain unavailable",
		"content":      "unselected content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	triggerID := dbfx.Comment(
		t,
		issueID,
		"please [/selected-workspace-skill](slash://skill/"+selectedID+")",
	)
	agentCommentID := dbfx.Comment(t, issueID,
		"[/agent-suggested](slash://skill/"+unselectedID+")",
		testutil.Cols{"author_type": "agent", "author_id": agentID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":            runtimeID,
		"issue_id":              issueID,
		"trigger_comment_id":    triggerID,
		"coalesced_comment_ids": []string{triggerID, agentCommentID},
	})

	req := newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+runtimeID+"/tasks/claim",
		nil,
		testWorkspaceID,
		"selected-skill-daemon",
	)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilitySkillBundlesV1)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var claim struct {
		Task *AgentTaskResponse `json:"task"`
	}
	w.JSON(&claim)
	if claim.Task == nil || claim.Task.Agent == nil {
		t.Fatalf("missing claimed task Agent: %s", w.Body.String())
	}
	var selectedRef service.AgentSkillRefData
	for _, ref := range claim.Task.Agent.SkillRefs {
		if ref.ID == selectedID {
			selectedRef = ref
		}
		if ref.ID == unselectedID {
			t.Fatalf("unselected workspace Skill leaked into claim: %+v", ref)
		}
	}
	if selectedRef.ID == "" || selectedRef.Hash == "" {
		t.Fatalf("selected workspace Skill missing from claim refs: %+v", claim.Task.Agent.SkillRefs)
	}

	var persistedContext string
	dbfx.QueryRow(t, `SELECT context::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&persistedContext)
	if !strings.Contains(persistedContext, selectedID) || strings.Contains(persistedContext, unselectedID) {
		t.Fatalf("persisted selected-Skill grant is not exact: %s", persistedContext)
	}

	resolveSelected := resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{
		ID: selectedRef.ID, Source: selectedRef.Source, Hash: selectedRef.Hash,
	}}}
	req = newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve",
		resolveSelected,
		testWorkspaceID,
		"selected-skill-daemon",
	)
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusOK)

	resolveUnselected := resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{
		ID: unselectedID, Source: selectedRef.Source, Hash: selectedRef.Hash,
	}}}
	req = newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve",
		resolveUnselected,
		testWorkspaceID,
		"selected-skill-daemon",
	)
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusNotFound)
}
