package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// iniWorkspace is a workspace of its own, so each test installs its own default
// pipeline. Product owns the validate stage, Analysis owns define.
type iniWorkspace struct {
	ID       string
	AdminID  string // a user who is the workspace's admin
	MemberID string // a user who is a plain member
	Product  string // squad id, product_discovery
	Analysis string // squad id, product_analysis
	Build    string // squad id, the SDLC build squad (build and learn)
	Leader   string // Product's leader agent id (a member of Product)
	Task     string // a running task of Leader
}

// newIniWorkspace creates the workspace, its people and two squads, and installs
// the default pipeline with the two owners resolved and the rest left unowned.
func newIniWorkspace(t *testing.T) iniWorkspace {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fx := iniWorkspace{}
	fx.ID = dbfx.Workspace(t, "Initiatives "+suffix, "ini-"+suffix)
	// Registered after the workspace, so it runs before the workspace row goes.
	t.Cleanup(func() { cleanInitiativeRows(fx.ID) })

	fx.AdminID = dbfx.User(t, "Ini Admin "+suffix, "ini-admin-"+suffix+"@example.com")
	dbfx.Member(t, fx.ID, fx.AdminID, "admin")
	fx.MemberID = dbfx.User(t, "Ini Member "+suffix, "ini-member-"+suffix+"@example.com")
	dbfx.Member(t, fx.ID, fx.MemberID, "member")

	fx.Leader = iniAgent(t, fx.ID, "Ini Leader "+suffix)
	fx.Task = createHandlerTestTaskForAgent(t, fx.Leader)
	fx.Product = dbfx.Squad(t, "Product "+suffix, fx.Leader, testutil.Cols{"workspace_id": fx.ID})
	dbfx.SquadMember(t, fx.Product, "agent", fx.Leader, testutil.Cols{"role": "leader"})
	analysisLeader := iniAgent(t, fx.ID, "Analysis Leader "+suffix)
	fx.Analysis = dbfx.Squad(t, "Analysis "+suffix, analysisLeader, testutil.Cols{"workspace_id": fx.ID})

	// The build squad carries the SDLC agents the build workflow hands to, by
	// the names the quickstart gives them.
	buildLead := iniAgent(t, fx.ID, "SDLC Lead")
	fx.Build = dbfx.Squad(t, "Build "+suffix, buildLead, testutil.Cols{"workspace_id": fx.ID})
	dbfx.SquadMember(t, fx.Build, "agent", buildLead, testutil.Cols{"role": "leader"})
	for _, name := range []string{"SDLC Developer", "SDLC Code Reviewer", "SDLC QA Tester"} {
		dbfx.SquadMember(t, fx.Build, "agent", iniAgent(t, fx.ID, name), testutil.Cols{"role": "member"})
	}

	ps := service.NewPipelineService(testHandler.Queries, testPool)
	res, err := ps.InstallDefault(context.Background(), service.InstallPipelineParams{
		WorkspaceID: parseUUID(fx.ID),
		ResolveSquad: func(_ context.Context, key string) (pgtype.UUID, bool, error) {
			switch key {
			case "product_discovery":
				return parseUUID(fx.Product), true, nil
			case "product_analysis":
				return parseUUID(fx.Analysis), true, nil
			case "support":
				return parseUUID(fx.Product), true, nil
			case "build", "learn":
				return parseUUID(fx.Build), true, nil
			}
			return pgtype.UUID{}, false, nil
		},
	})
	if err != nil {
		t.Fatalf("install default pipeline: %v", err)
	}
	if !res.Created {
		t.Fatalf("a fresh workspace should get a new pipeline")
	}
	return fx
}

// iniAgent makes a workspace-invocable agent, so a squad may be assigned to it.
func iniAgent(t *testing.T, ws, name string) string {
	t.Helper()
	id := dbfx.Agent(t, name, handlerTestRuntimeID(t), testutil.Cols{
		"workspace_id": ws, "visibility": "workspace", "permission_mode": "public_to",
	})
	dbfx.Exec(t, `INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2) ON CONFLICT (agent_id, target_type, target_id) DO NOTHING`, id, ws)
	return id
}

// cleanInitiativeRows removes what the API wrote in a test workspace.
func cleanInitiativeRows(ws string) {
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM initiative_artifact WHERE workspace_id = $1`,
		`DELETE FROM initiative_stage_event WHERE workspace_id = $1`,
		`DELETE FROM initiative WHERE workspace_id = $1`,
		`DELETE FROM pipeline WHERE workspace_id = $1`,
		`DELETE FROM proposal WHERE workspace_id = $1`,
		`DELETE FROM comment WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
		`DELETE FROM project WHERE workspace_id = $1`,
		`DELETE FROM squad_routine WHERE workspace_id = $1`,
		`DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE workspace_id = $1)`,
		`DELETE FROM autopilot WHERE workspace_id = $1`,
		`DELETE FROM company_floor WHERE workspace_id = $1`,
		`DELETE FROM knowledge_link WHERE workspace_id = $1`,
		`DELETE FROM knowledge_publish_policy WHERE workspace_id = $1`,
		`DELETE FROM knowledge_kind WHERE workspace_id = $1`,
		`DELETE FROM knowledge_base WHERE workspace_id = $1`,
		`DELETE FROM workspace_knowledge WHERE workspace_id = $1`,
	} {
		testPool.Exec(ctx, stmt, ws)
	}
}

// inWorkspace points a request at the test workspace and acts as the user.
func (fx iniWorkspace) as(req *http.Request, userID string) *http.Request {
	return testutil.WithHeaders(req, "X-Workspace-ID", fx.ID, "X-User-ID", userID)
}

func (fx iniWorkspace) asAgent(req *http.Request, agentID, taskID string) *http.Request {
	return testutil.WithHeaders(req, "X-Workspace-ID", fx.ID, "X-Agent-ID", agentID, "X-Task-ID", taskID)
}

// createInitiative starts one as the admin and returns its id.
func (fx iniWorkspace) createInitiative(t *testing.T, title string) iniRef {
	t.Helper()
	req := fx.as(newRequest("POST", "/api/initiatives", map[string]any{
		"title": title, "thesis": "Customers pay for invoices in bulk.",
	}), fx.AdminID)
	var out InitiativeResponse
	testutil.Call(t, testHandler.CreateInitiative, req).Want(http.StatusCreated).JSON(&out)
	return iniRef{ID: out.ID, Identifier: out.Identifier}
}

type iniRef struct{ ID, Identifier string }

