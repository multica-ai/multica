package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueNumberCollisionResponse(t *testing.T) {
	err := fmt.Errorf("create issue: %w", &service.IssueNumberCollisionError{
		Allocated: 1095, Constraint: service.IssueNumberConstraint,
	})
	var response struct {
		Code       string `json:"code"`
		Error      string `json:"error"`
		Allocated  int32  `json:"allocated"`
		Constraint string `json:"constraint"`
		Repair     string `json:"repair"`
	}
	testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
		if !writeIssueNumberCollision(w, err) {
			t.Fatal("issue number collision was not handled")
		}
	}, testutil.JSONRequest(http.MethodPost, "/api/issues", nil)).Want(http.StatusConflict).JSON(&response)
	if response.Code != "issue_number_collision" || response.Allocated != 1095 || response.Constraint != service.IssueNumberConstraint {
		t.Fatalf("collision response = %+v", response)
	}
	if response.Error == "" || response.Repair == "" {
		t.Fatalf("collision response lacks an explanation or recovery guidance: %+v", response)
	}
	if strings.Contains(response.Repair, "server/") || strings.Contains(response.Repair, "HealIssueCounter") {
		t.Errorf("recovery guidance exposes server implementation details: %q", response.Repair)
	}
}
