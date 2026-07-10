package modelregistry

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/versioning"
)

// approveAndMerge applies a pending change request in one transaction: lock
// the change-request row AND the singleton registry row, re-validate semver
// against the freshly-read registry, write the snapshot, append the version,
// and mark the change request merged. Two reviewers racing on the same
// proposal can't both win — the FOR UPDATE locks plus the status re-check
// serialize them. On success the new table is published to the in-process
// store (and thereby pkg/pricing).
func (h *Handler) approveAndMerge(r *http.Request, cr cerebrodb.ModelRegistryChangeRequest, reviewerID pgtype.UUID, comment string) (cerebrodb.ModelRegistryChangeRequest, error) {
	ctx := r.Context()
	tx, err := h.Svc.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Svc.Cerebro.WithTx(tx)

	locked, err := qtx.GetModelRegistryChangeRequestForUpdate(ctx, cr.ID)
	if err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("failed to lock change request: %w", err)
	}
	if locked.Status != "pending" {
		return cerebrodb.ModelRegistryChangeRequest{}, versioning.ErrNotPending
	}

	fresh, err := qtx.GetModelRegistryForUpdate(ctx)
	if err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("failed to reload registry: %w", err)
	}
	if !versioning.SemverGT(locked.ProposedVersion, fresh.CurrentVersion) {
		return cerebrodb.ModelRegistryChangeRequest{}, versioning.ErrStaleProposal
	}

	snap := DecodeSnapshot(locked.ProposedSnapshot)
	if err := ValidateSnapshot(snap); err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("proposed snapshot is not valid: %w", err)
	}
	if _, err := h.Svc.ApplySnapshotTx(ctx, qtx, snap, locked.ProposedVersion); err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, err
	}
	if _, err := qtx.CreateModelRegistryVersion(ctx, cerebrodb.CreateModelRegistryVersionParams{
		RegistryID:  locked.RegistryID,
		Version:     locked.ProposedVersion,
		Snapshot:    locked.ProposedSnapshot,
		Description: locked.Title,
		CreatedBy:   reviewerID,
	}); err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("failed to snapshot version: %w", err)
	}
	updated, err := qtx.ReviewModelRegistryChangeRequest(ctx, cerebrodb.ReviewModelRegistryChangeRequestParams{
		ID:            locked.ID,
		Status:        "merged",
		ReviewedBy:    reviewerID,
		ReviewComment: comment,
	})
	if err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("failed to mark merged: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.ModelRegistryChangeRequest{}, fmt.Errorf("failed to commit: %w", err)
	}
	// Make the approved table live for this process immediately; other
	// replicas converge via the periodic refresher.
	Publish(snap, locked.ProposedVersion)
	return updated, nil
}
