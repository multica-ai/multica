package agentoffice

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/versioning"
)

// The stale-proposal / not-pending sentinels and their HTTP mapping moved to
// the shared versioning package (FIR-2698); the local names below keep the
// call sites unchanged.
var (
	errStaleProposal = versioning.ErrStaleProposal
	errNotPending    = versioning.ErrNotPending
	// errIncompatibleSystemPromptMode (FIR-3212) is a caller error, not a
	// conflict: the proposal asks for a mode the agent's runtime cannot honour.
	errIncompatibleSystemPromptMode = errors.New("incompatible system_prompt_mode")
)

// approveAndMerge applies a pending change request in one transaction: lock the
// row, re-validate semver against the freshly-read agent, write the snapshot onto
// the agent, append the version, and mark the change request merged. Two
// reviewers racing on the same proposal can't both win — the FOR UPDATE lock plus
// the status re-check serialize them.
func (h *Handler) approveAndMerge(r *http.Request, agent cerebrodb.Agent, cr cerebrodb.AgentChangeRequest, reviewerID pgtype.UUID, comment string) (cerebrodb.AgentChangeRequest, error) {
	ctx := r.Context()
	tx, err := h.Svc.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.AgentChangeRequest{}, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Svc.Cerebro.WithTx(tx)

	locked, err := qtx.GetAgentChangeRequestForUpdate(ctx, cr.ID)
	if err != nil {
		return cerebrodb.AgentChangeRequest{}, fmt.Errorf("failed to lock change request: %w", err)
	}
	if locked.Status != "pending" {
		return cerebrodb.AgentChangeRequest{}, errNotPending
	}

	fresh, err := qtx.GetAgentContextInWorkspace(ctx, cerebrodb.GetAgentContextInWorkspaceParams{
		ID:          agent.ID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		return cerebrodb.AgentChangeRequest{}, fmt.Errorf("failed to reload agent: %w", err)
	}
	if !SemverGT(locked.ProposedVersion, fresh.ContextVersion) {
		return cerebrodb.AgentChangeRequest{}, errStaleProposal
	}

	snap := DecodeSnapshot(locked.ProposedSnapshot)
	// FIR-3212: re-check the mode against the agent's CURRENT runtime, inside the
	// tx. The create path already checked it, but an agent can be moved to another
	// runtime between proposing and approving — so a combination that was valid at
	// propose time can be impossible by the time it is rolled out. Without this,
	// approval is exactly the path that ships a setting the runtime silently drops.
	if mode, present := rawSystemPromptMode(snap); present {
		provider, perr := qtx.GetAgentProvider(ctx, agent.ID)
		if perr != nil {
			// Unresolvable runtime is "no authoritative answer", never a rejection.
			provider = ""
		}
		if verr := ValidateSystemPromptModeForProvider(provider, mode); verr != nil {
			return cerebrodb.AgentChangeRequest{}, fmt.Errorf("%w: %s", errIncompatibleSystemPromptMode, verr)
		}
	}
	if _, err := h.Svc.ApplySnapshotTx(ctx, qtx, agent.ID, snap, locked.ProposedVersion); err != nil {
		return cerebrodb.AgentChangeRequest{}, err
	}
	if _, err := qtx.CreateAgentContextVersion(ctx, cerebrodb.CreateAgentContextVersionParams{
		AgentID:     agent.ID,
		Version:     locked.ProposedVersion,
		Snapshot:    locked.ProposedSnapshot,
		Description: locked.Title,
		CreatedBy:   reviewerID,
	}); err != nil {
		return cerebrodb.AgentChangeRequest{}, fmt.Errorf("failed to snapshot version: %w", err)
	}
	updated, err := qtx.ReviewAgentChangeRequest(ctx, cerebrodb.ReviewAgentChangeRequestParams{
		ID:            locked.ID,
		Status:        "merged",
		ReviewedBy:    reviewerID,
		ReviewComment: comment,
	})
	if err != nil {
		return cerebrodb.AgentChangeRequest{}, fmt.Errorf("failed to mark merged: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.AgentChangeRequest{}, fmt.Errorf("failed to commit: %w", err)
	}
	return updated, nil
}

// statusForMergeError maps a merge error to an HTTP status.
func statusForMergeError(err error) int {
	// FIR-3212: an incompatible mode is the caller's input being wrong, so it is
	// a 400 — not the 409 the versioning sentinels mean, nor a 500.
	if errors.Is(err, errIncompatibleSystemPromptMode) {
		return http.StatusBadRequest
	}
	return versioning.StatusForMergeError(err)
}
