package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// InboxNotifier delivers Multica inbox items as direct Lark messages.
// Delivery is best-effort: the inbox row and realtime event are the source of
// truth, and Lark is only an external notification channel.
type InboxNotifier struct {
	Queries     InboxNotifierQueries
	Credentials CredentialsResolver
	Client      APIClient
	Logger      *slog.Logger
}

type InboxNotifierQueries interface {
	GetActiveLarkMemberBindingForWorkspaceUser(ctx context.Context, arg db.GetActiveLarkMemberBindingForWorkspaceUserParams) (db.LarkUserBinding, error)
	GetLarkWorkspaceNotificationPolicy(ctx context.Context, workspaceID pgtype.UUID) (db.LarkWorkspaceNotificationPolicy, error)
	GetLarkInstallation(ctx context.Context, id pgtype.UUID) (db.LarkInstallation, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	CreateLarkInboxNotification(ctx context.Context, arg db.CreateLarkInboxNotificationParams) (db.LarkInboxNotification, error)
}

func (n *InboxNotifier) Handle(ctx context.Context, payload any) {
	if n == nil || n.Queries == nil || n.Credentials == nil || n.Client == nil || !n.Client.IsConfigured() {
		return
	}
	item, ok := inboxEventItem(payload)
	if !ok {
		return
	}
	if item.recipientType != "member" {
		return
	}
	policy, err := n.Queries.GetLarkWorkspaceNotificationPolicy(ctx, item.workspaceID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			n.log().Warn("lark inbox notifier: load notification policy failed",
				"workspace_id", uuidString(item.workspaceID),
				"err", err.Error(),
			)
		}
		return
	}
	if !shouldDeliverInboxNotification(item, policy.EventTypes) {
		return
	}
	binding, err := n.Queries.GetActiveLarkMemberBindingForWorkspaceUser(ctx, db.GetActiveLarkMemberBindingForWorkspaceUserParams{
		WorkspaceID:   item.workspaceID,
		MulticaUserID: item.recipientID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			n.log().Warn("lark inbox notifier: load binding failed",
				"workspace_id", uuidString(item.workspaceID),
				"recipient_id", uuidString(item.recipientID),
				"err", err.Error(),
			)
		}
		return
	}
	inst, err := n.Queries.GetLarkInstallation(ctx, binding.InstallationID)
	if err != nil {
		n.log().Warn("lark inbox notifier: load installation failed",
			"installation_id", uuidString(binding.InstallationID),
			"err", err.Error(),
		)
		return
	}
	creds, err := n.installationCredentials(inst)
	if err != nil {
		n.log().Warn("lark inbox notifier: decrypt credentials failed",
			"installation_id", uuidString(inst.ID),
			"err", err.Error(),
		)
		return
	}
	text := n.notificationText(ctx, item)
	messageID, err := n.Client.SendDirectTextMessage(ctx, SendDirectTextParams{
		InstallationID: creds,
		OpenID:         OpenID(binding.LarkOpenID),
		Text:           text,
	})
	if err != nil {
		n.log().Warn("lark inbox notifier: send direct message failed",
			"installation_id", uuidString(inst.ID),
			"inbox_item_id", uuidString(item.id),
			"open_id", binding.LarkOpenID,
			"err", err.Error(),
		)
		return
	}
	if !item.issueID.Valid {
		return
	}
	if _, err := n.Queries.CreateLarkInboxNotification(ctx, db.CreateLarkInboxNotificationParams{
		WorkspaceID:     item.workspaceID,
		InstallationID:  inst.ID,
		InboxItemID:     item.id,
		IssueID:         item.issueID,
		RecipientUserID: item.recipientID,
		LarkOpenID:      binding.LarkOpenID,
		LarkMessageID:   messageID,
	}); err != nil {
		n.log().Warn("lark inbox notifier: persist delivery mapping failed",
			"installation_id", uuidString(inst.ID),
			"inbox_item_id", uuidString(item.id),
			"lark_message_id", messageID,
			"err", err.Error(),
		)
	}
}

func (n *InboxNotifier) log() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.Default()
}

func (n *InboxNotifier) installationCredentials(inst db.LarkInstallation) (InstallationCredentials, error) {
	secret, err := n.Credentials.DecryptAppSecret(inst)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("decrypt app_secret: %w", err)
	}
	creds := InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(inst.Region),
	}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}
	return creds, nil
}

