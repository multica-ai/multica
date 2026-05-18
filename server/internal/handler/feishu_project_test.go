package handler

import "testing"

func TestFeishuProjectNameFromRequestPrefersProjectName(t *testing.T) {
	got := feishuProjectNameFromRequest(UpdateFeishuProjectIntegrationRequest{
		ProjectName: "  space_name  ",
		ProjectKey:  "old-space-key",
	})
	if got != "space_name" {
		t.Fatalf("project name = %q, want space_name", got)
	}
}

func TestFeishuProjectNameFromRequestFallsBackToProjectKey(t *testing.T) {
	got := feishuProjectNameFromRequest(UpdateFeishuProjectIntegrationRequest{
		ProjectKey: "  old-space-key  ",
	})
	if got != "old-space-key" {
		t.Fatalf("project name fallback = %q, want old-space-key", got)
	}
}
