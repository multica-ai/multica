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
