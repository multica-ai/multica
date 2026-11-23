package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubLiveRuntimeModels stands in for the daemon-reported model catalog so
// the save gate's model check runs without a database or a real runtime.
type stubLiveRuntimeModels struct {
	ids   map[string]struct{}
	known bool
}

func (s stubLiveRuntimeModels) LiveModelIDs(ctx context.Context, runtimeID string) (map[string]struct{}, bool) {
	return s.ids, s.known
}

// TestPrepareAgentRuntimeCurrentConfigModelGate pins the save-gate
// contract: a model outside the live catalog is refused with a message that
// names it (the generic "runtime model unavailable" gives the user nothing to
// act on), while an unknown catalog still fails open.
func TestPrepareAgentRuntimeCurrentConfigModelGate(t *testing.T) {
	ctx := context.Background()
	runtime := db.AgentRuntime{
		ID:       parseUUID(uuid.NewString()),
		OwnerID:  parseUUID(uuid.NewString()),
		Provider: "codex",
	}
	member := db.Member{UserID: runtime.OwnerID, Role: "owner"}
	liveCatalog := stubLiveRuntimeModels{ids: map[string]struct{}{"gpt-6.1-sol": {}}, known: true}

	t.Run("names the model missing from the live catalog", func(t *testing.T) {
		_, inputErr := prepareAgentRuntimeCurrentConfig(ctx, liveCatalog, db.Agent{}, member, runtime, "gpt-6.2-sol", "", "", 1, nil)
		if inputErr == nil {
			t.Fatal("expected the out-of-catalog model to be refused")
		}
		if inputErr.HTTPStatus != http.StatusConflict || inputErr.Code != string(service.RuntimeConfigResolutionModelUnavailable) {
			t.Fatalf("refusal = status %d code %q, want 409 model_unavailable", inputErr.HTTPStatus, inputErr.Code)
		}
		if inputErr.ResolutionStatus != service.RuntimeConfigResolutionModelUnavailable {
			t.Fatalf("resolution status = %q, want %q", inputErr.ResolutionStatus, service.RuntimeConfigResolutionModelUnavailable)
		}
		if !strings.Contains(inputErr.Message, "gpt-6.2-sol") {
			t.Fatalf("refusal message %q does not name the refused model", inputErr.Message)
		}
	})

	t.Run("fails open when the runtime reported no catalog", func(t *testing.T) {
		_, inputErr := prepareAgentRuntimeCurrentConfig(ctx, stubLiveRuntimeModels{known: false}, db.Agent{}, member, runtime, "gpt-6.2-sol", "", "", 1, nil)
		if inputErr != nil {
			t.Fatalf("unknown catalog must fail open, got %v", inputErr)
		}
	})

	t.Run("accepts a model inside the live catalog and an empty one", func(t *testing.T) {
		for _, model := range []string{"gpt-6.1-sol", ""} {
			prepared, inputErr := prepareAgentRuntimeCurrentConfig(ctx, liveCatalog, db.Agent{}, member, runtime, model, "", "", 1, nil)
			if inputErr != nil {
				t.Fatalf("model %q must save, got %v", model, inputErr)
			}
			if prepared.Model != model {
				t.Fatalf("prepared model = %q, want %q", prepared.Model, model)
			}
		}
	})
}

