package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// Security review of the governance surface (spec 4.9). Each test pins one
// point of the review: an agent cannot edit the policy engine, the governance
// writes and the import are size-capped, the metric webhook is rate-limited and
// workspace-bound, and the CSV export is row-capped with the cap named.

const securityUnknownID = "00000000-0000-4000-8000-0000000000aa"

// oversizedBody is a JSON object whose encoding is larger than limit bytes.
func oversizedBody(limit int) map[string]any {
	return map[string]any{"name": strings.Repeat("a", limit)}
}

// TestGovernanceWritesRefuseBodiesOverTheirCap: every governance write that
// reads a JSON body answers 413 for a body over its cap. The caller is the owner,
// so the cap is the only thing refusing the request.
func TestGovernanceWritesRefuseBodiesOverTheirCap(t *testing.T) {
	fx := newCompanyWorkspace(t)
	big := oversizedBody(int(governanceJSONBodyLimit))
	cases := []struct {
		name    string
		method  string
		path    string
		params  []string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"metric reading", http.MethodPost, "/api/metrics/security.cap/readings", []string{"key", "security.cap"}, testHandler.ReportMetricReading},
		{"goal create", http.MethodPost, "/api/goals", nil, testHandler.CreateGoal},
		{"goal update", http.MethodPatch, "/api/goals/" + securityUnknownID, []string{"id", securityUnknownID}, testHandler.UpdateGoal},
		{"goal status", http.MethodPost, "/api/goals/" + securityUnknownID + "/status", []string{"id", securityUnknownID}, testHandler.SetGoalStatus},
		{"budget create", http.MethodPost, "/api/budgets", nil, testHandler.CreateBudget},
		{"budget update", http.MethodPatch, "/api/budgets/" + securityUnknownID, []string{"id", securityUnknownID}, testHandler.UpdateBudget},
		{"budget extend", http.MethodPost, "/api/budgets/" + securityUnknownID + "/extend", []string{"id", securityUnknownID}, testHandler.ExtendBudget},
		{"standing approval create", http.MethodPost, "/api/approval-policies", nil, testHandler.CreateApprovalPolicy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := companyReq(fx.Owner, fx.ID, c.method, c.path, big)
			if c.params != nil {
				req = testutil.WithURLParams(req, c.params...)
			}
			testutil.Call(t, c.handler, req).Want(http.StatusRequestEntityTooLarge)
		})
	}
}

// TestCompanyImportRefusesBodiesOverItsCap: the import is the one endpoint that
// takes a whole company document, so its cap is the largest.
func TestCompanyImportRefusesBodiesOverItsCap(t *testing.T) {
	fx := newCompanyWorkspace(t)
	req := companyReq(fx.Owner, fx.ID, http.MethodPost, "/api/company/import", oversizedBody(int(companyImportBodyLimit)))
	testutil.Call(t, testHandler.ImportCompany, req).Want(http.StatusRequestEntityTooLarge)
}

// TestAgentActorsCannotCreateOrRevokeApprovalPolicies: the policy engine is
// edited by the boss and admins only. An agent acting for a workspace member,
// as a task stamps it, is refused on both writes, and the policy is untouched.
// The agent proposes a standing approval instead; the policy comes from the
// boss's decision.
func TestAgentActorsCannotCreateOrRevokeApprovalPolicies(t *testing.T) {
	fx := newCompanyWorkspace(t)
	wsfx := testutil.New(testPool, fx.ID, fx.Owner)
	runtime := wsfx.Runtime(t, "policy security runtime")
	leader := wsfx.Agent(t, "policy security agent", runtime)
	task := wsfx.Task(t, leader, testutil.Cols{"runtime_id": runtime})

	var policy ApprovalPolicyResponse
	testutil.Call(t, testHandler.CreateApprovalPolicy,
		companyReq(fx.Owner, fx.ID, http.MethodPost, "/api/approval-policies", map[string]any{"name": "Boss-owned", "kind": "decision"})).
		Want(http.StatusCreated).JSON(&policy)

	createReq := testutil.WithHeaders(
		companyReq(fx.Owner, fx.ID, http.MethodPost, "/api/approval-policies", map[string]any{"name": "By an agent", "kind": "decision"}),
		"X-Agent-ID", leader, "X-Task-ID", task, "X-Actor-Source", "task_token")
	testutil.Call(t, testHandler.CreateApprovalPolicy, createReq).Want(http.StatusForbidden)

	revokePath := "/api/approval-policies/" + policy.ID + "/revoke"
	revokeReq := testutil.WithURLParams(testutil.WithHeaders(
		companyReq(fx.Owner, fx.ID, http.MethodPost, revokePath, nil),
		"X-Agent-ID", leader, "X-Task-ID", task, "X-Actor-Source", "task_token"), "id", policy.ID)
	testutil.Call(t, testHandler.RevokeApprovalPolicy, revokeReq).Want(http.StatusForbidden)

	var list ListApprovalPoliciesResponse
	testutil.Call(t, testHandler.ListApprovalPolicies, companyReq(fx.Member, fx.ID, http.MethodGet, "/api/approval-policies", nil)).
		Want(http.StatusOK).JSON(&list)
	if len(list.Policies) != 1 || !list.Policies[0].Active {
		t.Fatalf("after the agent's refused writes the policies are %+v, want the one active policy", list.Policies)
	}
}

