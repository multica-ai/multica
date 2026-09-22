package workflow

import (
	"context"
	"strings"
)

func (s *Service) PreviewUpgrade(ctx context.Context, ws, wid string) (GraphUpgradeResult, error) {
	w, err := s.Get(ctx, ws, wid)
	if err != nil {
		return GraphUpgradeResult{}, err
	}
	if graphVersion(w.Graph) >= CurrentGraphSchemaVersion {
		result := UpgradeV1Graph(w.Graph)
		result.AutoConvertible = true
		return result, nil
	}
	return UpgradeV1Graph(w.Graph), nil
}

// UpgradeDraftKey applies exactly the graph shown by PreviewUpgrade, recording
// any semantic repair items on the draft. The normal edit transaction still
// owns revision checks, history, and the command response, so applying a
// preview is safe to retry and cannot overwrite a newer edit.
func (s *Service) UpgradeDraftKey(ctx context.Context, ws, user, wid string, expectedRevision int64, idempotencyKey string) (Workflow, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Workflow{}, Bad("idempotency_key is required")
	}
	w, err := s.Get(ctx, ws, wid)
	if err != nil {
		return Workflow{}, err
	}
	result := UpgradeV1Graph(w.Graph)
	if result.SourceSchemaVersion >= CurrentGraphSchemaVersion {
		return Workflow{}, Bad("workflow is already using schema v2")
	}
	issues := append([]ValidationDetail(nil), result.Issues...)
	return s.EditKey(ctx, ws, user, wid, Edit{
		Graph:            &result.Graph,
		ExpectedRevision: expectedRevision,
		UpgradeIssues:    &issues,
	}, "upgrade_v1", idempotencyKey)
}
