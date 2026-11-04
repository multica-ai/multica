package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/engineering"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type engineeringFixture struct {
	t                    *testing.T
	project, agent, task string
}

func newEngineeringFixture(t *testing.T) engineeringFixture {
	t.Helper()
	f := engineeringFixture{t: t, project: dbfx.Project(t, "Engineering")}
	f.agent = dbfx.Agent(t, "Engineering agent "+uuid.NewString(), testRuntimeID)
	f.task = dbfx.Task(t, f.agent, testutil.Cols{"runtime_id": testRuntimeID})
	dbfx.Cleanup(t, `DELETE FROM project_baseline_revision WHERE workspace_id=$1 AND project_id=$2`, testWorkspaceID, f.project)
	dbfx.Cleanup(t, `DELETE FROM project_baseline_doc WHERE workspace_id=$1 AND project_id=$2`, testWorkspaceID, f.project)
	dbfx.Cleanup(t, `DELETE FROM project_engineering WHERE workspace_id=$1 AND project_id=$2`, testWorkspaceID, f.project)
	return f
}
func (f engineeringFixture) request(method, path string, body any, credential string) *http.Request {
	req := newRequest(method, "/api/projects/"+f.project+path, body)
	req = withURLParam(req, "id", f.project)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	identity := auth.Identity{UserID: testUserID, CredentialKind: credential}
	switch credential {
	case "session":
		return req
	case "task", "other_workspace_task":
		identity.CredentialKind = "task"
		identity.WorkspaceID = testWorkspaceID
		if credential == "other_workspace_task" {
			identity.WorkspaceID = uuid.NewString()
		}
		identity.AgentID = f.agent
		identity.TaskID = f.task
		req.Header.Set("X-Actor-Source", "task_token")
	case "cloud_pat":
		req.Header.Set("X-Actor-Source", "cloud_pat")
	}
	return req.WithContext(auth.WithIdentity(req.Context(), identity))
}
func (f engineeringFixture) enable() {
	f.t.Helper()
	testutil.Call(f.t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"enabled": true}, "session")).Want(200)
}
func (f engineeringFixture) propose(chapter, content string, base, replaces *string) baselineRevisionResponse {
	f.t.Helper()
	var out baselineRevisionResponse
	testutil.Call(f.t, testHandler.LabrastroProposeBaseline, f.request("POST", "/baseline/candidates", map[string]any{"chapter": chapter, "content_markdown": content, "change_summary": "Change", "base_revision_id": base, "replaces_candidate_id": replaces}, "task")).Want(201).JSON(&out)
	return out
}
func decision(action string, revisions ...baselineRevisionResponse) any {
	items := []map[string]any{}
	for _, r := range revisions {
		items = append(items, map[string]any{"revision_id": uuidToString(r.ID), "expected_adopted_revision_id": uuidToPtr(r.BaseRevisionID)})
	}
	return map[string]any{"action": action, "revisions": items}
}
func wantEngineeringCode(t *testing.T, response *testutil.Response, code string) {
	t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	response.JSON(&out)
	if out.Code != code {
		t.Fatalf("code=%q want %q", out.Code, code)
	}
}
func (f engineeringFixture) readRevision(id string) db.ProjectBaselineRevision {
	f.t.Helper()
	r, err := testHandler.Queries.LabrastroGetBaselineRevision(context.Background(), db.LabrastroGetBaselineRevisionParams{WorkspaceID: parseUUID(testWorkspaceID), ProjectID: parseUUID(f.project), ID: parseUUID(id)})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestLabrastroEngineeringEnableLeavesBaselineEmpty(t *testing.T) {
	f := newEngineeringFixture(t)
	var out struct {
		Engineering engineeringConfigResponse `json:"engineering"`
		Chapters    []baselineChapterResponse `json:"chapters"`
	}
	testutil.Call(t, testHandler.LabrastroGetEngineering, f.request("GET", "/engineering", nil, "session")).Want(200).JSON(&out)
	if out.Engineering.Enabled || out.Engineering.CurrentStage != 1 || len(out.Chapters) != 4 {
		t.Fatalf("defaults=%+v", out)
	}
	for _, c := range out.Chapters {
		if c.Adopted != nil || c.Candidate != nil {
			t.Fatal("unopened project has baseline")
		}
	}
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroProposeBaseline, f.request("POST", "/baseline/candidates", map[string]any{"chapter": "overview", "content_markdown": "hi"}, "session")).Want(409), "engineering_disabled")
	f.enable()
	testutil.Call(t, testHandler.LabrastroGetEngineering, f.request("GET", "/engineering", nil, "session")).Want(200).JSON(&out)
	for _, c := range out.Chapters {
		if c.Adopted != nil || c.Candidate != nil {
			t.Fatal("enabled project has an injected baseline")
		}
	}
	if string(out.Engineering.Stages) != "[]" {
		t.Fatalf("default stages=%s", out.Engineering.Stages)
	}

	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"enabled": false}, "pat")).Want(200)
	f.enable()
	for _, table := range []string{"project_baseline_doc", "project_baseline_revision"} {
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE project_id=$1", f.project); n != 0 {
			t.Fatalf("enable created %s: %d", table, n)
		}
	}
}