// pendingGates maps each initiative id to the pending stage gate that names it.
func pendingGates(t *testing.T, ws string) map[string]string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT payload->>'initiative_id', id::text FROM proposal
		WHERE workspace_id = $1 AND kind = 'stage_gate' AND state = 'pending'`, ws)
	if err != nil {
		t.Fatalf("list gates: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var initiativeID, proposalID string
		if err := rows.Scan(&initiativeID, &proposalID); err != nil {
			t.Fatalf("scan gate: %v", err)
		}
		out[initiativeID] = proposalID
	}
	return out
}

// transition posts a body to one of the initiative's transition routes as a user.
func (fx iniWorkspace) transition(t *testing.T, fn http.HandlerFunc, as string, id string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(fx.as(newRequest("POST", "/api/initiatives/"+id, body), as), "id", id)
	return testutil.Call(t, fn, req)
}

// linkStage takes the named artifacts into an initiative's current stage, as a
// published document of each kind would.
func (fx iniWorkspace) linkStage(t *testing.T, id string, kinds ...string) {
	t.Helper()
	for _, k := range kinds {
		linked, err := testHandler.initiativeService().LinkArtifact(context.Background(), parseUUID(fx.ID), parseUUID(id),
			parseUUID(uuid.NewString()), k, service.InitiativeActor{Type: "system"})
		if err != nil || !linked {
			t.Fatalf("link %s: linked=%v err=%v", k, linked, err)
		}
	}
}

// putSquadOnFloor seats a squad on a floor by name, as the starter company does.
func (fx iniWorkspace) putSquadOnFloor(t *testing.T, squadID, floor string) {
	t.Helper()
	floorID := dbfx.Insert(t, "company_floor", testutil.Cols{"workspace_id": fx.ID, "name": floor, "position": 0})
	dbfx.Exec(t, `UPDATE squad SET floor_id = $1 WHERE id = $2`, floorID, squadID)
}

// makeAutoExit makes the stage at index idx of the default pipeline auto.
func (fx iniWorkspace) makeAutoExit(t *testing.T, idx int) {
	t.Helper()
	dbfx.Exec(t, fmt.Sprintf(`UPDATE pipeline SET stages = jsonb_set(stages, '{%d,exit}', '{"mode":"auto","criteria":"Auto."}') WHERE workspace_id = $1 AND is_default`, idx), fx.ID)
}

// detail reads an initiative with its checklist, timeline and gates.
func (fx iniWorkspace) detail(t *testing.T, id string) InitiativeDetailResponse {
	t.Helper()
	var out InitiativeDetailResponse
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+id, nil), fx.AdminID), "id", id)).
		Want(http.StatusOK).JSON(&out)
	return out
}

// eventCount counts the timeline rows of one kind.
func eventCount(d InitiativeDetailResponse, kind string) int {
	n := 0
	for _, e := range d.Events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// reviewGate returns the operate review gate of one day for an initiative.
func reviewGate(t *testing.T, ws, initiativeID string, day int) string {
	t.Helper()
	var id string
	dbfx.QueryRow(t, `SELECT id::text FROM proposal WHERE workspace_id = $1 AND kind = 'stage_gate'
		AND payload->>'initiative_id' = $2 AND payload->>'gate_day' = $3`, ws, initiativeID, strconv.Itoa(day)).Scan(&id)
	return id
}

// decideAsAdmin answers a proposal as the workspace's admin.
func (fx iniWorkspace) decideAsAdmin(t *testing.T, proposalID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(fx.as(newRequest("POST", "/api/proposals/"+proposalID+"/decision", body), fx.AdminID), "id", proposalID)
	return testutil.Call(t, testHandler.DecideProposal, req)
}

// knowledgeFor enables knowledge, on a fake WeKnora, for one initiative test
// workspace. The service is wired to the initiative service as the server does.
func knowledgeFor(t *testing.T, ws, userID string) *service.KnowledgeService {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 11)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	svc := &service.KnowledgeService{
		Queries: testHandler.Queries, DB: testPool, Client: knowledge.NewFake(), Box: box,
		Embedding:   knowledge.EmbeddingModelSpec{Name: "bge-m3", Provider: "ollama", BaseURL: "http://ollama.test", Dimension: 1024},
		UIURL:       "http://ui.test",
		Initiatives: testHandler.initiativeService(),
	}
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM knowledge_link WHERE workspace_id = $1`,
			`DELETE FROM knowledge_publish_policy WHERE workspace_id = $1`,
			`DELETE FROM knowledge_kind WHERE workspace_id = $1`,
			`DELETE FROM knowledge_base WHERE workspace_id = $1`,
			`DELETE FROM workspace_knowledge WHERE workspace_id = $1`,
		} {
			testPool.Exec(context.Background(), stmt, ws)
		}
	})
	if _, err := svc.Enable(context.Background(), parseUUID(ws), parseUUID(userID)); err != nil {
		t.Fatalf("enable knowledge: %v", err)
	}
	return svc
}

func TestInitiative_CreateNeedsAnAdminAndAValidBody(t *testing.T) {
	fx := newIniWorkspace(t)

	// A plain member and an agent may not start one directly.
	testutil.Call(t, testHandler.CreateInitiative,
		fx.as(newRequest("POST", "/api/initiatives", map[string]any{"title": "x", "thesis": "y"}), fx.MemberID)).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CreateInitiative,
		fx.asAgent(newRequest("POST", "/api/initiatives", map[string]any{"title": "x", "thesis": "y"}), fx.Leader, fx.Task)).
		Want(http.StatusForbidden)

	// Malformed input is refused before anything is written.
	for name, body := range map[string]map[string]any{
		"no title":     {"thesis": "y"},
		"bad pipeline": {"title": "x", "thesis": "y", "pipeline_id": "not-a-uuid"},
		"long title":   {"title": strings.Repeat("a", 201), "thesis": "y"},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.CreateInitiative,
				fx.as(newRequest("POST", "/api/initiatives", body), fx.AdminID)).Want(http.StatusBadRequest)
		})
	}
	req := fx.as(newRequest("POST", "/api/initiatives", nil), fx.AdminID)
	req.Body = io.NopCloser(strings.NewReader("{not json"))
	testutil.Call(t, testHandler.CreateInitiative, req).Want(http.StatusBadRequest)

	// Spec 3.1 gives the thesis column an empty default: a title alone is enough.
	testutil.Call(t, testHandler.CreateInitiative,
		fx.as(newRequest("POST", "/api/initiatives", map[string]any{"title": "Thesis later"}), fx.AdminID)).Want(http.StatusCreated)
}

func TestInitiative_AdminStartsAtValidateWithChecklistAndHandoff(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Invoicing for freelancers")

	var detail InitiativeDetailResponse
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+created.ID, nil), fx.MemberID), "id", created.ID)).
		Want(http.StatusOK).JSON(&detail)

	got := detail.Initiative
	if got.StageKey != "validate" || got.Status != "active" || got.Identifier != created.Identifier {
		t.Fatalf("initiative should start active at validate: %+v", got)
	}
	if got.OwnerSquadID == nil || *got.OwnerSquadID != fx.Product {
		t.Fatalf("validate is owned by the product squad, got %v", got.OwnerSquadID)
	}
	if got.ArtifactsRequired != 2 || got.ArtifactsPresent != 0 {
		t.Fatalf("validate requires two artifacts with none present: %+v", got)
	}
	if len(detail.Events) != 1 || detail.Events[0].Kind != "created" {
		t.Fatalf("the timeline starts with a created event: %+v", detail.Events)
	}
	if len(detail.Artifacts) != 3 {
		t.Fatalf("validate has three artifact rows, got %d", len(detail.Artifacts))
	}

	// The backing issue sits at the validate status and is handed to the squad.
	var status string
	var assigneeType, assigneeID *string
	dbfx.QueryRow(t, `SELECT status, assignee_type, assignee_id::text FROM issue WHERE id = $1`, got.IssueID).
		Scan(&status, &assigneeType, &assigneeID)
	if status != "ini_validate" || assigneeType == nil || *assigneeType != "squad" || assigneeID == nil || *assigneeID != fx.Product {
		t.Fatalf("backing issue = %s, %v %v; want ini_validate handed to the product squad", status, assigneeType, assigneeID)
	}
}

