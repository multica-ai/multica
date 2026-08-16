package handler

import (
	"strings"
	"testing"
)

func TestTaskMessageAuditSummary(t *testing.T) {
	tests := []struct {
		name       string
		message    TaskMessageRequest
		wantArgs   string
		wantResult string
		wantStatus string
		wantOK     bool
	}{
		{
			name:       "tool use",
			message:    TaskMessageRequest{Type: "tool_use", Tool: "mcp__builderlync__get_contact", Input: map[string]any{"contact_id": "c_1"}},
			wantArgs:   `{"contact_id":"c_1"}`,
			wantStatus: "started",
			wantOK:     true,
		},
		{
			name:       "tool result",
			message:    TaskMessageRequest{Type: "tool_result", Tool: "mcp__builderlync__get_contact", Output: "ok"},
			wantResult: "ok",
			wantStatus: "completed",
			wantOK:     true,
		},
		{name: "text ignored", message: TaskMessageRequest{Type: "text", Content: "hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, result, status, ok := taskMessageAuditSummary(tt.message)
			if args != tt.wantArgs || result != tt.wantResult || status != tt.wantStatus || ok != tt.wantOK {
				t.Fatalf("taskMessageAuditSummary() = %q, %q, %q, %v; want %q, %q, %q, %v", args, result, status, ok, tt.wantArgs, tt.wantResult, tt.wantStatus, tt.wantOK)
			}
		})
	}
}

func TestTaskMessageAuditSummaryRedactsSecrets(t *testing.T) {
	args, _, _, _ := taskMessageAuditSummary(TaskMessageRequest{
		Type:  "tool_use",
		Input: map[string]any{"token": "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
	})
	if strings.Contains(args, "ghp_") {
		t.Fatalf("tool arguments leaked a token: %s", args)
	}

	_, result, _, _ := taskMessageAuditSummary(TaskMessageRequest{
		Type:   "tool_result",
		Output: "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payloadpayload.signaturesignature",
	})
	if strings.Contains(result, "eyJhbGciOiJIUzI1NiJ9") {
		t.Fatalf("tool result leaked a bearer token: %s", result)
	}
}