func (n *InboxNotifier) notificationText(ctx context.Context, item inboxNotificationItem) string {
	if !item.issueID.Valid {
		return formatInboxNotificationText("", item)
	}
	identifier := uuidString(item.issueID)
	if issue, err := n.Queries.GetIssue(ctx, item.issueID); err == nil {
		item.title = issue.Title
		if ws, werr := n.Queries.GetWorkspace(ctx, issue.WorkspaceID); werr == nil && ws.IssuePrefix != "" {
			identifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
		} else {
			identifier = fmt.Sprintf("#%d", issue.Number)
		}
	}
	return formatInboxNotificationText(identifier, item)
}

func formatInboxNotificationText(identifier string, item inboxNotificationItem) string {
	lines := []string{
		"Multica 收件箱通知",
	}
	if identifier != "" {
		lines = append(lines, "Issue："+identifier)
	}
	lines = append(lines,
		"标题："+item.title,
		"类型："+inboxNotificationTypeLabel(item.typ),
		"级别："+inboxNotificationSeverityLabel(item.severity),
	)
	if item.body != "" {
		lines = append(lines, "", item.body)
	}
	if identifier != "" {
		lines = append(lines, "", "回复此飞书消息，即可在对应 issue 下发表评论。")
	}
	return strings.Join(lines, "\n")
}

func inboxNotificationTypeLabel(typ string) string {
	switch typ {
	case "issue_assigned":
		return "issue 已分配"
	case "unassigned":
		return "issue 已取消分配"
	case "assignee_changed":
		return "负责人已变更"
	case "mentioned":
		return "有人提及你"
	case "new_comment":
		return "新评论"
	case "reaction_added":
		return "新表情回应"
	case "status_changed":
		return "状态已变更"
	case "priority_changed":
		return "优先级已变更"
	case "start_date_changed":
		return "开始日期已变更"
	case "due_date_changed":
		return "截止日期已变更"
	case "task_failed":
		return "执行任务失败"
	case "quick_create_failed":
		return "快速创建失败"
	case "quick_create_done":
		return "快速创建完成"
	case "autopilot_paused":
		return "自动化已暂停"
	case "issue_subscribed":
		return "已订阅新 issue"
	default:
		return typ
	}
}

func inboxNotificationSeverityLabel(severity string) string {
	switch severity {
	case "info":
		return "普通"
	case "action_required":
		return "需要处理"
	case "attention":
		return "需要关注"
	default:
		return severity
	}
}

type inboxNotificationItem struct {
	id            pgtype.UUID
	workspaceID   pgtype.UUID
	recipientType string
	recipientID   pgtype.UUID
	issueID       pgtype.UUID
	typ           string
	severity      string
	title         string
	body          string
}

func shouldDeliverInboxNotification(item inboxNotificationItem, enabled []string) bool {
	if item.recipientType != "member" {
		return false
	}

	for _, eventType := range enabled {
		if item.typ == eventType {
			return true
		}
	}
	return false
}

func inboxEventItem(payload any) (inboxNotificationItem, bool) {
	root, ok := payload.(map[string]any)
	if !ok {
		return inboxNotificationItem{}, false
	}
	rawItem, ok := root["item"].(map[string]any)
	if !ok {
		return inboxNotificationItem{}, false
	}
	id, ok := uuidFromAny(rawItem["id"])
	if !ok {
		return inboxNotificationItem{}, false
	}
	workspaceID, ok := uuidFromAny(rawItem["workspace_id"])
	if !ok {
		return inboxNotificationItem{}, false
	}
	recipientID, ok := uuidFromAny(rawItem["recipient_id"])
	if !ok {
		return inboxNotificationItem{}, false
	}
	issueID, _ := uuidFromAny(rawItem["issue_id"])
	return inboxNotificationItem{
		id:            id,
		workspaceID:   workspaceID,
		recipientType: stringFromAny(rawItem["recipient_type"]),
		recipientID:   recipientID,
		issueID:       issueID,
		typ:           stringFromAny(rawItem["type"]),
		severity:      stringFromAny(rawItem["severity"]),
		title:         stringFromAny(rawItem["title"]),
		body:          stringFromAny(rawItem["body"]),
	}, true
}

func uuidFromAny(v any) (pgtype.UUID, bool) {
	switch x := v.(type) {
	case string:
		return parseInboxUUID(x)
	case *string:
		if x == nil {
			return pgtype.UUID{}, false
		}
		return parseInboxUUID(*x)
	default:
		return pgtype.UUID{}, false
	}
}

func parseInboxUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if strings.TrimSpace(s) == "" {
		return u, false
	}
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

func stringFromAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case *string:
		if x != nil {
			return *x
		}
	}
	return ""
}