func TestInitiative_TransitionsAreGatedByArtifactsAndExit(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Gated")

	// Required artifacts missing: the list says what is missing.
	var refusal map[string]any
	fx.transition(t, testHandler.AdvanceInitiative, fx.AdminID, created.ID, map[string]any{}).
		Want(http.StatusConflict).JSON(&refusal)
	if refusal["code"] != "stage_not_ready" || len(refusal["missing"].([]any)) != 2 {
		t.Fatalf("want stage_not_ready listing two missing artifacts, got %v", refusal)
	}

	// Validate exits through its gate, so advance is refused even with the artifacts.
	fx.linkStage(t, created.ID, "validation_report", "competitor_profile")
	fx.transition(t, testHandler.AdvanceInitiative, fx.AdminID, created.ID, map[string]any{}).
		Want(http.StatusConflict).JSON(&refusal)
	if refusal["code"] != "stage_is_gate" {
		t.Fatalf("want stage_is_gate, got %v", refusal)
	}

	// Attesting a gate stage is not an attestation stage either.
	fx.transition(t, testHandler.AttestInitiative, fx.AdminID, created.ID, map[string]any{}).
		Want(http.StatusConflict).JSON(&refusal)
	if refusal["code"] != "not_attest_stage" {
		t.Fatalf("want not_attest_stage, got %v", refusal)
	}

	// Send-back from the first stage has nowhere to go; a note is required anyway.
	fx.transition(t, testHandler.SendBackInitiative, fx.AdminID, created.ID, map[string]any{}).Want(http.StatusBadRequest)
	fx.transition(t, testHandler.SendBackInitiative, fx.AdminID, created.ID, map[string]any{"note": "again"}).
		Want(http.StatusConflict)
}

func TestInitiative_ShelveAndResumeAreForAdminsOnly(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Paused")

	fx.transition(t, testHandler.ShelveInitiative, fx.MemberID, created.ID, map[string]any{"note": "not mine"}).
		Want(http.StatusForbidden)
	req := fx.asAgent(newRequest("POST", "/api/initiatives/"+created.ID+"/shelve", map[string]any{"note": "agents cannot"}), fx.Leader, fx.Task)
	testutil.Call(t, testHandler.ShelveInitiative, withURLParam(req, "id", created.ID)).Want(http.StatusForbidden)

	var out InitiativeResponse
	fx.transition(t, testHandler.ShelveInitiative, fx.AdminID, created.ID, map[string]any{"note": "budget freeze"}).
		Want(http.StatusOK).JSON(&out)
	if out.Status != "shelved" {
		t.Fatalf("shelved initiative status = %s", out.Status)
	}

	var refusal map[string]any
	fx.transition(t, testHandler.AdvanceInitiative, fx.AdminID, created.ID, map[string]any{}).
		Want(http.StatusConflict).JSON(&refusal)
	if refusal["code"] != "initiative_shelved" {
		t.Fatalf("a shelved initiative must not move, got %v", refusal)
	}

	fx.transition(t, testHandler.ResumeInitiative, fx.AdminID, created.ID, map[string]any{}).Want(http.StatusOK).JSON(&out)
	if out.Status != "active" {
		t.Fatalf("resumed initiative status = %s", out.Status)
	}
}

func TestInitiative_GateApprovalAdvancesAndRejectionStays(t *testing.T) {
	fx := newIniWorkspace(t)
	approved := fx.createInitiative(t, "Approved gate")
	rejected := fx.createInitiative(t, "Rejected gate")
	for _, id := range []string{approved.ID, rejected.ID} {
		fx.linkStage(t, id, "validation_report", "competitor_profile")
	}

	// The sweep files one gate per ready initiative.
	if _, err := testHandler.RunInitiativeSweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	gates := pendingGates(t, fx.ID)
	if gates[approved.ID] == "" || gates[rejected.ID] == "" {
		t.Fatalf("both ready initiatives should have a pending gate, got %v", gates)
	}

	// Approve: the initiative moves to define, owned by the analysis squad.
	testutil.Call(t, testHandler.DecideProposal,
		withURLParam(fx.as(newRequest("POST", "/api/proposals/"+gates[approved.ID]+"/decision", map[string]any{"decision": "approve"}), fx.AdminID), "id", gates[approved.ID])).
		Want(http.StatusOK)
	var detail InitiativeDetailResponse
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+approved.ID, nil), fx.AdminID), "id", approved.ID)).
		Want(http.StatusOK).JSON(&detail)
	if detail.Initiative.StageKey != "define" || detail.Initiative.OwnerSquadID == nil || *detail.Initiative.OwnerSquadID != fx.Analysis {
		t.Fatalf("approved gate should move to define under the analysis squad: %+v", detail.Initiative)
	}

	// Reject with a note: the initiative stays at validate, and the timeline says so.
	testutil.Call(t, testHandler.DecideProposal,
		withURLParam(fx.as(newRequest("POST", "/api/proposals/"+gates[rejected.ID]+"/decision", map[string]any{"decision": "reject", "note": "Not convinced by the evidence."}), fx.AdminID), "id", gates[rejected.ID])).
		Want(http.StatusOK)
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+rejected.ID, nil), fx.AdminID), "id", rejected.ID)).
		Want(http.StatusOK).JSON(&detail)
	if detail.Initiative.StageKey != "validate" {
		t.Fatalf("rejected gate should leave the initiative at validate, got %s", detail.Initiative.StageKey)
	}
	last := detail.Events[len(detail.Events)-1]
	if last.Kind != "sent_back" || last.Note != "Not convinced by the evidence." {
		t.Fatalf("the timeline should record the send-back, got %+v", last)
	}
}

func TestInitiative_OpportunityProposalCreatesTheInitiative(t *testing.T) {
	fx := newIniWorkspace(t)
	fx.putSquadOnFloor(t, fx.Product, "Product")

	// Filing refusals: an unknown kind, a stage gate filed by a squad, and an
	// opportunity without its thesis.
	post := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateProposal,
			fx.asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.Product, "Opportunity "+fx.ID, body)), fx.Leader, fx.Task))
	}
	post(map[string]any{"kind": "teleport"}).Want(http.StatusBadRequest)
	post(map[string]any{"kind": "stage_gate", "payload": map[string]any{}}).Want(http.StatusBadRequest)
	var refusal map[string]any
	post(map[string]any{"kind": "opportunity", "payload": map[string]any{"market": "SMB"}}).
		Want(http.StatusBadRequest).JSON(&refusal)
	if refusal["code"] != "invalid_payload" {
		t.Fatalf("want invalid_payload, got %v", refusal)
	}

	var filed ProposalResponse
	post(map[string]any{"kind": "opportunity", "payload": map[string]any{"thesis": "SMBs pay late.", "market": "SMB", "target_stage": "validate", "evidence_doc_ids": []string{uuid.NewString()}}}).
		Want(http.StatusCreated).JSON(&filed)
	if filed.Kind != "opportunity" || filed.Payload == nil {
		t.Fatalf("the proposal should carry its kind and payload: %+v", filed)
	}

	testutil.Call(t, testHandler.DecideProposal,
		withURLParam(fx.as(newRequest("POST", "/api/proposals/"+filed.ID+"/decision", map[string]any{"decision": "approve"}), fx.AdminID), "id", filed.ID)).
		Want(http.StatusOK)

	var n int
	dbfx.QueryRow(t, `SELECT count(*) FROM initiative WHERE workspace_id = $1 AND source_proposal_id = $2 AND stage_key = 'validate'`, fx.ID, filed.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("approving the opportunity should create one initiative at validate, found %d", n)
	}
}

