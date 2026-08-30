package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateAgentOperatingModeValidationAndPersistence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	for _, value := range []any{"admin", "", nil} {
		t.Run("rejects_invalid_value", func(t *testing.T) {
			body := map[string]any{
				"name":           "mode-invalid-" + uuid.NewString(),
				"runtime_id":     runtimeID,
				"operating_mode": value,
			}
			w := httptest.NewRecorder()
			testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
			if w.Code == http.StatusCreated {
				var created struct {
					ID string `json:"id"`
				}
				_ = json.NewDecoder(w.Body).Decode(&created)
				if created.ID != "" {
					testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
				}
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "coding, operational, hybrid") {
				t.Fatalf("error does not explain the operating mode vocabulary: %s", w.Body.String())
			}
		})
	}

	for _, tc := range []struct {
		name     string
		mode     string
		provided bool
		want     string
	}{
		{name: "omitted_defaults_to_coding", want: "coding"},
		{name: "coding", mode: "coding", provided: true, want: "coding"},
		{name: "operational", mode: "operational", provided: true, want: "operational"},
		{name: "hybrid", mode: "hybrid", provided: true, want: "hybrid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{
				"name":       "mode-create-" + uuid.NewString(),
				"runtime_id": runtimeID,
			}
			if tc.provided {
				body["operating_mode"] = tc.mode
			}

			w := httptest.NewRecorder()
			testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
			}
			var response struct {
				ID            string `json:"id"`
				OperatingMode string `json:"operating_mode"`
			}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, response.ID)
			})
			if response.OperatingMode != tc.want {
				t.Fatalf("response operating_mode = %q, want %q", response.OperatingMode, tc.want)
			}
			var stored string
			if err := testPool.QueryRow(context.Background(),
				`SELECT operating_mode FROM agent WHERE id = $1`, response.ID,
			).Scan(&stored); err != nil {
				t.Fatalf("read operating_mode: %v", err)
			}
			if stored != tc.want {
				t.Fatalf("stored operating_mode = %q, want %q", stored, tc.want)
			}
		})
	}
}

func TestUpdateAgentOperatingModeValidationAndPersistence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "mode-update-"+uuid.NewString(), nil)
	update := func(value any) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
			"operating_mode": value,
		}), "id", agentID)
		testHandler.UpdateAgent(w, req)
		return w
	}
	readStored := func() string {
		t.Helper()
		var stored string
		if err := testPool.QueryRow(context.Background(),
			`SELECT operating_mode FROM agent WHERE id = $1`, agentID,
		).Scan(&stored); err != nil {
			t.Fatalf("read operating_mode: %v", err)
		}
		return stored
	}

	w := update("hybrid")
	if w.Code != http.StatusOK {
		t.Fatalf("hybrid update status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var response struct {
		OperatingMode string `json:"operating_mode"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OperatingMode != "hybrid" || readStored() != "hybrid" {
		t.Fatalf("hybrid update response=%q stored=%q", response.OperatingMode, readStored())
	}

	for _, invalid := range []any{"admin", "", nil} {
		w = update(invalid)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid update %v status = %d, want 400: %s", invalid, w.Code, w.Body.String())
		}
		if readStored() != "hybrid" {
			t.Fatalf("invalid update %v changed stored mode to %q", invalid, readStored())
		}
	}

	w = httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "mode unchanged",
	}), "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("omitted update status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if readStored() != "hybrid" {
		t.Fatalf("omitted update changed stored mode to %q", readStored())
	}
}
