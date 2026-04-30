package notifier

import (
	"context"
	"fmt"
	"strings"
)

type TaskNotificationPayload struct {
	Success         bool
	AgentName       string
	IssueID         string
	IssueIdentifier string
	IssueTitle      string
	Message         string
}

type Notifier interface {
	Notify(context.Context, string, string) error
}

func BuildNotificationContent(payload TaskNotificationPayload) (string, string) {
	agentName := strings.TrimSpace(payload.AgentName)
	if agentName == "" {
		agentName = "Agent"
	}
	issueRef, detailed := formatIssueReference(payload)

	if payload.Success {
		if detailed {
			return "Multica 任务已完成", fmt.Sprintf("%s 已完成 %s", agentName, issueRef)
		}
		return "Multica 任务已完成", fmt.Sprintf("%s 已完成任务 %s", agentName, issueRef)
	}

	msg := strings.TrimSpace(payload.Message)
	if msg == "" {
		msg = "未知错误"
	}
	if detailed {
		return "Multica 任务执行失败", fmt.Sprintf("%s 执行 %s 失败：%s", agentName, issueRef, msg)
	}
	return "Multica 任务执行失败", fmt.Sprintf("%s 执行任务 %s 失败：%s", agentName, issueRef, msg)
}

func formatIssueReference(payload TaskNotificationPayload) (string, bool) {
	identifier := strings.TrimSpace(payload.IssueIdentifier)
	title := strings.TrimSpace(payload.IssueTitle)
	if identifier != "" && title != "" {
		return fmt.Sprintf("%s: %s", identifier, title), true
	}

	issueID := strings.TrimSpace(payload.IssueID)
	if issueID == "" {
		issueID = "unknown-task"
	}
	return issueID, false
}
