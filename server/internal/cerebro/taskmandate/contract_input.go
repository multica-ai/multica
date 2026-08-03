package taskmandate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

// ContractInput is the normalized, immutable input to one task mandate claim
// generation. Platform operation identities are derived from the server-owned
// platform catalog; callers cannot submit a parallel list of operation names.
type ContractInput struct {
	taskID                      pgtype.UUID
	workspaceID                 pgtype.UUID
	agentID                     pgtype.UUID
	callableIdentities          []string
	platformOperationIdentities []PlatformOperationIdentity
	connectionScopeIdentities   []string
	sourceVersion               string
	discoveryVersion            string
}

// PlatformOperationIdentity retains both the exact callable binding and the
// canonical permission key resolved from the server-owned platform catalog.
// Its fields are private so callers cannot forge either half of the identity.
type PlatformOperationIdentity struct {
	callableIdentity string
	capabilityKey    string
}

// CallableIdentity returns the exact platform operation binding.
func (identity PlatformOperationIdentity) CallableIdentity() string {
	return identity.callableIdentity
}

// CapabilityKey returns the canonical Permissions key governing the operation.
func (identity PlatformOperationIdentity) CapabilityKey() string {
	return identity.capabilityKey
}

// newContractInput is deliberately package-internal: only the server-owned
// contract compiler may assemble raw identities. It validates exact identities,
// removes duplicates, and stores them in stable order.
func newContractInput(
	taskID, workspaceID, agentID pgtype.UUID,
	callableIdentities []string,
	connectionScopeIdentities []string,
	sourceVersion string,
) (ContractInput, error) {
	return newContractInputWithDiscoveryVersion(
		taskID, workspaceID, agentID, callableIdentities,
		connectionScopeIdentities, sourceVersion, discoveryVersionFromSource(sourceVersion),
	)
}

func newContractInputWithDiscoveryVersion(
	taskID, workspaceID, agentID pgtype.UUID,
	callableIdentities []string,
	connectionScopeIdentities []string,
	sourceVersion, discoveryVersion string,
) (ContractInput, error) {
	if !taskID.Valid || !workspaceID.Valid || !agentID.Valid {
		return ContractInput{}, fmt.Errorf("task mandate contract input: invalid task identity")
	}
	if sourceVersion == "" || strings.TrimSpace(sourceVersion) != sourceVersion {
		return ContractInput{}, fmt.Errorf("task mandate contract input: invalid source version")
	}
	if strings.TrimSpace(discoveryVersion) != discoveryVersion {
		return ContractInput{}, fmt.Errorf("task mandate contract input: invalid discovery version")
	}

	callables, err := normalizeContractIdentities("callable", callableIdentities, nil)
	if err != nil {
		return ContractInput{}, err
	}
	connectionScopes, err := normalizeContractIdentities(
		"connection scope",
		connectionScopeIdentities,
		func(identity string) bool {
			return strings.HasPrefix(identity, "connection:") && strings.TrimPrefix(identity, "connection:") != ""
		},
	)
	if err != nil {
		return ContractInput{}, err
	}

	platformOperations := make([]PlatformOperationIdentity, 0, len(callables))
	for _, callable := range callables {
		if capability, ok := platformcatalog.ByToolBinding(callable); ok {
			platformOperations = append(platformOperations, PlatformOperationIdentity{
				callableIdentity: callable,
				capabilityKey:    capability.Key,
			})
		}
	}

	return ContractInput{
		taskID:                      taskID,
		workspaceID:                 workspaceID,
		agentID:                     agentID,
		callableIdentities:          callables,
		platformOperationIdentities: platformOperations,
		connectionScopeIdentities:   connectionScopes,
		sourceVersion:               sourceVersion,
		discoveryVersion:            discoveryVersion,
	}, nil
}

// NewClaimInput builds the server-owned input used by a new local-runtime or
// Gateway claim. The MCP discovery version is derived from the exact callable
// identities, so a later tools/list surface cannot be mistaken for the one
// that was finalized for this task.
func NewClaimInput(
	taskID, workspaceID, agentID pgtype.UUID,
	callableIdentities []string,
	connectionScopeIdentities []string,
	sourceVersion string,
) (ContractInput, error) {
	return newContractInputWithDiscoveryVersion(
		taskID, workspaceID, agentID, callableIdentities,
		connectionScopeIdentities, sourceVersion,
		MCPDiscoveryVersion(callableIdentities),
	)
}

// UnmarshalJSON rejects external request/model assembly. ContractInput is built
// only from the server-owned final callable and Connection inventory.
func (*ContractInput) UnmarshalJSON([]byte) error {
	return fmt.Errorf("task mandate contract input: JSON assembly is not allowed")
}

func normalizeContractIdentities(kind string, identities []string, valid func(string) bool) ([]string, error) {
	normalized := append([]string(nil), identities...)
	for _, identity := range normalized {
		if identity == "" || strings.TrimSpace(identity) != identity || (valid != nil && !valid(identity)) {
			return nil, fmt.Errorf("task mandate contract input: invalid %s identity %q", kind, identity)
		}
	}
	sort.Strings(normalized)
	return slices.Compact(normalized), nil
}

// TaskIdentity returns the exact task, workspace, and agent identity carried by
// this input.
func (in ContractInput) TaskIdentity() (pgtype.UUID, pgtype.UUID, pgtype.UUID) {
	return in.taskID, in.workspaceID, in.agentID
}

// CallableIdentities returns a copy of the exact final callable identities.
func (in ContractInput) CallableIdentities() []string {
	return append([]string(nil), in.callableIdentities...)
}

// PlatformOperationIdentities returns a copy of the exact callables that the
// server-owned platform catalog classifies as platform operations.
func (in ContractInput) PlatformOperationIdentities() []PlatformOperationIdentity {
	return append([]PlatformOperationIdentity(nil), in.platformOperationIdentities...)
}

// ConnectionScopeIdentities returns a copy of the canonical Connection scope
// identities associated with the final callable surface.
func (in ContractInput) ConnectionScopeIdentities() []string {
	return append([]string(nil), in.connectionScopeIdentities...)
}

// SourceVersion returns the exact inventory/discovery source version used to
// assemble this input.
func (in ContractInput) SourceVersion() string { return in.sourceVersion }

// DiscoveryVersion returns the exact MCP discovery version carried by this
// input. It is empty when the claim has no MCP callable identities.
func (in ContractInput) DiscoveryVersion() string { return in.discoveryVersion }

// MCPDiscoveryVersion returns a stable content version for the MCP callable
// surface. The version is intentionally based on exact provider spellings,
// including an explicit raw-server wildcard when one was granted.
func MCPDiscoveryVersion(callableIdentities []string) string {
	names := make([]string, 0, len(callableIdentities))
	for _, identity := range callableIdentities {
		identity = strings.TrimSpace(identity)
		if strings.HasPrefix(identity, "mcp__") {
			names = append(names, identity)
		}
	}
	sort.Strings(names)
	names = slices.Compact(names)
	if len(names) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(names, "\x00")))
	return "mcp:sha256:" + hex.EncodeToString(sum[:])
}

func discoveryVersionFromSource(sourceVersion string) string {
	for _, part := range strings.Split(sourceVersion, "/") {
		if strings.HasPrefix(part, "discovery:") && strings.TrimPrefix(part, "discovery:") != "" {
			return part
		}
	}
	return ""
}
