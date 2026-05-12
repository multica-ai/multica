package daemon

import "testing"

func TestDetectVisibleLanguageFromComment(t *testing.T) {
	t.Parallel()

	task := Task{TriggerCommentContent: "请先给我方案，再执行"}
	if got := detectVisibleLanguage(task); got != "zh" {
		t.Fatalf("detectVisibleLanguage() = %q, want zh", got)
	}
}

func TestLocalizedPlanDraftingMessageChinese(t *testing.T) {
	t.Parallel()

	got := localizedPlanDraftingMessage("zh")
	if got == "" || got == "Drafting a plan only. No implementation should run in this task." {
		t.Fatalf("expected localized Chinese drafting message, got %q", got)
	}
}
