package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

type fakeWorkflowCompletionGuidanceError struct{}

func (fakeWorkflowCompletionGuidanceError) Error() string { return "completion rejected" }

func (fakeWorkflowCompletionGuidanceError) WorkflowCompletionGuidance() service.WorkflowCompletionGuidance {
	return service.WorkflowCompletionGuidance{
		Code:         "workflow_gate_rejected",
		Requirement:  "Create a wakeup",
		Alternatives: []string{"Create a wakeup", "Ask a member"},
		Attempt:      1,
	}
}

func TestWriteWorkflowCompletionGuidance(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	if !writeWorkflowCompletionGuidance(recorder, fakeWorkflowCompletionGuidanceError{}) {
		t.Fatal("structured guidance error was not handled")
	}
	if recorder.Code != http.StatusConflict || strings.TrimSpace(recorder.Body.String()) != `{"code":"workflow_gate_rejected","requirement":"Create a wakeup","alternatives":["Create a wakeup","Ask a member"],"attempt":1}` {
		t.Fatalf("response status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	if writeWorkflowCompletionGuidance(httptest.NewRecorder(), errors.New("ordinary error")) {
		t.Fatal("ordinary error was misclassified as completion guidance")
	}
}
