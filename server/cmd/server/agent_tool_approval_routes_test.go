package main

import (
	"os"
	"strings"
	"testing"
)

func TestRouterRegistersOperatorAndDaemonToolApprovalRoutes(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	source := string(raw)
	for _, route := range []string{
		`r.Post("/tasks/{taskId}/tool-invocations", h.CreateDaemonToolInvocation)`,
		`r.Get("/tasks/{taskId}/tool-approvals/{approvalId}", h.GetDaemonToolApproval)`,
		`r.Post("/tasks/{taskId}/tool-approvals/{approvalId}/consume", h.ConsumeDaemonToolApproval)`,
		`r.Post("/tasks/{taskId}/tool-invocations/{invocationId}/events", h.CommitDaemonToolInvocationEvent)`,
		`r.With(handler.RequireHumanActor).Get("/api/approvals", h.ListAgentToolApprovals)`,
		`r.With(handler.RequireHumanActor).Get("/api/approvals/{approvalId}", h.GetAgentToolApproval)`,
		`r.With(handler.RequireHumanActor).Post("/api/approvals/{approvalId}/decision", h.DecideAgentToolApproval)`,
	} {
		if !strings.Contains(source, route) {
			t.Errorf("router missing %s", route)
		}
	}
}