func TestLabrastroEngineeringClaimProjectDescription(t *testing.T) {
	f := newEngineeringFixture(t)
	const original = "  项目原文\n\nKeep this whitespace.\n"
	dbfx.Exec(t, `UPDATE project SET description=$2 WHERE id=$1`, f.project, original)
	resolve := func() string {
		t.Helper()
		got, err := testHandler.resolveClaimProjectContext(context.Background(), parseUUID(f.project), parseUUID(testWorkspaceID))
		if err != nil {
			t.Fatal(err)
		}
		return got.Description
	}
	if got := resolve(); got != original {
		t.Fatalf("unconfigured description=%q, want original", got)
	}
	f.enable()
	if got := resolve(); !strings.Contains(got, "当前尚未建立生效基线") || !strings.Contains(got, "Stage 1") {
		t.Fatalf("empty baseline context=%q", got)
	}
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{
		"current_stage": 9, "mode_summary": "逐步交付\n每阶段复盘",
		"stages": []engineeringStage{{Stage: 2, Title: "历史阶段"}, {Stage: 9, Title: "核心接口", Goal: "打通任务主链"}},
	}, "session")).Want(200)
	for _, chapter := range engineering.Chapters {
		adopted := f.propose(chapter, "## "+chapter+" 标题\nBASELINE_BODY_MUST_NOT_APPEAR\n### 验收约束\n正文\n```md\n## FENCED_HEADING_MUST_NOT_APPEAR\n```", nil, nil)
		testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", adopted), "session")).Want(200)
		if chapter == "domain_rules" {
			adopted = f.propose(chapter, "## 新规则\n规则正文\n### 验收边界\n验收正文", uuidToPtr(adopted.ID), nil)
			testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", adopted), "session")).Want(200)
			f.propose(chapter, "## CANDIDATE_HEADING_MUST_NOT_APPEAR\n候选正文", uuidToPtr(adopted.ID), nil)
		}
	}
	got := resolve()
	if !strings.HasPrefix(got, original) {
		t.Fatalf("original description was changed: %q", got)
	}
	for _, want := range []string{
		"Stage 9 · 核心接口", "打通任务主链", "逐步交付 每阶段复盘",
		"overview v1：overview 标题", "domain_rules v2：新规则", "验收边界",
		"architecture v1：architecture 标题", "exclusions_and_risks v1：exclusions_and_risks 标题",
		"待审候选（candidate，尚未生效）：domain_rules",
		"Project ID: " + f.project, "multica project baseline get --project " + f.project, "strixnova-*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("context missing %q: %s", want, got)
		}
	}
	for _, unwanted := range []string{"BASELINE_BODY_MUST_NOT_APPEAR", "FENCED_HEADING_MUST_NOT_APPEAR", "CANDIDATE_HEADING_MUST_NOT_APPEAR", "历史阶段", "当前尚未建立生效基线"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("context contains %q: %s", unwanted, got)
		}
	}
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"enabled": false}, "session")).Want(200)
	if got := resolve(); got != original {
		t.Fatalf("disabled description=%q, want original", got)
	}
	var stored string
	dbfx.QueryRow(t, `SELECT description FROM project WHERE id=$1`, f.project).Scan(&stored)
	if stored != original {
		t.Fatalf("stored description changed: %q", stored)
	}
}

