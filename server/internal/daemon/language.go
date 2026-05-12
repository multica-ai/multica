package daemon

import "unicode"

func detectVisibleLanguage(task Task) string {
	for _, sample := range []string{task.TriggerCommentContent, task.ChatMessage} {
		if containsHan(sample) {
			return "zh"
		}
	}
	return "en"
}

func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func localizedPlanDraftingMessage(language string) string {
	if language == "zh" {
		return "当前处于计划阶段。本次运行只产出方案，不执行实现。"
	}
	return "Drafting a plan only. No implementation should run in this task."
}