func TestInitiative_ReadsAreIsolatedByWorkspace(t *testing.T) {
	fx := newIniWorkspace(t)
	other := newIniWorkspace(t)
	created := fx.createInitiative(t, "Private")

	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(other.as(newRequest("GET", "/api/initiatives/"+created.ID, nil), other.AdminID), "id", created.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.AdvanceInitiative,
		withURLParam(other.as(newRequest("POST", "/api/initiatives/"+created.ID+"/advance", map[string]any{}), other.AdminID), "id", created.ID)).
		Want(http.StatusNotFound)

	var list struct {
		Initiatives []InitiativeResponse `json:"initiatives"`
	}
	testutil.Call(t, testHandler.ListInitiatives, other.as(newRequest("GET", "/api/initiatives", nil), other.MemberID)).
		Want(http.StatusOK).JSON(&list)
	for _, i := range list.Initiatives {
		if i.ID == created.ID {
			t.Fatalf("another workspace's initiative leaked into the list")
		}
	}
}

func TestInitiative_ListValidatesFiltersAndNeverLeaksPipelines(t *testing.T) {
	fx := newIniWorkspace(t)
	fx.createInitiative(t, "Listed")

	testutil.Call(t, testHandler.ListInitiatives,
		fx.as(newRequest("GET", "/api/initiatives?pipeline_id=nope", nil), fx.MemberID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ListInitiatives,
		fx.as(newRequest("GET", "/api/initiatives?status=bogus", nil), fx.MemberID)).Want(http.StatusBadRequest)

	var list struct {
		Initiatives []InitiativeResponse `json:"initiatives"`
	}
	testutil.Call(t, testHandler.ListInitiatives,
		fx.as(newRequest("GET", "/api/initiatives?owner_squad_id="+fx.Product+"&status=active", nil), fx.MemberID)).
		Want(http.StatusOK).JSON(&list)
	if len(list.Initiatives) != 1 {
		t.Fatalf("filter by owner and status should find the one initiative, got %d", len(list.Initiatives))
	}

	var pipelines struct {
		Pipelines []PipelineResponse `json:"pipelines"`
	}
	testutil.Call(t, testHandler.ListPipelines, fx.as(newRequest("GET", "/api/pipelines", nil), fx.MemberID)).
		Want(http.StatusOK).JSON(&pipelines)
	if len(pipelines.Pipelines) != 1 || !pipelines.Pipelines[0].IsDefault || len(pipelines.Pipelines[0].Stages) != 7 {
		t.Fatalf("want the default pipeline with seven stages, got %+v", pipelines.Pipelines)
	}
	if pipelines.Pipelines[0].Stages[0].OwnerSquadID == nil || *pipelines.Pipelines[0].Stages[1].OwnerSquadID != fx.Analysis {
		t.Fatalf("stage owners should be the resolved squad ids")
	}
}

func TestPipelineInstall_IsIdempotentAndReportsUnownedStages(t *testing.T) {
	fx := newIniWorkspace(t)
	ps := service.NewPipelineService(testHandler.Queries, testPool)

	// A second install changes nothing and reports the stages still unowned.
	res, err := ps.InstallDefault(context.Background(), service.InstallPipelineParams{
		WorkspaceID: parseUUID(fx.ID),
		ResolveSquad: func(context.Context, string) (pgtype.UUID, bool, error) {
			return pgtype.UUID{}, false, nil
		},
	})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if res.Created {
		t.Fatalf("a second install must not create a pipeline")
	}
	want := []string{"plan", "launch"}
	if fmt.Sprint(res.UnownedStages) != fmt.Sprint(want) {
		t.Fatalf("unowned stages = %v, want %v", res.UnownedStages, want)
	}

	var statuses int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_status WHERE workspace_id = $1 AND key LIKE 'ini\_%'`, fx.ID).Scan(&statuses)
	if statuses != 7 {
		t.Fatalf("each stage has one status, found %d", statuses)
	}
}

func TestSquadDirectory_ShowsIntakeResponseTimeAndOpenRequests(t *testing.T) {
	fx := newIniWorkspace(t)
	// Product takes requests with a 24 hour response time.
	dbfx.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, mission, intake, response_hours)
		VALUES ($1, $2, 'Validate opportunities.', 'Send a thesis and a market.', 24)`, fx.Product, fx.ID)

	var out struct {
		Squads []SquadDirectoryEntryResponse `json:"squads"`
	}
	testutil.Call(t, testHandler.ListSquadDirectory, fx.as(newRequest("GET", "/api/squads/directory", nil), fx.MemberID)).
		Want(http.StatusOK).JSON(&out)
	var product *SquadDirectoryEntryResponse
	for i := range out.Squads {
		if out.Squads[i].ID == fx.Product {
			product = &out.Squads[i]
		}
	}
	if product == nil || product.ResponseHours != 24 || product.Intake != "Send a thesis and a market." || product.Mission != "Validate opportunities." {
		t.Fatalf("directory entry wrong: %+v", product)
	}

	// Another workspace sees none of these squads.
	other := newIniWorkspace(t)
	testutil.Call(t, testHandler.ListSquadDirectory, other.as(newRequest("GET", "/api/squads/directory", nil), other.MemberID)).
		Want(http.StatusOK).JSON(&out)
	for _, s := range out.Squads {
		if s.ID == fx.Product || s.ID == fx.Analysis {
			t.Fatalf("squad from another workspace leaked into the directory")
		}
	}
}

// forceInitiative moves an initiative to a stage as the admin.
func (fx iniWorkspace) forceInitiative(t *testing.T, id, stage string) {
	t.Helper()
	fx.transition(t, testHandler.ForceInitiative, fx.AdminID, id, map[string]any{"to_stage": stage, "reason": "test"}).
		Want(http.StatusOK)
}

