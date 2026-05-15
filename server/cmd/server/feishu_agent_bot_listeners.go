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

func registerFeishuAgentBotListeners(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(protocol.EventChatDone, func(e events.Event) {
		payload, ok := e.Payload.(protocol.ChatDonePayload)
		if !ok || payload.ChatSessionID == "" || payload.TaskID == "" {
			return
		}
		go dispatchFeishuChatDone(queries, payload.ChatSessionID, payload.TaskID)
	})

	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		if e.ActorType != "agent" {
			return
		}
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		comment, ok := payload["comment"].(handler.CommentResponse)
		if !ok || comment.Content == "" {
			return
		}
		go dispatchFeishuIssueComment(queries, comment.IssueID, comment.Content)
	})
}

func dispatchFeishuChatDone(queries *db.Queries, chatSessionID, taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	binding, err := queries.GetFeishuAgentChatBindingBySession(ctx, util.MustParseUUID(chatSessionID))
	if err != nil {
		return
	}
	message, err := queries.GetChatMessageByTaskID(ctx, util.MustParseUUID(taskID))
	if err != nil || message.Content == "" {
		return
	}
	cfg, err := queries.GetAgentFeishuBotConfigByAppID(ctx, binding.AppID)
	if err != nil {
		return
	}
	client := service.NewFeishuBotClient(cfg.AppID, cfg.AppSecret)
	if err := client.SendText(ctx, "chat_id", binding.FeishuChatID, message.Content); err != nil {
		slog.Warn("Feishu agent bot chat reply failed", "chat_session_id", chatSessionID, "error", err)
	}
}

func dispatchFeishuIssueComment(queries *db.Queries, issueID, content string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	threads, err := queries.ListFeishuIssueThreadsByIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		return
	}
	for _, thread := range threads {
		cfg, err := queries.GetAgentFeishuBotConfigByAppID(ctx, thread.AppID)
		if err != nil {
			continue
		}
		client := service.NewFeishuBotClient(cfg.AppID, cfg.AppSecret)
		if err := client.ReplyText(ctx, thread.FeishuThreadID, content); err != nil {
			slog.Warn("Feishu agent bot issue reply failed", "issue_id", issueID, "error", err)
		}
	}
}
