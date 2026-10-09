package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestProviderQuotaPoolManagementScopesOwnerAndAuditsTransitions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, fmt.Sprintf("quota-api-%d", time.Now().UnixNano()), nil)
	request := newRequest(http.MethodPost, "/api/provider-quota-pools", map[string]any{
		"name": fmt.Sprintf("vps-openai-%d", time.Now().UnixNano()), "provider_hint": "openai", "timezone": "America/Los_Angeles",
	})
	w := httptest.NewRecorder()
	testHandler.CreateProviderQuotaPool(w, request)
	if w.Code != http.StatusCreated {
		t.Fatalf("create pool: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode created pool: %v %s", err, w.Body.String())
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `DELETE FROM provider_quota_pool_agent WHERE pool_id = $1`, created.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM provider_quota_pool_event WHERE pool_id = $1`, created.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM provider_quota_pool WHERE id = $1`, created.ID)
	})
	poolPath := "/api/provider-quota-pools/" + created.ID
	memberID := createPermissionTestMember(t, fmt.Sprintf("quota-reader-%d@multica.test", time.Now().UnixNano()))
	w = httptest.NewRecorder()
	testHandler.GetProviderQuotaPool(w, withURLParam(newRequestAs(memberID, http.MethodGet, poolPath, nil), "poolId", created.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unrelated member read another user's pool: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.AssignAgentToProviderQuotaPool(w, withURLParams(newRequestAs(testUserID, http.MethodPut, poolPath+"/agents/"+agentID, nil),
		"poolId", created.ID, "agentId", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("assign agent: %d %s", w.Code, w.Body.String())
	}
	var mappedPoolID string
	if err := testPool.QueryRow(context.Background(), `SELECT pool_id::text FROM provider_quota_pool_agent WHERE agent_id = $1`, agentID).Scan(&mappedPoolID); err != nil {
		t.Fatalf("read agent pool membership: %v", err)
	}
	if mappedPoolID != created.ID {
		t.Fatalf("agent mapped to pool %s, want %s", mappedPoolID, created.ID)
	}
	w = httptest.NewRecorder()
	testHandler.SetProviderQuotaPoolProbeAgent(w, withURLParams(newRequestAs(memberID, http.MethodPut, poolPath+"/probe-agent/"+agentID, nil),
		"poolId", created.ID, "agentId", agentID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unrelated member selected another user's probe: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.SetProviderQuotaPoolProbeAgent(w, withURLParams(newRequestAs(testUserID, http.MethodPut, poolPath+"/probe-agent/"+agentID, nil),
		"poolId", created.ID, "agentId", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("select probe agent: %d %s", w.Code, w.Body.String())
	}
	selected, err := testHandler.Queries.GetProviderQuotaPool(context.Background(), util.MustParseUUID(created.ID))
	if err != nil || selected.ProbeAgentID != util.MustParseUUID(agentID) {
		t.Fatalf("selected probe agent = %+v, %v", selected.ProbeAgentID, err)
	}
	w = httptest.NewRecorder()
	testHandler.HoldProviderQuotaPool(w, withURLParam(newRequestAs(memberID, http.MethodPost, poolPath+"/hold", map[string]any{
		"reason": "unauthorized hold attempt",
	}), "poolId", created.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unrelated member held another user's pool: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.HoldProviderQuotaPool(w, withURLParam(newRequestAs(testUserID, http.MethodPost, poolPath+"/hold", map[string]any{
		"reset_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), "reason": "provider weekly limit",
	}), "poolId", created.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("manual hold: %d %s", w.Code, w.Body.String())
	}
	pool, err := testHandler.Queries.GetProviderQuotaPool(context.Background(), util.MustParseUUID(created.ID))
	if err != nil || pool.State != "held_exact" {
		t.Fatalf("held pool = %+v, %v", pool, err)
	}
	w = httptest.NewRecorder()
	testHandler.ReleaseProviderQuotaPool(w, withURLParam(newRequestAs(testUserID, http.MethodPost, poolPath+"/release", map[string]any{
		"reason": "operator confirmed provider access",
	}), "poolId", created.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("manual release: %d %s", w.Code, w.Body.String())
	}
	pool, err = testHandler.Queries.GetProviderQuotaPool(context.Background(), util.MustParseUUID(created.ID))
	if err != nil || pool.State != "open" {
		t.Fatalf("released pool = %+v, %v", pool, err)
	}
	w = httptest.NewRecorder()
	testHandler.ClearProviderQuotaPoolProbeAgent(w, withURLParam(newRequestAs(testUserID, http.MethodDelete, poolPath+"/probe-agent", nil),
		"poolId", created.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("clear probe agent: %d %s", w.Code, w.Body.String())
	}
	pool, err = testHandler.Queries.GetProviderQuotaPool(context.Background(), util.MustParseUUID(created.ID))
	if err != nil || pool.ProbeAgentID.Valid {
		t.Fatalf("cleared probe agent = %+v, %v", pool.ProbeAgentID, err)
	}
	w = httptest.NewRecorder()
	testHandler.SetProviderQuotaPoolProbeAgent(w, withURLParams(newRequestAs(testUserID, http.MethodPut, poolPath+"/probe-agent/"+agentID, nil),
		"poolId", created.ID, "agentId", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("reselect probe agent: %d %s", w.Code, w.Body.String())
	}
	var events int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM provider_quota_pool_event WHERE pool_id = $1`, created.ID).Scan(&events); err != nil || events != 6 {
		t.Fatalf("pool events = %d, %v; want assignment, probe selection, hold, release, clear, reselect", events, err)
	}
	w = httptest.NewRecorder()
	testHandler.RemoveAgentFromProviderQuotaPool(w, withURLParams(newRequestAs(testUserID, http.MethodDelete, poolPath+"/agents/"+agentID, nil),
		"poolId", created.ID, "agentId", agentID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove probe agent: %d %s", w.Code, w.Body.String())
	}
	pool, err = testHandler.Queries.GetProviderQuotaPool(context.Background(), util.MustParseUUID(created.ID))
	if err != nil || pool.ProbeAgentID.Valid {
		t.Fatalf("removed agent remained selected as probe: %+v, %v", pool.ProbeAgentID, err)
	}
}
