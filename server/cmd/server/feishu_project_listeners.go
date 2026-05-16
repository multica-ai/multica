package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func registerFeishuProjectListeners(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["status_changed"] != true {
			return
		}
		if source, _ := payload["source"].(string); source == handler.FeishuProjectLocalStatusUpdateSource {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok || issue.ID == "" || issue.Status == "" {
			return
		}
		go syncFeishuProjectStatus(queries, issue.WorkspaceID, issue.ID, issue.Status)
	})
}

func syncFeishuProjectStatus(queries *db.Queries, workspaceID, issueID, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	binding, err := queries.GetFeishuProjectIssueBindingByIssue(ctx, db.GetFeishuProjectIssueBindingByIssueParams{
		WorkspaceID: util.MustParseUUID(workspaceID),
		IssueID:     util.MustParseUUID(issueID),
	})
	if err != nil {
		return
	}
	cfg, err := queries.GetFeishuProjectIntegrationByID(ctx, binding.IntegrationID)
	if err != nil || !cfg.Enabled {
		return
	}
	target := service.MapMulticaStatusToFeishu(cfg.ReverseStatusMapping, binding.WorkItemType, status)
	if target == "" {
		slog.Info("Feishu Project status sync skipped: missing reverse mapping", "issue_id", issueID, "status", status)
		return
	}
	if err := service.NewFeishuProjectClient().TransitionStatus(ctx, cfg, binding.WorkItemID, binding.WorkItemType, target); err != nil {
		slog.Warn("Feishu Project status sync failed", "issue_id", issueID, "work_item_id", binding.WorkItemID, "target", target, "error", err)
	}
}