func TestLabrastroEngineeringClaimDescriptionBounded(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{
		"mode_summary": strings.Repeat("方针\n", 90),
		"stages":       []engineeringStage{{Stage: 1, Title: strings.Repeat("阶", 60), Goal: strings.Repeat("标", 280)}},
	}, "session")).Want(200)
	for _, chapter := range engineering.Chapters {
		adopted := f.propose(chapter, strings.Repeat("## "+strings.Repeat("节", 100)+"\n正文\n", 30), nil, nil)
		testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", adopted), "session")).Want(200)
		f.propose(chapter, "候选", uuidToPtr(adopted.ID), nil)
	}
	got, err := testHandler.resolveClaimProjectContext(context.Background(), parseUUID(f.project), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got.Description) || utf8.RuneCountInString(got.Description) > 2000 || len(strings.Split(got.Description, "\n")) > 14 {
		t.Fatalf("summary exceeded its context budget: %s", got.Description)
	}
	if !strings.Contains(got.Description, "...") || !strings.Contains(got.Description, "multica project baseline get --project "+f.project) {
		t.Fatalf("truncated summary lost its full-baseline guidance: %s", got.Description)
	}
}

func TestLabrastroEngineeringClaimDescriptionQueryFailure(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	adopted := f.propose("overview", "## 已生效标题\n正文", nil, nil)
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", adopted), "session")).Want(200)
	const original = "Original project context\n"
	dbfx.Exec(t, `UPDATE project SET description=$2 WHERE id=$1`, f.project, original)
	for _, query := range []string{"LabrastroGetEngineering", "LabrastroListBaselineDocs", "LabrastroGetBaselineRevision"} {
		t.Run(query, func(t *testing.T) {
			h := *testHandler
			h.Queries = db.New(failQueryDBTX{DBTX: testPool, failOn: query, err: errors.New("database read failed")})
			got, err := h.resolveClaimProjectContext(context.Background(), parseUUID(f.project), parseUUID(testWorkspaceID))
			if err != nil || got.Description != original {
				t.Fatalf("engineering read failure blocked or changed claim context: %+v, %v", got, err)
			}
		})
	}
}

func TestLabrastroEngineeringDecisionCredentialMatrix(t *testing.T) {
	for _, tt := range []struct {
		name, credential, action, code string
		nonDecider, removal            bool
		status                         int
	}{
		{name: "session", credential: "session", action: "adopt", status: 200},
		{name: "pat", credential: "pat", action: "adopt", status: 200},
		{name: "session reject", credential: "session", action: "reject", status: 200},
		{name: "pat reject", credential: "pat", action: "reject", status: 200},
		{name: "non-decider session", credential: "session", action: "adopt", nonDecider: true, status: 403, code: "baseline_not_decider"},
		{name: "non-decider pat", credential: "pat", action: "adopt", nonDecider: true, status: 403, code: "baseline_not_decider"},
		{name: "task adopt", credential: "task", action: "adopt", nonDecider: true, status: 403, code: "baseline_human_required"},
		{name: "task reject", credential: "task", action: "reject", nonDecider: true, status: 403, code: "baseline_human_required"},
		{name: "task remove section", credential: "task", action: "adopt", nonDecider: true, removal: true, status: 403, code: "baseline_human_required"},
		{name: "task reject removal", credential: "task", action: "reject", nonDecider: true, removal: true, status: 403, code: "baseline_human_required"},
		{name: "other workspace task", credential: "other_workspace_task", action: "adopt", status: 403, code: "baseline_credential_denied"},
		{name: "cloud", credential: "cloud_pat", action: "adopt", status: 403, code: "baseline_credential_denied"},
		{name: "unknown", credential: "future", action: "adopt", status: 403, code: "baseline_credential_denied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newEngineeringFixture(t)
			f.enable()
			var base *string
			content := "### [Rule-1] Rule\nMust always verify the full result."
			if tt.removal {
				prior := f.propose("domain_rules", content, nil, nil)
				testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", prior), "session")).Want(200)
				base = uuidToPtr(prior.ID)
				content = ""
			}
			candidate := f.propose("domain_rules", content, base, nil)
			req := f.request("POST", "/baseline/decisions", decision(tt.action, candidate), tt.credential)
			if tt.nonDecider {
				member := dbfx.User(t, "Non-decider", uuid.NewString()+"@test.local")
				dbfx.Member(t, testWorkspaceID, member, "member")
				req.Header.Set("X-User-ID", member)
				if identity, ok := auth.IdentityFromContext(req.Context()); ok {
					identity.UserID = member
					req = req.WithContext(auth.WithIdentity(req.Context(), identity))
				}
			}
			response := testutil.Call(t, testHandler.LabrastroDecideBaseline, req).Want(tt.status)
			got := f.readRevision(uuidToString(candidate.ID))
			if tt.status != 200 {
				wantEngineeringCode(t, response, tt.code)
				if got.Status != "candidate" || got.DecidedByID.Valid || got.DecidedAt.Valid {
					t.Fatal("denied operation mutated revision")
				}
				docs, err := testHandler.Queries.LabrastroListBaselineDocs(context.Background(), db.LabrastroListBaselineDocsParams{WorkspaceID: parseUUID(testWorkspaceID), ProjectID: parseUUID(f.project)})
				if err != nil {
					t.Fatal(err)
				}
				doc := baselineDoc(docs, candidate.Chapter)
				if doc.CandidateRevisionID != candidate.ID || doc.AdoptedRevisionID != candidate.BaseRevisionID {
					t.Fatal("denied operation mutated baseline pointers")
				}
				return
			}
			wantStatus := "adopted"
			if tt.action == "reject" {
				wantStatus = "rejected"
			}
			wantCredential := tt.credential
			if got.Status != wantStatus || got.DecisionCredential.String != wantCredential {
				t.Fatalf("audit=%+v", got)
			}
			if got.DecidedByType.String != "member" || got.DecidedByID != parseUUID(testUserID) || got.DecidedByTaskID.Valid {
				t.Fatalf("wrong member attribution: %+v", got)
			}
		})
	}
}

