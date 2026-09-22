package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (s *Service) ActivateRelease(ctx context.Context, ws, user, wid, releaseID string, expectedRevision int64) (Workflow, error) {
	return s.activateRelease(ctx, ws, user, wid, releaseID, expectedRevision, "")
}

func (s *Service) ActivateReleaseKey(ctx context.Context, ws, user, wid, releaseID string, expectedRevision int64, idempotencyKey string) (Workflow, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Workflow{}, Bad("idempotency_key is required")
	}
	return s.activateRelease(ctx, ws, user, wid, releaseID, expectedRevision, idempotencyKey)
}

func (s *Service) activateRelease(ctx context.Context, ws, user, wid, releaseID string, expectedRevision int64, idempotencyKey string) (Workflow, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback(ctx)
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return Workflow{}, err
	}
	if err = requireWorkflowManager(ctx, tx, user, w); err != nil {
		return Workflow{}, err
	}
	requestHash := workflowCommandHash("workflow_release.activate", wid, struct {
		ReleaseID        string `json:"release_id"`
		ExpectedRevision int64  `json:"expected_revision"`
	}{ReleaseID: releaseID, ExpectedRevision: expectedRevision})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, user, "workflow_release.activate", wid, idempotencyKey, requestHash)
	if err != nil {
		return Workflow{}, err
	}
	if len(responseBody) > 0 {
		var replay Workflow
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return Workflow{}, err
		}
		return replay, nil
	}
	if expectedRevision > 0 && expectedRevision != w.Revision {
		return Workflow{}, Conflict("workflow changed; refresh before activating a release")
	}
	release, graph, _, err := loadRelease(ctx, tx, ws, wid, releaseID)
	if err != nil {
		return Workflow{}, err
	}
	if err = Validate(graph, true); err != nil {
		return Workflow{}, err
	}
	if err = requireV2Graph(graph); err != nil {
		return Workflow{}, err
	}
	if err = s.validateAgents(ctx, ws, user, graph, true); err != nil {
		return Workflow{}, err
	}
	if err = s.validateMembers(ctx, ws, graph, true); err != nil {
		return Workflow{}, err
	}
	w.PublishedReleaseID = release.ID
	w.UpdatedAt = time.Now().UTC()
	if _, err = tx.Exec(ctx, `UPDATE workflow SET body=$3,published_release_id=$4,graph_schema_version=$5,updated_at=now() WHERE workspace_id=$1 AND id=$2`, ws, wid, body(w), release.ID, release.GraphSchemaVersion); err != nil {
		return Workflow{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, user, "workflow_release.activate", wid, idempotencyKey, requestHash, w); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Workflow{}, err
	}
	s.publish("workflow:updated", ws, wid, "")
	return w, nil
}

func (s *Service) CopyReleaseToDraft(ctx context.Context, ws, user, wid, releaseID string, expectedRevision int64) (Workflow, error) {
	return s.copyReleaseToDraft(ctx, ws, user, wid, releaseID, expectedRevision, "")
}

func (s *Service) CopyReleaseToDraftKey(ctx context.Context, ws, user, wid, releaseID string, expectedRevision int64, idempotencyKey string) (Workflow, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Workflow{}, Bad("idempotency_key is required")
	}
	return s.copyReleaseToDraft(ctx, ws, user, wid, releaseID, expectedRevision, idempotencyKey)
}

func (s *Service) copyReleaseToDraft(ctx context.Context, ws, user, wid, releaseID string, expectedRevision int64, idempotencyKey string) (Workflow, error) {
	w, err := s.Get(ctx, ws, wid)
	if err != nil {
		return Workflow{}, err
	}
	if err = requireWorkflowManager(ctx, s.DB, user, w); err != nil {
		return Workflow{}, err
	}
	_, graph, _, err := s.GetRelease(ctx, ws, wid, releaseID)
	if err != nil {
		return Workflow{}, err
	}
	if idempotencyKey == "" {
		return s.Edit(ctx, ws, user, wid, Edit{Graph: &graph, ExpectedRevision: expectedRevision}, "")
	}
	return s.EditKey(ctx, ws, user, wid, Edit{Graph: &graph, ExpectedRevision: expectedRevision}, "copy_release:"+releaseID, idempotencyKey)
}

func (s *Service) Archive(ctx context.Context, ws, user, wid string, expectedRevision int64, archived bool) (Workflow, error) {
	return s.archiveWorkflow(ctx, ws, user, wid, expectedRevision, archived, "")
}

func (s *Service) ArchiveKey(ctx context.Context, ws, user, wid string, expectedRevision int64, archived bool, idempotencyKey string) (Workflow, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Workflow{}, Bad("idempotency_key is required")
	}
	return s.archiveWorkflow(ctx, ws, user, wid, expectedRevision, archived, idempotencyKey)
}

func (s *Service) archiveWorkflow(ctx context.Context, ws, user, wid string, expectedRevision int64, archived bool, idempotencyKey string) (Workflow, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback(ctx)
	w, _, _, err := load(ctx, tx, ws, wid, true)
	if err != nil {
		return Workflow{}, err
	}
	if err = requireWorkflowManager(ctx, tx, user, w); err != nil {
		return Workflow{}, err
	}
	requestHash := workflowCommandHash("workflow.archive", wid, struct {
		ExpectedRevision int64 `json:"expected_revision"`
		Archived         bool  `json:"archived"`
	}{ExpectedRevision: expectedRevision, Archived: archived})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, user, "workflow.archive", wid, idempotencyKey, requestHash)
	if err != nil {
		return Workflow{}, err
	}
	if len(responseBody) > 0 {
		var replay Workflow
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return Workflow{}, err
		}
		return replay, nil
	}
	if expectedRevision > 0 && expectedRevision != w.Revision {
		return Workflow{}, Conflict("workflow changed; refresh before archiving")
	}
	if archived {
		now := time.Now().UTC()
		w.ArchivedAt = &now
	} else {
		w.ArchivedAt = nil
	}
	if _, err = tx.Exec(ctx, "UPDATE workflow SET body=$3,archived_at=$4,updated_at=now() WHERE workspace_id=$1 AND id=$2", ws, wid, body(w), w.ArchivedAt); err != nil {
		return Workflow{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, user, "workflow.archive", wid, idempotencyKey, requestHash, w); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Workflow{}, err
	}
	s.publish("workflow:updated", ws, wid, "")
	return w, nil
}
