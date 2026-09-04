package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func TestTaskConfigResourceViewsStripUnknownSecretFields(t *testing.T) {
	ref := json.RawMessage(`{"provider":"aws_secrets_manager","provider_ref":"ref","version":"v1","path":"deploy/terraform/backend.hcl","mode":384,"repo":"repo","target":"main","account":"acct","region":"ap-southeast-2","value":"unique-backend-sentinel"}`)
	response := projectResourceToResponse(db.ProjectResource{ResourceType: "task_config", ResourceRef: ref})
	if strings.Contains(string(response.ResourceRef), "unique-backend-sentinel") {
		t.Fatal("API resource response contains an unknown secret field")
	}

	claim, _ := projectResourcesForClaim([]db.ProjectResource{{ResourceType: "task_config", ResourceRef: ref}})
	if len(claim) != 1 || strings.Contains(string(claim[0].ResourceRef), "unique-backend-sentinel") {
		t.Fatal("claim resource contains an unknown secret field")
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
	request := TaskConfigResolveRequest{WorkspaceID: "workspace", TaskID: "task", RuntimeID: "runtime", AgentID: "agent", Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1", Selectors: TaskConfigSelectors{Repo: "repo", Target: "main", Account: "acct", Region: "ap-southeast-2"}}
	provider := NewSecretsManagerTaskConfigProvider(fakeSecretsManager{value: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("unique-backend-sentinel")}})
	got, err := provider.Resolve(context.Background(), request)
	if err != nil || string(got) != "unique-backend-sentinel" {
		t.Fatalf("Resolve() = %q, %v", got, err)
	}
	failing := NewSecretsManagerTaskConfigProvider(fakeSecretsManager{err: errors.New("sentinel provider error")})
	_, err = failing.Resolve(context.Background(), request)
	if err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("provider error = %v, expected stable redacted error", err)
	}
	for name, bad := range map[string]TaskConfigResolveRequest{
		"missing workspace": func() TaskConfigResolveRequest { r := request; r.WorkspaceID = ""; return r }(),
		"missing task":      func() TaskConfigResolveRequest { r := request; r.TaskID = ""; return r }(),
		"missing runtime":   func() TaskConfigResolveRequest { r := request; r.RuntimeID = ""; return r }(),
		"missing agent":     func() TaskConfigResolveRequest { r := request; r.AgentID = ""; return r }(),
		"missing selector":  func() TaskConfigResolveRequest { r := request; r.Selectors.Region = ""; return r }(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.Resolve(context.Background(), bad); err == nil {
				t.Fatal("provider accepted an unbound task identity")
			}
		})
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
