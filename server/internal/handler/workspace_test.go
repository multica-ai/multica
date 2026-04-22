package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	// Drive the test off the actual reservedSlugs map so the test can never
	// drift from the source of truth. New entries are covered automatically.
	reserved := make([]string, 0, len(reservedSlugs))
	for slug := range reservedSlugs {
		reserved = append(reserved, slug)
	}
	sort.Strings(reserved) // deterministic test order

	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			testHandler.CreateWorkspace(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdateWorkspace_PersistsTelegramSettings(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID, map[string]any{
		"settings": map[string]any{
			"existing": "kept",
			"telegram": map[string]any{
				"bot_token": "123:token",
				"user_id":   "456789",
			},
		},
	})
	req = withURLParam(req, "id", testWorkspaceID)

	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWorkspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	settingsMap, ok := resp.Settings.(map[string]any)
	if !ok {
		t.Fatalf("expected settings object in response, got %T", resp.Settings)
	}
	telegramMap, ok := settingsMap["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("expected telegram settings in response, got %T", settingsMap["telegram"])
	}
	if got := telegramMap["bot_token"]; got != "123:token" {
		t.Fatalf("bot_token = %v, want %q", got, "123:token")
	}
	if got := telegramMap["user_id"]; got != "456789" {
		t.Fatalf("user_id = %v, want %q", got, "456789")
	}
	if got := settingsMap["existing"]; got != "kept" {
		t.Fatalf("existing setting = %v, want %q", got, "kept")
	}

	var rawSettings []byte
	if err := testPool.QueryRow(context.Background(), `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&rawSettings); err != nil {
		t.Fatalf("load persisted settings: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(rawSettings, &persisted); err != nil {
		t.Fatalf("unmarshal persisted settings: %v", err)
	}
	persistedTelegram, ok := persisted["telegram"].(map[string]any)
	if !ok {
		t.Fatalf("expected persisted telegram settings, got %T", persisted["telegram"])
	}
	if got := persistedTelegram["bot_token"]; got != "123:token" {
		t.Fatalf("persisted bot_token = %v, want %q", got, "123:token")
	}
	if got := persistedTelegram["user_id"]; got != "456789" {
		t.Fatalf("persisted user_id = %v, want %q", got, "456789")
	}
}