func TestLabrastroEngineeringConfigurationPermissions(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	member := dbfx.User(t, "Decider", uuid.NewString()+"@test.local")
	dbfx.Member(t, testWorkspaceID, member, "member")
	candidate := f.propose("overview", "### [Stage-1] First\nStart", nil, nil)
	req := f.request("POST", "/baseline/decisions", decision("adopt", candidate), "session")
	req.Header.Set("X-User-ID", member)
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroDecideBaseline, req).Want(403), "baseline_not_decider")
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"decider_member_ids": []string{member}}, "session")).Want(200)
	for _, body := range []map[string]any{{"enabled": false}, {"decider_member_ids": []string{}}} {
		req := f.request("PATCH", "/engineering", body, "session")
		req.Header.Set("X-User-ID", member)
		testutil.Call(t, testHandler.LabrastroPatchEngineering, req).Want(403)
	}
	req = f.request("PATCH", "/engineering", map[string]any{"current_stage": 9, "mode_summary": "Iterate", "stages": []any{map[string]any{"stage": 9, "title": "Release"}}}, "session")
	req.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.LabrastroPatchEngineering, req).Want(200)
	req = f.request("POST", "/baseline/decisions", decision("adopt", candidate), "session")
	req.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.LabrastroDecideBaseline, req).Want(200)
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"enabled": true}, "task")).Want(403)
	for _, removed := range []any{true, false, nil} {
		wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"agent_decisions_enabled": removed}, "session")).Want(400), "invalid_request")
	}
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"decider_member_ids": []string{uuid.NewString()}}, "session")).Want(422)
	// The owner keeps decision authority even when an explicit list is present.
	c := f.propose("architecture", "### [ADR-1] Decision\nUse PostgreSQL", nil, nil)
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", c), "pat")).Want(200)
}

