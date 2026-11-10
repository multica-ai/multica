package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func issueBlockerGraphTouched(fields map[string]json.RawMessage) bool {
	for _, key := range []string{"blocked_by", "remove_blocked_by", "parent_issue_id", "stage"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

type IssueBlockerResponse struct {
	IssueRefResponse
	Via    string `json:"via"`
	Closed bool   `json:"closed"`
}

func writeIssueBlockerError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, service.ErrIssueBlockerNotFound) {
		writeError(w, 400, err.Error())
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.HasPrefix(pgErr.ConstraintName, "issue_blocker_") {
		writeError(w, http.StatusBadRequest, pgErr.Message)
		return true
	}
	return false
}

func (h *Handler) parseBlockerIDs(w http.ResponseWriter, values []string) ([]pgtype.UUID, bool) {
	if len(values) > 100 {
		writeError(w, http.StatusBadRequest, "at most 100 blockers per request")
		return nil, false
	}
	ids := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		id, ok := parseUUIDOrBadRequest(w, value, "blocked_by")
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// A page loads all its blocker references in one query; single-item callers
// can lazily fill the one missing issue through the same response path.
func (h *Handler) newIssueBlockerFiller(ctx context.Context, ws pgtype.UUID, ids []pgtype.UUID) func(*IssueResponse) {
	cache := map[string][]IssueBlockerResponse{}
	load := func(ids []pgtype.UUID) {
		for _, id := range ids {
			cache[uuidToString(id)] = []IssueBlockerResponse{}
		}
		rows, err := h.Queries.ListIssueBlockers(ctx, db.ListIssueBlockersParams{WorkspaceID: ws, IssueIds: ids})
		if err != nil {
			return
		}
		prefix := h.getIssuePrefix(ctx, ws)
		for _, row := range rows {
			key := uuidToString(row.IssueID)
			cache[key] = append(cache[key], IssueBlockerResponse{IssueRefResponse: IssueRefResponse{ID: uuidToString(row.ID), Identifier: fmt.Sprintf("%s-%d", prefix, row.Number), Title: row.Title, Status: row.Status}, Via: row.Via, Closed: row.Closed.Bool})
		}
	}
	if len(ids) > 0 {
		load(ids)
	}
	return func(resp *IssueResponse) {
		if _, ok := cache[resp.ID]; !ok {
			load([]pgtype.UUID{parseUUID(resp.ID)})
		}
		resp.BlockedBy = cache[resp.ID]
		resp.BlockedCount = 0
		for _, b := range resp.BlockedBy {
			if !b.Closed {
				resp.BlockedCount++
			}
		}
	}
}
