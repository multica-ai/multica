package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// tokenFeed is a workspace with a metric token minted by its owner.
type tokenFeed struct {
	ws    companyWorkspace
	token string
	id    string
}

func newTokenFeed(t *testing.T) tokenFeed {
	t.Helper()
	ws := newCompanyWorkspace(t)
	var out CreateMetricTokenResponse
	testutil.Call(t, testHandler.CreateMetricWebhookToken,
		companyReq(ws.Owner, ws.ID, http.MethodPost, "/api/metric-tokens", map[string]any{"name": "Signups feed"})).
		Want(http.StatusCreated).JSON(&out)
	if len(out.Token) < 4 || out.Token[:4] != "mwt_" {
		t.Fatalf("minted token %q does not carry the mwt_ prefix", out.Token)
	}
	return tokenFeed{ws: ws, token: out.Token, id: out.ID}
}

// reportWithToken posts a reading to the webhook route with the given token.
func reportWithToken(token, key string, body map[string]any, extra ...string) *http.Request {
	req := newRequest(http.MethodPost, "/api/webhooks/metrics/"+key+"/readings", body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	return withURLParam(req, "key", key)
}

func countReadingsIn(t *testing.T, wsID, key string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM metric_reading WHERE workspace_id = $1 AND metric_key = $2`, wsID, key).Scan(&n); err != nil {
		t.Fatalf("count readings: %v", err)
	}
	return n
}

// TestMetricTokensAreMintedByOwnersAndAdminsOnly: a member cannot mint or list
// tokens; an owner can.
func TestMetricTokensAreMintedByOwnersAndAdminsOnly(t *testing.T) {
	ws := newCompanyWorkspace(t)
	testutil.Call(t, testHandler.CreateMetricWebhookToken,
		companyReq(ws.Member, ws.ID, http.MethodPost, "/api/metric-tokens", map[string]any{"name": "Member feed"})).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ListMetricWebhookTokens,
		companyReq(ws.Member, ws.ID, http.MethodGet, "/api/metric-tokens", nil)).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CreateMetricWebhookToken,
		companyReq(ws.Owner, ws.ID, http.MethodPost, "/api/metric-tokens", map[string]any{"name": "  "})).
		Want(http.StatusBadRequest)
}

// TestMetricTokenReportsLandInItsOwnWorkspace: a reading sent with a token is
// stored in the token's workspace, and a workspace header in the request does not
// move it.
func TestMetricTokenReportsLandInItsOwnWorkspace(t *testing.T) {
	feed := newTokenFeed(t)
	other := newCompanyWorkspace(t)
	const key = "signups.total"

	testutil.Call(t, testHandler.ReportMetricReadingByToken,
		reportWithToken(feed.token, key, map[string]any{"value": 12, "name": "Signups", "unit": "count"},
			"X-Workspace-ID", other.ID)).
		Want(http.StatusCreated)

	if n := countReadingsIn(t, feed.ws.ID, key); n != 1 {
		t.Fatalf("readings in the token's workspace = %d, want 1", n)
	}
	if n := countReadingsIn(t, other.ID, key); n != 0 {
		t.Fatalf("a token's reading landed in another workspace: %d", n)
	}
}

// TestMetricTokenRefusesAScopeOfAnotherWorkspace: a reading whose scope names a
// squad of another workspace is refused, and nothing is written there.
func TestMetricTokenRefusesAScopeOfAnotherWorkspace(t *testing.T) {
	feed := newTokenFeed(t)

	// A squad in a second workspace, built with the testutil fixtures.
	base := testutil.New(testPool, "", "")
	owner := base.User(t, "Foreign owner", fmt.Sprintf("foreign-%d@example.com", time.Now().UnixNano()))
	foreignWS := base.Workspace(t, "Foreign", fmt.Sprintf("foreign-%d", time.Now().UnixNano()))
	base.Member(t, foreignWS, owner, "owner")
	fx := testutil.New(testPool, foreignWS, owner)
	foreignSquad := fx.Squad(t, "Foreign squad", fx.Agent(t, "Foreign lead", fx.Runtime(t, "Foreign runtime")))

	const key = "signups.scoped"
	testutil.Call(t, testHandler.ReportMetricReadingByToken,
		reportWithToken(feed.token, key, map[string]any{
			"value": 3, "name": "Scoped", "unit": "count", "scope_type": "squad", "scope_id": foreignSquad,
		})).
		Want(http.StatusBadRequest)

	if n := countReadingsIn(t, foreignWS, key); n != 0 {
		t.Fatalf("a refused scope wrote %d readings into the other workspace", n)
	}
	if n := countReadingsIn(t, feed.ws.ID, key); n != 0 {
		t.Fatalf("a refused reading was still written to the token's workspace: %d", n)
	}
}

// TestMetricTokenRefusesMissingWrongAndRevokedTokens: no token, a token that is
// not one of ours, and a revoked token all answer 401. Revoking is one owner's
// call, and it is refused to another workspace's owner.
func TestMetricTokenRefusesMissingWrongAndRevokedTokens(t *testing.T) {
	feed := newTokenFeed(t)
	const key = "signups.auth"
	body := map[string]any{"value": 1, "name": "Auth", "unit": "count"}

	testutil.Call(t, testHandler.ReportMetricReadingByToken, reportWithToken("", key, body)).Want(http.StatusUnauthorized)
	testutil.Call(t, testHandler.ReportMetricReadingByToken, reportWithToken("mwt_not-a-real-token", key, body)).Want(http.StatusUnauthorized)

	// Another workspace's owner cannot revoke this token.
	outsider := newCompanyWorkspace(t)
	testutil.Call(t, testHandler.RevokeMetricWebhookToken,
		withURLParam(companyReq(outsider.Owner, outsider.ID, http.MethodDelete, "/api/metric-tokens/"+feed.id, nil), "id", feed.id)).
		Want(http.StatusNotFound)

	testutil.Call(t, testHandler.RevokeMetricWebhookToken,
		withURLParam(companyReq(feed.ws.Owner, feed.ws.ID, http.MethodDelete, "/api/metric-tokens/"+feed.id, nil), "id", feed.id)).
		Want(http.StatusOK)
	testutil.Call(t, testHandler.ReportMetricReadingByToken, reportWithToken(feed.token, key, body)).Want(http.StatusUnauthorized)

	var listed struct {
		Tokens []MetricTokenResponse `json:"tokens"`
	}
	testutil.Call(t, testHandler.ListMetricWebhookTokens,
		companyReq(feed.ws.Owner, feed.ws.ID, http.MethodGet, "/api/metric-tokens", nil)).
		Want(http.StatusOK).JSON(&listed)
	if len(listed.Tokens) != 1 || listed.Tokens[0].RevokedAt == nil {
		t.Fatalf("the list after a revoke = %+v, want the token kept with revoked_at set", listed.Tokens)
	}
}

// TestMetricTokenReportsAreRateLimitedPerToken: a token has its own budget, which
// a second token does not share.
func TestMetricTokenReportsAreRateLimitedPerToken(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()
	tokenA := uuid.NewString()
	for i := 0; i < metricTokenReportsPerMinute; i++ {
		if !h.allowMetricTokenReport(ctx, tokenA) {
			t.Fatalf("report %d refused inside the token budget", i+1)
		}
	}
	if h.allowMetricTokenReport(ctx, tokenA) {
		t.Fatalf("report %d allowed past the token budget", metricTokenReportsPerMinute+1)
	}
	if !h.allowMetricTokenReport(ctx, uuid.NewString()) {
		t.Fatalf("a second token shares the first token's budget")
	}
}
