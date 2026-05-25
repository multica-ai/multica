package handler

// CEREBRO-PATCH(agent-infisical-secrets): tests for agent Infisical folder normalization.

import "testing"

func TestNormalizeAgentInfisicalFolders(t *testing.T) {
	got, err := normalizeAgentInfisicalFolders([]AgentInfisicalFolderResponse{
		{Environment: " production ", SecretPath: ""},
	})
	if err != nil {
		t.Fatalf("normalizeAgentInfisicalFolders returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Environment != "production" || got[0].SecretPath != "/" {
		t.Fatalf("unexpected normalized row: %#v", got[0])
	}
}

func TestNormalizeAgentInfisicalFoldersRejectsDuplicates(t *testing.T) {
	_, err := normalizeAgentInfisicalFolders([]AgentInfisicalFolderResponse{
		{Environment: "production", SecretPath: "/app"},
		{Environment: "production", SecretPath: "/app"},
	})
	if err == nil {
		t.Fatalf("expected duplicate folder error")
	}
}

func TestNormalizeAgentInfisicalFoldersRequiresEnvironment(t *testing.T) {
	_, err := normalizeAgentInfisicalFolders([]AgentInfisicalFolderResponse{
		{Environment: "", SecretPath: "/app"},
	})
	if err == nil {
		t.Fatalf("expected error for missing environment")
	}
}

// CEREBRO-PATCH(user-infisical-folders): tests for the owner allow-list gate.
func TestPartitionInfisicalFoldersByAllowList(t *testing.T) {
	allowed := map[string]struct{}{
		infisicalFolderKey("production", "/firtal-agents"): {},
		infisicalFolderKey("staging", "/"):                 {},
	}
	folders := []AgentInfisicalFolderResponse{
		{Environment: "production", SecretPath: "/firtal-agents"}, // allowed
		{Environment: "production", SecretPath: "/secret-keys"},   // denied: path not granted
		{Environment: "staging", SecretPath: "/"},                 // allowed
		{Environment: "dev", SecretPath: "/"},                     // denied: environment not granted
	}

	ok, denied := partitionInfisicalFoldersByAllowList(folders, allowed)
	if len(ok) != 2 {
		t.Fatalf("allowed count = %d, want 2: %#v", len(ok), ok)
	}
	if len(denied) != 2 {
		t.Fatalf("denied count = %d, want 2: %#v", len(denied), denied)
	}
	if denied[0].SecretPath != "/secret-keys" || denied[1].Environment != "dev" {
		t.Fatalf("unexpected denied set: %#v", denied)
	}
}

func TestPartitionInfisicalFoldersEmptyAllowListDeniesAll(t *testing.T) {
	folders := []AgentInfisicalFolderResponse{
		{Environment: "production", SecretPath: "/firtal-agents"},
	}
	ok, denied := partitionInfisicalFoldersByAllowList(folders, map[string]struct{}{})
	if len(ok) != 0 || len(denied) != 1 {
		t.Fatalf("empty allow-list should deny all: ok=%#v denied=%#v", ok, denied)
	}
}
