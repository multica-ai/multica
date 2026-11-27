package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

type graphTestNode struct {
	ID      string         `json:"id"`
	Kind    string         `json:"kind"`
	IssueID string         `json:"issue_id"`
	RunID   string         `json:"run_id"`
	Title   string         `json:"title"`
	Details map[string]any `json:"details"`
}

type graphTestResponse struct {
	Root  string          `json:"root_issue_id"`
	Nodes []graphTestNode `json:"nodes"`
	Edges []struct {
		ID   string `json:"id"`
		From string `json:"from"`
		To   string `json:"to"`
		Kind string `json:"kind"`
	} `json:"edges"`
	Findings []struct {
		Code   string `json:"code"`
		NodeID string `json:"node_id"`
	} `json:"findings"`
	Evidence []any `json:"evidence"`
	Coverage struct {
		Complete  bool     `json:"complete"`
		Truncated bool     `json:"truncated"`
		Warnings  []string `json:"warnings"`
	} `json:"coverage"`
}

func trajectoryGraphTestHandler() Handler {
	h := *testHandler
	flags := featureflag.NewStaticProvider()
	flags.Set(featureflags.TrajectoryV1, featureflag.Rule{Default: true})
	h.FeatureFlags = featureflag.NewService(flags)
	return h
}

func graphTestCall(t *testing.T, h *Handler, user, root, scope string, ids []string) (*httptest.ResponseRecorder, graphTestResponse) {
	t.Helper()
	var req *http.Request
	var call http.HandlerFunc
	if ids != nil {
		req = asUser(newRequest("POST", "/api/issues/trajectory", map[string]any{"issue_ids": ids}), user)
		call = h.ListTaskTrajectory
	} else {
		req = asUser(withURLParam(newRequest("GET", "/api/issues/"+root+"/trajectory"+scope, nil), "id", root), user)
		call = h.GetTrajectory
	}
	w := httptest.NewRecorder()
	call(w, req)
	var graph graphTestResponse
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &graph); err != nil {
			t.Fatal(err)
		}
		if graph.Nodes == nil || graph.Edges == nil || graph.Findings == nil || graph.Evidence == nil || graph.Coverage.Warnings == nil {
			t.Fatalf("missing graph arrays: %s", w.Body.String())
		}
	}
	return w, graph
}

func graphTestNodeMap(graph graphTestResponse) map[string]graphTestNode {
	nodes := map[string]graphTestNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	return nodes
}

func graphTestTask(t *testing.T, agent, issue string, over testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{"issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")}
	for key, value := range over {
		cols[key] = value
	}
	return dbfx.Task(t, agent, cols)
}

func graphTestAttachment(t *testing.T, issue, comment, task, title string) string {
	t.Helper()
	cols := testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": issue, "filename": title, "content_type": "text/plain", "size_bytes": 1, "url": "STORAGE_URL_SENTINEL", "uploader_type": "member", "uploader_id": testUserID}
	if comment != "" {
		cols["comment_id"] = comment
	}
	if task != "" {
		cols["task_id"] = task
	}
	return dbfx.Insert(t, "attachment", cols)
}