func TestInitiative_OperateReviewSunsetClosesAndRunsLearn(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Launched")
	fx.forceInitiative(t, created.ID, "operate")

	// The initiative has been live for 31 days, so its 30-day review is due.
	dbfx.Exec(t, `UPDATE initiative SET operating_since = now() - interval '31 days' WHERE id = $1`, created.ID)
	if _, err := testHandler.RunInitiativeSweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	gate := pendingGates(t, fx.ID)[created.ID]
	if gate == "" {
		t.Fatalf("the 30-day review should be filed")
	}
	var gateDay string
	dbfx.QueryRow(t, `SELECT payload->>'gate_day' FROM proposal WHERE id = $1`, gate).Scan(&gateDay)
	if gateDay != "30" {
		t.Fatalf("gate_day = %s, want 30", gateDay)
	}

	// An approval without an outcome is refused: the review's choice is required.
	testutil.Call(t, testHandler.DecideProposal,
		withURLParam(fx.as(newRequest("POST", "/api/proposals/"+gate+"/decision", map[string]any{"decision": "approve"}), fx.AdminID), "id", gate)).
		Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.DecideProposal,
		withURLParam(fx.as(newRequest("POST", "/api/proposals/"+gate+"/decision", map[string]any{"decision": "approve", "outcome": "sunset"}), fx.AdminID), "id", gate)).
		Want(http.StatusOK)

	var detail InitiativeDetailResponse
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+created.ID, nil), fx.AdminID), "id", created.ID)).
		Want(http.StatusOK).JSON(&detail)
	if detail.Initiative.Status != "closed" || detail.Initiative.StageKey != "learn" || detail.Initiative.ClosedReason != "sunset" {
		t.Fatalf("sunset should close the initiative at learn: %+v", detail.Initiative)
	}

	// The review is filed once: a second run finds the day already filed.
	if _, err := testHandler.RunInitiativeSweep(context.Background()); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	var n int
	dbfx.QueryRow(t, `SELECT count(*) FROM proposal WHERE workspace_id = $1 AND kind = 'stage_gate' AND payload->>'gate_day' = '30'`, fx.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("the 30-day review should be filed once, found %d", n)
	}
}

func TestInitiative_ForceToBuildCreatesTheBuildProjectOnTheSDLCWorkflow(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Shipping")
	fx.forceInitiative(t, created.ID, "build")

	var detail InitiativeDetailResponse
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+created.ID, nil), fx.AdminID), "id", created.ID)).
		Want(http.StatusOK).JSON(&detail)
	if detail.Initiative.BuildProjectID == nil {
		t.Fatalf("the build stage should record its build project")
	}

	// The project runs the SDLC workflow, not the workspace default.
	var workflowName string
	dbfx.QueryRow(t, `SELECT w.name FROM project p JOIN issue_workflow w ON w.id = p.workflow_id WHERE p.id = $1`,
		*detail.Initiative.BuildProjectID).Scan(&workflowName)
	if workflowName != "SDLC Auto" {
		t.Fatalf("the build project should use the SDLC Auto workflow, got %q", workflowName)
	}
	// Its developer, reviewer and QA steps hand to the squad's SDLC agents.
	var agentSteps int
	dbfx.QueryRow(t, `SELECT count(*) FROM project p JOIN issue_workflow w ON w.id = p.workflow_id,
		jsonb_array_elements(w.steps) AS s
		WHERE p.id = $1 AND s->'handler'->>'type' = 'agent' AND s->>'status_key' IN ('todo', 'in_progress', 'in_review', 'qa')`,
		*detail.Initiative.BuildProjectID).Scan(&agentSteps)
	if agentSteps != 4 {
		t.Fatalf("developer, in progress, reviewer and QA steps should hand to agents, found %d", agentSteps)
	}

	// The kickoff sits in the developer step and is assigned to the developer agent.
	var kickoffs int
	var assigneeType, status string
	dbfx.QueryRow(t, `SELECT count(*), max(assignee_type), max(status) FROM issue WHERE project_id = $1`,
		*detail.Initiative.BuildProjectID).Scan(&kickoffs, &assigneeType, &status)
	if kickoffs != 1 || assigneeType != "agent" || status != "todo" {
		t.Fatalf("want one kickoff, agent-assigned, in todo; got %d %s %s", kickoffs, assigneeType, status)
	}

	// A second build by the same squad reuses the workflow.
	second := fx.createInitiative(t, "Shipping again")
	fx.forceInitiative(t, second.ID, "build")
	var workflows int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_workflow WHERE workspace_id = $1 AND name LIKE 'SDLC Auto%'`, fx.ID).Scan(&workflows)
	if workflows != 1 {
		t.Fatalf("the SDLC workflow should be reused by the same squad, found %d", workflows)
	}
}

func TestInitiative_LaunchStartsTheWeeklyMetricsRoutine(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Launching")
	fx.forceInitiative(t, created.ID, "operate")

	var routines int
	var purpose, cron, squad string
	dbfx.QueryRow(t, `SELECT count(*), max(r.purpose), max(t.cron_expression), max(r.squad_id::text)
		FROM squad_routine r
		JOIN autopilot a ON a.id = r.autopilot_id
		JOIN autopilot_trigger t ON t.autopilot_id = a.id
		WHERE r.workspace_id = $1 AND r.expected_output = 'report'`, fx.ID).Scan(&routines, &purpose, &cron, &squad)
	if routines != 1 {
		t.Fatalf("launch should link one routine, found %d", routines)
	}
	if squad != fx.Product || cron != "0 9 * * 1" || purpose != "Weekly metrics: "+created.Identifier+" Launching" {
		t.Fatalf("routine = squad %s cron %q purpose %q", squad, cron, purpose)
	}

	// Entering operate again (a force after a send-back) does not start a second one.
	fx.forceInitiative(t, created.ID, "learn")
	fx.forceInitiative(t, created.ID, "operate")
	dbfx.QueryRow(t, `SELECT count(*) FROM squad_routine WHERE workspace_id = $1 AND expected_output = 'report'`, fx.ID).Scan(&routines)
	if routines != 1 {
		t.Fatalf("a second launch should not add a routine, found %d", routines)
	}
}

func TestDigest_PipelineStalledAndOverdueRequests(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Stalled one")

	// Validate's SLA is 14 days; this initiative has been there for 20.
	dbfx.Exec(t, `UPDATE initiative SET stage_entered_at = now() - interval '20 days' WHERE id = $1`, created.ID)
	// A squad request to Product, which answers within 24 hours, is three days old.
	dbfx.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, response_hours) VALUES ($1, $2, 24)`, fx.Product, fx.ID)
	dbfx.Insert(t, "issue", testutil.Cols{
		"workspace_id":  fx.ID,
		"title":         "Please review the pricing note",
		"status":        "todo",
		"priority":      "none",
		"creator_type":  "member",
		"creator_id":    fx.AdminID,
		"number":        testutil.Raw("(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = '" + fx.ID + "')"),
		"position":      0,
		"assignee_type": "squad",
		"assignee_id":   fx.Product,
		"origin_type":   "squad_request",
		"origin_id":     testutil.Raw("gen_random_uuid()"),
		"created_at":    testutil.Raw("now() - interval '3 days'"),
	})

	d, err := service.NewDigestService(testHandler.Queries).Compute(context.Background(), parseUUID(fx.ID), time.Now().Add(-24*time.Hour), "INI")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	var found bool
	for _, p := range d.Pipeline {
		if p.ID == created.ID {
			found = true
			if !p.Stalled || p.DaysInStage < 20 || p.SLADays != 14 || p.ArtifactsRequired != 2 || p.ArtifactsPresent != 0 {
				t.Fatalf("stalled initiative reported wrong: %+v", p)
			}
			if len(p.Blockers) == 0 {
				t.Fatalf("the blockers should name the missing artifacts and the stall")
			}
		}
	}
	if !found {
		t.Fatalf("the digest should list the active initiative")
	}
	if len(d.OverdueRequests) != 1 || d.OverdueRequests[0].Title != "Please review the pricing note" || d.OverdueRequests[0].HoursOverdue < 40 {
		t.Fatalf("the request is two days past a 24 hour response time: %+v", d.OverdueRequests)
	}
}

