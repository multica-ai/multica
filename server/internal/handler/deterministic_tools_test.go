package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

// The handler runs the sandbox in-process and touches no Handler dependencies,
// so a zero-value Handler is enough to exercise the request/response contract.
func postTest(t *testing.T, body string) (*httptest.ResponseRecorder, dettools.Result) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/deterministic-tools/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	(&Handler{}).TestDeterministicTool(w, req)

	var res dettools.Result
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

func TestDeterministicToolHandler_OK(t *testing.T) {
	src := `package step
func Run(input map[string]any) map[string]any {
	return map[string]any{"status": "ok", "summary": "ran"}
}`
	body, _ := json.Marshal(map[string]any{"source": src, "input": map[string]any{}})
	w, res := postTest(t, string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if res.Status != dettools.StatusOK || res.Summary != "ran" {
		t.Fatalf("result = %+v, want ok/ran", res)
	}
}

func TestDeterministicToolHandler_CompileErrorIs200WithEnvelope(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"source": "package step\nnot go"})
	w, res := postTest(t, string(body))
	// A compile error is a step-level outcome, not an HTTP error.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (compile error rides in the envelope)", w.Code)
	}
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodeInvalidInput {
		t.Fatalf("result = %+v, want error/INVALID_INPUT", res)
	}
}

func TestDeterministicToolHandler_EmptySourceIs400(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"source": "   "})
	w, _ := postTest(t, string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty source", w.Code)
	}
}

func TestDeterministicToolHandler_MalformedBodyIs400(t *testing.T) {
	w, _ := postTest(t, "{not json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for malformed body", w.Code)
	}
}
