package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// SessionBrowseFilter selects a workspace directory, archive state and search.
// A nil GroupID means all directories; an empty ID means unassigned sessions.
type SessionBrowseFilter struct {
	GroupID   *string `json:"group_id"`
	Archived  bool    `json:"archived"`
	Search    string  `json:"search"`
	ExcludeID string  `json:"exclude_id"`
	SourceID  string  `json:"source_id"`
}

type SessionBrowsePosition struct {
	ID     string `json:"id"`
	Pinned int64  `json:"pinned"`
	SortAt string `json:"sort_at"`
}

type SessionBrowseGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Total int64  `json:"total"`
}

// SessionGroups reads counts and folder metadata without loading summaries.
func (s *Store) SessionGroups(ctx context.Context, scopeID int64, filter SessionBrowseFilter) ([]SessionBrowseGroup, int64, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	seq, err := q.GetScopeSeq(ctx, scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	folders, err := q.ListFolderProjects(ctx, scopeID)
	if err != nil {
		return nil, 0, err
	}
	counts, err := q.CountSessionBrowseGroups(ctx, sqlitedb.CountSessionBrowseGroupsParams{ScopeID: scopeID, Archived: boolInt(filter.Archived), Search: filter.Search})
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]int64, len(counts))
	for _, count := range counts {
		byID[count.GroupID] = count.Total
	}
	groups := make([]SessionBrowseGroup, 0, len(folders)+1)
	showEmpty := !filter.Archived && filter.Search == ""
	for _, folder := range folders {
		if byID[folder.ID] > 0 || showEmpty {
			groups = append(groups, SessionBrowseGroup{ID: folder.ID, Title: folder.Title, Path: folder.Path, Total: byID[folder.ID]})
		}
	}
	if byID[""] > 0 || (showEmpty && len(groups) > 0) {
		groups = append(groups, SessionBrowseGroup{Total: byID[""]})
	}
	return groups, seq, tx.Commit()
}

// SessionPage uses a seek position so tied timestamps and changing counts do
// not turn the page number into a different offset. Read one lookahead row.
func (s *Store) SessionPage(ctx context.Context, scopeID int64, filter SessionBrowseFilter, after SessionBrowsePosition, limit int) ([]ConversationSummary, int64, bool, error) {
	if limit < 1 || limit > 100 {
		return nil, 0, false, errors.New("invalid session page size")
	}
	sortAt := ""
	if after.ID != "" {
		at, err := time.Parse(time.RFC3339Nano, after.SortAt)
		if err != nil {
			return nil, 0, false, errors.New("invalid session cursor time")
		}
		// Match the fixed-width UTC key used by BrowseSessionPage.
		sortAt = at.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, false, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	seq, err := q.GetScopeSeq(ctx, scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, false, ErrNotFound
	}
	if err != nil {
		return nil, 0, false, err
	}
	groupID := ""
	if filter.GroupID != nil {
		groupID = *filter.GroupID
	}
	rows, err := q.BrowseSessionPage(ctx, sqlitedb.BrowseSessionPageParams{ScopeID: scopeID, Archived: boolInt(filter.Archived), FilterGroup: boolInt(filter.GroupID != nil), GroupID: groupID, ExcludeID: filter.ExcludeID, SourceID: filter.SourceID, Search: filter.Search, CursorID: after.ID, CursorPinned: after.Pinned, CursorSortAt: sortAt, RowLimit: int64(limit + 1)})
	if err != nil {
		return nil, 0, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	summaries := make([]ConversationSummary, len(rows))
	for i, row := range rows {
		summaries[i] = ConversationSummary{Conversation: row.Conversation, LastTurnSentAt: row.LastTurnSentAt}
	}
	return summaries, seq, hasMore, tx.Commit()
}