func TestLabrastroEngineeringCandidateCASAtomicDecisionAndHistory(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	first := f.propose("overview", "### [Stage-1] First\nOne", nil, nil)
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroProposeBaseline, f.request("POST", "/baseline/candidates", map[string]any{"chapter": "overview", "content_markdown": "Changed"}, "session")).Want(409), "baseline_candidate_open")
	second := f.propose("overview", "### [Stage-1] First\nTwo", nil, uuidToPtr(first.ID))
	if f.readRevision(uuidToString(first.ID)).Status != "withdrawn" || second.RevisionNumber != 2 {
		t.Fatal("replacement lost revision history")
	}
	architecture := f.propose("architecture", "### [ADR-1] Decision\nOne", nil, nil)
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", architecture, first), "session")).Want(409), "baseline_candidate_closed")
	if f.readRevision(uuidToString(architecture.ID)).Status != "candidate" {
		t.Fatal("failed multi-chapter adopt partially committed")
	}
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", second, architecture), "session")).Want(200)
	stale := uuidToString(first.ID)
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroProposeBaseline, f.request("POST", "/baseline/candidates", map[string]any{"chapter": "overview", "content_markdown": "old", "base_revision_id": stale}, "session")).Want(409), "baseline_base_stale")
	rollback := f.propose("overview", first.ContentMarkdown, uuidToPtr(second.ID), nil)
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", rollback), "session")).Want(200)
	if f.readRevision(uuidToString(second.ID)).Status != "superseded" {
		t.Fatal("old adopted was not superseded")
	}
	var history struct {
		Revisions []baselineRevisionResponse `json:"revisions"`
		More      bool                       `json:"has_more"`
		Before    int                        `json:"next_before"`
	}
	testutil.Call(t, testHandler.LabrastroBaselineHistory, f.request("GET", "/baseline/revisions?chapter=overview&limit=2", nil, "session")).Want(200).JSON(&history)
	if len(history.Revisions) != 2 || history.Revisions[0].RevisionNumber != 3 || !history.More || history.Before != 2 {
		t.Fatalf("history=%+v", history)
	}
	testutil.Call(t, testHandler.LabrastroBaselineHistory, f.request("GET", "/baseline/revisions?chapter=overview&before=2", nil, "session")).Want(200).JSON(&history)
	if len(history.Revisions) != 1 || history.Revisions[0].RevisionNumber != 1 || history.More {
		t.Fatalf("last page=%+v", history)
	}
	var diff engineering.Diff
	testutil.Call(t, testHandler.LabrastroBaselineHistory, f.request("GET", "/baseline/revisions?chapter=overview&diff_from=1&diff_to=2", nil, "session")).Want(200).JSON(&diff)
	if len(diff.Modified) != 1 || diff.Modified[0].Before != "One" || diff.Modified[0].After != "Two" {
		t.Fatalf("diff=%+v", diff)
	}
}

func TestLabrastroEngineeringConcurrentProposals(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			testHandler.LabrastroProposeBaseline(w, f.request("POST", "/baseline/candidates", map[string]any{"chapter": "overview", "content_markdown": "### [Stage-1] First"}, "session"))
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent results=%v", counts)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM project_baseline_revision WHERE project_id=$1 AND chapter='overview'`, f.project); n != 1 {
		t.Fatalf("created %d revisions", n)
	}
}

func TestLabrastroEngineeringOpenIssuesAndIsolation(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": testWorkspaceID, "key": "eng_discussion", "name": "Discuss", "category": "unstarted", "color": "#888888", "position": 99, "is_system": false})
	dbfx.Issue(t, "custom queued", testutil.Cols{"project_id": f.project, "status": "eng_discussion"})
	dbfx.Issue(t, "started", testutil.Cols{"project_id": f.project, "status": "in_review", "assignee_type": "agent", "assignee_id": f.agent})
	dbfx.Issue(t, "finished", testutil.Cols{"project_id": f.project, "status": "done"})
	other := dbfx.Project(t, "Other project")
	dbfx.Issue(t, "other project", testutil.Cols{"project_id": other})
	c := f.propose("overview", "### [Stage-1] First", nil, nil)
	var response struct {
		Open baselineOpenIssues `json:"open_issues"`
	}
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", c), "session")).Want(200).JSON(&response)
	if len(response.Open.Queued) != 1 || len(response.Open.InFlight) != 1 || response.Open.Queued[0].Category != "unstarted" || response.Open.InFlight[0].AssigneeID == nil || *response.Open.InFlight[0].AssigneeID != f.agent {
		t.Fatalf("open=%+v", response)
	}
	req := f.request("GET", "/engineering", nil, "session")
	req.Header.Set("X-Workspace-ID", uuid.NewString())
	testutil.Call(t, testHandler.LabrastroGetEngineering, req).Want(404)
}

