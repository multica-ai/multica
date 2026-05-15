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

func localizedPlanUnsupportedMessage(language, provider string) string {
	if language == "zh" {
		return "当前 agent 使用的 " + provider + " runtime 不支持原生 plan mode。本次请求会按普通执行模式运行。"
	}
	return provider + " does not support native plan mode. This request will run normally."
}
