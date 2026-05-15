package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type AgentFeishuBotConfigResponse struct {
	Enabled           bool    `json:"enabled"`
	AppID             string  `json:"app_id"`
	HasAppSecret      bool    `json:"has_app_secret"`
	VerificationToken *string `json:"verification_token"`
	CallbackURLPath   string  `json:"callback_url_path"`
	CreatedAt         string  `json:"created_at,omitempty"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
}

type UpdateAgentFeishuBotConfigRequest struct {
	Enabled           bool    `json:"enabled"`
	AppID             string  `json:"app_id"`
	AppSecret         *string `json:"app_secret"`
	VerificationToken *string `json:"verification_token"`
}

func (h *Handler) GetAgentFeishuBotConfig(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	cfg, err := h.Queries.GetAgentFeishuBotConfig(r.Context(), agent.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, AgentFeishuBotConfigResponse{CallbackURLPath: "/api/integrations/feishu/agent-bot"})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load Feishu bot config")
		return
	}
	writeJSON(w, http.StatusOK, feishuBotConfigToResponse(cfg))
}

func (h *Handler) UpdateAgentFeishuBotConfig(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	var req UpdateAgentFeishuBotConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.Enabled && strings.TrimSpace(req.AppID) == "" && req.AppSecret == nil {
		if err := h.Queries.DeleteAgentFeishuBotConfig(r.Context(), agent.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete Feishu bot config")
			return
		}
		writeJSON(w, http.StatusOK, AgentFeishuBotConfigResponse{CallbackURLPath: "/api/integrations/feishu/agent-bot"})
		return
	}

	appID := strings.TrimSpace(req.AppID)
	if appID == "" {
		writeError(w, http.StatusBadRequest, "app_id is required")
		return
	}
	appSecret := ""
	if req.AppSecret != nil {
		appSecret = strings.TrimSpace(*req.AppSecret)
	}
	if appSecret == "" {
		existing, err := h.Queries.GetAgentFeishuBotConfig(r.Context(), agent.ID)
		if err == nil && existing.AppID == appID {
			appSecret = existing.AppSecret
		}
	}
	if appSecret == "" {
		writeError(w, http.StatusBadRequest, "app_secret is required")
		return
	}
	var verificationToken pgtype.Text
	if req.VerificationToken != nil && strings.TrimSpace(*req.VerificationToken) != "" {
		verificationToken = pgtype.Text{String: strings.TrimSpace(*req.VerificationToken), Valid: true}
	}
	cfg, err := h.Queries.UpsertAgentFeishuBotConfig(r.Context(), db.UpsertAgentFeishuBotConfigParams{
		AgentID:           agent.ID,
		WorkspaceID:       agent.WorkspaceID,
		AppID:             appID,
		AppSecret:         appSecret,
		Enabled:           req.Enabled,
		VerificationToken: verificationToken,
	})
	if err != nil {
		slog.Warn("update Feishu bot config failed", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update Feishu bot config")
		return
	}
	writeJSON(w, http.StatusOK, feishuBotConfigToResponse(cfg))
}

func feishuBotConfigToResponse(cfg db.AgentFeishuBotConfig) AgentFeishuBotConfigResponse {
	return AgentFeishuBotConfigResponse{
		Enabled:           cfg.Enabled,
		AppID:             cfg.AppID,
		HasAppSecret:      cfg.AppSecret != "",
		VerificationToken: textToPtr(cfg.VerificationToken),
		CallbackURLPath:   "/api/integrations/feishu/agent-bot",
		CreatedAt:         timestampToString(cfg.CreatedAt),
		UpdatedAt:         timestampToString(cfg.UpdatedAt),
	}
}

type feishuBotCallback struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
	Header    struct {
		AppID     string `json:"app_id"`
		EventType string `json:"event_type"`
	} `json:"header"`
	Event feishuBotMessageEvent `json:"event"`
}

type feishuBotMessageEvent struct {
	Sender struct {
		SenderID struct {
			OpenID string `json:"open_id"`
			UserID string `json:"user_id"`
		} `json:"sender_id"`
	} `json:"sender"`
	Message struct {
		MessageID string `json:"message_id"`
		RootID    string `json:"root_id"`
		ParentID  string `json:"parent_id"`
		ChatID    string `json:"chat_id"`
		ChatType  string `json:"chat_type"`
		MsgType   string `json:"message_type"`
		Content   string `json:"content"`
	} `json:"message"`
}

func (h *Handler) FeishuAgentBotCallback(w http.ResponseWriter, r *http.Request) {
	var cb feishuBotCallback
	if err := json.NewDecoder(r.Body).Decode(&cb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid callback body")
		return
	}
	if cb.Type == "url_verification" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": cb.Challenge})
		return
	}
	if cb.Header.EventType != "im.message.receive_v1" {
		slog.Debug("Feishu agent bot callback ignored", "app_id", cb.Header.AppID, "event_type", cb.Header.EventType)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	cfg, err := h.Queries.GetAgentFeishuBotConfigByAppID(r.Context(), cb.Header.AppID)
	if err != nil {
		slog.Warn("Feishu agent bot callback unknown app", "app_id", cb.Header.AppID, "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"status": "unknown_app"})
		return
	}
	if cfg.VerificationToken.Valid && cb.Token != cfg.VerificationToken.String {
		slog.Warn("Feishu agent bot callback invalid token", "app_id", cb.Header.AppID)
		writeError(w, http.StatusUnauthorized, "invalid verification token")
		return
	}
	if err := h.handleFeishuBotMessage(r.Context(), cfg, cb.Event); err != nil {
		slog.Warn("Feishu agent bot message failed", "app_id", cb.Header.AppID, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleFeishuAgentBotMessage(ctx context.Context, appID, senderOpenID, senderUserID, messageID, rootID, parentID, chatID, chatType, messageType, content string) error {
	cfg, err := h.Queries.GetAgentFeishuBotConfigByAppID(ctx, appID)
	if err != nil {
		return err
	}
	return h.handleFeishuBotMessage(ctx, cfg, feishuBotMessageEvent{
		Sender: struct {
			SenderID struct {
				OpenID string `json:"open_id"`
				UserID string `json:"user_id"`
			} `json:"sender_id"`
		}{
			SenderID: struct {
				OpenID string `json:"open_id"`
				UserID string `json:"user_id"`
			}{
				OpenID: senderOpenID,
				UserID: senderUserID,
			},
		},
		Message: struct {
			MessageID string `json:"message_id"`
			RootID    string `json:"root_id"`
			ParentID  string `json:"parent_id"`
			ChatID    string `json:"chat_id"`
			ChatType  string `json:"chat_type"`
			MsgType   string `json:"message_type"`
			Content   string `json:"content"`
		}{
			MessageID: messageID,
			RootID:    rootID,
			ParentID:  parentID,
			ChatID:    chatID,
			ChatType:  chatType,
			MsgType:   messageType,
			Content:   content,
		},
	})
}

func (h *Handler) handleFeishuBotMessage(ctx context.Context, cfg db.AgentFeishuBotConfig, e feishuBotMessageEvent) error {
	text := feishuTextContent(e.Message.Content)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	senderID := e.Sender.SenderID.OpenID
	if senderID == "" {
		senderID = e.Sender.SenderID.UserID
	}
	if senderID == "" {
		return fmt.Errorf("missing Feishu sender id")
	}
	client := service.NewFeishuBotClient(cfg.AppID, cfg.AppSecret)
	user, err := h.resolveFeishuSender(ctx, client, senderID, cfg.WorkspaceID)
	if err != nil {
		_ = client.SendText(ctx, "chat_id", e.Message.ChatID, "无法识别你的 Multica 账号，请确认飞书邮箱已和 Multica 账号一致。")
		return err
	}

	threadID := e.Message.RootID
	if threadID == "" {
		threadID = e.Message.ParentID
	}
	if threadID == "" {
		threadID = e.Message.MessageID
	}
	if issue, ok := h.resolveIssueFromText(ctx, cfg.WorkspaceID, text); ok {
		return h.handleFeishuIssueMessage(ctx, client, cfg, user, issue, e.Message.ChatID, threadID, text)
	}
	if binding, err := h.Queries.GetFeishuIssueThread(ctx, db.GetFeishuIssueThreadParams{
		AppID:          cfg.AppID,
		FeishuChatID:   e.Message.ChatID,
		FeishuThreadID: threadID,
	}); err == nil {
		issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: binding.IssueID, WorkspaceID: cfg.WorkspaceID})
		if err == nil {
			return h.handleFeishuIssueMessage(ctx, client, cfg, user, issue, e.Message.ChatID, threadID, text)
		}
	}
	if e.Message.ChatType == "p2p" {
		return h.handleFeishuChatMessage(ctx, client, cfg, user, senderID, e.Message.ChatID, text)
	}
	return client.SendText(ctx, "chat_id", e.Message.ChatID, "请带上 issue 编号或链接，例如：处理 MUL-123。")
}

func (h *Handler) resolveFeishuSender(ctx context.Context, client *service.FeishuBotClient, senderID string, workspaceID pgtype.UUID) (db.User, error) {
	info, err := client.GetUserInfo(ctx, senderID)
	if err != nil {
		return db.User{}, err
	}
	if info.Email == "" {
		return db.User{}, fmt.Errorf("Feishu user has no email")
	}
	user, err := h.Queries.GetUserByEmail(ctx, info.Email)
	if err != nil {
		return db.User{}, err
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return db.User{}, err
	}
	return user, nil
}

func (h *Handler) handleFeishuChatMessage(ctx context.Context, client *service.FeishuBotClient, cfg db.AgentFeishuBotConfig, user db.User, senderID, chatID, text string) error {
	binding, err := h.Queries.GetFeishuAgentChatBinding(ctx, db.GetFeishuAgentChatBindingParams{
		AppID:          cfg.AppID,
		FeishuChatID:   chatID,
		FeishuSenderID: senderID,
	})
	var session db.ChatSession
	if err == nil {
		session, err = h.Queries.GetChatSession(ctx, binding.ChatSessionID)
	}
	if err != nil {
		agent, err := h.Queries.GetAgent(ctx, cfg.AgentID)
		if err != nil {
			return err
		}
		session, err = h.Queries.CreateChatSession(ctx, db.CreateChatSessionParams{
			WorkspaceID: cfg.WorkspaceID,
			AgentID:     cfg.AgentID,
			CreatorID:   user.ID,
			Title:       "Feishu - " + agent.Name,
		})
		if err != nil {
			return err
		}
		if _, err := h.Queries.UpsertFeishuAgentChatBinding(ctx, db.UpsertFeishuAgentChatBindingParams{
			AppID:          cfg.AppID,
			WorkspaceID:    cfg.WorkspaceID,
			AgentID:        cfg.AgentID,
			UserID:         user.ID,
			FeishuChatID:   chatID,
			FeishuSenderID: senderID,
			ChatSessionID:  session.ID,
		}); err != nil {
			return err
		}
	}
	msg, err := h.Queries.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       text,
	})
	if err != nil {
		return err
	}
	task, err := h.TaskService.EnqueueChatTask(ctx, session)
	if err != nil {
		return err
	}
	_ = h.Queries.TouchChatSession(ctx, session.ID)
	h.publishChat(protocol.EventChatMessage, uuidToString(cfg.WorkspaceID), "member", uuidToString(user.ID), uuidToString(session.ID), protocol.ChatMessagePayload{
		ChatSessionID: uuidToString(session.ID),
		MessageID:     uuidToString(msg.ID),
		Role:          "user",
		Content:       text,
		TaskID:        uuidToString(task.ID),
		CreatedAt:     timestampToString(msg.CreatedAt),
	})
	return client.SendText(ctx, "chat_id", chatID, "已收到，正在处理。")
}

func (h *Handler) handleFeishuIssueMessage(ctx context.Context, client *service.FeishuBotClient, cfg db.AgentFeishuBotConfig, user db.User, issue db.Issue, chatID, threadID, text string) error {
	issue, err := h.assignIssueToFeishuAgent(ctx, cfg, user, issue)
	if err != nil {
		return err
	}
	if _, err := h.Queries.UpsertFeishuIssueThread(ctx, db.UpsertFeishuIssueThreadParams{
		AppID:          cfg.AppID,
		WorkspaceID:    cfg.WorkspaceID,
		IssueID:        issue.ID,
		AgentID:        cfg.AgentID,
		UserID:         user.ID,
		FeishuChatID:   chatID,
		FeishuThreadID: threadID,
	}); err != nil {
		return err
	}
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: cfg.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    user.ID,
		Content:     text,
		Type:        "comment",
	})
	if err != nil {
		return err
	}
	resp := commentToResponse(comment, nil, nil)
	h.publish(protocol.EventCommentCreated, uuidToString(cfg.WorkspaceID), "member", uuidToString(user.ID), map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
	if h.shouldEnqueueOnComment(ctx, issue) {
		_, _ = h.TaskService.EnqueueTaskForIssue(ctx, issue, comment.ID)
	}
	if threadID != "" {
		return client.ReplyText(ctx, threadID, "已同步到 "+h.issueIdentifier(ctx, issue)+"，并派给当前智能体。")
	}
	return client.SendText(ctx, "chat_id", chatID, "已同步到 "+h.issueIdentifier(ctx, issue)+"，并派给当前智能体。")
}

func (h *Handler) assignIssueToFeishuAgent(ctx context.Context, cfg db.AgentFeishuBotConfig, user db.User, issue db.Issue) (db.Issue, error) {
	if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" && issue.AssigneeID == cfg.AgentID {
		return issue, nil
	}
	prev := issue
	issue, err := h.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:            issue.ID,
		AssigneeType:  pgtype.Text{String: "agent", Valid: true},
		AssigneeID:    cfg.AgentID,
		DueDate:       issue.DueDate,
		ParentIssueID: issue.ParentIssueID,
		ProjectID:     issue.ProjectID,
	})
	if err != nil {
		return db.Issue{}, err
	}
	resp := issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.publish(protocol.EventIssueUpdated, uuidToString(cfg.WorkspaceID), "member", uuidToString(user.ID), map[string]any{
		"issue":               resp,
		"assignee_changed":    true,
		"status_changed":      false,
		"priority_changed":    false,
		"due_date_changed":    false,
		"description_changed": false,
		"title_changed":       false,
		"prev_title":          prev.Title,
		"prev_assignee_type":  textToPtr(prev.AssigneeType),
		"prev_assignee_id":    uuidToPtr(prev.AssigneeID),
		"prev_status":         prev.Status,
		"prev_priority":       prev.Priority,
		"prev_due_date":       timestampToPtr(prev.DueDate),
		"prev_description":    textToPtr(prev.Description),
		"creator_type":        prev.CreatorType,
		"creator_id":          uuidToString(prev.CreatorID),
	})
	h.TaskService.CancelTasksForIssue(ctx, issue.ID)
	if h.shouldEnqueueAgentTask(ctx, issue) {
		_, _ = h.TaskService.EnqueueTaskForIssue(ctx, issue)
	}
	return issue, nil
}

var issueMentionFromTextRe = regexp.MustCompile(`(?i)([a-z][a-z0-9]{1,9}-[0-9]+)|/issues/([0-9a-f-]{36})`)

func (h *Handler) resolveIssueFromText(ctx context.Context, workspaceID pgtype.UUID, text string) (db.Issue, bool) {
	matches := issueMentionFromTextRe.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if match[2] != "" {
			issueUUID, err := util.ParseUUID(match[2])
			if err == nil {
				issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueUUID, WorkspaceID: workspaceID})
				if err == nil {
					return issue, true
				}
			}
			continue
		}
		parts := splitIdentifier(match[1])
		if parts == nil {
			continue
		}
		issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{WorkspaceID: workspaceID, Number: parts.number})
		if err == nil {
			return issue, true
		}
	}
	return db.Issue{}, false
}

func (h *Handler) issueIdentifier(ctx context.Context, issue db.Issue) string {
	return h.getIssuePrefix(ctx, issue.WorkspaceID) + "-" + fmt.Sprintf("%d", issue.Number)
}

func feishuTextContent(raw string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed.Text != "" {
		return strings.TrimSpace(parsed.Text)
	}
	return strings.TrimSpace(raw)
}