func TestLabrastroEngineeringDeleteProjectCleansOnlyItsRows(t *testing.T) {
	a, b := newEngineeringFixture(t), newEngineeringFixture(t)
	a.enable()
	b.enable()
	b.propose("overview", "## Preserve\nBody", nil, nil)
	a.propose("overview", "### [Stage-1] First", nil, nil)
	testutil.Call(t, testHandler.DeleteProject, a.request("DELETE", "", nil, "session")).Want(204)
	for _, table := range []string{"project_engineering", "project_baseline_doc", "project_baseline_revision"} {
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE project_id=$1", a.project); n != 0 {
			t.Fatalf("%s retained %d rows", table, n)
		}
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE project_id=$1", b.project); n == 0 {
			t.Fatalf("%s deleted other project", table)
		}
	}
}

func TestLabrastroEngineeringRejectsInvalidInputAndAuditFields(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	for _, tt := range []struct {
		chapter, content, code string
		status                 int
	}{
		{"wrong", "", "invalid_request", 400},
		{"overview", strings.Repeat("x", 65537), "baseline_too_large", 422},
		{"overview", "\x00", "baseline_content_invalid", 422},
	} {
		wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroProposeBaseline, f.request("POST", "/baseline/candidates", map[string]any{"chapter": tt.chapter, "content_markdown": tt.content}, "session")).Want(tt.status), tt.code)
	}
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroBaselineHistory, f.request("GET", "/baseline/revisions?chapter=wrong", nil, "session")).Want(400), "invalid_request")
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", map[string]any{"action": "adopt", "decided_by_id": testUserID}, "session")).Want(400)
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", "invalid body", "task")).Want(403), "baseline_human_required")
}

func TestLabrastroEngineeringWorkspaceDeleteAndOpenListBound(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	for i := 0; i < 201; i++ {
		dbfx.Issue(t, "Queued", testutil.Cols{"project_id": f.project})
	}
	c := f.propose("overview", "### [Stage-1] First", nil, nil)
	var response struct {
		Open baselineOpenIssues `json:"open_issues"`
	}
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", c), "session")).Want(200).JSON(&response)
	if len(response.Open.Queued) != 200 || !response.Open.Truncated {
		t.Fatalf("unbounded open issues: %d, %v", len(response.Open.Queued), response.Open.Truncated)
	}
	ws := dbfx.Workspace(t, "Engineering cleanup", "engineering-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	project := dbfx.Project(t, "Deleted project", testutil.Cols{"workspace_id": ws})
	req := newRequest("PATCH", "/api/projects/"+project+"/engineering", map[string]any{"enabled": true})
	req = withURLParam(req, "id", project)
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.LabrastroPatchEngineering, req).Want(200)
	req = newRequest("POST", "/api/projects/"+project+"/baseline/candidates", map[string]any{"chapter": "overview", "content_markdown": "## Cleanup"})
	req = withURLParam(req, "id", project)
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.LabrastroProposeBaseline, req).Want(201)
	for _, table := range []string{"project_engineering", "project_baseline_doc", "project_baseline_revision"} {
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", ws); n != 1 {
			t.Fatalf("fixture %s: %d", table, n)
		}
	}
	req = newRequest("DELETE", "/api/workspaces/"+ws, nil)
	req = withURLParam(req, "id", ws)
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(204)
	for _, table := range []string{"project_engineering", "project_baseline_doc", "project_baseline_revision"} {
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", ws); n != 0 {
			t.Fatalf("workspace left %s: %d", table, n)
		}
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE project_id=$1", f.project); n == 0 {
			t.Fatalf("workspace cleanup deleted unrelated %s", table)
		}
	}
}

func TestLabrastroEngineeringDefaultLeadAndConcurrentDecision(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	lead := dbfx.User(t, "Lead", uuid.NewString()+"@test.local")
	dbfx.Member(t, testWorkspaceID, lead, "member")
	dbfx.Exec(t, `UPDATE project SET lead_type='member',lead_id=$2 WHERE id=$1`, f.project, lead)
	candidate := f.propose("overview", "### [Stage-1] First", nil, nil)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := f.request("POST", "/baseline/decisions", decision("adopt", candidate), "session")
			req.Header.Set("X-User-ID", lead)
			w := httptest.NewRecorder()
			testHandler.LabrastroDecideBaseline(w, req)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("decision results=%v", counts)
	}
	if got := f.readRevision(uuidToString(candidate.ID)); got.DecidedByID != parseUUID(lead) || got.DecidedByType.String != "member" {
		t.Fatalf("lead audit=%+v", got)
	}
}