func TestInitiative_ScaleStartsAFollowOnAtDefineAndKeepsOperating(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Scaling")
	fx.forceInitiative(t, created.ID, "operate")
	dbfx.Exec(t, `UPDATE initiative SET operating_since = now() - interval '31 days' WHERE id = $1`, created.ID)
	if _, err := testHandler.RunInitiativeSweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	gate := pendingGates(t, fx.ID)[created.ID]
	if gate == "" {
		t.Fatalf("the 30-day review should be filed")
	}
	testutil.Call(t, testHandler.DecideProposal,
		withURLParam(fx.as(newRequest("POST", "/api/proposals/"+gate+"/decision", map[string]any{"decision": "approve", "outcome": "scale"}), fx.AdminID), "id", gate)).
		Want(http.StatusOK)

	var detail InitiativeDetailResponse
	testutil.Call(t, testHandler.GetInitiative,
		withURLParam(fx.as(newRequest("GET", "/api/initiatives/"+created.ID, nil), fx.AdminID), "id", created.ID)).
		Want(http.StatusOK).JSON(&detail)
	if detail.Initiative.Status != "operating" {
		t.Fatalf("scale keeps the original operating, got %s", detail.Initiative.Status)
	}
	var follow string
	dbfx.QueryRow(t, `SELECT stage_key FROM initiative WHERE workspace_id = $1 AND title = $2`, fx.ID, "Scaling (scaled)").Scan(&follow)
	if follow != "define" {
		t.Fatalf("the follow-on starts at define, got %q", follow)
	}

	// Spec 3.6: Learn runs when an initiative is scaled, so the original gets a
	// retrospective candidate of its own, under the scaled trigger.
	var scaledEvents int
	dbfx.QueryRow(t, `SELECT count(*) FROM initiative_stage_event WHERE initiative_id = $1 AND kind = 'scaled'`, created.ID).Scan(&scaledEvents)
	if scaledEvents != 1 {
		t.Fatalf("scaled events on the original = %d, want 1", scaledEvents)
	}
	candidates, err := (&service.RetroService{Queries: testHandler.Queries}).Candidates(context.Background(), parseUUID(fx.ID), time.Now())
	if err != nil {
		t.Fatalf("retro candidates: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Trigger == service.RetroTriggerInitiativeScaled && c.SubjectID == parseUUID(created.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no initiative_scaled retrospective candidate for the scaled initiative: %+v", candidates)
	}
}

func TestSquadRequest_IssueToASquadUnderAParentIsTagged(t *testing.T) {
	fx := newIniWorkspace(t)
	parent := fx.createInitiative(t, "Needs a PRD")
	var parentIssue string
	dbfx.QueryRow(t, `SELECT issue_id::text FROM initiative WHERE id = $1`, parent.ID).Scan(&parentIssue)

	req := fx.as(newRequest("POST", "/api/issues", map[string]any{
		"title":           "Draft the PRD",
		"assignee_type":   "squad",
		"assignee_id":     fx.Analysis,
		"parent_issue_id": parentIssue,
	}), fx.AdminID)
	var out IssueResponse
	testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated).JSON(&out)

	var originType, originID string
	dbfx.QueryRow(t, `SELECT origin_type, origin_id::text FROM issue WHERE id = $1`, out.ID).Scan(&originType, &originID)
	if originType != "squad_request" || originID != parentIssue {
		t.Fatalf("origin = %s/%s, want squad_request/%s", originType, originID, parentIssue)
	}
}

func TestInitiative_OpportunityNeedsAResearchOrProductSquadAndEvidence(t *testing.T) {
	fx := newIniWorkspace(t)
	fx.putSquadOnFloor(t, fx.Product, "Product")
	// A squad on the Build floor, led by an agent of its own.
	ops := iniAgent(t, fx.ID, "Ops Leader")
	opsTask := createHandlerTestTaskForAgent(t, ops)
	opsSquad := dbfx.Squad(t, "Ops", ops, testutil.Cols{"workspace_id": fx.ID})
	dbfx.SquadMember(t, opsSquad, "agent", ops, testutil.Cols{"role": "leader"})
	fx.putSquadOnFloor(t, opsSquad, "Build")

	file := func(squad, agent, task string, payload map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateProposal,
			fx.asAgent(newRequest("POST", "/api/proposals", proposalBody(squad, "Opportunity from "+squad,
				map[string]any{"kind": "opportunity", "payload": payload})), agent, task))
	}
	evidence := []string{uuid.NewString()}

	// A squad off the Research and Product floors may not file one.
	var refusal map[string]any
	file(opsSquad, ops, opsTask, map[string]any{"thesis": "Bulk invoicing.", "evidence_doc_ids": evidence}).
		Want(http.StatusBadRequest).JSON(&refusal)
	if refusal["code"] != "invalid_payload" {
		t.Fatalf("want invalid_payload for a Build squad, got %v", refusal)
	}

	// The Product squad must cite the evidence the thesis rests on.
	file(fx.Product, fx.Leader, fx.Task, map[string]any{"thesis": "Bulk invoicing."}).Want(http.StatusBadRequest)
	file(fx.Product, fx.Leader, fx.Task, map[string]any{"thesis": "Bulk invoicing.", "evidence_doc_ids": []string{}}).
		Want(http.StatusBadRequest)

	file(fx.Product, fx.Leader, fx.Task, map[string]any{"thesis": "Bulk invoicing.", "evidence_doc_ids": evidence}).
		Want(http.StatusCreated)
}

func TestInitiative_CreateIsOpenToTheSecretaryOnRequest(t *testing.T) {
	fx := newIniWorkspace(t)
	owner := dbfx.User(t, "Ini Owner "+fx.ID, "ini-owner-"+fx.ID+"@example.com")
	dbfx.Member(t, fx.ID, owner, "owner")
	secretary := iniAgent(t, fx.ID, "Ini Secretary "+fx.ID)
	secretaryTask := createHandlerTestTaskForAgent(t, secretary)
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, secretary, fx.ID)

	body := map[string]any{"title": "Asked for by the boss", "thesis": "Customers want bulk invoicing."}
	testutil.Call(t, testHandler.CreateInitiative, fx.asAgent(newRequest("POST", "/api/initiatives", body), fx.Leader, fx.Task)).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CreateInitiative, fx.as(newRequest("POST", "/api/initiatives", body), fx.MemberID)).
		Want(http.StatusForbidden)

	// The Secretary starts it, and the workspace's owner sponsors it.
	var out InitiativeResponse
	testutil.Call(t, testHandler.CreateInitiative, fx.asAgent(newRequest("POST", "/api/initiatives", body), secretary, secretaryTask)).
		Want(http.StatusCreated).JSON(&out)
	var sponsor string
	dbfx.QueryRow(t, `SELECT sponsor_user_id::text FROM initiative WHERE id = $1`, out.ID).Scan(&sponsor)
	if sponsor != owner {
		t.Fatalf("the Secretary's initiative should be sponsored by the owner %s, got %s", owner, sponsor)
	}

	fx.createInitiative(t, "Started by the admin")
}

