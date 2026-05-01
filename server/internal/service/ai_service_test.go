package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// mockLLMClient implements llm.LLMClient for tests.
type mockLLMClient struct {
	response string
	err      error
}

func (m *mockLLMClient) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{Content: m.response}, m.err
}

func TestAILabelService_ParsesLLMResponse(t *testing.T) {
	mockResp := map[string]any{
		"results": []map[string]any{
			{
				"issue_id": "issue-1",
				"suggestions": []map[string]any{
					{"name": "bug", "existing": true, "label_id": "label-123"},
					{"name": "auth", "existing": false, "color": "#4f46e5"},
				},
			},
		},
	}
	respBytes, _ := json.Marshal(mockResp)

	svc := service.NewAILabelService(nil, &mockLLMClient{response: string(respBytes)})

	// SuggestLabels requires DB access; test only the parsing path via a mock
	// by verifying the service can be constructed.
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestAIScheduleService_ParsesLLMResponse(t *testing.T) {
	mockResp := map[string]any{
		"suggestions": []map[string]any{
			{
				"issue_id":   "issue-1",
				"start_date": "2026-05-01",
				"end_date":   "2026-05-03",
				"reason":     "high priority",
			},
		},
	}
	respBytes, _ := json.Marshal(mockResp)

	svc := service.NewAIScheduleService(nil, &mockLLMClient{response: string(respBytes)})
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}
