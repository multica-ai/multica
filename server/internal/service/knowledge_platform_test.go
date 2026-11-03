package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// platformEnv is an enabled knowledge workspace with a fixture builder, for the
// platform tests: restricted bases, the upload rules, the tenant lifecycle, the
// models step and the owner's login.
type platformEnv struct {
	*knowledgeEnv
	fx *testutil.Fixture
}

func newPlatformEnv(t *testing.T) *platformEnv {
	t.Helper()
	e := newKnowledgeEnv(t)
	fx := testutil.New(e.pool, e.wsID, e.userID)
	t.Cleanup(func() {
		ctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM knowledge_acl WHERE workspace_id = $1`,
			`DELETE FROM knowledge_owner_account WHERE workspace_id = $1`,
			`DELETE FROM knowledge_tenant_deprovision WHERE workspace_id = $1`,
		} {
			if _, err := e.pool.Exec(ctx, stmt, e.wsID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})
	return &platformEnv{knowledgeEnv: e, fx: fx}
}

func (e *platformEnv) agentActor(id string) KnowledgeActor {
	return KnowledgeActor{Type: KnowledgeActorAgent, ID: mustUUID(e.t, id)}
}

func (e *platformEnv) memberActor(curator bool) KnowledgeActor {
	return KnowledgeActor{Type: KnowledgeActorMember, ID: e.ws, Curator: curator}
}

// canSee reports whether an actor can list the base and search it.
func (e *platformEnv) canSee(actor KnowledgeActor, key string) bool {
	e.t.Helper()
	ctx := context.Background()
	bases, err := e.svc.BasesFor(ctx, e.ws, actor)
	if err != nil {
		e.t.Fatalf("BasesFor: %v", err)
	}
	listed := false
	for _, b := range bases.KnowledgeBases {
		if b.Key == key {
			listed = true
		}
	}
	_, err = e.svc.ListDocumentsFor(ctx, e.ws, actor, key, "")
	named := err == nil
	if listed != named {
		e.t.Fatalf("base %s: listed=%v but named=%v (%v)", key, listed, named, err)
	}
	return listed
}

// restrict makes the product base restricted, as an owner would.
func (e *platformEnv) restrictProduct() {
	e.t.Helper()
	if _, err := e.svc.SetKnowledgeVisibility(context.Background(), e.ws, e.user, "product", KnowledgeVisibilityRestricted); err != nil {
		e.t.Fatalf("restrict product: %v", err)
	}
}

func (e *platformEnv) grant(subjectType, subjectID, access string) {
	e.t.Helper()
	if _, err := e.svc.SetKnowledgeGrant(context.Background(), e.ws, "product", subjectType, subjectID, access); err != nil {
		e.t.Fatalf("grant %s %s %s: %v", subjectType, subjectID, access, err)
	}
}

// bound reports whether an agent is bound to the product base's endpoint.
func (e *platformEnv) bound(agentID string) bool {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_mcp_server ams JOIN workspace_mcp_server s ON s.id = ams.server_id
		 WHERE ams.agent_id = $1 AND s.name = $2 AND s.workspace_id = $3`,
		agentID, knowledgeRestrictedEndpointName("product"), e.wsID).Scan(&n)
	if err != nil {
		e.t.Fatal(err)
	}
	return n > 0
}

// companyReadKBIDs returns the WeKnora ids company-read may read, from the fake.
func (e *platformEnv) companyReadKBIDs() []string {
	e.t.Helper()
	ws := e.fake.Workspace(e.workspaceKey(e.t))
	eps, err := ws.ListMCPEndpoints(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, ep := range eps {
		if ep.Name == KnowledgeMCPServerName {
			return ep.KnowledgeBaseIDs
		}
	}
	e.t.Fatal("company-read endpoint not found")
	return nil
}

// workspaceKey returns the workspace's WeKnora key as the service holds it.
func (e *platformEnv) workspaceKey(t *testing.T) string {
	t.Helper()
	row, err := e.q.GetWorkspaceKnowledge(context.Background(), e.ws)
	if err != nil {
		t.Fatal(err)
	}
	key, err := e.svc.Box.Open(row.ApiKeyCiphertext)
	if err != nil {
		t.Fatal(err)
	}
	return string(key)
}

func (e *platformEnv) productWeKnoraID() string {
	e.t.Helper()
	b, err := e.q.GetKnowledgeBaseByKey(context.Background(), dbBaseKey(e.knowledgeEnv, "product"))
	if err != nil {
		e.t.Fatal(err)
	}
	return b.WeknoraKbID
}

// TestRestrictedBaseIsVisibleOnlyToListedSubjects is the acceptance check of
// spec 3.8: a restricted base is invisible to a squad outside its list and
// visible to one inside it. Floors and agents are subjects too.
func TestRestrictedBaseIsVisibleOnlyToListedSubjects(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	ctx := context.Background()

	leaderA := e.fx.Agent(t, "leader-a", "")
	leaderB := e.fx.Agent(t, "leader-b", "")
	agentA := e.fx.Agent(t, "agent-a", "")
	agentB := e.fx.Agent(t, "agent-b", "")
	outsider := e.fx.Agent(t, "outsider", "")
	floor := e.fx.Insert(t, "office_floor", testutil.Cols{"workspace_id": e.wsID, "name": "Research"})
	squadA := e.fx.Squad(t, "Squad A", leaderA, testutil.Cols{"floor_id": floor})
	squadB := e.fx.Squad(t, "Squad B", leaderB)
	e.fx.SquadMember(t, squadA, "agent", agentA)
	e.fx.SquadMember(t, squadB, "agent", agentB)

	e.restrictProduct()
	for _, a := range []string{agentA, agentB, outsider} {
		if e.canSee(e.agentActor(a), "product") {
			t.Fatalf("agent %s sees a restricted base with no grant", a)
		}
		if e.bound(a) {
			t.Fatalf("agent %s is bound to the restricted endpoint with no grant", a)
		}
	}
	if e.canSee(e.memberActor(false), "product") {
		t.Fatal("a member who is not an owner or admin sees a restricted base")
	}
	if !e.canSee(e.memberActor(true), "product") {
		t.Fatal("an owner or admin cannot see a restricted base")
	}
	if !e.canSee(e.agentActor(agentA), "company") {
		t.Fatal("a workspace-visible base is hidden from an agent")
	}

	// A squad grant reaches its members only.
	e.grant(KnowledgeSubjectSquad, squadA, KnowledgeAccessRead)
	if !e.canSee(e.agentActor(agentA), "product") || !e.bound(agentA) {
		t.Fatal("a squad's agent cannot read the base its squad was granted")
	}
	if e.canSee(e.agentActor(agentB), "product") || e.bound(agentB) {
		t.Fatal("an agent of another squad reads a base it was not granted")
	}

	// A floor grant reaches the squads on that floor. Squad B is not on it.
	e.grant(KnowledgeSubjectFloor, floor, KnowledgeAccessRead)
	if e.canSee(e.agentActor(agentB), "product") {
		t.Fatal("a floor grant reached a squad that is not on the floor")
	}

	// The restricted base is never in company-read.
	restrictedID := e.productWeKnoraID()
	for _, id := range e.companyReadKBIDs() {
		if id == restrictedID {
			t.Fatal("a restricted base is served by company-read")
		}
	}

	// Automatic issue context never reaches a restricted base, even for an
	// agent that has a grant.
	res, err := e.svc.SearchKindFor(ctx, e.ws, KnowledgeActor{Type: KnowledgeActorAgent}, "anything", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Hits {
		if h.KnowledgeBase == "product" {
			t.Fatal("automatic context returned a restricted base")
		}
	}
}

// TestGrantAccessIsEnforcedOnWrites: a read grant lets an agent read but not
// write; a write grant lets it write a draft; a member who is not a curator
// cannot write to a restricted base.
func TestGrantAccessIsEnforcedOnWrites(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	leader := e.fx.Agent(t, "leader", "")
	agent := e.fx.Agent(t, "agent", "")
	squad := e.fx.Squad(t, "Squad", leader)
	e.fx.SquadMember(t, squad, "agent", agent)
	e.restrictProduct()
	in := KnowledgeDocumentInput{KB: "product", Kind: "prd", Title: "Plan", Content: prdContent}

	e.grant(KnowledgeSubjectSquad, squad, KnowledgeAccessRead)
	if _, _, err := e.create(e.agentActor(agent), in); !errors.Is(err, ErrKnowledgeForbidden) {
		t.Fatalf("a read grant wrote a document: %v", err)
	}
	e.grant(KnowledgeSubjectSquad, squad, KnowledgeAccessWrite)
	if _, _, err := e.create(e.agentActor(agent), in); err != nil {
		t.Fatalf("a write grant could not write a draft: %v", err)
	}
	if _, _, err := e.create(e.memberActor(false), in); !errors.Is(err, ErrKnowledgeForbidden) {
		t.Fatalf("a non-curator member wrote to a restricted base: %v", err)
	}
	if _, _, err := e.create(e.memberActor(true), in); err != nil {
		t.Fatalf("a curator could not write to a restricted base: %v", err)
	}
}

// prdContent carries every heading the prd kind requires.
const prdContent = "# Plan\n\n## Problem\n\nx\n\n## Requirements\n\nx\n\n## Non-goals\n\nx\n\n## Acceptance criteria\n\nx\n"

// TestRemovedMemberLosesRestrictedAccess: removing an agent from the squad its
// grant reached removes the endpoint binding and the read at once.
func TestRemovedMemberLosesRestrictedAccess(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	leader := e.fx.Agent(t, "leader", "")
	agent := e.fx.Agent(t, "agent", "")
	squad := e.fx.Squad(t, "Squad", leader)
	e.fx.SquadMember(t, squad, "agent", agent)
	e.restrictProduct()
	e.grant(KnowledgeSubjectSquad, squad, KnowledgeAccessRead)
	if !e.bound(agent) {
		t.Fatal("setup: the member is not bound")
	}

	e.pool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1 AND member_id = $2`, squad, agent)
	if err := e.svc.RecomputeAgentKnowledgeAccess(context.Background(), e.ws, mustUUID(t, agent)); err != nil {
		t.Fatalf("RecomputeAgentKnowledgeAccess: %v", err)
	}
	if e.bound(agent) || e.canSee(e.agentActor(agent), "product") {
		t.Fatal("a removed member kept access to the restricted base")
	}
}

// TestOpeningABaseRevokesItsEndpoint: making a restricted base workspace-visible
// again takes its endpoint out of the library, unbinds every agent, rotates the
// WeKnora token and puts the base back in company-read.
func TestOpeningABaseRevokesItsEndpoint(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	leader := e.fx.Agent(t, "leader", "")
	agent := e.fx.Agent(t, "agent", "")
	squad := e.fx.Squad(t, "Squad", leader)
	e.fx.SquadMember(t, squad, "agent", agent)
	e.restrictProduct()
	e.grant(KnowledgeSubjectSquad, squad, KnowledgeAccessRead)

	rotations := e.fake.Calls(knowledge.OpRotateMCPToken)
	if _, err := e.svc.SetKnowledgeVisibility(context.Background(), e.ws, e.user, "product", KnowledgeVisibilityWorkspace); err != nil {
		t.Fatalf("open product: %v", err)
	}
	if e.bound(agent) {
		t.Fatal("an agent stayed bound to the revoked endpoint")
	}
	if _, err := e.q.GetWorkspaceMcpServerByName(context.Background(), db.GetWorkspaceMcpServerByNameParams{WorkspaceID: e.ws, Name: knowledgeRestrictedEndpointName("product")}); err == nil {
		t.Fatal("the restricted library entry survived")
	}
	if e.fake.Calls(knowledge.OpRotateMCPToken) <= rotations {
		t.Fatal("the revoked endpoint's token was not rotated")
	}
	found := false
	for _, id := range e.companyReadKBIDs() {
		found = found || id == e.productWeKnoraID()
	}
	if !found {
		t.Fatal("the base is not back in company-read")
	}
	if !e.canSee(e.agentActor(agent), "product") {
		t.Fatal("a workspace-visible base is hidden from an agent")
	}
}

// TestKnowledgeUploadRules covers the upload path of spec 3.3: the size limit,
// the kind being required, the curator's publish rule, and agents being refused.
func TestKnowledgeUploadRules(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	e.svc.UploadMaxBytes = 16
	ctx := context.Background()
	member := e.memberActor(false)
	upload := func(in KnowledgeUploadInput, actor KnowledgeActor) (KnowledgeDocumentView, bool, error) {
		if in.Content == nil {
			in.Content = strings.NewReader("")
		}
		return e.svc.UploadDocument(ctx, e.ws, actor, in)
	}

	// The limit is checked against the declared size, before anything is sent.
	_, _, err := upload(KnowledgeUploadInput{KB: "market", Kind: "market_report", Name: "big.md", Size: 17}, member)
	var tooLarge *KnowledgeTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Max != 16 {
		t.Fatalf("an oversized file err = %v, want KnowledgeTooLargeError at 16 bytes", err)
	}
	if n := e.fake.Calls(knowledge.OpCreateFile); n != 0 {
		t.Fatalf("an oversized file reached WeKnora %d times", n)
	}

	for _, in := range []KnowledgeUploadInput{
		{KB: "market", Name: "a.md", Size: 1},
		{KB: "market", Kind: "Bad Kind", Name: "a.md", Size: 1},
		{KB: "", Kind: "market_report", Name: "a.md", Size: 1},
	} {
		if _, _, err := upload(in, member); err == nil {
			t.Fatalf("an upload without a valid kind or base was accepted: %+v", in)
		}
	}

	// A member's upload is published at once and is searchable.
	// The limit above is 16 bytes for the size check; a document with four headings needs more.
	e.svc.UploadMaxBytes = 1 << 20
	pilot := "# Pilot\n\n## Summary\n\npilot\n\n## Market size\n\nx\n\n## Trends\n\nx\n\n## Sources\n\nx\n"
	view, created, err := upload(KnowledgeUploadInput{KB: "market", Kind: "market_report", Name: "report.md", Size: int64(len(pilot)), Content: strings.NewReader(pilot)}, member)
	if err != nil || !created || view.State != KnowledgeLinkPublished {
		t.Fatalf("member upload = %+v, created=%v, err=%v", view, created, err)
	}
	if r, err := e.svc.Search(ctx, e.ws, "pilot", "", 0); err != nil || len(r.Hits) != 1 {
		t.Fatalf("an uploaded file is not searchable: %+v, %v", r, err)
	}

	// Agents write drafts; they do not upload files.
	if _, _, err := upload(KnowledgeUploadInput{KB: "market", Kind: "market_report", Name: "x.md", Size: 1, Content: strings.NewReader("x")}, e.agentActor(e.fx.Agent(t, "agent", ""))); !errors.Is(err, ErrKnowledgeForbidden) {
		t.Fatalf("an agent uploaded a file: %v", err)
	}

	// The same idempotency key uploads once.
	twice := "# Twice\n\n## Summary\n\nx\n\n## Market size\n\nx\n\n## Trends\n\nx\n\n## Sources\n\nx\n"
	again, created, err := upload(KnowledgeUploadInput{KB: "market", Kind: "market_report", Name: "twice.md", Size: int64(len(twice)), Content: strings.NewReader(twice), IdempotencyKey: "k-1"}, member)
	if err != nil || !created {
		t.Fatalf("first keyed upload: %v", err)
	}
	dup, created, err := upload(KnowledgeUploadInput{KB: "market", Kind: "market_report", Name: "twice.md", Size: int64(len(twice)), Content: strings.NewReader(twice), IdempotencyKey: "k-1"}, member)
	if err != nil || created || dup.ID != again.ID {
		t.Fatalf("a retried upload = %+v, created=%v, err=%v", dup, created, err)
	}
}

// TestTenantRemovalWaitsForTheHoldAndIsRecoverable: the queued tenant is kept
// until its hold has passed, a purge can be cancelled during the hold, and a
// failed removal is retried.
func TestTenantRemovalWaitsForTheHoldAndIsRecoverable(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	ctx := context.Background()
	tenants := len(e.fake.Tenants())

	if err := QueueKnowledgeTeardown(ctx, e.q, e.ws, time.Now()); err != nil {
		t.Fatal(err)
	}
	row, err := e.q.GetKnowledgeDeprovision(ctx, e.ws)
	if err != nil {
		t.Fatalf("no tenant was queued: %v", err)
	}
	if row.State != "held" || time.Until(row.HoldUntil.Time) < KnowledgeTenantHold-time.Minute {
		t.Fatalf("queued row = %+v; the hold must be seven days", row)
	}

	// Before the hold, nothing is removed.
	if n, err := e.svc.DeprovisionDue(ctx); err != nil || n != 0 {
		t.Fatalf("DeprovisionDue before the hold = %d, %v", n, err)
	}
	if len(e.fake.Tenants()) != tenants {
		t.Fatal("the tenant was removed before its hold passed")
	}

	// Recoverable: a purge cancelled during the hold keeps the tenant.
	if err := e.svc.CancelKnowledgeDeprovision(ctx, e.ws); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := e.svc.CancelKnowledgeDeprovision(ctx, e.ws); !errors.Is(err, ErrKnowledgeDeprovisionNotHeld) {
		t.Fatalf("a second cancel = %v", err)
	}
	if err := QueueKnowledgeTeardown(ctx, e.q, e.ws, time.Now().Add(-KnowledgeTenantHold-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// The hold has passed, but WeKnora is down: the removal fails and waits.
	e.pool.Exec(ctx, `UPDATE knowledge_tenant_deprovision SET hold_until = now() - interval '1 minute' WHERE workspace_id = $1`, e.wsID)
	e.fake.FailAlways(knowledge.OpDeleteTenant, knowledge.ErrUnavailable)
	if n, err := e.svc.DeprovisionDue(ctx); err != nil || n != 0 {
		t.Fatalf("a failing removal = %d, %v", n, err)
	}
	row, _ = e.q.GetKnowledgeDeprovision(ctx, e.ws)
	if row.State != "held" || row.Attempts != 1 || row.LastError == "" || !row.NextAttemptAt.Time.After(time.Now()) {
		t.Fatalf("a failed removal must wait for a retry: %+v", row)
	}

	// Once WeKnora answers and the retry is due, the tenant goes.
	e.fake.ClearFailures()
	e.pool.Exec(ctx, `UPDATE knowledge_tenant_deprovision SET next_attempt_at = now() WHERE workspace_id = $1`, e.wsID)
	if n, err := e.svc.DeprovisionDue(ctx); err != nil || n != 1 {
		t.Fatalf("DeprovisionDue after the hold = %d, %v", n, err)
	}
	if len(e.fake.Tenants()) != tenants-1 {
		t.Fatal("the tenant was not removed after the hold")
	}
	row, _ = e.q.GetKnowledgeDeprovision(ctx, e.ws)
	if row.State != "done" {
		t.Fatalf("state = %q, want done", row.State)
	}
}

// TestTenantQueueFollowsTheDeleteTransaction: a rolled-back workspace delete
// queues nothing; a committed one queues the tenant.
func TestTenantQueueFollowsTheDeleteTransaction(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := QueueKnowledgeTeardown(ctx, e.q.WithTx(tx), e.ws, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.GetKnowledgeDeprovision(ctx, e.ws); err == nil {
		t.Fatal("a rolled-back delete queued a tenant")
	}
}

// TestModelsStepRegistersAndTestsEachModel: the configured chat and rerank
// models are registered, Wiki is on for the three default bases only when a
// chat model exists, and a failing model is named with WeKnora's reason.
func TestModelsStepRegistersAndTestsEachModel(t *testing.T) {
	e := newPlatformEnv(t)
	e.svc.LLM = &KnowledgeLLM{BaseURL: "https://llm.example.test/v1", ChatModel: "summary-chat", RerankModel: "rerank-model"}
	e.enabled()
	ctx := context.Background()

	company, _ := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e.knowledgeEnv, "company"))
	decisions, _ := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e.knowledgeEnv, "decisions"))
	ventures, _ := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e.knowledgeEnv, KnowledgeVentureBaseKey))
	market, _ := e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e.knowledgeEnv, "market"))
	if !company.WikiEnabled || !decisions.WikiEnabled || !ventures.WikiEnabled || market.WikiEnabled {
		t.Fatalf("wiki defaults: company=%v decisions=%v ventures=%v market=%v",
			company.WikiEnabled, decisions.WikiEnabled, ventures.WikiEnabled, market.WikiEnabled)
	}
	if !company.SummariesEnabled || !market.SummariesEnabled {
		t.Fatal("document summaries are not on for the bases")
	}

	e.fake.FailModelTest("rerank-model", errors.New("upstream returned 404: model not found"))
	report, err := e.svc.TestModels(ctx, e.ws)
	if err != nil || !report.Configured || len(report.Checks) != 2 {
		t.Fatalf("TestModels = %+v, %v", report, err)
	}
	chat, rerank := report.Checks[0], report.Checks[1]
	if chat.Role != KnowledgeModelChat || !chat.OK || chat.Error != "" {
		t.Fatalf("chat check = %+v", chat)
	}
	if rerank.Role != KnowledgeModelRerank || rerank.OK || !strings.Contains(rerank.Error, "model not found") {
		t.Fatalf("rerank check must name the failure: %+v", rerank)
	}

	// Wiki is limited to three bases, and the three defaults already have it.
	on, off := true, false
	if _, err := e.svc.UpdateBaseSettings(ctx, e.ws, "market", KnowledgeBaseSettingsPatch{Wiki: &on}); err == nil {
		t.Fatal("a fourth base got Wiki")
	}
	if _, err := e.svc.UpdateBaseSettings(ctx, e.ws, KnowledgeVentureBaseKey, KnowledgeBaseSettingsPatch{Wiki: &off}); err != nil {
		t.Fatalf("turning Wiki off: %v", err)
	}
	if _, err := e.svc.UpdateBaseSettings(ctx, e.ws, "market", KnowledgeBaseSettingsPatch{Wiki: &on}); err != nil {
		t.Fatalf("a base after one is turned off: %v", err)
	}
	if _, err := e.svc.UpdateBaseSettings(ctx, e.ws, "product", KnowledgeBaseSettingsPatch{Wiki: &on}); err == nil {
		t.Fatal("a fourth base got Wiki after the switch")
	}
	// An admin's choice survives a later enable.
	if _, err := e.enable(); err != nil {
		t.Fatalf("second enable: %v", err)
	}
	market, _ = e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e.knowledgeEnv, "market"))
	ventures, _ = e.q.GetKnowledgeBaseByKey(ctx, dbBaseKey(e.knowledgeEnv, KnowledgeVentureBaseKey))
	if !market.WikiEnabled || ventures.WikiEnabled {
		t.Fatalf("a later enable undid an admin's choice: market=%v ventures=%v", market.WikiEnabled, ventures.WikiEnabled)
	}
}

// TestModelsStepIsDegradable: a model that will not register leaves knowledge
// ready, and no LLM means no model and no Wiki.
func TestModelsStepIsDegradable(t *testing.T) {
	t.Run("a model that will not register", func(t *testing.T) {
		e := newPlatformEnv(t)
		e.svc.LLM = &KnowledgeLLM{BaseURL: "https://llm.example.test/v1", ChatModel: "summary-chat"}
		e.fake.FailAlways(knowledge.OpEnsureModelOfType, errors.New("upstream unreachable"))
		status, err := e.enable()
		if err != nil || status.State != KnowledgeStateReady {
			t.Fatalf("a failing model made enable fail: %+v, %v", status, err)
		}
	})
	t.Run("no LLM configured", func(t *testing.T) {
		bare := newPlatformEnv(t)
		bare.enabled()
		report, err := bare.svc.TestModels(context.Background(), bare.ws)
		if err != nil || report.Configured || len(report.Checks) != 0 {
			t.Fatalf("no LLM configured: %+v, %v", report, err)
		}
		on := true
		if _, err := bare.svc.UpdateBaseSettings(context.Background(), bare.ws, "company", KnowledgeBaseSettingsPatch{Wiki: &on}); !errors.Is(err, ErrKnowledgeLLMNotConfigured) {
			t.Fatalf("Wiki without a chat model = %v", err)
		}
	})
}

// TestOwnerLoginIsShownOnce: enable gives the owner a WeKnora login that is an
// admin of the tenant; the password is revealed once, then refused.
func TestOwnerLoginIsShownOnce(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	ctx := context.Background()

	login, err := e.svc.RevealOwnerPassword(ctx, e.ws, e.user)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if login.Password == "" || login.Email == "" {
		t.Fatalf("login = %+v", login)
	}
	if got := e.fake.UserPassword(login.Email); got != login.Password {
		t.Fatal("the revealed password is not the one WeKnora was given")
	}
	tenants := e.fake.Tenants()
	if role := e.fake.Members(tenants[0].ID)[login.Email]; role != "admin" {
		t.Fatalf("owner role in the tenant = %q", role)
	}
	if _, err := e.svc.RevealOwnerPassword(ctx, e.ws, e.user); !errors.Is(err, ErrKnowledgeOwnerPasswordShown) {
		t.Fatalf("a second reveal = %v", err)
	}
	// A retried enable does not create a second login or change the password.
	if _, err := e.enable(); err != nil {
		t.Fatalf("second enable: %v", err)
	}
	if got := e.fake.UserPassword(login.Email); got != login.Password {
		t.Fatal("a retried enable changed the owner's password")
	}
}

// TestOwnerLoginFailureDoesNotBreakKnowledge: when WeKnora refuses the login,
// knowledge is still ready; the owner is told to enable again.
func TestOwnerLoginFailureDoesNotBreakKnowledge(t *testing.T) {
	e := newPlatformEnv(t)
	e.fake.FailAlways(knowledge.OpEnsureUser, knowledge.ErrForbidden)
	status, err := e.enable()
	if err != nil || status.State != KnowledgeStateReady {
		t.Fatalf("a refused login broke enable: %+v, %v", status, err)
	}
	if _, err := e.svc.RevealOwnerPassword(context.Background(), e.ws, e.user); !errors.Is(err, ErrKnowledgeOwnerLoginMissing) {
		t.Fatalf("reveal without a login = %v", err)
	}
	e.fake.ClearFailures()
	if _, err := e.enable(); err != nil {
		t.Fatalf("enable again: %v", err)
	}
	if _, err := e.svc.RevealOwnerPassword(context.Background(), e.ws, e.user); err != nil {
		t.Fatalf("reveal after enabling again: %v", err)
	}
}

// TestFloorGrantFollowsTheSquadsSeat: an agent reached through its squad's floor
// loses the endpoint when the squad leaves the floor, and regains it on return.
func TestFloorGrantFollowsTheSquadsSeat(t *testing.T) {
	e := newPlatformEnv(t)
	e.enabled()
	ctx := context.Background()
	leader := e.fx.Agent(t, "leader", "")
	agent := e.fx.Agent(t, "agent", "")
	floor := e.fx.Insert(t, "office_floor", testutil.Cols{"workspace_id": e.wsID, "name": "Product"})
	squad := e.fx.Squad(t, "Squad", leader, testutil.Cols{"floor_id": floor})
	e.fx.SquadMember(t, squad, "agent", agent)
	e.restrictProduct()
	e.grant(KnowledgeSubjectFloor, floor, KnowledgeAccessRead)
	if !e.bound(agent) {
		t.Fatal("setup: a floor grant did not bind the squad's agent")
	}

	// The squad leaves the floor, then the handler recomputes its agents.
	e.pool.Exec(ctx, `UPDATE squad SET floor_id = NULL WHERE id = $1`, squad)
	if err := e.svc.RecomputeAgentKnowledgeAccess(ctx, e.ws, mustUUID(t, agent)); err != nil {
		t.Fatal(err)
	}
	if e.bound(agent) {
		t.Fatal("an agent kept a floor's access after its squad left the floor")
	}

	e.pool.Exec(ctx, `UPDATE squad SET floor_id = $2 WHERE id = $1`, squad, floor)
	if err := e.svc.RecomputeAgentKnowledgeAccess(ctx, e.ws, mustUUID(t, agent)); err != nil {
		t.Fatal(err)
	}
	if !e.bound(agent) {
		t.Fatal("an agent did not regain a floor's access when its squad returned")
	}
}
