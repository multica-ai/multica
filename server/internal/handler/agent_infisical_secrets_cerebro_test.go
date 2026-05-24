package handler

// CEREBRO-PATCH(agent-infisical-secrets): tests for agent secret-ref normalization.

import "testing"

func TestNormalizeAgentInfisicalSecrets(t *testing.T) {
	got, err := normalizeAgentInfisicalSecrets([]AgentInfisicalSecretResponse{
		{EnvVarName: " DATABASE_URL ", SecretName: " DATABASE_URL ", Environment: " production ", SecretPath: ""},
	})
	if err != nil {
		t.Fatalf("normalizeAgentInfisicalSecrets returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].EnvVarName != "DATABASE_URL" || got[0].SecretPath != "/" {
		t.Fatalf("unexpected normalized row: %#v", got[0])
	}
}

func TestNormalizeAgentInfisicalSecretsRejectsDuplicateEnvNames(t *testing.T) {
	_, err := normalizeAgentInfisicalSecrets([]AgentInfisicalSecretResponse{
		{EnvVarName: "DATABASE_URL", SecretName: "A", Environment: "production", SecretPath: "/"},
		{EnvVarName: "database_url", SecretName: "B", Environment: "production", SecretPath: "/"},
	})
	if err == nil {
		t.Fatalf("expected duplicate env var error")
	}
}
