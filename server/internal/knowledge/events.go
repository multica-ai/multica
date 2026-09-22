package knowledge

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// notifyKnowledgeInvalidation publishes the small, permission-aware event
// consumed by the clients' React Query cache. Recipient IDs are intentionally
// kept in the in-process payload only; listeners remove them before the frame
// is serialized so a private base never discloses its ACL to another member.
func (s *Service) notifyKnowledgeInvalidation(ctx context.Context, workspaceID, baseID, actorID string, extraRecipients ...string) {
	if s == nil || s.eventBus == nil || s.db == nil {
		return
	}
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		slog.Warn("knowledge invalidation skipped: invalid workspace", "workspace_id", workspaceID, "error", err)
		return
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		slog.Warn("knowledge invalidation skipped: invalid base", "knowledge_base_id", baseID, "error", err)
		return
	}

	var visibility, creatorID string
	var revision, aclRevision int64
	err = s.db.QueryRow(ctx, `
		SELECT visibility, creator_id::text, revision, acl_revision
		FROM knowledge_base
		WHERE id=$1 AND workspace_id=$2`, base, workspace).Scan(&visibility, &creatorID, &revision, &aclRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		// A hard delete is not part of the knowledge contract, but avoiding an
		// event without an authoritative revision is safer than inventing one.
		return
	}
	if err != nil {
		slog.Warn("knowledge invalidation skipped: failed to load base state", "workspace_id", workspaceID, "knowledge_base_id", baseID, "error", err)
		return
	}

	recipients := uniqueKnowledgeRecipients(extraRecipients...)
	if visibility == VisibilityWorkspace {
		members, memberErr := s.workspaceMemberIDs(ctx, workspace)
		if memberErr != nil {
			slog.Warn("knowledge invalidation skipped: failed to load workspace members", "workspace_id", workspaceID, "knowledge_base_id", baseID, "error", memberErr)
			return
		}
		recipients = uniqueKnowledgeRecipients(append(recipients, members...)...)
	} else {
		recipients = uniqueKnowledgeRecipients(append(recipients, creatorID)...)
	}
	if len(recipients) == 0 {
		return
	}

	actorType := "system"
	if actorID != "" {
		actorType = "member"
	}
	s.eventBus.Publish(events.Event{
		Type:        protocol.EventKnowledgeInvalidate,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"workspace_id":      workspaceID,
			"knowledge_base_id": baseID,
			"revision":          revision,
			"acl_revision":      aclRevision,
			// This key is consumed by the in-process listener and must never
			// cross the WebSocket boundary.
			"recipient_ids": recipients,
		},
	})
}

func (s *Service) workspaceMemberIDs(ctx context.Context, workspaceID any) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT user_id::text FROM member WHERE workspace_id=$1`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return uniqueKnowledgeRecipients(ids...), nil
}

func uniqueKnowledgeRecipients(ids ...string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// notifyWorkspaceInvalidations is used for workspace-level model/provider
// changes. Each base still gets its own revision-bearing event, which lets the
// client invalidate only the affected workspace/base cache while preserving
// the private-base recipient boundary.
func (s *Service) notifyWorkspaceInvalidations(ctx context.Context, workspaceID, actorID string) {
	if s == nil || s.eventBus == nil || s.db == nil {
		return
	}
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return
	}
	rows, err := s.db.Query(ctx, `SELECT id::text FROM knowledge_base WHERE workspace_id=$1 AND deleted_at IS NULL`, workspace)
	if err != nil {
		slog.Warn("knowledge workspace invalidation skipped: failed to list bases", "workspace_id", workspaceID, "error", err)
		return
	}
	defer rows.Close()
	baseIDs := make([]string, 0)
	for rows.Next() {
		var baseID string
		if scanErr := rows.Scan(&baseID); scanErr != nil {
			return
		}
		baseIDs = append(baseIDs, baseID)
	}
	if rows.Err() != nil {
		return
	}
	for _, baseID := range baseIDs {
		s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	}
}