func TestLabrastroEngineeringProposalWaitsForProjectDelete(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := testHandler.Queries.WithTx(tx)
	if _, err = q.LockProjectForDelete(ctx, db.LockProjectForDeleteParams{WorkspaceID: parseUUID(testWorkspaceID), ID: parseUUID(f.project)}); err != nil {
		t.Fatal(err)
	}
	if err = q.LabrastroDeleteProjectEngineering(ctx, db.LabrastroDeleteProjectEngineeringParams{WorkspaceID: parseUUID(testWorkspaceID), ProjectID: parseUUID(f.project)}); err != nil {
		t.Fatal(err)
	}
	req := f.request("POST", "/baseline/candidates", map[string]any{"chapter": "overview", "content_markdown": "### [Stage-1] First"}, "task")
	reqCtx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
	defer cancel()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); testHandler.LabrastroProposeBaseline(response, req.WithContext(reqCtx)) }()
	defer func() { tx.Rollback(context.Background()); cancel(); <-done }()
	if !waitForWaiterBlockedBy(t, holderBackendPID(t, ctx, tx), 10*time.Second) {
		t.Fatal("proposal did not wait for project deletion")
	}
	if err = q.DeleteProject(ctx, db.DeleteProjectParams{WorkspaceID: parseUUID(testWorkspaceID), ID: parseUUID(f.project)}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if response.Code != 404 {
			t.Fatalf("proposal after delete: %d %s", response.Code, response.Body.String())
		}
	case <-reqCtx.Done():
		t.Fatal("proposal did not finish after deletion")
	}

	for _, table := range []string{"project_engineering", "project_baseline_doc", "project_baseline_revision"} {
		if n := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE project_id=$1", f.project); n != 0 {
			t.Fatalf("concurrent deletion left %s: %d", table, n)
		}
	}
}

func TestLabrastroEngineeringTaskIsolationAndCapabilitySwitch(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	candidate := f.propose("overview", "### [Stage-1] First", nil, nil)
	req := f.request("POST", "/baseline/decisions", decision("adopt", candidate), "task")
	identity, _ := auth.IdentityFromContext(req.Context())
	identity.WorkspaceID = uuid.NewString()
	req = req.WithContext(auth.WithIdentity(req.Context(), identity))
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroDecideBaseline, req).Want(403), "baseline_credential_denied")
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"agent_decisions_enabled": true}, "session")).Want(400)
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"enabled": false}, "session")).Want(200)
	for _, credential := range []string{"session", "pat"} {
		for _, action := range []string{"adopt", "reject"} {
			wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision(action, candidate), credential)).Want(409), "engineering_disabled")
		}
	}
	if got := f.readRevision(uuidToString(candidate.ID)); got.Status != "candidate" || got.DecidedByID.Valid {
		t.Fatal("denied decision changed the candidate")
	}
}