func TestInitiative_ReviewDaysAreSetByAnOwnerOrAdminAndValidated(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Reviewed")
	patch := func(as string, body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateInitiative,
			withURLParam(fx.as(newRequest("PATCH", "/api/initiatives/"+created.ID, body), as), "id", created.ID))
	}

	patch(fx.MemberID, map[string]any{"gate_days": []int{45}}).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateInitiative,
		withURLParam(fx.asAgent(newRequest("PATCH", "/api/initiatives/"+created.ID, map[string]any{"gate_days": []int{45}}), fx.Leader, fx.Task), "id", created.ID)).
		Want(http.StatusForbidden)

	// A PATCH with no gate_days, or with days the rule refuses, changes nothing.
	var refusal map[string]any
	patch(fx.AdminID, map[string]any{}).Want(http.StatusBadRequest).JSON(&refusal)
	if refusal["error"] != "gate_days is required" {
		t.Fatalf("a PATCH without gate_days should say so, got %v", refusal)
	}
	for name, body := range map[string]map[string]any{
		"empty":    {"gate_days": []int{}},
		"zero":     {"gate_days": []int{0}},
		"too long": {"gate_days": []int{366}},
		"repeated": {"gate_days": []int{30, 30}},
		"too many": {"gate_days": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
	} {
		refusal = nil
		patch(fx.AdminID, body).Want(http.StatusBadRequest).JSON(&refusal)
		if refusal["code"] != "invalid_gate_days" {
			t.Fatalf("%s: want invalid_gate_days, got %v", name, refusal)
		}
	}
	patch(fx.AdminID, map[string]any{"gate_days": []float64{30.5}}).Want(http.StatusBadRequest)
	var days string
	dbfx.QueryRow(t, `SELECT array_to_string(gate_days, ',') FROM initiative WHERE id = $1`, created.ID).Scan(&days)
	if days != "30,90" {
		t.Fatalf("refused changes must not be written, gate_days = %s", days)
	}

	// An admin sets them, sorted, and the timeline records the change.
	var out InitiativeResponse
	patch(fx.AdminID, map[string]any{"gate_days": []int{120, 45}}).Want(http.StatusOK).JSON(&out)
	dbfx.QueryRow(t, `SELECT array_to_string(gate_days, ',') FROM initiative WHERE id = $1`, created.ID).Scan(&days)
	if days != "45,120" {
		t.Fatalf("gate_days = %s, want 45,120", days)
	}
	if n := eventCount(fx.detail(t, created.ID), "gate_days_changed"); n != 1 {
		t.Fatalf("the change should be on the timeline once, found %d", n)
	}
}

func TestInitiative_OperateReviewsFileOncePerDayAndEachOutcomeHasItsEffect(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Reviewed twice")
	fx.forceInitiative(t, created.ID, "operate")
	// Live for 91 days: the 30 and the 90-day reviews are both due.
	dbfx.Exec(t, `UPDATE initiative SET operating_since = now() - interval '91 days' WHERE id = $1`, created.ID)
	for i := 0; i < 2; i++ {
		if _, err := testHandler.RunInitiativeSweep(context.Background()); err != nil {
			t.Fatalf("sweep %d: %v", i+1, err)
		}
	}

	// Each review day is filed once, and the operate stage files no generic gate.
	var reviews, generic int
	dbfx.QueryRow(t, `SELECT count(*) FILTER (WHERE payload->>'gate_day' IS NOT NULL),
		count(*) FILTER (WHERE payload->>'gate_day' IS NULL)
		FROM proposal WHERE workspace_id = $1 AND kind = 'stage_gate' AND payload->>'initiative_id' = $2`,
		fx.ID, created.ID).Scan(&reviews, &generic)
	if reviews != 2 || generic != 0 {
		t.Fatalf("two sweeps: want 2 reviews and no generic gate, got %d reviews and %d generic", reviews, generic)
	}
	var options string
	dbfx.QueryRow(t, `SELECT i.description FROM proposal p JOIN issue i ON i.id = p.issue_id WHERE p.id = $1`,
		reviewGate(t, fx.ID, created.ID, 30)).Scan(&options)
	for _, want := range []string{"Continue:", "Scale:", "Sunset:"} {
		if !strings.Contains(options, want) {
			t.Fatalf("the review should offer %q, got %q", want, options)
		}
	}

	// An outcome must be one of the three, and approval needs one.
	gate30, gate90 := reviewGate(t, fx.ID, created.ID, 30), reviewGate(t, fx.ID, created.ID, 90)
	var refusal map[string]any
	fx.decideAsAdmin(t, gate30, map[string]any{"decision": "approve", "outcome": "delete"}).
		Want(http.StatusBadRequest).JSON(&refusal)
	if refusal["code"] != "invalid_outcome" {
		t.Fatalf("a malformed outcome: want invalid_outcome, got %v", refusal)
	}
	fx.decideAsAdmin(t, gate30, map[string]any{"decision": "approve"}).Want(http.StatusBadRequest)

	// Continue keeps the initiative operating, and the 90-day review stays open.
	fx.decideAsAdmin(t, gate30, map[string]any{"decision": "approve", "outcome": "continue"}).Want(http.StatusOK)
	d := fx.detail(t, created.ID)
	last := d.Events[len(d.Events)-1]
	if d.Initiative.Status != "operating" || d.Initiative.StageKey != "operate" || !strings.HasPrefix(last.Note, "Continue:") {
		t.Fatalf("continue should keep the initiative operating: %+v, last event %+v", d.Initiative, last)
	}
	var pending string
	dbfx.QueryRow(t, `SELECT state FROM proposal WHERE id = $1`, gate90).Scan(&pending)
	if pending != "pending" {
		t.Fatalf("the 90-day review should still be pending, is %s", pending)
	}

	// Sunset at the 90-day review closes the initiative through learn.
	fx.decideAsAdmin(t, gate90, map[string]any{"decision": "approve", "outcome": "sunset"}).Want(http.StatusOK)
	d = fx.detail(t, created.ID)
	if d.Initiative.Status != "closed" || d.Initiative.StageKey != "learn" || d.Initiative.ClosedReason != "sunset" {
		t.Fatalf("sunset should close the initiative at learn: %+v", d.Initiative)
	}
}

