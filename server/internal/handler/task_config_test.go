package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

func TestValidateTaskConfigResourceRef(t *testing.T) {
	valid := json.RawMessage(`{"provider":"aws_secrets_manager","provider_ref":"secret-ref","version":"v1","path":"deploy/terraform/backend.hcl","mode":384,"repo":"repo","target":"main","account":"acct","region":"ap-southeast-2"}`)
	got, err := validateAndNormalizeResourceRef("task_config", valid)
	if err != nil {
		t.Fatalf("valid task_config: %v", err)
	}
	if strings.Contains(string(got), "unique-backend-sentinel") {
		t.Fatal("normalized resource contains secret sentinel")
	}
	for _, tc := range []struct {
		name string
		ref  string
	}{
		{"absolute path", `{"provider":"aws_secrets_manager","provider_ref":"r","version":"v1","path":"/tmp/backend.hcl","mode":384}`},
		{"unsafe path", `{"provider":"aws_secrets_manager","provider_ref":"r","version":"v1","path":"../backend.hcl","mode":384}`},
		{"wrong mode", `{"provider":"aws_secrets_manager","provider_ref":"r","version":"v1","path":"backend.hcl","mode":420}`},
		{"missing version", `{"provider":"aws_secrets_manager","provider_ref":"r","path":"backend.hcl","mode":384}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateAndNormalizeResourceRef("task_config", json.RawMessage(tc.ref)); err == nil {
				t.Fatal("accepted invalid task_config")
			}
		})
	}
}

func TestTaskConfigClaimSerializationDoesNotContainProviderBytes(t *testing.T) {
	ref := taskConfigRef{
		Provider:    "aws_secrets_manager",
		ProviderRef: "secret-ref",
		Version:     "v1",
		Path:        "deploy/terraform/backend.hcl",
		Mode:        0o600,
	}
	payload, err := json.Marshal(ProjectResourceData{ResourceType: "task_config", ResourceRef: mustMarshal(ref)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "unique-backend-sentinel") {
		t.Fatal("claim payload contains provider bytes")
	}
}

type fakeSecretsManager struct {
	value *secretsmanager.GetSecretValueOutput
	err   error
}

func (f fakeSecretsManager) GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return f.value, f.err
}

func TestSecretsManagerTaskConfigProviderReturnsBytesWithoutEchoingFailures(t *testing.T) {
	provider := NewSecretsManagerTaskConfigProvider(fakeSecretsManager{value: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("unique-backend-sentinel")}})
	got, err := provider.Resolve(context.Background(), TaskConfigResolveRequest{Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1"})
	if err != nil || string(got) != "unique-backend-sentinel" {
		t.Fatalf("Resolve() = %q, %v", got, err)
	}
	failing := NewSecretsManagerTaskConfigProvider(fakeSecretsManager{err: errors.New("sentinel provider error")})
	_, err = failing.Resolve(context.Background(), TaskConfigResolveRequest{Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1"})
	if err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("provider error = %v, expected stable redacted error", err)
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
