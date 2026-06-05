package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAgentInternalFlag covers create + update behaviour for the SLE-53
// internal flag. The flag must round-trip correctly through both endpoints and
// be absent (false) by default.
func TestAgentInternalFlag(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	claudeRuntimeID := createClaudeProviderRuntime(t)

	t.Run("default false on create", func(t *testing.T) {
		body := map[string]any{
			"name":                 "internal-test-default",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		if v, ok := resp["internal"]; !ok || v.(bool) != false {
			t.Errorf("expected internal=false in response, got %v (present=%v)", v, ok)
		}
	})

	t.Run("internal=true on create is persisted", func(t *testing.T) {
		body := map[string]any{
			"name":                 "internal-test-true",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"internal":             true,
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		if v, ok := resp["internal"]; !ok || v.(bool) != true {
			t.Errorf("expected internal=true in response, got %v (present=%v)", v, ok)
		}

		agentID := resp["id"].(string)

		// Update: clear the flag.
		t.Run("update to false", func(t *testing.T) {
			upd := map[string]any{"internal": false}
			wu := httptest.NewRecorder()
			testHandler.UpdateAgent(wu, withURLParam(
				newRequest(http.MethodPut, "/api/agents/"+agentID, upd),
				"id", agentID,
			))
			if wu.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", wu.Code, wu.Body.String())
			}
			var ur map[string]any
			json.NewDecoder(wu.Body).Decode(&ur)
			if v := ur["internal"]; v.(bool) != false {
				t.Errorf("expected internal=false after update, got %v", v)
			}
		})

		// Update: set it back.
		t.Run("update to true", func(t *testing.T) {
			upd := map[string]any{"internal": true}
			wu := httptest.NewRecorder()
			testHandler.UpdateAgent(wu, withURLParam(
				newRequest(http.MethodPut, "/api/agents/"+agentID, upd),
				"id", agentID,
			))
			if wu.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", wu.Code, wu.Body.String())
			}
			var ur map[string]any
			json.NewDecoder(wu.Body).Decode(&ur)
			if v := ur["internal"]; v.(bool) != true {
				t.Errorf("expected internal=true after update, got %v", v)
			}
		})
	})

	t.Run("omitting internal on update does not change it", func(t *testing.T) {
		// Create with internal=true, then update name only, internal must stay.
		body := map[string]any{
			"name":                 "internal-test-stable",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"internal":             true,
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		agentID := resp["id"].(string)

		upd := map[string]any{"name": "internal-test-stable-renamed"}
		wu := httptest.NewRecorder()
		testHandler.UpdateAgent(wu, withURLParam(
			newRequest(http.MethodPut, "/api/agents/"+agentID, upd),
			"id", agentID,
		))
		if wu.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", wu.Code, wu.Body.String())
		}
		var ur map[string]any
		json.NewDecoder(wu.Body).Decode(&ur)
		if v := ur["internal"]; v.(bool) != true {
			t.Errorf("expected internal=true to be preserved after unrelated update, got %v", v)
		}
	})
}
