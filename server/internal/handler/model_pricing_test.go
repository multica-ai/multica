package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/pricing"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestModelPricingWorkspaceSharingAndPermissions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "Price test", "price-sharing-test")
	dbfx.Member(t, ws, testUserID, "owner")
	colleague := dbfx.User(t, "Price reader", "price-reader@example.test")
	dbfx.Member(t, ws, colleague, "member")
	other := dbfx.Workspace(t, "Other price test", "other-price-test")
	dbfx.Member(t, other, testUserID, "owner")
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM workspace_model_pricing WHERE workspace_id = $1", ws) })
	request := func(method, workspace, user string, body any) *http.Request {
		req := withURLParam(testutil.JSONRequest(method, "/api/workspaces/"+workspace+"/model-pricing", body), "id", workspace)
		return testutil.WithHeaders(req, "X-User-ID", user, "X-Workspace-ID", workspace)
	}
	var initial modelPricingSnapshot
	testutil.Call(t, testHandler.GetModelPricing, request("GET", ws, testUserID, nil)).Want(200).JSON(&initial)
	if !initial.CanManage || initial.Revision != 0 {
		t.Fatalf("initial: %+v", initial)
	}
	body := map[string]any{"revision": 0, "overrides": map[string]modelPricingRow{"kimi-k3": {Input: 7, Output: 9, CacheRead: 1, CacheWrite: 7}}}
	var saved modelPricingSnapshot
	testutil.Call(t, testHandler.SaveModelPricing, request("PUT", ws, testUserID, body)).Want(200).JSON(&saved)
	if saved.Revision != 1 {
		t.Fatal("revision did not advance")
	}
	var shared modelPricingSnapshot
	testutil.Call(t, testHandler.GetModelPricing, request("GET", ws, colleague, nil)).Want(200).JSON(&shared)
	if shared.CanManage || shared.Overrides["kimi-k3"].Input != 7 {
		t.Fatal("colleague did not receive shared read-only prices")
	}
	testutil.Call(t, testHandler.SaveModelPricing, request("PUT", ws, colleague, body)).Want(403)
	testutil.Call(t, testHandler.RefreshModelPricing, request("POST", ws, colleague, nil)).Want(403)
	testutil.Call(t, testHandler.SaveModelPricing, request("PUT", ws, testUserID, body)).Want(409)
	var separate modelPricingSnapshot
	testutil.Call(t, testHandler.GetModelPricing, request("GET", other, testUserID, nil)).Want(200).JSON(&separate)
	if len(separate.Overrides) != 0 {
		t.Fatal("override leaked to another workspace")
	}
	testutil.Call(t, testHandler.GetModelPricing, request("GET", other, colleague, nil)).Want(404)
	testutil.Call(t, testHandler.SaveModelPricing, request("PUT", ws, testUserID, `{"revision":1,"overrides":{"x":{"input":null,"output":0,"cache_read":0,"cache_write":0}}}`)).Want(400)
	testutil.Call(t, testHandler.SaveModelPricing, request("PUT", ws, testUserID, map[string]any{"revision": 1, "overrides": map[string]modelPricingRow{}})).Want(200)
	deleteRequest := withURLParam(testutil.JSONRequest(http.MethodDelete, "/api/workspaces/"+ws, nil), "id", ws)
	deleteRequest = testutil.WithHeaders(deleteRequest, "X-User-ID", testUserID, "X-Workspace-ID", ws)
	testutil.Call(t, testHandler.DeleteWorkspace, deleteRequest).Want(http.StatusNoContent)
	if _, err := testHandler.Queries.GetWorkspaceModelPricing(deleteRequest.Context(), parseUUID(ws)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("workspace teardown retained pricing overrides: %v", err)
	}
}

func TestModelPricingHTTPContract(t *testing.T) {
	response := modelPricingResponse(pricing.Snapshot{
		Catalog: pricing.Catalog{Rows: map[string]pricing.Row{"example": {
			Input: 1, Output: 2, CacheRead: 0.25, CacheWrite: 1.25, SourceURL: "https://example.test/prices",
		}}},
	})
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Rows map[string]map[string]json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cache_read", "cache_write", "source_url"} {
		if _, exists := wire.Rows["example"][key]; !exists {
			t.Fatalf("HTTP response omitted %s: %s", key, payload)
		}
	}
	for _, key := range []string{"cacheRead", "cacheWrite", "sourceUrl"} {
		if _, exists := wire.Rows["example"][key]; exists {
			t.Fatalf("HTTP response exposed document field %s", key)
		}
	}
	for _, malformed := range []string{
		`{"input":1,"output":2,"cacheRead":0,"cacheWrite":0}`,
		`{"input":1,"output":2,"cache_read":null,"cache_write":0}`,
		`{"input":1,"output":2,"cache_read":-1,"cache_write":0}`,
	} {
		var row modelPricingRow
		if err := json.Unmarshal([]byte(malformed), &row); err == nil {
			t.Fatalf("accepted malformed HTTP price: %s", malformed)
		}
	}
}