func TestLabrastroEngineeringStages(t *testing.T) {
	f := newEngineeringFixture(t)
	read := func() engineeringConfigResponse {
		var out struct {
			Engineering engineeringConfigResponse `json:"engineering"`
		}
		testutil.Call(t, testHandler.LabrastroGetEngineering, f.request("GET", "/engineering", nil, "session")).Want(200).JSON(&out)
		return out.Engineering
	}
	if got := read(); string(got.Stages) != "[]" {
		t.Fatalf("unset stages=%s", got.Stages)
	}
	stages := []any{map[string]any{"stage": 9, "title": " 上线 ", "goal": " 验收 "}, map[string]any{"stage": 2, "title": "开发"}}
	var patched struct {
		Engineering engineeringConfigResponse `json:"engineering"`
	}
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"stages": stages, "current_stage": 9}, "session")).Want(200).JSON(&patched)
	want := `[{"stage":2,"title":"开发","goal":""},{"stage":9,"title":"上线","goal":"验收"}]`
	equalJSON := func(got json.RawMessage) bool {
		var a, b any
		if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
			return false
		}
		return reflect.DeepEqual(a, b)
	}
	if patched.Engineering.CurrentStage != 9 || !equalJSON(patched.Engineering.Stages) {
		t.Fatalf("patch=%+v", patched)
	}
	for _, body := range []any{map[string]any{"mode_summary": "Iterate"}, map[string]any{"stages": nil}} {
		testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", body, "session")).Want(200)
		if got := read(); got.CurrentStage != 9 || !equalJSON(got.Stages) {
			t.Fatalf("partial update=%+v", got)
		}
	}
	for _, tt := range []struct {
		name        string
		stage       any
		title, goal string
	}{
		{"zero", 0, "valid", ""}, {"negative", -1, "valid", ""}, {"fraction", 1.5, "valid", ""},
		{"string", "1", "valid", ""}, {"missing", nil, "valid", ""}, {"empty", 1, " \t ", ""},
		{"long title", 1, strings.Repeat("字", 61), ""}, {"long goal", 1, "valid", strings.Repeat("字", 281)},
		{"title newline", 1, "valid\n", ""}, {"title carriage", 1, "valid\r", ""}, {"title NUL", 1, "valid\x00", ""},
		{"goal newline", 1, "valid", "\ngoal"}, {"goal carriage", 1, "valid", "goal\r"}, {"goal NUL", 1, "valid", "\x00"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{"stages": []any{map[string]any{"stage": tt.stage, "title": tt.title, "goal": tt.goal}}}
			wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", body, "session")).Want(422), "invalid_request")
		})
	}
	duplicate := []any{map[string]any{"stage": 1, "title": "one"}, map[string]any{"stage": 1, "title": "two"}}
	tooMany := make([]any, 51)
	for i := range tooMany {
		tooMany[i] = map[string]any{"stage": i + 1, "title": "Stage"}
	}
	for _, invalid := range []any{duplicate, tooMany,
		[]any{map[string]any{"stage": 1, "title": 42}},
		[]any{map[string]any{"stage": 1, "title": "Title", "goal": false}},
		[]any{map[string]any{"stage": 1, "title": "Title", "goal": nil}},
	} {
		wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"stages": invalid}, "session")).Want(422), "invalid_request")
	}
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"stages": []any{map[string]any{"stage": 1, "title": "one", "extra": true}}}, "session")).Want(400), "invalid_request")
	if got := read(); !equalJSON(got.Stages) {
		t.Fatalf("invalid patch mutated stages=%s", got.Stages)
	}
	// Boundary lengths count characters, not UTF-8 bytes.
	tooMany[0] = map[string]any{"stage": 1, "title": strings.Repeat("字", 60), "goal": strings.Repeat("字", 280)}
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"stages": tooMany[:50]}, "session")).Want(200)
	testutil.Call(t, testHandler.LabrastroPatchEngineering, f.request("PATCH", "/engineering", map[string]any{"stages": []any{}}, "session")).Want(200)
	if got := read(); string(got.Stages) != "[]" {
		t.Fatalf("clear=%s", got.Stages)
	}
}

func TestLabrastroEngineeringDiffIgnoresOtherChaptersAndRechecksStoredDiff(t *testing.T) {
	f := newEngineeringFixture(t)
	f.enable()
	initial := f.propose("domain_rules", "## Rules\n### [Rule-1] Verify\nAlways verify", nil, nil)
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", initial), "session")).Want(200)
	cut := f.propose("domain_rules", "## Rules", uuidToPtr(initial.ID), nil)
	f.propose("exclusions_and_risks", "## Later\nRule-1 deferred without issue", nil, nil)
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", cut), "session")).Want(200)
	// Historical algorithm output must be reviewed again instead of silently adopted.
	next := f.propose("domain_rules", "## Rules\nChanged", uuidToPtr(cut.ID), nil)
	dbfx.Exec(t, `UPDATE project_baseline_revision SET anchors_diff=jsonb_set(anchors_diff,'{warnings}','[{"code":"rule_removed","anchor":"Rule-1"}]') WHERE id=$1`, uuidToString(next.ID))
	wantEngineeringCode(t, testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", next), "pat")).Want(409), "baseline_base_stale")
	if got := f.readRevision(uuidToString(next.ID)); got.Status != "candidate" || got.DecidedByID.Valid {
		t.Fatal("stale diff changed candidate")
	}
	replacement := f.propose("domain_rules", next.ContentMarkdown, uuidToPtr(cut.ID), uuidToPtr(next.ID))
	testutil.Call(t, testHandler.LabrastroDecideBaseline, f.request("POST", "/baseline/decisions", decision("adopt", replacement), "pat")).Want(200)
}
