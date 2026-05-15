package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var feishuTaskProgressCounts sync.Map

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

	bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		payload, ok := e.Payload.(protocol.TaskMessagePayload)
		if !ok || payload.ChatSessionID == "" || payload.TaskID == "" || payload.Type != "tool_use" {
			return
		}
		go dispatchFeishuChatProgress(queries, payload)
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
	if err := client.SendCard(ctx, "chat_id", binding.FeishuChatID, feishuAgentReplyCard(message.Content)); err != nil {
		slog.Warn("Feishu agent bot chat reply failed", "chat_session_id", chatSessionID, "error", err)
	}
	feishuTaskProgressCounts.Delete(taskID)
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

func dispatchFeishuChatProgress(queries *db.Queries, payload protocol.TaskMessagePayload) {
	count := nextFeishuProgressCount(payload.TaskID)
	if count > 6 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	binding, err := queries.GetFeishuAgentChatBindingBySession(ctx, util.MustParseUUID(payload.ChatSessionID))
	if err != nil {
		return
	}
	cfg, err := queries.GetAgentFeishuBotConfigByAppID(ctx, binding.AppID)
	if err != nil {
		return
	}
	client := service.NewFeishuBotClient(cfg.AppID, cfg.AppSecret)
	text := fmt.Sprintf("处理中 %d/6：%s", count, summarizeFeishuToolUse(payload.Tool, payload.Input))
	if count == 6 {
		text += "\n后续步骤会继续执行，完成后我会汇总回复。"
	}
	if err := client.SendText(ctx, "chat_id", binding.FeishuChatID, text); err != nil {
		slog.Warn("Feishu agent bot progress failed", "task_id", payload.TaskID, "error", err)
	}
}

func nextFeishuProgressCount(taskID string) int {
	val, _ := feishuTaskProgressCounts.LoadOrStore(taskID, int32(0))
	next := val.(int32) + 1
	feishuTaskProgressCounts.Store(taskID, next)
	return int(next)
}

func summarizeFeishuToolUse(tool string, input map[string]any) string {
	label := feishuToolLabel(tool)
	detail := feishuToolDetail(input)
	if detail == "" {
		return label
	}
	return label + "：" + detail
}

func feishuToolLabel(tool string) string {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "websearch":
		return "搜索资料"
	case "webfetch":
		return "读取网页"
	case "exec_command":
		return "执行命令"
	case "apply_patch":
		return "修改代码"
	case "read_mcp_resource":
		return "读取资源"
	default:
		if strings.TrimSpace(tool) == "" {
			return "继续处理"
		}
		return "调用 " + tool
	}
}

func feishuToolDetail(input map[string]any) string {
	for _, key := range []string{"q", "query", "url", "cmd", "pattern", "ref_id"} {
		if v, ok := input[key]; ok {
			if s := truncateFeishuProgress(fmt.Sprint(v), 80); s != "" {
				return s
			}
		}
	}
	return ""
}

func truncateFeishuProgress(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "..."
}

func feishuAgentReplyCard(content string) map[string]any {
	return map[string]any{
		"config": map[string]bool{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "wathet",
			"title": map[string]string{
				"tag":     "plain_text",
				"content": "Multica 回复",
			},
		},
		"elements": []map[string]any{
			{
				"tag": "div",
				"text": map[string]string{
					"tag":     "lark_md",
					"content": content,
				},
			},
		},
	}
}
