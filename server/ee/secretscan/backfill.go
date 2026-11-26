package secretscan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/ee/secretscan/scandb"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const backfillBatch = 500

// StartBackfill scans the workspace's existing comments in the background.
// It records what it finds for people to act on and never rewrites a
// comment, whatever the workspace's mode.
func (s *Service) StartBackfill(w http.ResponseWriter, r *http.Request) {
	c, ok := s.admin(w, r)
	if !ok {
		return
	}
	if _, err := s.q.EnsureSetting(r.Context(), c.workspace); err != nil {
		slog.Warn("secret scan: load setting", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to start scan")
		return
	}
	setting, err := s.q.StartBackfill(r.Context(), c.workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		handler.WriteError(w, http.StatusConflict, "a scan is already running")
		return
	}
	if err != nil {
		slog.Warn("secret scan: start backfill", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to start scan")
		return
	}
	actorType, actorID := s.h.Actor(r, c.userID, c.workspaceID)
	s.h.Publish(EventChanged, c.workspaceID, actorType, actorID, map[string]any{})
	go s.backfill(setting.WorkspaceID, setting.Salt)
	handler.WriteJSON(w, http.StatusAccepted, settingsResponse(setting))
}

func (s *Service) backfill(workspace pgtype.UUID, salt []byte) {
	ctx := context.Background()
	workspaceID := util.UUIDToString(workspace)
	status := "failed"
	defer func() {
		if p := recover(); p != nil {
			slog.Error("secret scan: backfill panicked", "workspace_id", workspaceID, "panic", fmt.Sprint(p))
		}
		if err := s.q.FinishBackfill(ctx, scandb.FinishBackfillParams{WorkspaceID: workspace, BackfillStatus: status}); err != nil {
			slog.Warn("secret scan: finish backfill", "workspace_id", workspaceID, "error", err)
		}
		s.h.Publish(EventChanged, workspaceID, "system", "", map[string]any{})
	}()

	var cursor pgtype.UUID
	for {
		rows, err := s.q.ListCommentsAfter(ctx, scandb.ListCommentsAfterParams{WorkspaceID: workspace, Cursor: cursor, Batch: backfillBatch})
		if err != nil {
			slog.Warn("secret scan: backfill batch", "workspace_id", workspaceID, "error", err)
			return
		}
		if len(rows) == 0 {
			status = "done"
			return
		}
		touched := map[pgtype.UUID]bool{}
		for _, c := range rows {
			for _, f := range findingsOf(salt, redact.Find(c.Content)) {
				if err := s.q.RecordFinding(ctx, scandb.RecordFindingParams{
					WorkspaceID: workspace,
					IssueID:     c.IssueID,
					CommentID:   c.ID,
					AuthorType:  c.AuthorType,
					AuthorID:    c.AuthorID,
					Rule:        f.rule,
					Masked:      f.masked,
					Fingerprint: f.fingerprint,
					Status:      StatusOpen,
				}); err != nil {
					slog.Warn("secret scan: backfill record", "comment_id", util.UUIDToString(c.ID), "error", err)
					return
				}
				touched[c.IssueID] = true
			}
		}
		cursor = rows[len(rows)-1].ID
		if err := s.q.AdvanceBackfill(ctx, scandb.AdvanceBackfillParams{WorkspaceID: workspace, Cursor: cursor, Scanned: int64(len(rows))}); err != nil {
			slog.Warn("secret scan: backfill progress", "workspace_id", workspaceID, "error", err)
			return
		}
		for issueID := range touched {
			s.publishIssue(workspaceID, issueID, "system", "")
		}
	}
}
