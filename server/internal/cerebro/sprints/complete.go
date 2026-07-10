package sprints

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Ways to handle the issues still assigned to a sprint at completion time.
// FIR-2828: there was previously no way to end a sprint at all, so every
// completion silently left remaining issues where they were.
const (
	IncompleteLeave        = "leave"
	IncompleteBacklog      = "backlog"
	IncompleteMoveToSprint = "move_to_sprint"
)

func validIncompleteIssuesAction(a string) bool {
	switch a {
	case IncompleteLeave, IncompleteBacklog, IncompleteMoveToSprint:
		return true
	default:
		return false
	}
}

type completeSprintRequest struct {
	IncompleteIssuesAction string  `json:"incomplete_issues_action"`
	TargetSprintID         *string `json:"target_sprint_id,omitempty"`
}

type completeSprintResponse struct {
	Sprint      SprintResponse `json:"sprint"`
	IssuesMoved int            `json:"issues_moved"`
}

// CompleteSprint marks a sprint done and applies the chosen handling for any
// issue still assigned to it: leave in place, move to the backlog, or move
// into another open sprint. This is the operator-facing counterpart to the
// sweeper's automatic end-of-sprint handling in sweeper.go.
func (h *Handler) CompleteSprint(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	sprintID, ok := parseUUIDParam(w, r, "sprintID")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroSprint(r.Context(), sprintID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load sprint failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}
	if existing.Status == StatusDone || existing.Status == StatusCancelled {
		writeError(w, http.StatusBadRequest, "sprint is already "+existing.Status)
		return
	}

	var req completeSprintRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	action := strings.TrimSpace(req.IncompleteIssuesAction)
	if action == "" {
		action = IncompleteLeave
	}
	if !validIncompleteIssuesAction(action) {
		writeError(w, http.StatusBadRequest, "invalid incomplete_issues_action")
		return
	}

	var targetSprint cerebrodb.CerebroSprint
	if action == IncompleteMoveToSprint {
		if req.TargetSprintID == nil || strings.TrimSpace(*req.TargetSprintID) == "" {
			writeError(w, http.StatusBadRequest, "target_sprint_id is required for move_to_sprint")
			return
		}
		targetUUID, err := util.ParseUUID(*req.TargetSprintID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid target_sprint_id")
			return
		}
		if uuidEqual(targetUUID, sprintID) {
			writeError(w, http.StatusBadRequest, "target sprint must differ from the sprint being completed")
			return
		}
		targetSprint, err = h.Cerebro.GetCerebroSprint(r.Context(), targetUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "target sprint not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "load target sprint failed")
			return
		}
		if !uuidEqual(targetSprint.ProjectID, existing.ProjectID) {
			writeError(w, http.StatusBadRequest, "target sprint must belong to the same project")
			return
		}
		if targetSprint.Status == StatusDone || targetSprint.Status == StatusCancelled {
			writeError(w, http.StatusBadRequest, "target sprint is not open")
			return
		}
	}

	var (
		updated     cerebrodb.CerebroSprint
		issuesMoved int
	)
	err = h.runInTx(r.Context(), func(ctx context.Context, cqtx *cerebrodb.Queries) error {
		ids, err := cqtx.ListIncompleteIssuesInCerebroSprint(ctx, sprintID)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			switch action {
			case IncompleteBacklog:
				if err := cqtx.MoveIncompleteCerebroSprintIssuesToStatus(ctx, cerebrodb.MoveIncompleteCerebroSprintIssuesToStatusParams{
					Column1: ids,
					Status:  "backlog",
				}); err != nil {
					return err
				}
				if err := cqtx.RemoveCerebroSprintIssuesBatch(ctx, ids); err != nil {
					return err
				}
				issuesMoved = len(ids)
			case IncompleteMoveToSprint:
				if err := cqtx.MoveCerebroSprintIssuesBatch(ctx, cerebrodb.MoveCerebroSprintIssuesBatchParams{
					SprintID: targetSprint.ID,
					Column2:  ids,
				}); err != nil {
					return err
				}
				issuesMoved = len(ids)
			case IncompleteLeave:
				// No-op: issues stay assigned to the now-completed sprint.
			}
		}

		if err := cqtx.SetCerebroSprintStatus(ctx, cerebrodb.SetCerebroSprintStatusParams{
			ID:     sprintID,
			Status: StatusDone,
		}); err != nil {
			return err
		}
		updated, err = cqtx.GetCerebroSprint(ctx, sprintID)
		return err
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "complete sprint failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, completeSprintResponse{
		Sprint:      sprintToResponse(updated),
		IssuesMoved: issuesMoved,
	})
}

// runInTx wraps fn in a transaction against h.Pool, handing it a
// transaction-scoped Queries so every call inside fn commits or rolls back
// atomically. Mirrors Sweeper.runInTx in sweeper.go.
func (h *Handler) runInTx(ctx context.Context, fn func(context.Context, *cerebrodb.Queries) error) error {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(ctx, h.Cerebro.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
