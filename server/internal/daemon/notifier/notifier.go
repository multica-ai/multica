package notifier

import (
	"context"
	"fmt"
	"strings"
)

type TaskNotificationPayload struct {
	Success   bool
	AgentName string
	IssueID   string
	Message   string
}

type Notifier interface {
	Notify(context.Context, string, string) error
}

func BuildNotificationContent(payload TaskNotificationPayload) (string, string) {
	agentName := strings.TrimSpace(payload.AgentName)
	if agentName == "" {
		agentName = "Agent"
	}
	issueID := strings.TrimSpace(payload.IssueID)
	if issueID == "" {
		issueID = "unknown-task"
	}

	if payload.Success {
		return "Multica 任务已完成", fmt.Sprintf("%s 已完成任务 %s", agentName, issueID)
	}

	msg := strings.TrimSpace(payload.Message)
	if msg == "" {
		msg = "未知错误"
	}
	return "Multica 任务执行失败", fmt.Sprintf("%s 执行任务 %s 失败：%s", agentName, issueID, msg)
}
