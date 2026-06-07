package wakeup

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func RegisterListeners(bus *events.Bus, svc *Service) {
	if bus == nil || svc == nil {
		return
	}
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["status_changed"] != true {
			return
		}
		issueID, status := issueIDAndStatus(payload)
		if !issueID.Valid || status == "" {
			return
		}
		rows, err := svc.ClaimIssueStatus(context.Background(), issueID, status, 50)
		if err != nil {
			slog.Error("cerebro wakeup issue-status claim failed", "error", err)
			return
		}
		for _, row := range rows {
			go svc.Dispatch(context.Background(), row)
		}
	})
	bus.Subscribe(protocol.EventPullRequestUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		ids := linkedIssueIDs(payload["linked_issue_ids"])
		rows, err := svc.ClaimGithubCI(context.Background(), ids, 50)
		if err != nil {
			slog.Error("cerebro wakeup github-ci claim failed", "error", err)
			return
		}
		for _, row := range rows {
			go svc.Dispatch(context.Background(), row)
		}
	})
}

func issueIDAndStatus(payload map[string]any) (pgtype.UUID, string) {
	data, err := json.Marshal(payload["issue"])
	if err != nil {
		return pgtype.UUID{}, ""
	}
	var issue struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &issue); err != nil {
		return pgtype.UUID{}, ""
	}
	id, err := util.ParseUUID(issue.ID)
	if err != nil {
		return pgtype.UUID{}, ""
	}
	return id, issue.Status
}

func linkedIssueIDs(v any) []pgtype.UUID {
	raw, ok := v.([]string)
	if !ok {
		if anySlice, ok := v.([]any); ok {
			raw = make([]string, 0, len(anySlice))
			for _, item := range anySlice {
				if s, ok := item.(string); ok {
					raw = append(raw, s)
				}
			}
		}
	}
	out := make([]pgtype.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := util.ParseUUID(s)
		if err == nil {
			out = append(out, id)
		}
	}
	return out
}
