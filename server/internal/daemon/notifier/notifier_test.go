package notifier

import "testing"

func TestBuildNotificationContentSuccess(t *testing.T) {
	title, body := BuildNotificationContent(TaskNotificationPayload{
		Success:   true,
		AgentName: "Tony Bot",
		IssueID:   "issue-123",
	})

	if title != "Multica 任务已完成" {
		t.Fatalf("title = %q", title)
	}
	if body != "Tony Bot 已完成任务 issue-123" {
		t.Fatalf("body = %q", body)
	}
}

func TestBuildNotificationContentUsesIssueIdentifierAndTitle(t *testing.T) {
	title, body := BuildNotificationContent(TaskNotificationPayload{
		Success:         true,
		AgentName:       "Tony Bot",
		IssueID:         "issue-123",
		IssueIdentifier: "OPE-251",
		IssueTitle:      "新增通知渠道 系统通知栏",
	})

	if title != "Multica 任务已完成" {
		t.Fatalf("title = %q", title)
	}
	if body != "Tony Bot 已完成 OPE-251: 新增通知渠道 系统通知栏" {
		t.Fatalf("body = %q", body)
	}
}

func TestBuildNotificationContentFailureFallsBack(t *testing.T) {
	title, body := BuildNotificationContent(TaskNotificationPayload{
		Success: false,
		Message: "",
	})

	if title != "Multica 任务执行失败" {
		t.Fatalf("title = %q", title)
	}
	if body != "Agent 执行任务 unknown-task 失败：未知错误" {
		t.Fatalf("body = %q", body)
	}
}

func TestBuildNotificationContentFailureUsesIssueIdentifierAndTitle(t *testing.T) {
	title, body := BuildNotificationContent(TaskNotificationPayload{
		Success:         false,
		AgentName:       "Tony Bot",
		IssueID:         "issue-123",
		IssueIdentifier: "OPE-251",
		IssueTitle:      "新增通知渠道 系统通知栏",
		Message:         "disk full",
	})

	if title != "Multica 任务执行失败" {
		t.Fatalf("title = %q", title)
	}
	if body != "Tony Bot 执行 OPE-251: 新增通知渠道 系统通知栏 失败：disk full" {
		t.Fatalf("body = %q", body)
	}
}
