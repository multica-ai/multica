package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateAgent_MaxConcurrentTasksBoundsAndDefault(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name     string
		value    any
		provided bool
		wantCode int
		want     int32
	}{
		{name: "omitted defaults to six", provided: false, wantCode: http.StatusCreated, want: 6},
		{name: "null defaults to six", value: nil, provided: true, wantCode: http.StatusCreated, want: 6},
		{name: "minimum accepted", value: 1, provided: true, wantCode: http.StatusCreated, want: 1},
		{name: "maximum accepted", value: 50, provided: true, wantCode: http.StatusCreated, want: 50},
		{name: "zero rejected", value: 0, provided: true, wantCode: http.StatusBadRequest},
		{name: "negative rejected", value: -1, provided: true, wantCode: http.StatusBadRequest},
		{name: "above maximum rejected", value: 51, provided: true, wantCode: http.StatusBadRequest},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentName := fmt.Sprintf("concurrency-create-%d", i)
			body := map[string]any{
				"name":       agentName,
				"runtime_id": handlerTestRuntimeID(t),
			}
			if tt.provided {
				body["max_concurrent_tasks"] = tt.value
			}

			w := httptest.NewRecorder()
			testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
			if w.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.wantCode, w.Body.String())
			}

			if tt.wantCode == http.StatusBadRequest {
				if !strings.Contains(w.Body.String(), "between 1 and 50") {
					t.Fatalf("error should explain the 1-50 range: %s", w.Body.String())
				}
				return
			}

			var response AgentResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, response.ID)
			})
			if response.MaxConcurrentTasks != tt.want {
				t.Fatalf("max_concurrent_tasks = %d, want %d", response.MaxConcurrentTasks, tt.want)
			}
		})
	}
}

func TestUpdateAgent_MaxConcurrentTasksBoundsAndOmission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "concurrency-update", nil)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET max_concurrent_tasks = 17 WHERE id = $1`, agentID,
	); err != nil {
		t.Fatalf("seed max_concurrent_tasks: %v", err)
	}

	readPersisted := func() int32 {
		t.Helper()
		var got int32
		if err := testPool.QueryRow(context.Background(),
			`SELECT max_concurrent_tasks FROM agent WHERE id = $1`, agentID,
		).Scan(&got); err != nil {
			t.Fatalf("read max_concurrent_tasks: %v", err)
		}
		return got
	}

	for _, value := range []int32{0, -1, 51} {
		t.Run(fmt.Sprintf("rejects_%d", value), func(t *testing.T) {
			w := httptest.NewRecorder()
			req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
				"max_concurrent_tasks": value,
			}), "id", agentID)
			testHandler.UpdateAgent(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "between 1 and 50") {
				t.Fatalf("error should explain the 1-50 range: %s", w.Body.String())
			}
			if got := readPersisted(); got != 17 {
				t.Fatalf("rejected update persisted %d, want existing value 17", got)
			}
		})
	}

	for _, value := range []int32{1, 50} {
		t.Run(fmt.Sprintf("accepts_%d", value), func(t *testing.T) {
			w := httptest.NewRecorder()
			req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
				"max_concurrent_tasks": value,
			}), "id", agentID)
			testHandler.UpdateAgent(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if got := readPersisted(); got != value {
				t.Fatalf("persisted max_concurrent_tasks = %d, want %d", got, value)
			}
		})
	}

	t.Run("omitted preserves existing value", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
			"description": "concurrency unchanged",
		}), "id", agentID)
		testHandler.UpdateAgent(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		if got := readPersisted(); got != 50 {
			t.Fatalf("omitted max_concurrent_tasks changed value to %d, want 50", got)
		}
	})

	t.Run("null preserves existing value", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
			"max_concurrent_tasks": nil,
		}), "id", agentID)
		testHandler.UpdateAgent(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		if got := readPersisted(); got != 50 {
			t.Fatalf("null max_concurrent_tasks changed value to %d, want 50", got)
		}
	})
}

func TestCreateAgentFromTemplate_MaxConcurrentTasksBoundsAndDefault(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	const templateSlug = "commit-message"
	if _, ok := agentTemplates.Get(templateSlug); !ok {
		t.Fatalf("expected template %q to be loaded", templateSlug)
	}

	tests := []struct {
		name     string
		value    any
		provided bool
		wantCode int
		want     int32
	}{
		{name: "omitted defaults to six", provided: false, wantCode: http.StatusCreated, want: 6},
		{name: "null defaults to six", value: nil, provided: true, wantCode: http.StatusCreated, want: 6},
		{name: "minimum accepted", value: 1, provided: true, wantCode: http.StatusCreated, want: 1},
		{name: "maximum accepted", value: 50, provided: true, wantCode: http.StatusCreated, want: 50},
		{name: "zero rejected", value: 0, provided: true, wantCode: http.StatusBadRequest},
		{name: "negative rejected", value: -1, provided: true, wantCode: http.StatusBadRequest},
		{name: "above maximum rejected", value: 51, provided: true, wantCode: http.StatusBadRequest},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{
				"template_slug": templateSlug,
				"name":          fmt.Sprintf("concurrency-template-%d", i),
				"runtime_id":    handlerTestRuntimeID(t),
			}
			if tt.provided {
				body["max_concurrent_tasks"] = tt.value
			}

			w := httptest.NewRecorder()
			testHandler.CreateAgentFromTemplate(w, newRequest(http.MethodPost, "/api/agents/from-template", body))
			if w.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.wantCode, w.Body.String())
			}

			if tt.wantCode == http.StatusBadRequest {
				if !strings.Contains(w.Body.String(), "between 1 and 50") {
					t.Fatalf("error should explain the 1-50 range: %s", w.Body.String())
				}
				return
			}

			var response CreateAgentFromTemplateResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, response.Agent.ID)
			})
			if response.Agent.MaxConcurrentTasks != tt.want {
				t.Fatalf("max_concurrent_tasks = %d, want %d", response.Agent.MaxConcurrentTasks, tt.want)
			}
		})
	}
}