func TestInitiative_ALinkedArtifactIsRecordedAndAnAutoStageAdvancesOnTheLast(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Auto validate")
	fx.makeAutoExit(t, 0)
	link := func(kind string) bool {
		t.Helper()
		linked, err := testHandler.initiativeService().LinkArtifact(context.Background(), parseUUID(fx.ID), parseUUID(created.ID),
			parseUUID(uuid.NewString()), kind, service.InitiativeActor{Type: "member", ID: parseUUID(fx.AdminID)})
		if err != nil {
			t.Fatalf("link %s: %v", kind, err)
		}
		return linked
	}

	// A kind the stage does not require links nothing and writes no event.
	if link("market_report") || link("customer_insight") {
		t.Fatalf("a kind the stage does not require must not link")
	}
	if n := eventCount(fx.detail(t, created.ID), "artifact_linked"); n != 0 {
		t.Fatalf("a refused link must not be on the timeline, found %d", n)
	}

	// A required artifact links and is recorded; the stage waits for the last one.
	if !link("validation_report") {
		t.Fatalf("validation_report is required at validate")
	}
	d := fx.detail(t, created.ID)
	if d.Initiative.StageKey != "validate" || d.Initiative.ArtifactsPresent != 1 || eventCount(d, "artifact_linked") != 1 {
		t.Fatalf("one of two artifacts: %+v, events %+v", d.Initiative, d.Events)
	}

	// The last required artifact moves the auto stage on, as the sweep would.
	if !link("competitor_profile") {
		t.Fatalf("competitor_profile is required at validate")
	}
	d = fx.detail(t, created.ID)
	if d.Initiative.StageKey != "define" || d.Initiative.OwnerSquadID == nil || *d.Initiative.OwnerSquadID != fx.Analysis {
		t.Fatalf("the last artifact should advance validate to define under the analysis squad: %+v", d.Initiative)
	}
	last := d.Events[len(d.Events)-1]
	if last.Kind != "advanced" || last.ActorType != "system" || eventCount(d, "artifact_linked") != 2 {
		t.Fatalf("the advance should follow the two links on the timeline: last %+v", last)
	}
}

func TestInitiative_APublishedDocumentTakesItsArtifact(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Documented")
	fx.makeAutoExit(t, 0)
	svc := knowledgeFor(t, fx.ID, fx.AdminID)
	ctx := context.Background()
	ws, ini := parseUUID(fx.ID), parseUUID(created.ID)
	admin := service.KnowledgeActor{Type: service.KnowledgeActorMember, ID: parseUUID(fx.AdminID), Curator: true}
	write := func(kind, content string, publish bool) service.KnowledgeDocumentView {
		t.Helper()
		v, _, err := svc.CreateDocument(ctx, ws, admin, service.KnowledgeDocumentInput{
			Kind: kind, Title: kind + " for Documented", Content: content, Publish: publish, InitiativeID: ini,
		})
		if err != nil {
			t.Fatalf("write %s: %v", kind, err)
		}
		return v
	}
	validation := "## Question\n\nDo bulk buyers pay?\n\n## Findings\n\nYes.\n\n## Recommendation\n\nBuild it.\n"
	competitors := "## Summary\n\nTwo rivals.\n\n## Offering\n\nSeats.\n\n## Pricing\n\nPer seat.\n\n## Strengths and weaknesses\n\nSlow.\n\n## Sources\n\nThe sites.\n"
	market := "## Summary\n\nGrowing.\n\n## Market size\n\nLarge.\n\n## Trends\n\nBulk.\n\n## Sources\n\nA survey.\n"

	// A draft is not published, so it takes nothing.
	draft := write("validation_report", validation, false)
	if n := fx.detail(t, created.ID).Initiative.ArtifactsPresent; n != 0 {
		t.Fatalf("a draft must not satisfy the checklist, present = %d", n)
	}

	// Publishing the draft takes the artifact.
	if _, err := svc.PublishDocument(ctx, ws, parseUUID(draft.ID), admin); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if n := fx.detail(t, created.ID).Initiative.ArtifactsPresent; n != 1 {
		t.Fatalf("a published validation_report should satisfy one artifact, present = %d", n)
	}

	// A document of a kind the stage does not require takes nothing.
	write("market_report", market, true)
	if d := fx.detail(t, created.ID); d.Initiative.ArtifactsPresent != 1 || d.Initiative.StageKey != "validate" {
		t.Fatalf("a wrong kind must not satisfy the checklist: %+v", d.Initiative)
	}

	// The last required document published moves the auto stage on.
	write("competitor_profile", competitors, true)
	if d := fx.detail(t, created.ID); d.Initiative.StageKey != "define" || eventCount(d, "artifact_linked") != 2 {
		t.Fatalf("the second required document should advance validate: %+v", d.Initiative)
	}
}

// The operate gates come from the hourly sweep, which reads operating initiatives
// (status operating), not the active ones. The clock is moved by setting the
// launch time back, as days pass in production.
func TestInitiative_OperateGatesFileFromTheSweepAtThirtyAndNinetyDays(t *testing.T) {
	fx := newIniWorkspace(t)
	created := fx.createInitiative(t, "Launched on the clock")
	fx.forceInitiative(t, created.ID, "operate")
	ctx := context.Background()
	sweep := func() {
		t.Helper()
		if _, err := testHandler.RunInitiativeSweep(ctx); err != nil {
			t.Fatalf("sweep: %v", err)
		}
	}
	reviewsFiled := func() int {
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM proposal WHERE workspace_id = $1 AND kind = 'stage_gate'
			AND payload->>'initiative_id' = $2 AND payload->>'gate_day' IS NOT NULL`, fx.ID, created.ID).Scan(&n)
		return n
	}
	setLive := func(days int) {
		t.Helper()
		dbfx.Exec(t, fmt.Sprintf(`UPDATE initiative SET operating_since = now() - interval '%d days' WHERE id = $1`, days), created.ID)
	}

	// On launch day no review is due.
	sweep()
	if n := reviewsFiled(); n != 0 {
		t.Fatalf("no review is due on launch day, filed %d", n)
	}

	// Thirty days on, the sweep files the 30-day review, once.
	setLive(30)
	sweep()
	sweep()
	if n := reviewsFiled(); n != 1 {
		t.Fatalf("at 30 days one review should be filed, and not again the same day: filed %d", n)
	}
	gate30 := reviewGate(t, fx.ID, created.ID, 30)

	// Continue keeps it operating and the initiative is not closed by the review.
	fx.decideAsAdmin(t, gate30, map[string]any{"decision": "approve", "outcome": "continue"}).Want(http.StatusOK)
	d := fx.detail(t, created.ID)
	if d.Initiative.Status != "operating" || d.Initiative.StageKey != "operate" {
		t.Fatalf("continue should keep the initiative operating: %+v", d.Initiative)
	}

	// Ninety days on, the 90-day review is filed; the 30-day one is not filed again.
	setLive(90)
	sweep()
	sweep()
	if n := reviewsFiled(); n != 2 {
		t.Fatalf("at 90 days the second review should be filed once: filed %d", n)
	}

	// Sunset at the 90-day review closes it through learn.
	fx.decideAsAdmin(t, reviewGate(t, fx.ID, created.ID, 90), map[string]any{"decision": "approve", "outcome": "sunset"}).Want(http.StatusOK)
	d = fx.detail(t, created.ID)
	if d.Initiative.Status != "closed" || d.Initiative.StageKey != "learn" || d.Initiative.ClosedReason != "sunset" {
		t.Fatalf("sunset should close the initiative at learn: %+v", d.Initiative)
	}
}
