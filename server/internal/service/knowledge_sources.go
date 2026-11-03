package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

// KnowledgeSourceView is one sync connection that feeds a knowledge base. The
// connection is read from WeKnora; creating one happens in WeKnora's own UI.
type KnowledgeSourceView struct {
	ID               string     `json:"id"`
	KnowledgeBase    string     `json:"knowledge_base"`
	Name             string     `json:"name"`
	Type             string     `json:"type"`
	Status           string     `json:"status"`
	SyncSchedule     string     `json:"sync_schedule"`
	LastSyncAt       *time.Time `json:"last_sync_at"`
	ErrorMessage     string     `json:"error_message"`
	TotalItemsSynced int64      `json:"total_items_synced"`
}

// KnowledgeSourcesView is the Sources panel. State is "ready", or "offline" when
// WeKnora did not answer; UIURL is where a source is created.
type KnowledgeSourcesView struct {
	State   string                `json:"state"`
	UIURL   string                `json:"ui_url"`
	Sources []KnowledgeSourceView `json:"sources"`
}

// ListSources reads the data sources of every knowledge base. WeKnora not
// answering is the "offline" state, not an error.
func (s *KnowledgeService) ListSources(ctx context.Context, workspaceID pgtype.UUID) (KnowledgeSourcesView, error) {
	out := KnowledgeSourcesView{State: KnowledgeStateReady, UIURL: s.UIURL, Sources: []KnowledgeSourceView{}}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return out, fmt.Errorf("list knowledge bases: %w", err)
	}
	for _, b := range bases {
		list, err := ws.ListDataSources(ctx, b.WeknoraKbID)
		if errors.Is(err, knowledge.ErrUnavailable) {
			out.State = KnowledgeStateOffline
			return out, nil
		}
		if err != nil {
			return out, err
		}
		for _, d := range list {
			out.Sources = append(out.Sources, KnowledgeSourceView{
				ID: d.ID, KnowledgeBase: b.Key, Name: d.Name, Type: d.Type, Status: d.Status,
				SyncSchedule: d.SyncSchedule, LastSyncAt: d.LastSyncAt, ErrorMessage: d.ErrorMessage,
				TotalItemsSynced: d.TotalItemsSynced,
			})
		}
	}
	return out, nil
}
