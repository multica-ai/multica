package taskmandate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func contractInputUUID(value byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{value}, Valid: true}
}

func TestContractInputCarriesTaskAndSourceIdentity(t *testing.T) {
	t.Parallel()
	taskID := contractInputUUID(1)
	workspaceID := contractInputUUID(2)
	agentID := contractInputUUID(3)

	input, err := newContractInput(
		taskID,
		workspaceID,
		agentID,
		[]string{"tools:Read"},
		nil,
		"inventory:v7/discovery:v3",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	gotTaskID, gotWorkspaceID, gotAgentID := input.TaskIdentity()
	if gotTaskID != taskID || gotWorkspaceID != workspaceID || gotAgentID != agentID {
		t.Fatalf(
			"TaskIdentity = (%v, %v, %v), want (%v, %v, %v)",
			gotTaskID, gotWorkspaceID, gotAgentID, taskID, workspaceID, agentID,
		)
	}
	if got := input.SourceVersion(); got != "inventory:v7/discovery:v3" {
		t.Fatalf("SourceVersion = %q, want exact source version", got)
	}
	if got := input.DiscoveryVersion(); got != "discovery:v3" {
		t.Fatalf("DiscoveryVersion = %q, want parsed discovery version", got)
	}
}

func TestNewClaimInputCarriesMCPDiscoveryVersion(t *testing.T) {
	t.Parallel()
	input, err := NewClaimInput(
		contractInputUUID(1),
		contractInputUUID(2),
		contractInputUUID(3),
		[]string{"mcp__atlas__search", "tools:Read"},
		nil,
		"daemon-claim:v1",
	)
	if err != nil {
		t.Fatalf("NewClaimInput: %v", err)
	}
	if got := input.DiscoveryVersion(); got == "" || !strings.HasPrefix(got, "mcp:sha256:") {
		t.Fatalf("DiscoveryVersion = %q, want stable MCP content version", got)
	}
	withoutMCP, err := NewClaimInput(
		contractInputUUID(1),
		contractInputUUID(2),
		contractInputUUID(3),
		[]string{"tools:Read"},
		nil,
		"daemon-claim:v1",
	)
	if err != nil {
		t.Fatalf("NewClaimInput without MCP: %v", err)
	}
	if got := withoutMCP.DiscoveryVersion(); got != "" {
		t.Fatalf("non-MCP DiscoveryVersion = %q, want empty", got)
	}
}

func TestContractInputNormalizesExactCallableAndConnectionScopeIdentities(t *testing.T) {
	t.Parallel()
	input, err := newContractInput(
		contractInputUUID(1),
		contractInputUUID(2),
		contractInputUUID(3),
		[]string{"tools:Write", "tools:Read", "tools:Write"},
		[]string{"connection:registry", "connection:customer-service", "connection:registry"},
		"inventory:v7/discovery:v3",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	if got, want := input.CallableIdentities(), []string{"tools:Read", "tools:Write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CallableIdentities = %v, want %v", got, want)
	}
	if got, want := input.ConnectionScopeIdentities(), []string{"connection:customer-service", "connection:registry"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConnectionScopeIdentities = %v, want %v", got, want)
	}
}

func TestContractInputDerivesPlatformOperationsFromCatalogBindings(t *testing.T) {
	t.Parallel()
	input, err := newContractInput(
		contractInputUUID(1),
		contractInputUUID(2),
		contractInputUUID(3),
		[]string{"model_supplied_operation", "create_workflow_hook"},
		nil,
		"inventory:v7/discovery:v3",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	got := input.PlatformOperationIdentities()
	if len(got) != 1 {
		t.Fatalf("PlatformOperationIdentities = %v, want one server-owned catalog binding", got)
	}
	if got[0].CallableIdentity() != "create_workflow_hook" || got[0].CapabilityKey() != "hooks:write" {
		t.Fatalf(
			"PlatformOperationIdentities[0] = (%q, %q), want exact binding and canonical capability",
			got[0].CallableIdentity(), got[0].CapabilityKey(),
		)
	}
}

func TestContractInputReturnsDefensiveIdentityCopies(t *testing.T) {
	t.Parallel()
	callables := []string{"create_workflow_hook"}
	connectionScopes := []string{"connection:registry"}
	input, err := newContractInput(
		contractInputUUID(1),
		contractInputUUID(2),
		contractInputUUID(3),
		callables,
		connectionScopes,
		"inventory:v7/discovery:v3",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	callables[0] = "tools:Write"
	connectionScopes[0] = "connection:changed"
	gotCallables := input.CallableIdentities()
	gotScopes := input.ConnectionScopeIdentities()
	gotOperations := input.PlatformOperationIdentities()
	gotCallables[0] = "tools:Write"
	gotScopes[0] = "connection:changed"
	if len(gotOperations) > 0 {
		gotOperations[0] = PlatformOperationIdentity{}
	}

	if got := input.CallableIdentities(); !reflect.DeepEqual(got, []string{"create_workflow_hook"}) {
		t.Fatalf("CallableIdentities mutated through caller-owned slice: %v", got)
	}
	if got := input.ConnectionScopeIdentities(); !reflect.DeepEqual(got, []string{"connection:registry"}) {
		t.Fatalf("ConnectionScopeIdentities mutated through caller-owned slice: %v", got)
	}
	if got := input.PlatformOperationIdentities(); len(got) != 1 || got[0].CallableIdentity() != "create_workflow_hook" {
		t.Fatalf("PlatformOperationIdentities mutated through returned slice: %v", got)
	}
}

func TestContractInputRejectsNonExactIdentities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		callables        []string
		connectionScopes []string
		sourceVersion    string
	}{
		{name: "callable whitespace", callables: []string{" tools:Read"}, sourceVersion: "v1"},
		{name: "blank callable", callables: []string{""}, sourceVersion: "v1"},
		{name: "connection whitespace", callables: []string{"tools:Read"}, connectionScopes: []string{"connection:registry "}, sourceVersion: "v1"},
		{name: "untyped connection scope", callables: []string{"tools:Read"}, connectionScopes: []string{"registry"}, sourceVersion: "v1"},
		{name: "blank source version", callables: []string{"tools:Read"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newContractInput(
				contractInputUUID(1),
				contractInputUUID(2),
				contractInputUUID(3),
				tt.callables,
				tt.connectionScopes,
				tt.sourceVersion,
			); err == nil {
				t.Fatal("newContractInput succeeded, want exact-identity validation error")
			}
		})
	}
}

func TestContractInputRejectsJSONAssembly(t *testing.T) {
	t.Parallel()
	var input ContractInput
	err := json.Unmarshal(
		[]byte(`{"callableIdentities":["create_workflow_hook"],"platformOperationIdentities":["manage_workflows"]}`),
		&input,
	)
	if err == nil {
		t.Fatal("json.Unmarshal succeeded, want ContractInput to reject model/request assembly")
	}
}
