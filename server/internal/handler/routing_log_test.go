package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetTaskRoutingLog_InvalidTaskIDReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/not-a-uuid/routing-log", nil)
	req = withURLParams(req, "taskId", "not-a-uuid")

	(&Handler{}).GetTaskRoutingLog(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid task id, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "task_id") {
		t.Fatalf("expected task_id validation error, got %s", w.Body.String())
	}
}

func TestPutRuntimeTierModelMap_InvalidRuntimeIDReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/runtimes/invalid-id/tier-model-map", bytes.NewBufferString(`{}`))
	req = withURLParams(req, "runtimeId", "invalid-id")

	(&Handler{}).PutRuntimeTierModelMap(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid runtime id, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "runtime_id") {
		t.Fatalf("expected runtime_id validation error, got %s", w.Body.String())
	}
}
