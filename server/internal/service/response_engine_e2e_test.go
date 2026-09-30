package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHTTPTaskResponseFinalizerExternalE2E(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("RESPONSE_ENGINE_E2E_URL"))
	token := strings.TrimSpace(os.Getenv("RESPONSE_ENGINE_E2E_TOKEN"))
	if endpoint == "" || token == "" {
		t.Skip("RESPONSE_ENGINE_E2E_URL/TOKEN not configured")
	}

	finalizer, err := NewHTTPTaskResponseFinalizer(HTTPTaskResponseFinalizerConfig{
		BaseURL: endpoint,
		Token:   token,
		Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPTaskResponseFinalizer: %v", err)
	}
	result, err := finalizer.FinalizeTaskCompletion(context.Background(), TaskResponseFinalizationInput{
		SchemaVersion: "1",
		ResponseID:    "multica:e2e-task:terminal:r7",
		TaskID:        "e2e-task",
		WorkspaceID:   "e2e-workspace",
		Attempt:       1,
		TaskStatus:    "running",
		RawOutput:     "RAW E2E OUTPUT MUST NOT LEAK",
		Issue: &ResponseIssueSnapshot{
			ID:         "e2e-issue",
			Identifier: "MCIT-990",
			Title:      "Cross Boundary Smoke",
			Status:     "in_progress",
			Revision:   7,
			Metadata:   map[string]any{"readiness": "ready"},
		},
	})
	if err != nil {
		t.Fatalf("FinalizeTaskCompletion: %v", err)
	}
	if !strings.HasPrefix(result.Rendered, "[Goal]\nCross Boundary Smoke") {
		t.Fatalf("rendered=%q, want four-section Goal header", result.Rendered)
	}
	if strings.Contains(result.Rendered, "RAW E2E OUTPUT") {
		t.Fatalf("raw output leaked into rendered response: %q", result.Rendered)
	}
	if !strings.Contains(result.Rendered, "และต่อไปคืออะไร?") {
		t.Fatalf("rendered response missing deterministic next-action section: %q", result.Rendered)
	}
}
