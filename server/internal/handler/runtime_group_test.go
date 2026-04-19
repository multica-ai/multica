package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateRuntimeGroup_RoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM runtime_group WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, "rt-grp-roundtrip")
	})

	body := map[string]any{
		"name":        "rt-grp-roundtrip",
		"description": "test",
		"runtime_ids": []string{testRuntimeID},
	}
	w := httptest.NewRecorder()
	testHandler.CreateRuntimeGroup(w, newRequest(http.MethodPost, "/api/runtime-groups", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateRuntimeGroup: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "rt-grp-roundtrip" {
		t.Fatalf("unexpected name: %v", resp["name"])
	}
	runtimes, _ := resp["runtimes"].([]any)
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}
}

func TestCreateRuntimeGroup_RejectsForeignRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	foreign := createRuntimeInOtherWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, foreign)
	})
	body := map[string]any{
		"name":        "foreign-grp",
		"runtime_ids": []string{foreign},
	}
	w := httptest.NewRecorder()
	testHandler.CreateRuntimeGroup(w, newRequest(http.MethodPost, "/api/runtime-groups", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-workspace runtime, got %d", w.Code)
	}
}

func TestSetRuntimeGroupOverride_RejectsNonMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rt2 := createSecondRuntimeInTestWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt2)
	})
	groupID := createRuntimeGroupWithMembers(t, "nonmem-grp", []string{testRuntimeID})

	body := map[string]any{
		"runtime_id": rt2,
		"ends_at":    time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	}
	w := httptest.NewRecorder()
	testHandler.SetRuntimeGroupOverride(w, newRequestWithParam(http.MethodPut, "/api/runtime-groups/"+groupID+"/override", body, "id", groupID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-member runtime, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetRuntimeGroupOverride_ReplacesExisting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rt2 := createSecondRuntimeInTestWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt2)
	})
	groupID := createRuntimeGroupWithMembers(t, "replace-grp", []string{testRuntimeID, rt2})

	setOverride := func(rid string) {
		body := map[string]any{
			"runtime_id": rid,
			"ends_at":    time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		}
		w := httptest.NewRecorder()
		testHandler.SetRuntimeGroupOverride(w, newRequestWithParam(http.MethodPut, "/api/runtime-groups/"+groupID+"/override", body, "id", groupID))
		if w.Code != http.StatusOK {
			t.Fatalf("SetRuntimeGroupOverride: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	setOverride(testRuntimeID)
	setOverride(rt2)

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM runtime_group_override WHERE group_id = $1 AND starts_at <= now() AND now() < ends_at`,
		groupID,
	).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 active override, got %d", count)
	}
	var activeRuntimeID string
	testPool.QueryRow(context.Background(),
		`SELECT runtime_id::text FROM runtime_group_override WHERE group_id = $1 AND starts_at <= now() AND now() < ends_at`,
		groupID,
	).Scan(&activeRuntimeID)
	if activeRuntimeID != rt2 {
		t.Fatalf("expected active runtime_id = rt2, got %s", activeRuntimeID)
	}
}

func TestRemovingMemberCascadesOverride(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rt2 := createSecondRuntimeInTestWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt2)
	})
	groupID := createRuntimeGroupWithMembers(t, "cascade-grp", []string{testRuntimeID, rt2})

	body := map[string]any{
		"runtime_id": rt2,
		"ends_at":    time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	}
	w := httptest.NewRecorder()
	testHandler.SetRuntimeGroupOverride(w, newRequestWithParam(http.MethodPut, "/api/runtime-groups/"+groupID+"/override", body, "id", groupID))
	if w.Code != http.StatusOK {
		t.Fatalf("SetOverride: expected 200, got %d", w.Code)
	}

	patch := map[string]any{"runtime_ids": []string{testRuntimeID}}
	w = httptest.NewRecorder()
	testHandler.UpdateRuntimeGroup(w, newRequestWithParam(http.MethodPatch, "/api/runtime-groups/"+groupID, patch, "id", groupID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateRuntimeGroup: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM runtime_group_override WHERE group_id = $1`, groupID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 override rows after member removal, got %d", count)
	}
}
