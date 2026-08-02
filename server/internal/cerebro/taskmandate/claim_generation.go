package taskmandate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ClaimLifecycleState records whether a claim generation predates generation
// metadata, is still being assembled, or has been finalized for enforcement.
type ClaimLifecycleState string

const (
	ClaimLifecycleLegacy    ClaimLifecycleState = "legacy"
	ClaimLifecycleDraft     ClaimLifecycleState = "draft"
	ClaimLifecycleFinalized ClaimLifecycleState = "finalized"
)

// ClaimGeneration is the additive generation metadata stored beside a task
// mandate. Legacy rows intentionally leave producer, finalizer, versions, and
// digest nil until a later claim is finalized through the generation contract.
type ClaimGeneration struct {
	Generation           int64
	Producer             *string
	Finalizer            *string
	LifecycleState       ClaimLifecycleState
	InventoryVersion     *string
	DiscoveryVersion     *string
	FinalizedGrantDigest *string
}

var (
	ErrIdentityMismatch        = errors.New("task mandate identity mismatch")
	ErrFinalizedGrantsChanged  = errors.New("task mandate finalized grants changed")
	ErrStaleFinalizationWriter = errors.New("task mandate stale finalization writer")
)

// FinalizeClaim creates the task's single immutable generation. An identical
// retry is idempotent; a different identity, grant set, or writer is rejected.
func (s *Store) FinalizeClaim(
	ctx context.Context,
	input ContractInput,
	producer, finalizer string,
	expiresAt time.Time,
) (ClaimGeneration, error) {
	taskID, workspaceID, agentID := input.TaskIdentity()
	if s == nil || s.db == nil || !taskID.Valid || !workspaceID.Valid || !agentID.Valid {
		return ClaimGeneration{}, fmt.Errorf("task mandate: invalid finalization input")
	}
	if producer == "" || strings.TrimSpace(producer) != producer || finalizer == "" || strings.TrimSpace(finalizer) != finalizer {
		return ClaimGeneration{}, fmt.Errorf("task mandate: invalid finalization writer")
	}
	expiresAt = expiresAt.UTC().Truncate(time.Microsecond)
	if !expiresAt.After(s.now()) {
		return ClaimGeneration{}, ErrExpired
	}

	tools := input.CallableIdentities()
	if tools == nil {
		tools = []string{}
	}
	rawTools, err := json.Marshal(tools)
	if err != nil {
		return ClaimGeneration{}, err
	}
	digest, err := contractGrantDigest(input)
	if err != nil {
		return ClaimGeneration{}, err
	}
	sourceVersion := input.SourceVersion()

	generation := ClaimGeneration{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO cerebro_task_mandate (
			task_id, workspace_id, agent_id, allowed_tools, expires_at,
			claim_generation, producer, finalizer, lifecycle_state,
			inventory_version, discovery_version, finalized_grant_digest
		)
		VALUES ($1, $2, $3, $4, $5, 1, $6, $7, 'finalized', $8, NULL, $9)
		ON CONFLICT (task_id) DO NOTHING
		RETURNING claim_generation, producer, finalizer, lifecycle_state,
		          inventory_version, discovery_version, finalized_grant_digest`,
		taskID, workspaceID, agentID, rawTools, expiresAt,
		producer, finalizer, sourceVersion, digest,
	).Scan(
		&generation.Generation,
		&generation.Producer,
		&generation.Finalizer,
		&generation.LifecycleState,
		&generation.InventoryVersion,
		&generation.DiscoveryVersion,
		&generation.FinalizedGrantDigest,
	)
	if err == nil {
		return generation, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ClaimGeneration{}, err
	}

	return s.matchFinalizedClaim(ctx, input, producer, finalizer, digest, expiresAt)
}

func (s *Store) matchFinalizedClaim(
	ctx context.Context,
	input ContractInput,
	producer, finalizer, digest string,
	expiresAt time.Time,
) (ClaimGeneration, error) {
	taskID, workspaceID, agentID := input.TaskIdentity()
	var (
		storedWorkspaceID pgtype.UUID
		storedAgentID     pgtype.UUID
		storedExpiresAt   time.Time
		generation        ClaimGeneration
	)
	err := s.db.QueryRow(ctx, `
		SELECT workspace_id, agent_id, expires_at, claim_generation, producer, finalizer,
		       lifecycle_state, inventory_version, discovery_version,
		       finalized_grant_digest
		FROM cerebro_task_mandate
		WHERE task_id = $1`, taskID,
	).Scan(
		&storedWorkspaceID,
		&storedAgentID,
		&storedExpiresAt,
		&generation.Generation,
		&generation.Producer,
		&generation.Finalizer,
		&generation.LifecycleState,
		&generation.InventoryVersion,
		&generation.DiscoveryVersion,
		&generation.FinalizedGrantDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClaimGeneration{}, ErrMissing
	}
	if err != nil {
		return ClaimGeneration{}, err
	}
	if storedWorkspaceID != workspaceID || storedAgentID != agentID {
		return ClaimGeneration{}, ErrIdentityMismatch
	}
	if generation.Generation != 1 ||
		generation.LifecycleState != ClaimLifecycleFinalized ||
		!storedExpiresAt.Equal(expiresAt) ||
		generation.Producer == nil || *generation.Producer != producer ||
		generation.Finalizer == nil || *generation.Finalizer != finalizer ||
		generation.InventoryVersion == nil || *generation.InventoryVersion != input.SourceVersion() ||
		generation.DiscoveryVersion != nil {
		return ClaimGeneration{}, ErrStaleFinalizationWriter
	}
	if generation.FinalizedGrantDigest == nil || *generation.FinalizedGrantDigest != digest {
		return ClaimGeneration{}, ErrFinalizedGrantsChanged
	}
	return generation, nil
}

func contractGrantDigest(input ContractInput) (string, error) {
	platformOperations := input.PlatformOperationIdentities()
	bindings := make([][2]string, 0, len(platformOperations))
	for _, operation := range platformOperations {
		bindings = append(bindings, [2]string{operation.CallableIdentity(), operation.CapabilityKey()})
	}
	payload, err := json.Marshal(struct {
		Callables          []string    `json:"callables"`
		PlatformOperations [][2]string `json:"platform_operations"`
		ConnectionScopes   []string    `json:"connection_scopes"`
	}{
		Callables:          append([]string{}, input.CallableIdentities()...),
		PlatformOperations: bindings,
		ConnectionScopes:   append([]string{}, input.ConnectionScopeIdentities()...),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum), nil
}