func TestTrajectoryGraphNativeProjectionAndRelationships(t *testing.T) {
	h := trajectoryGraphTestHandler()
	parent := dbfx.Issue(t, "historical parent", testutil.Cols{"status": "done", "created_at": testutil.Raw("'2025-01-01'::timestamptz")})
	issue := dbfx.Issue(t, "graph child", testutil.Cols{"parent_issue_id": parent, "stage": 2, "created_at": testutil.Raw("'2025-02-01'::timestamptz")})
	agent := createHandlerTestAgent(t, "graph native agent", []byte("[]"))
	source := graphTestTask(t, agent, parent, nil)
	run := graphTestTask(t, agent, issue, testutil.Cols{"retry_of_task_id": source, "rerun_of_task_id": source, "delegated_from_task_id": source, "parent_task_id": source, "context": `{"instructions":"stored instructions","trigger_kind":"future_trigger","secret":"RAW_CONTEXT_SENTINEL"}`, "issue_snapshot": `{"title":"stored input"}`, "result": fmt.Sprintf(`{"output":%q,"secret":"RAW_RESULT_SENTINEL"}`, strings.Repeat("界", 16001))})
	comment := dbfx.Comment(t, issue, "native body", testutil.Cols{"source_task_id": run})
	deleted := dbfx.Comment(t, issue, "DELETED_SENTINEL", testutil.Cols{"deleted_at": testutil.Raw("now()")})
	artifact := graphTestAttachment(t, issue, comment, "", "proof.txt")
	orphan := graphTestAttachment(t, issue, "", run, "orphan.txt")
	deletedArtifact := graphTestAttachment(t, issue, deleted, "", "DELETED_ARTIFACT_SENTINEL")
	dbfx.Exec(t, "UPDATE issue SET origin_type='agent_create',origin_id=$1 WHERE id=$2", source, issue)
	dbfx.Exec(t, "UPDATE agent_task_queue SET trigger_comment_id=$1,trigger_evidence_kind='comment',trigger_evidence_ref_id=$1 WHERE id=$2", comment, run)
	statusEvent := dbfx.Insert(t, "activity_log", testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": parent, "action": "status_changed", "actor_type": "member", "actor_id": testUserID, "created_at": testutil.Raw("'2025-01-20'::timestamptz"), "details": `{"to":"in_review"}`})
	dbfx.Insert(t, "activity_log", testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": parent, "action": "status_changed", "actor_type": "member", "actor_id": testUserID, "created_at": testutil.Raw("'2025-02-20'::timestamptz"), "details": `{"to":"done"}`})
	event := dbfx.Insert(t, "activity_log", testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": issue, "action": "future_action", "actor_type": "member", "actor_id": testUserID, "details": `{"native":true}`})
	dependency := dbfx.Insert(t, "issue_dependency", testutil.Cols{"issue_id": issue, "depends_on_issue_id": parent, "type": "blocked_by"})
	related := dbfx.Insert(t, "issue_dependency", testutil.Cols{"issue_id": issue, "depends_on_issue_id": parent, "type": "related"})
	duplicate := dbfx.Issue(t, "duplicate", testutil.Cols{"status": "cancelled", "duplicate_of_issue_id": issue})
	for index, model := range []string{"model-z", "model-a", "model-z", ""} {
		dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": run, "provider": fmt.Sprintf("fixture-%d", index), "model": model})
	}
	dbfx.Insert(t, "task_message", testutil.Cols{"task_id": run, "seq": 1, "type": "text", "content": "LOG_MUST_NOT_PRELOAD"})
	w, graph := graphTestCall(t, &h, testUserID, issue, "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, sentinel := range []string{"DELETED_SENTINEL", "DELETED_ARTIFACT_SENTINEL", "STORAGE_URL_SENTINEL", "RAW_CONTEXT_SENTINEL", "RAW_RESULT_SENTINEL", "LOG_MUST_NOT_PRELOAD"} {
		if strings.Contains(w.Body.String(), sentinel) {
			t.Errorf("disclosed %s", sentinel)
		}
	}
	nodes := graphTestNodeMap(graph)
	if len(nodes) != len(graph.Nodes) {
		t.Fatal("duplicate node identity")
	}
	if _, ok := nodes["attachment:"+deletedArtifact]; ok {
		t.Fatal("deleted-comment artifact included")
	}
	child := nodes["issue:"+issue]
	if child.Details["creation_issue_id"] != parent || child.Details["creation_run_id"] != source || child.Details["creation_source_status"] != "in_review" || child.Details["creation_status_event_id"] != statusEvent || child.Details["stage"] != float64(2) {
		t.Fatalf("historical creation: %+v", child.Details)
	}
	r := nodes["run:"+run]
	if r.Details["instructions"] != "stored instructions" || r.Details["trigger_content"] != "native body" || r.Details["output_truncated"] != true || utf8.RuneCountInString(r.Details["output"].(string)) != 16000 {
		t.Fatalf("run projection: %+v", r.Details)
	}
	models := r.Details["models"].([]any)
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-z" {
		t.Fatalf("models=%v", models)
	}
	if nodes["fact:"+event].Details["source_kind"] != "platform_fact" || nodes["attachment:"+artifact].RunID != run || nodes["attachment:"+orphan].RunID != "" {
		t.Fatal("native attribution changed")
	}
	counts := map[string]int{}
	edges := map[string]bool{}
	for _, edge := range graph.Edges {
		if nodes[edge.From].ID == "" || nodes[edge.To].ID == "" || edges[edge.ID] {
			t.Fatalf("invalid or duplicate edge: %+v", edge)
		}
		edges[edge.ID] = true
		counts[edge.Kind]++
	}
	for _, kind := range []string{"parent", "created_from", "contains", "retry", "rerun", "delegation", "produces", "attachment", "issue_blocks", "issue_related", "issue_duplicate"} {
		if counts[kind] == 0 {
			t.Errorf("missing relation %s", kind)
		}
	}
	if counts["delegation"] != 1 || !edges["issue_dependency:"+dependency] || !edges["issue_dependency:"+related] || !edges["issue_duplicate:"+duplicate] {
		t.Fatal("native relation IDs or de-duplication changed")
	}
	if graph.Coverage.Complete || graph.Coverage.Truncated || len(graph.Findings) != 1 || graph.Findings[0].Code != "missing_input_snapshot" || graph.Findings[0].NodeID != "run:"+source {
		t.Fatalf("snapshot coverage: %+v findings=%v", graph.Coverage, graph.Findings)
	}
	_, own := graphTestCall(t, &h, testUserID, issue, "?scope=issue", nil)
	for _, node := range own.Nodes {
		if node.IssueID != issue {
			t.Fatalf("own scope widened: %+v", node)
		}
	}
}

func TestTrajectoryGraphProjectsOnlyNativeRunCallProof(t *testing.T) {
	h := trajectoryGraphTestHandler()
	sourceIssue := dbfx.Issue(t, "call source")
	targetIssue := dbfx.Issue(t, "call target")
	agent := createHandlerTestAgent(t, "call proof agent", []byte("[]"))
	source := graphTestTask(t, agent, sourceIssue, nil)
	comment := dbfx.Comment(t, targetIssue, "trigger", testutil.Cols{"author_type": "agent", "author_id": agent, "source_task_id": source, "created_at": testutil.Raw("'2025-01-01'::timestamptz")})
	deleted := dbfx.Comment(t, targetIssue, "deleted trigger", testutil.Cols{"author_type": "agent", "author_id": agent, "source_task_id": source, "deleted_at": testutil.Raw("now()")})
	// Even an Issue created by this Run can be assigned/promoted later by another actor.
	dbfx.Exec(t, "UPDATE issue SET origin_type='agent_create',origin_id=$1 WHERE id=$2", source, targetIssue)
	supported := map[string]string{}
	excluded := map[string]string{}
	for _, tc := range []struct {
		label string
		over  testutil.Cols
	}{
		{"valid", nil},
		{"comment source", testutil.Cols{"originator_source": "comment_source"}},
		{"owner fallback", testutil.Cols{"originator_source": "owner_fallback"}},
		{"missing comment", testutil.Cols{"trigger_comment_id": nil}},
		{"mismatched Run", testutil.Cols{"delegated_from_task_id": nil}},
		{"not agent-triggered", testutil.Cols{"originator_source": "direct_human"}},
		{"retry", testutil.Cols{"retry_of_task_id": source}},
		{"rerun", testutil.Cols{"rerun_of_task_id": source}},
		{"wakeup", testutil.Cols{"context": fmt.Sprintf(`{"wakeup_id":%q}`, uuid.NewString())}},
		{"autopilot", testutil.Cols{"context": `{"trigger_kind":"autopilot_run"}`}},
		{"merged input", testutil.Cols{"coalesced_comment_ids": testutil.Raw(fmt.Sprintf("ARRAY['%s']::uuid[]", deleted))}},
		{"later primary comment", testutil.Cols{"created_at": testutil.Raw("'2024-01-01'::timestamptz")}},
		{"deleted evidence", testutil.Cols{"trigger_comment_id": deleted, "trigger_evidence_ref_id": deleted}},
		{"issue assignment attribution", testutil.Cols{"trigger_comment_id": nil, "trigger_evidence_kind": "issue_assignment", "trigger_evidence_ref_id": targetIssue}},
	} {
		fields := testutil.Cols{"trigger_comment_id": comment, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": comment, "originator_source": "delegation", "delegated_from_task_id": source}
		for name, value := range tc.over {
			fields[name] = value
		}
		run := graphTestTask(t, agent, targetIssue, fields)
		if tc.label == "valid" || tc.label == "comment source" || tc.label == "owner fallback" {
			supported[run] = tc.label
		} else {
			excluded[run] = tc.label
		}
	}
	w, graph := graphTestCall(t, &h, testUserID, targetIssue, "?scope=issue", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("read graph: %s", w.Body)
	}
	nodes := graphTestNodeMap(graph)
	for run, label := range supported {
		proof, _ := nodes["run:"+run].Details["run_call"].(map[string]any)
		if proof["comment_id"] != comment || proof["source_issue_id"] != sourceIssue || nodes["comment:"+comment].RunID != source {
			t.Errorf("%s: missing call proof or comment source binding", label)
		}
	}
	for run, label := range excluded {
		node := nodes["run:"+run]
		if node.ID == "" || node.Details["run_call"] != nil {
			t.Errorf("%s: missing Run or unexpected call proof %v", label, node.Details["run_call"])
		}
	}
	if _, exists := nodes["run:"+source]; exists {
		t.Fatal("own-Issue graph widened to fetch the source Run")
	}
}

func TestTrajectoryGraphRunCallProofHonorsGuestNoProjectScope(t *testing.T) {
	h := trajectoryGraphTestHandler()
	guest := dbfx.User(t, "trajectory guest", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, guest, "guest")
	project := dbfx.Project(t, "trajectory guest project")
	addProjectMemberRow(t, project, guest)
	sourceIssue := dbfx.Issue(t, "outside any project")
	targetIssue := dbfx.Issue(t, "guest project target", testutil.Cols{"project_id": project})
	agent := createHandlerTestAgent(t, "guest proof agent", []byte("[]"))
	source := graphTestTask(t, agent, sourceIssue, nil)
	comment := dbfx.Comment(t, targetIssue, "visible trigger", testutil.Cols{"author_type": "agent", "author_id": agent, "source_task_id": source})
	target := graphTestTask(t, agent, targetIssue, testutil.Cols{"trigger_comment_id": comment, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": comment, "originator_source": "delegation", "delegated_from_task_id": source})

	// The HTTP route remains closed to guests. Exercise the record projection
	// against a real guest hidden set independently of that route allowlist.
	if w, _ := graphTestCall(t, &h, guest, targetIssue, "?scope=issue", nil); w.Code != http.StatusForbidden {
		t.Fatalf("trajectory guest route status = %d", w.Code)
	}
	workspaceID := util.MustParseUUID(testWorkspaceID)
	targetID, sourceID := util.MustParseUUID(targetIssue), util.MustParseUUID(sourceIssue)
	req := asUser(newRequest("GET", "/api/issues/"+targetIssue, nil), guest)
	hidden, err := testHandler.hiddenProjects(req, testWorkspaceID)
	if err != nil || hidden.CanSeeProject(pgtype.UUID{}) {
		t.Fatalf("guest no-project scope unavailable: %v", err)
	}
	member, err := testHandler.getWorkspaceMember(t.Context(), guest, testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testPool.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	nodes, err := readTrajectoryGraphNodes(t.Context(), tx, trajectoryGraphRunsSQL, workspaceID, []pgtype.UUID{targetID}, hidden.Param(), member.UserID, false)
	if err != nil || len(nodes) != 1 || nodes[0].ID != "run:"+target {
		t.Fatalf("expected visible target Run: length=%d, err=%v", len(nodes), err)
	}
	if nodes[0].Details["run_call"] != nil {
		t.Fatal("guest received call proof about an Issue outside its projects")
	}
	issues, err := readTrajectoryGraphNodes(t.Context(), tx, trajectoryGraphIssueSQL, workspaceID, []pgtype.UUID{sourceID, targetID}, hidden.Param())
	if err != nil || len(issues) != 1 || issues[0].IssueID != targetIssue {
		t.Fatalf("expected only the project issue: length=%d, err=%v", len(issues), err)
	}
	comments, err := readTrajectoryGraphNodes(t.Context(), tx, trajectoryGraphCommentsSQL, workspaceID, []pgtype.UUID{targetID}, hidden.Param())
	if err != nil || len(comments) != 1 || comments[0].RunID != "" {
		t.Fatalf("expected only the project comment without an out-of-scope run: length=%d, err=%v", len(comments), err)
	}
}

func TestTrajectoryGraphPreservesNativeVisibility(t *testing.T) {
	f := newPrivateProjectFixture(t)
	h := trajectoryGraphTestHandler()
	agent := dbfx.Agent(t, "PRIVATE_AGENT_NAME_SENTINEL", "", testutil.Cols{"owner_id": f.insider, "permission_mode": "private"})
	secretRun := graphTestTask(t, agent, f.secretIssue, nil)
	publicRun := graphTestTask(t, agent, f.publicIssue, testutil.Cols{"trigger_comment_id": f.secretComment, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": f.secretComment})
	dbfx.Exec(t, "UPDATE issue SET parent_issue_id=$1,origin_type='agent_create',origin_id=$2 WHERE id=$3", f.secretIssue, secretRun, f.publicIssue)
	comment := dbfx.Comment(t, f.publicIssue, "visible comment", testutil.Cols{"author_type": "agent", "author_id": agent, "source_task_id": secretRun})
	// The comment is visible, but the caller is not visible to outsiders.
	hiddenCallerTarget := graphTestTask(t, agent, f.publicIssue, testutil.Cols{"trigger_comment_id": comment, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": comment, "originator_source": "delegation", "delegated_from_task_id": secretRun})
	artifact := graphTestAttachment(t, f.publicIssue, comment, "", "visible.txt")
	chat := dbfx.ChatSession(t, agent, testutil.Cols{"creator_id": f.insider})
	chatRun := graphTestTask(t, agent, f.publicIssue, testutil.Cols{"chat_session_id": chat, "result": `{"output":"CHAT_RUN_SENTINEL"}`})
	chatComment := dbfx.Comment(t, f.publicIssue, "public comment with unavailable source", testutil.Cols{"source_task_id": chatRun})
	chatArtifact := graphTestAttachment(t, f.publicIssue, chatComment, "", "public artifact with unavailable source")
	foreignWS := dbfx.Workspace(t, "foreign graph", uuid.NewString())
	foreign := dbfx.Issue(t, "FOREIGN_GRAPH_SENTINEL", testutil.Cols{"workspace_id": foreignWS, "parent_issue_id": f.publicIssue})
	for _, user := range []string{f.outsider, f.outsideAdmin} {
		w, graph := graphTestCall(t, &h, user, f.publicIssue, "", nil)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		for _, sentinel := range []string{"private project secret", "CHAT_RUN_SENTINEL", "FOREIGN_GRAPH_SENTINEL"} {
			if strings.Contains(w.Body.String(), sentinel) {
				t.Errorf("private payload %s", sentinel)
			}
		}
		if user == f.outsider && strings.Contains(w.Body.String(), "PRIVATE_AGENT_NAME_SENTINEL") {
			t.Fatal("private name exposed to regular member")
		}
		nodes := graphTestNodeMap(graph)
		for _, absent := range []string{"issue:" + f.secretIssue, "issue:" + foreign, "run:" + secretRun, "run:" + chatRun} {
			if nodes[absent].ID != "" {
				t.Errorf("inaccessible node=%s", absent)
			}
		}
		if nodes["issue:"+f.publicIssue].Details["parent_id"] != nil || nodes["issue:"+f.publicIssue].Details["creation_run_id"] != nil || nodes["issue:"+f.publicIssue].Details["run_count"] != float64(2) {
			t.Fatal("hidden reference or chat count exposed")
		}
		if nodes["run:"+hiddenCallerTarget].ID == "" || nodes["run:"+hiddenCallerTarget].Details["run_call"] != nil {
			t.Fatal("missing target or hidden caller proof exposed")
		}
		if nodes["run:"+publicRun].Details["trigger_content"] != nil {
			t.Fatal("hidden trigger body exposed")
		}
		if user == f.outsider && nodes["run:"+publicRun].Details["agent_name"] != nil {
			t.Fatal("private agent name exposed")
		}
		for _, key := range []string{"comment:" + comment, "attachment:" + artifact, "comment:" + chatComment, "attachment:" + chatArtifact} {
			if nodes[key].RunID != "" || nodes[key].Details["source_run_id"] != nil {
				t.Errorf("unavailable producer mapped: %s", key)
			}
		}
		w, _ = graphTestCall(t, &h, user, f.secretIssue, "?scope=issue", nil)
		if w.Code != 404 {
			t.Errorf("hidden issue status=%d", w.Code)
		}
		w, _ = graphTestCall(t, &h, user, "", "", []string{f.publicIssue, f.secretIssue})
		if w.Code != 404 {
			t.Errorf("list private member status=%d", w.Code)
		}
	}
	w, _ := graphTestCall(t, &h, f.insider, f.secretIssue, "?scope=issue", nil)
	if w.Code != 200 {
		t.Fatal("native project insider lost access")
	}
	w, insiderGraph := graphTestCall(t, &h, f.insider, f.publicIssue, "?scope=issue", nil)
	proof, _ := graphTestNodeMap(insiderGraph)["run:"+hiddenCallerTarget].Details["run_call"].(map[string]any)
	if w.Code != http.StatusOK || proof["comment_id"] != comment || proof["source_issue_id"] != f.secretIssue {
		t.Fatal("authorized insider lost visible caller proof")
	}
	// A context member from another workspace must not act as a membership grant.
	member, err := h.getWorkspaceMember(t.Context(), testUserID, testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	req := asUser(newRequest("POST", "/api/issues/trajectory", map[string]any{"issue_ids": []string{foreign}}), testUserID)
	req.Header.Set("X-Workspace-ID", foreignWS)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), foreignWS, member))
	testutil.Call(t, h.ListTaskTrajectory, req).Want(404)
}

func TestTrajectoryGraphDeletedTriggerAliasesKeepNativeMetadata(t *testing.T) {
	h := trajectoryGraphTestHandler()
	issue := dbfx.Issue(t, "deleted trigger graph")
	agent := createHandlerTestAgent(t, "deleted trigger graph agent", []byte("[]"))
	live := dbfx.Comment(t, issue, "live allowed trigger")
	deleted := dbfx.Comment(t, issue, "DELETED_TRIGGER_SENTINEL", testutil.Cols{"deleted_at": testutil.Raw("now()")})
	for _, refs := range []testutil.Cols{
		{"trigger_comment_id": deleted},
		{"trigger_comment_id": live, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": deleted},
		{"trigger_comment_id": live, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": uuid.NewString()},
	} {
		refs["context"] = `{"instructions":"native instructions remain"}`
		refs["issue_snapshot"] = `{"title":"native snapshot remains"}`
		refs["result"] = `{"output":"native output remains"}`
		graphTestTask(t, agent, issue, refs)
	}
	w, graph := graphTestCall(t, &h, testUserID, issue, "?scope=issue", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "DELETED_TRIGGER_SENTINEL") {
		t.Fatal("deleted body present")
	}
	for _, node := range graph.Nodes {
		if node.Kind != "run" {
			continue
		}
		if node.Details["trigger_content"] != nil || node.Details["instructions"] != "native instructions remain" || node.Details["output"] != "native output remains" || node.Details["issue_snapshot"] == nil {
			t.Fatalf("trigger gating or native metadata: %+v", node.Details)
		}
	}
}

func TestTrajectoryGraphCommentSummaryTitlesRespectSourceAccess(t *testing.T) {
	f := newPrivateProjectFixture(t)
	h := trajectoryGraphTestHandler()
	agent := createHandlerTestAgent(t, "comment title alias graph agent", []byte("[]"))
	live := dbfx.Comment(t, f.publicIssue, "live allowed comment")
	deleted := dbfx.Comment(t, f.publicIssue, "DELETED_TITLE_SENTINEL", testutil.Cols{"deleted_at": testutil.Raw("now()")})
	foreignWorkspace := dbfx.Workspace(t, "foreign title alias", uuid.NewString())
	foreignIssue := dbfx.Issue(t, "foreign title alias issue", testutil.Cols{"workspace_id": foreignWorkspace})
	foreignComment := dbfx.Comment(t, foreignIssue, "FOREIGN_TITLE_SENTINEL", testutil.Cols{"workspace_id": foreignWorkspace})
	var expected = map[string]string{}
	for _, tc := range []struct {
		name, summary, want string
		refs                testutil.Cols
	}{
		{"deleted direct", "DELETED_TITLE_SENTINEL", "Agent run", testutil.Cols{"trigger_comment_id": deleted}},
		{"deleted evidence", "DELETED_EVIDENCE_TITLE_SENTINEL", "Agent run", testutil.Cols{"trigger_comment_id": live, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": deleted}},
		{"foreign direct", "FOREIGN_TITLE_SENTINEL", "Agent run", testutil.Cols{"trigger_comment_id": foreignComment}},
		{"missing evidence", "MISSING_EVIDENCE_TITLE_SENTINEL", "Agent run", testutil.Cols{"trigger_comment_id": live, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": uuid.NewString()}},
		{"private direct", "PRIVATE_TITLE_SENTINEL", "Agent run", testutil.Cols{"trigger_comment_id": f.secretComment}},
		{"private evidence", "PRIVATE_EVIDENCE_TITLE_SENTINEL", "Agent run", testutil.Cols{"trigger_evidence_kind": "comment", "trigger_evidence_ref_id": f.secretComment}},
		{"live summary", "allowed live summary", "allowed live summary", testutil.Cols{"trigger_comment_id": live, "trigger_evidence_kind": "comment", "trigger_evidence_ref_id": live}},
		{"unrelated summary", "allowed arbitrary native summary", "allowed arbitrary native summary", testutil.Cols{}},
	} {
		tc.refs["trigger_summary"] = tc.summary
		tc.refs["issue_snapshot"] = `{"title":"native input remains"}`
		run := graphTestTask(t, agent, f.publicIssue, tc.refs)
		expected["run:"+run] = tc.want
	}
	w, graph := graphTestCall(t, &h, f.outsider, f.publicIssue, "?scope=issue", nil)
	if w.Code != 200 {
		t.Fatalf("graph status=%d body=%s", w.Code, w.Body.String())
	}
	for _, node := range graph.Nodes {
		if node.Kind != "run" {
			continue
		}
		if node.Title != expected[node.ID] {
			t.Errorf("comment-derived title=%q want=%q node=%s", node.Title, expected[node.ID], node.ID)
		}
	}
	for _, sentinel := range []string{"DELETED_TITLE_SENTINEL", "DELETED_EVIDENCE_TITLE_SENTINEL", "FOREIGN_TITLE_SENTINEL", "MISSING_EVIDENCE_TITLE_SENTINEL", "PRIVATE_TITLE_SENTINEL", "PRIVATE_EVIDENCE_TITLE_SENTINEL"} {
		if strings.Contains(w.Body.String(), sentinel) {
			t.Errorf("inaccessible comment summary leaked: %s", sentinel)
		}
	}
}

func TestTrajectoryGraphInputAndIssueWindowBounds(t *testing.T) {
	h := trajectoryGraphTestHandler()
	for _, count := range []int{0, 1, 100, 200, 201} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ids := []string{}
			for range count {
				ids = append(ids, dbfx.Issue(t, "input window"))
			}
			w, graph := graphTestCall(t, &h, testUserID, "", "", ids)
			if count > 200 {
				if w.Code != 400 {
					t.Fatalf("oversized input status=%d", w.Code)
				}
				return
			}
			if w.Code != 200 || len(graph.Nodes) != count || graph.Coverage.Truncated || !graph.Coverage.Complete {
				t.Fatalf("window count=%d status=%d graph=%+v", count, w.Code, graph.Coverage)
			}
		})
	}
	for _, body := range []string{`{"issue_ids":["invalid"]}`, `{"issue_ids":[]} {}`, `{"issue_ids":[]` + strings.Repeat(" ", 32<<10) + `}`} {
		req := newRequest("POST", "/api/issues/trajectory", nil)
		req.Body = io.NopCloser(strings.NewReader(body))
		testutil.Call(t, h.ListTaskTrajectory, req).Want(400)
	}
	issue := dbfx.Issue(t, "invalid scope")
	w, _ := graphTestCall(t, &h, testUserID, issue, "?scope=related", nil)
	if w.Code != 400 {
		t.Fatal("unsupported scope accepted")
	}
	for _, children := range []int{199, 200} {
		root := dbfx.Issue(t, "expanded limit root")
		for range children {
			dbfx.Issue(t, "expanded child", testutil.Cols{"parent_issue_id": root})
		}
		w, graph := graphTestCall(t, &h, testUserID, "", "", []string{root})
		if w.Code != 200 || len(graph.Nodes) != 200 || graph.Coverage.Truncated != (children == 200) || graph.Coverage.Complete != (children == 199) {
			t.Fatalf("expanded count=%d graph=%+v nodes=%d status=%d", children, graph.Coverage, len(graph.Nodes), w.Code)
		}
	}
	root := dbfx.Issue(t, "eight-hop root")
	previous := root
	for range 9 {
		previous = dbfx.Issue(t, "hop", testutil.Cols{"parent_issue_id": previous})
	}
	_, graph := graphTestCall(t, &h, testUserID, "", "", []string{root})
	if len(graph.Nodes) != 9 || !graph.Coverage.Truncated || graph.Coverage.Complete || len(graph.Coverage.Warnings) != 1 {
		t.Fatalf("eight hops: nodes=%d coverage=%+v", len(graph.Nodes), graph.Coverage)
	}
}

type graphTestSnapshotStarter struct {
	txStarter
	observed      bool
	readOnly      bool
	isolation     string
	cancel        context.CancelFunc
	afterSnapshot func()
}
type graphTestSnapshotTx struct {
	pgx.Tx
	owner *graphTestSnapshotStarter
}

func (s *graphTestSnapshotStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &graphTestSnapshotTx{Tx: tx, owner: s}, nil
}
func (tx *graphTestSnapshotTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(sql, "-- trajectory graph issues") && !tx.owner.observed {
		tx.owner.observed = true
		var readOnly string
		if err := tx.Tx.QueryRow(ctx, "SELECT current_setting('transaction_read_only'),current_setting('transaction_isolation')").Scan(&readOnly, &tx.owner.isolation); err != nil {
			return nil, err
		}
		tx.owner.readOnly = readOnly == "on"
		if tx.owner.afterSnapshot != nil {
			tx.owner.afterSnapshot()
		}
		if tx.owner.cancel != nil {
			tx.owner.cancel()
			return nil, context.Canceled
		}
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func TestTrajectoryGraphReadOnlySnapshotAndCancellation(t *testing.T) {
	h := trajectoryGraphTestHandler()
	issue := dbfx.Issue(t, "snapshot graph")
	starter := &graphTestSnapshotStarter{txStarter: testPool}
	starter.afterSnapshot = func() { dbfx.Exec(t, "UPDATE issue SET title='changed after snapshot' WHERE id=$1", issue) }
	h.TxStarter = starter
	w, graph := graphTestCall(t, &h, testUserID, issue, "?scope=issue", nil)
	if w.Code != 200 || !starter.observed || !starter.readOnly || starter.isolation != "repeatable read" {
		t.Fatalf("snapshot status=%d observed=%t readonly=%t isolation=%s", w.Code, starter.observed, starter.readOnly, starter.isolation)
	}
	if graph.Nodes[0].Title != "snapshot graph" {
		t.Fatal("graph observed a concurrent post-snapshot change")
	}
	req := withURLParam(newRequest("GET", "/api/issues/"+issue+"/trajectory?scope=issue", nil), "id", issue)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	h.TxStarter = &graphTestSnapshotStarter{txStarter: testPool, cancel: cancel}
	response := testutil.Call(t, h.GetTrajectory, req.WithContext(ctx)).Want(499)
	if response.Body.Len() != 0 {
		t.Fatal("cancelled graph body emitted")
	}
	// A database timeout without a parent cancellation remains an error.
	h.TxStarter = graphTestFailStarter{}
	testutil.Call(t, h.GetTrajectory, req).Want(500)
}

type graphTestFailStarter struct{}

func (graphTestFailStarter) Begin(context.Context) (pgx.Tx, error) {
	return nil, &pgconn.PgError{Code: "57014", Message: "SQL_PARAMETER_SECRET"}
}

func TestTrajectoryGraphNativeRecordLimitSentinels(t *testing.T) {
	h := trajectoryGraphTestHandler()
	agent := createHandlerTestAgent(t, "bounded graph agent", []byte("[]"))
	for _, source := range []struct {
		kind, table, sql string
		limit            int
	}{
		{"run", "agent_task_queue", `INSERT INTO agent_task_queue (agent_id,issue_id,status,completed_at,issue_snapshot,created_at)
 SELECT $1,$2,'completed',now(),'{}'::jsonb,'2025-01-01'::timestamptz + n * interval '1 second' FROM generate_series(1,$3) n`, 1000},
		{"platform_event", "activity_log", `INSERT INTO activity_log (actor_id,issue_id,workspace_id,actor_type,action,details,created_at)
 SELECT $1,$2,$4,'member','limit fixture','{}'::jsonb,'2025-01-01'::timestamptz + n * interval '1 second' FROM generate_series(1,$3) n`, 2000},
		{"comment", "comment", `INSERT INTO comment (author_id,issue_id,workspace_id,author_type,content,type,created_at)
 SELECT $1,$2,$4,'member','limit fixture','comment','2025-01-01'::timestamptz + n * interval '1 second' FROM generate_series(1,$3) n`, 2000},
		{"artifact", "attachment", `INSERT INTO attachment (uploader_id,issue_id,workspace_id,uploader_type,filename,content_type,size_bytes,url,created_at)
 SELECT $1,$2,$4,'member','limit fixture','text/plain',1,'fixture://attachment','2025-01-01'::timestamptz + n * interval '1 second' FROM generate_series(1,$3) n`, 2000},
	} {
		for _, count := range []int{source.limit, source.limit + 1} {
			t.Run(fmt.Sprintf("%s/%d", source.kind, count), func(t *testing.T) {
				issue := dbfx.Issue(t, "record bound")
				actor := testUserID
				args := []any{actor, issue, count, testWorkspaceID}
				if source.kind == "run" {
					args = []any{agent, issue, count}
				}
				dbfx.Exec(t, source.sql, args...)
				t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM "+source.table+" WHERE issue_id=$1", issue) })
				w, graph := graphTestCall(t, &h, testUserID, issue, "?scope=issue", nil)
				if w.Code != 200 {
					t.Fatalf("record bound status=%d body=%s", w.Code, w.Body.String())
				}
				if len(graph.Nodes) != source.limit+1 || graph.Coverage.Truncated != (count > source.limit) || graph.Coverage.Complete != (count == source.limit) || len(graph.Findings) != 0 {
					t.Fatalf("%s count=%d nodes=%d coverage=%+v findings=%v", source.kind, count, len(graph.Nodes), graph.Coverage, graph.Findings)
				}
				if (len(graph.Coverage.Warnings) > 0) != (count > source.limit) {
					t.Fatal("sentinel warning does not match overflow")
				}
				for _, node := range graph.Nodes {
					if node.Kind != "issue" && node.Kind != source.kind {
						t.Fatalf("preloaded unrelated record type: %s", node.Kind)
					}
				}
			})
		}
	}
}

func TestTrajectoryGraphNativeRelationshipLimitSentinels(t *testing.T) {
	h := trajectoryGraphTestHandler()
	for _, count := range []int{2000, 2001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			parent := dbfx.Issue(t, "relation bound parent")
			child := dbfx.Issue(t, "relation bound child", testutil.Cols{"parent_issue_id": parent})
			dbfx.Exec(t, `INSERT INTO issue_dependency (issue_id,depends_on_issue_id,type)
 SELECT $1,$2,'related' FROM generate_series(1,$3)`, parent, child, count)
			t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM issue_dependency WHERE issue_id=$1", parent) })
			w, graph := graphTestCall(t, &h, testUserID, "", "", []string{parent, child})
			if w.Code != 200 || len(graph.Edges) != 2001 || graph.Coverage.Truncated != (count > 2000) || graph.Coverage.Complete != (count == 2000) {
				t.Fatalf("native edges count=%d edges=%d coverage=%+v status=%d", count, len(graph.Edges), graph.Coverage, w.Code)
			}
			if count > 2000 && (len(graph.Coverage.Warnings) != 1 || graph.Coverage.Warnings[0] != "Platform relationship limit reached (2000).") {
				t.Fatalf("native edge warning=%v", graph.Coverage.Warnings)
			}
		})
	}
}