// TestUpdateAgentRuntimeCurrentConfigModelUnavailableNamesModel is the
// end-to-end refusal: a custom save carrying the agent's template model
// against a known catalog that no longer contains it must come back 409 with
// a message naming the model, while saving the runtime default (empty model)
// and an in-catalog model both succeed.
func TestUpdateAgentRuntimeCurrentConfigModelUnavailableNamesModel(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	withModelListStores(t)
	request, err := testHandler.ModelListStore.Create(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("create model list request: %v", err)
	}
	if w := reportModelList(t, request.ID, map[string]any{
		"status":    "completed",
		"supported": true,
		"models": []map[string]any{
			{"id": "gpt-6.1-sol", "label": "GPT 6.1", "default": true},
		},
	}); w.Code != http.StatusOK {
		t.Fatalf("report catalog: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	runtimeID := handlerTestRuntimeID(t)
	if runtimeID != testRuntimeID {
		t.Fatalf("handler test runtime = %q, want the seeded %q", runtimeID, testRuntimeID)
	}
	agentID := createRuntimeStrategyTestAgent(t, "gpt-6.2-sol", 2)

	refused := newRequestAs(testUserID, http.MethodPut, "/api/agents/"+agentID+"/runtime-config/current", map[string]any{
		"strategy":             "custom",
		"runtime_id":           runtimeID,
		"model":                "gpt-6.2-sol",
		"thinking_level":       "",
		"max_concurrent_tasks": 2,
		"runtime_config":       map[string]any{},
	})
	refused = withURLParam(refused, "id", agentID)
	payload := testutil.Call(t, testHandler.UpdateAgentRuntimeCurrentConfig, refused).Want(http.StatusConflict).Map()
	if payload["code"] != string(service.RuntimeConfigResolutionModelUnavailable) {
		t.Fatalf("refusal code = %v, want %q", payload["code"], service.RuntimeConfigResolutionModelUnavailable)
	}
	message, _ := payload["resolution_message"].(string)
	if !strings.Contains(message, "gpt-6.2-sol") {
		t.Fatalf("resolution_message %q does not name the refused model", message)
	}
	if payload["error"] != payload["resolution_message"] {
		t.Fatalf("error = %v, want the same named message as resolution_message", payload["error"])
	}

	saved := putRuntimeConfigCurrent(t, agentID, map[string]any{
		"strategy":             "custom",
		"runtime_id":           runtimeID,
		"model":                "",
		"thinking_level":       "",
		"max_concurrent_tasks": 3,
		"runtime_config":       map[string]any{},
	})
	if saved.Current.Strategy != AgentRuntimeConfigStrategyCustom || saved.Current.Model != "" {
		t.Fatalf("runtime-default save = strategy %q model %q, want custom/empty", saved.Current.Strategy, saved.Current.Model)
	}

	saved = putRuntimeConfigCurrent(t, agentID, map[string]any{
		"strategy":             "custom",
		"runtime_id":           runtimeID,
		"model":                "gpt-6.1-sol",
		"thinking_level":       "",
		"max_concurrent_tasks": 3,
		"runtime_config":       map[string]any{},
	})
	if saved.Current.Model != "gpt-6.1-sol" {
		t.Fatalf("in-catalog save = model %q, want gpt-6.1-sol", saved.Current.Model)
	}
}

func createRuntimeStrategyTestAgent(t *testing.T, model string, maxConcurrentTasks int32) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, share_runtime, model
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', $4, $5,
		        '', '{}'::jsonb, '[]'::jsonb, false, $6)
		RETURNING id
	`, testWorkspaceID, "Runtime Strategy Test Agent", handlerTestRuntimeID(t), maxConcurrentTasks, testUserID, model).Scan(&agentID); err != nil {
		t.Fatalf("failed to create runtime strategy test agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("failed to seed invocation target: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_user_runtime_config WHERE agent_id = $1`, agentID); err != nil {
			t.Errorf("cleanup runtime strategy override: %v", err)
		}
		if _, err := testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID); err != nil {
			t.Errorf("cleanup runtime strategy agent: %v", err)
		}
	})

	return agentID
}

func putRuntimeConfigCurrent(t *testing.T, agentID string, body map[string]any) AgentRuntimeConfigSummary {
	t.Helper()
	return putRuntimeConfigCurrentAs(t, testUserID, agentID, body)
}

func putRuntimeConfigCurrentAs(t *testing.T, userID, agentID string, body map[string]any) AgentRuntimeConfigSummary {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequestAs(userID, "PUT", "/api/agents/"+agentID+"/runtime-config/current", body)
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgentRuntimeCurrentConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntimeCurrentConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var summary AgentRuntimeConfigSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode runtime config summary: %v", err)
	}
	return summary
}
