package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runtimeModelConnectionRequest(
	actorID, method, runtimeID string,
	body any,
) *http.Request {
	req := newRequestAs(
		actorID,
		method,
		"/api/runtimes/"+runtimeID+"/model-connection",
		body,
	)
	return withURLParam(req, "runtimeId", runtimeID)
}

func TestRuntimeModelConnectionLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	cache := withModelListStores(t)
	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)
	if _, err := testPool.Exec(
		ctx,
		`UPDATE agent_runtime SET provider = 'pi' WHERE id = $1`,
		runtimeID,
	); err != nil {
		t.Fatalf("make fixture a Pi runtime: %v", err)
	}
	if err := cache.Put(ctx, runtimeID, []ModelEntry{{ID: "No/models", Label: "No/models"}}, true); err != nil {
		t.Fatalf("seed stale model cache: %v", err)
	}

	secret := "deepseek-secret-that-must-never-leak"
	body := map[string]any{
		"provider": " deepseek ",
		"api":      "openai-completions",
		"base_url": "https://api.deepseek.com/",
		"model":    " deepseek-v4-flash ",
		"api_key":  secret,
	}
	w := httptest.NewRecorder()
	testHandler.UpdateRuntimeModelConnection(
		w,
		runtimeModelConnectionRequest(
			runtimeOwnerID,
			http.MethodPut,
			runtimeID,
			body,
		),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT model connection: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("PUT response leaked the API key")
	}
	var saved RuntimeModelConnectionResponse
	if err := json.NewDecoder(w.Body).Decode(&saved); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if !saved.Configured || !saved.HasAPIKey {
		t.Fatalf("saved connection should be configured: %+v", saved)
	}
	if snapshot, err := cache.Get(ctx, runtimeID); err != nil || snapshot != nil {
		t.Fatalf("PUT should invalidate stale model catalog: snapshot=%v err=%v", snapshot, err)
	}
	if saved.Config.Provider != "deepseek" ||
		saved.Config.BaseURL != "https://api.deepseek.com/" ||
		saved.Config.Model != "deepseek-v4-flash" {
		t.Fatalf("saved config was not normalized: %+v", saved.Config)
	}

	var persistedKey string
	if err := testPool.QueryRow(
		ctx,
		`SELECT default_model_api_key FROM agent_runtime WHERE id = $1`,
		runtimeID,
	).Scan(&persistedKey); err != nil {
		t.Fatalf("read persisted API key: %v", err)
	}
	if persistedKey != secret {
		t.Fatalf("persisted API key = %q, want submitted secret", persistedKey)
	}
	var auditDetails []byte
	if err := testPool.QueryRow(
		ctx,
		`SELECT details FROM activity_log
		 WHERE workspace_id = $1 AND action = $2
		 ORDER BY created_at DESC LIMIT 1`,
		testWorkspaceID,
		runtimeModelConnectionUpdated,
	).Scan(&auditDetails); err != nil {
		t.Fatalf("read model connection audit: %v", err)
	}
	if strings.Contains(string(auditDetails), secret) {
		t.Fatal("model connection audit leaked the API key")
	}

	// Omitting api_key edits the non-secret route while preserving the secret.
	body["model"] = "deepseek-v4-pro"
	delete(body, "api_key")
	w = httptest.NewRecorder()
	testHandler.UpdateRuntimeModelConnection(
		w,
		runtimeModelConnectionRequest(
			runtimeOwnerID,
			http.MethodPut,
			runtimeID,
			body,
		),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT preserving key: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := testPool.QueryRow(
		ctx,
		`SELECT default_model_api_key FROM agent_runtime WHERE id = $1`,
		runtimeID,
	).Scan(&persistedKey); err != nil {
		t.Fatalf("read preserved API key: %v", err)
	}
	if persistedKey != secret {
		t.Fatalf("omitted key replaced saved secret with %q", persistedKey)
	}

	// Every workspace member may inspect the non-secret connection state.
	w = httptest.NewRecorder()
	testHandler.GetRuntimeModelConnection(
		w,
		runtimeModelConnectionRequest(
			plainMemberID,
			http.MethodGet,
			runtimeID,
			nil,
		),
	)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), secret) {
		t.Fatalf("GET non-secret state: status=%d body=%s", w.Code, w.Body.String())
	}

	// A plain member cannot mutate someone else's private runtime.
	w = httptest.NewRecorder()
	testHandler.UpdateRuntimeModelConnection(
		w,
		runtimeModelConnectionRequest(
			plainMemberID,
			http.MethodPut,
			runtimeID,
			body,
		),
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member PUT: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	if err := cache.Put(ctx, runtimeID, []ModelEntry{{ID: "deepseek/old", Label: "deepseek/old"}}, true); err != nil {
		t.Fatalf("reseed stale model cache: %v", err)
	}
	w = httptest.NewRecorder()
	testHandler.DeleteRuntimeModelConnection(
		w,
		runtimeModelConnectionRequest(
			runtimeOwnerID,
			http.MethodDelete,
			runtimeID,
			nil,
		),
	)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE model connection: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cleared RuntimeModelConnectionResponse
	if err := json.NewDecoder(w.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode DELETE response: %v", err)
	}
	if cleared.Configured || cleared.HasAPIKey {
		t.Fatalf("deleted connection still appears configured: %+v", cleared)
	}
	if snapshot, err := cache.Get(ctx, runtimeID); err != nil || snapshot != nil {
		t.Fatalf("DELETE should invalidate stale model catalog: snapshot=%v err=%v", snapshot, err)
	}
}

func TestRuntimeModelConnectionRejectsNonPiRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, _ := runtimeVisibilityFixture(t)
	w := httptest.NewRecorder()
	testHandler.UpdateRuntimeModelConnection(
		w,
		runtimeModelConnectionRequest(
			runtimeOwnerID,
			http.MethodPut,
			runtimeID,
			map[string]any{},
		),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-Pi PUT: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