// TestMetricReadingsAreRateLimitedBeforeAnyWrite: a webhook reader that has
// spent its per-metric budget gets 429 and nothing is recorded. The budget is
// spent through the same limiter the handler consults.
func TestMetricReadingsAreRateLimitedBeforeAnyWrite(t *testing.T) {
	fx := newCompanyWorkspace(t)
	ctx := context.Background()
	const key = "security.ratelimit"
	// The shared test handler runs a generous limiter; this test needs the
	// production budget, so it installs one with that budget for its duration.
	prev := testHandler.WebhookRateLimiter
	testHandler.WebhookRateLimiter = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: metricReportsPerMinute, Window: time.Minute})
	t.Cleanup(func() { testHandler.WebhookRateLimiter = prev })
	for i := 0; i < metricReportsPerMinute; i++ {
		if !testHandler.allowMetricReport(ctx, fx.ID, key) {
			t.Fatalf("report %d refused inside the budget", i+1)
		}
	}
	req := companyReq(fx.Owner, fx.ID, http.MethodPost, "/api/metrics/"+key+"/readings",
		map[string]any{"value": 1, "source": "webhook"})
	testutil.Call(t, testHandler.ReportMetricReading, testutil.WithURLParams(req, "key", key)).Want(http.StatusTooManyRequests)

	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM metric_reading WHERE workspace_id = $1 AND metric_key = $2`, fx.ID, key).Scan(&n); err != nil {
		t.Fatalf("count readings: %v", err)
	}
	if n != 0 {
		t.Fatalf("a refused report wrote %d readings", n)
	}
}

// TestMetricReadingsAreBoundToTheirWorkspace: a token that belongs to one
// workspace's member cannot write a reading into another workspace by naming it
// in X-Workspace-ID. The membership check refuses it and nothing is written.
func TestMetricReadingsAreBoundToTheirWorkspace(t *testing.T) {
	home := newCompanyWorkspace(t)
	other := newCompanyWorkspace(t)
	ctx := context.Background()
	const key = "security.crossws"

	req := companyReq(home.Member, other.ID, http.MethodPost, "/api/metrics/"+key+"/readings",
		map[string]any{"value": 1, "source": "webhook"})
	testutil.Call(t, testHandler.ReportMetricReading, testutil.WithURLParams(req, "key", key)).Want(http.StatusNotFound)

	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM metric_reading WHERE workspace_id = $1 AND metric_key = $2`, other.ID, key).Scan(&n); err != nil {
		t.Fatalf("count readings: %v", err)
	}
	if n != 0 {
		t.Fatalf("a reading from another workspace's member was written %d times", n)
	}
}

// TestGovernanceExportIsCappedAndNamesTheCap: an export over the row cap is
// refused with 413 before any CSV is sent, and a served export names the cap in
// X-Export-Row-Limit.
func TestGovernanceExportIsCappedAndNamesTheCap(t *testing.T) {
	fx := newCompanyWorkspace(t)
	ctx := context.Background()
	wsUUID, err := util.ParseUUID(fx.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range []string{"first entry", "second entry"} {
		if err := service.NewGovernanceLog().Record(ctx, testHandler.Queries, service.GovernanceEvent{
			WorkspaceID: wsUUID, Kind: service.GovernanceKindCompanyPaused,
			ActorType: service.GovernanceActorSystem, SubjectType: "workspace", Summary: summary,
		}); err != nil {
			t.Fatalf("record %q: %v", summary, err)
		}
	}
	prev := governanceExportRowLimit
	t.Cleanup(func() { governanceExportRowLimit = prev })

	governanceExportRowLimit = 1
	req := companyReq(fx.Member, fx.ID, http.MethodGet, "/api/governance/events.csv", nil)
	refused := testutil.Call(t, testHandler.ExportGovernanceEvents, req).Want(http.StatusRequestEntityTooLarge)
	if !strings.Contains(refused.Text(), "limited to 1 rows") {
		t.Fatalf("the refusal does not name the cap: %s", refused.Text())
	}
	if strings.Contains(refused.Text(), "entry") {
		t.Fatalf("a refused export sent rows: %s", refused.Text())
	}

	governanceExportRowLimit = 2
	served := testutil.Call(t, testHandler.ExportGovernanceEvents,
		companyReq(fx.Member, fx.ID, http.MethodGet, "/api/governance/events.csv", nil)).Want(http.StatusOK)
	if got := served.Header().Get("X-Export-Row-Limit"); got != "2" {
		t.Fatalf("X-Export-Row-Limit = %q, want 2", got)
	}
}
