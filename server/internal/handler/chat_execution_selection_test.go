package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestChatExecutionSelectionSnapshotsFollowUpTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database unavailable")
	}
	runtimeID := createClaudeProviderRuntime(t)
	agentID := createAgentOnRuntimeWithModel(t, "chat-selection", runtimeID, "model-a")
	dbfx.Exec(t, "UPDATE agent SET thinking_level = 'low' WHERE id = $1", agentID)
	sessionID := createHandlerTestChatSession(t, agentID)

	previous := testHandler.ModelCatalogCache
	cache := NewInMemoryModelCatalogCache()
	testHandler.ModelCatalogCache = cache
	t.Cleanup(func() { testHandler.ModelCatalogCache = previous })
	if err := cache.Put(context.Background(), runtimeID, []ModelEntry{
		{ID: "model-a", Label: "A", Default: true, Thinking: &ModelThinking{SupportedLevels: []ThinkingLevel{{Value: "low"}}}},
		{ID: "model-b", Label: "B", Thinking: &ModelThinking{SupportedLevels: []ThinkingLevel{{Value: "high"}}}},
	}, nil, true); err != nil {
		t.Fatal(err)
	}

	send := func(content string) string {
		t.Helper()
		req := newRequest(http.MethodPost, "/api/chat/sessions/"+sessionID+"/messages", map[string]any{"content": content})
		req = withChatTestWorkspaceCtx(t, withURLParam(req, "sessionId", sessionID))
		w := httptest.NewRecorder()
		testHandler.SendChatMessage(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("send %q: %d %s", content, w.Code, w.Body.String())
		}
		var out SendChatMessageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.TaskID
	}
	patch := func(model, level any, want int) {
		t.Helper()
		req := newRequest(http.MethodPatch, "/api/chat/sessions/"+sessionID, map[string]any{
			"model_override": model, "thinking_level_override": level,
		})
		req = withChatTestWorkspaceCtx(t, withURLParam(req, "sessionId", sessionID))
		w := httptest.NewRecorder()
		testHandler.UpdateChatSession(w, req)
		if w.Code != want {
			t.Fatalf("patch: wanted %d, got %d: %s", want, w.Code, w.Body.String())
		}
	}
	assertTask := func(id string, model, level *string) {
		t.Helper()
		var gotModel, gotLevel *string
		if err := testPool.QueryRow(context.Background(),
			"SELECT chat_model, chat_thinking_level FROM agent_task_queue WHERE id = $1", id,
		).Scan(&gotModel, &gotLevel); err != nil {
			t.Fatal(err)
		}
		if !sameOptionalText(gotModel, model) || !sameOptionalText(gotLevel, level) {
			t.Fatalf("task %s selection = %v/%v, want %v/%v", id, gotModel, gotLevel, model, level)
		}
	}

	first := send("first")
	inheritedModel, inheritedLevel := "model-a", "low"
	assertTask(first, &inheritedModel, &inheritedLevel)
	patch("model-b", "high", http.StatusOK)
	second := send("second")
	selectedModel, selectedLevel := "model-b", "high"
	assertTask(second, &selectedModel, &selectedLevel)
	storedTask, err := testHandler.Queries.GetAgentTask(context.Background(), util.MustParseUUID(second))
	if err != nil {
		t.Fatal(err)
	}
	runRecord := taskToResponse(storedTask, testWorkspaceID)
	if !sameOptionalText(runRecord.ChatModel, &selectedModel) ||
		!sameOptionalText(runRecord.ChatThinkingLevel, &selectedLevel) {
		t.Fatalf("run record lost execution selection: %+v", runRecord)
	}
	getReq := withChatTestWorkspaceCtx(t, withURLParam(newRequest(http.MethodGet, "/api/chat/sessions/"+sessionID, nil), "sessionId", sessionID))
	getResponse := httptest.NewRecorder()
	testHandler.GetChatSession(getResponse, getReq)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET session: %d %s", getResponse.Code, getResponse.Body.String())
	}
	var storedSession ChatSessionResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &storedSession); err != nil {
		t.Fatal(err)
	}
	if !sameOptionalText(storedSession.ModelOverride, &selectedModel) ||
		!sameOptionalText(storedSession.ThinkingLevelOverride, &selectedLevel) {
		t.Fatalf("session did not persist selection: %+v", storedSession)
	}
	assertTask(first, &inheritedModel, &inheritedLevel)
	patch("model-a", "high", http.StatusBadRequest)
	patch("missing", "low", http.StatusBadRequest)
	patch("model-a[1m]", "low", http.StatusOK)
	variantTask := send("context window")
	variantModel, variantLevel := "model-a[1m]", "low"
	assertTask(variantTask, &variantModel, &variantLevel)
	patch(nil, nil, http.StatusOK)
	third := send("third")
	assertTask(third, &inheritedModel, &inheritedLevel)
	assertTask(second, &selectedModel, &selectedLevel)
}

func sameOptionalText(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
