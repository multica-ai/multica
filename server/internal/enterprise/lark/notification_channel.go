package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/notifications"
)

const (
	defaultTenantAccessTokenURL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
	defaultMessageURL           = "https://open.feishu.cn/open-apis/im/v1/messages"
)

type NotificationConfig struct {
	Enabled              bool
	AppID                string
	AppSecret            string
	TenantAccessTokenURL string
	MessageURL           string
}

type NotificationChannel struct {
	cfg        NotificationConfig
	httpClient *http.Client
	mu         sync.Mutex
	token      string
	tokenExp   time.Time
}

func NotificationConfigFromEnv() NotificationConfig {
	return NotificationConfig{
		Enabled:              envBool("LARK_NOTIFICATION_ENABLED"),
		AppID:                strings.TrimSpace(os.Getenv("LARK_APP_ID")),
		AppSecret:            firstNonEmpty(os.Getenv("LARK_APP_SECRET"), os.Getenv("LARK_APP_SECRET_REF")),
		TenantAccessTokenURL: firstNonEmpty(os.Getenv("LARK_TENANT_ACCESS_TOKEN_URL"), defaultTenantAccessTokenURL),
		MessageURL:           firstNonEmpty(os.Getenv("LARK_MESSAGE_URL"), defaultMessageURL),
	}
}

func NewNotificationChannelFromEnv() *NotificationChannel {
	return NewNotificationChannel(NotificationConfigFromEnv(), nil)
}

func NewNotificationChannel(cfg NotificationConfig, httpClient *http.Client) *NotificationChannel {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.TenantAccessTokenURL == "" {
		cfg.TenantAccessTokenURL = defaultTenantAccessTokenURL
	}
	if cfg.MessageURL == "" {
		cfg.MessageURL = defaultMessageURL
	}
	return &NotificationChannel{cfg: cfg, httpClient: httpClient}
}

func (c *NotificationChannel) Name() string {
	return ProviderName
}

func (c *NotificationChannel) Send(ctx context.Context, msg notifications.NotificationMessage) error {
	if !c.cfg.Enabled {
		return errors.New("Lark notification is not enabled")
	}
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		return errors.New("Lark notification is not configured")
	}
	if strings.TrimSpace(msg.RecipientExternal) == "" {
		return errors.New("Lark recipient open_id is required")
	}

	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	return c.sendCard(ctx, token, msg.RecipientExternal, buildNotificationCard(msg))
}

func (c *NotificationChannel) tenantAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-2*time.Minute)) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	body, _ := json.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TenantAccessTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var decoded struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int64  `json:"expire"`
	}
	if err := c.doJSON(req, &decoded); err != nil {
		return "", err
	}
	if decoded.Code != 0 {
		return "", fmt.Errorf("Lark tenant token rejected: %s", decoded.Msg)
	}
	if decoded.TenantAccessToken == "" {
		return "", errors.New("Lark tenant token response has no tenant_access_token")
	}
	expire := decoded.Expire
	if expire <= 0 {
		expire = 7200
	}

	c.mu.Lock()
	c.token = decoded.TenantAccessToken
	c.tokenExp = time.Now().Add(time.Duration(expire) * time.Second)
	c.mu.Unlock()
	return decoded.TenantAccessToken, nil
}

func (c *NotificationChannel) sendCard(ctx context.Context, token, openID string, card map[string]any) error {
	content, err := json.Marshal(card)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{
		"receive_id": openID,
		"msg_type":   "interactive",
		"content":    string(content),
	})
	u, err := url.Parse(c.cfg.MessageURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("receive_id_type", "open_id")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	var decoded struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := c.doJSON(req, &decoded); err != nil {
		return err
	}
	if decoded.Code != 0 {
		return fmt.Errorf("Lark message rejected: %s", decoded.Msg)
	}
	return nil
}

func (c *NotificationChannel) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Lark request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Lark response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Lark request returned %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to parse Lark response: %w", err)
	}
	return nil
}

func buildNotificationCard(msg notifications.NotificationMessage) map[string]any {
	lines := make([]string, 0, 4)
	if msg.IssueIdentifier != "" {
		lines = append(lines, "**任务：** "+msg.IssueIdentifier)
	}
	if msg.Title != "" {
		lines = append(lines, "**标题：** "+msg.Title)
	}
	if msg.IssueStatus != "" {
		lines = append(lines, "**状态：** "+issueStatusLabel(msg.IssueStatus))
	}
	if body := truncateRunes(strings.TrimSpace(msg.Body), 200); body != "" {
		lines = append(lines, "**内容：** "+body)
	}
	if len(lines) == 0 {
		lines = append(lines, "你有一条新的 Multica 通知。")
	}

	elements := []map[string]any{
		{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": strings.Join(lines, "\n"),
			},
		},
	}
	if msg.URL != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []map[string]any{
				{
					"tag": "button",
					"text": map[string]any{
						"tag":     "plain_text",
						"content": "打开 Multica",
					},
					"type": "primary",
					"url":  msg.URL,
				},
			},
		})
	}

	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": cardTemplate(msg),
			"title": map[string]any{
				"tag":     "plain_text",
				"content": notificationSubject(msg),
			},
		},
		"elements": elements,
	}
}

func notificationSubject(msg notifications.NotificationMessage) string {
	identifier := msg.IssueIdentifier
	if identifier == "" {
		identifier = "任务"
	}
	switch msg.Type {
	case "issue_assigned":
		return "你被指派了 " + identifier
	case "mentioned":
		return "你在 " + identifier + " 中被提及"
	case "new_comment":
		return identifier + " 有新评论"
	case "task_failed":
		return identifier + " 的智能体任务失败"
	case "task_completed", "agent_completed":
		return "智能体已完成 " + identifier
	default:
		return "新的 Multica 通知"
	}
}

func cardTemplate(msg notifications.NotificationMessage) string {
	switch msg.Type {
	case "task_failed":
		return "red"
	case "task_completed", "agent_completed":
		return "green"
	}

	switch strings.ToLower(strings.TrimSpace(msg.IssueStatus)) {
	case "blocked":
		return "red"
	case "done":
		return "green"
	case "in_progress", "in_review":
		return "yellow"
	case "todo", "backlog":
		return "blue"
	}

	return "blue"
}

func issueStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "backlog":
		return "待整理"
	case "todo":
		return "待办"
	case "in_progress":
		return "进行中"
	case "in_review":
		return "待评审"
	case "done":
		return "已完成"
	case "blocked":
		return "阻塞"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "..."
}
