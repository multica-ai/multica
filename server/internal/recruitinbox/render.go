package recruitinbox

import (
	"fmt"
	"strings"
)

func RenderReply(v Extraction) string {
	question := strings.TrimSpace(v.Clarification)
	if question != "" && !strings.HasSuffix(question, "？") && !strings.HasSuffix(question, "?") {
		question += "？"
	}
	lines := []string{
		"已收到。",
		guardrail,
		"结构化提取：",
		"- 角色：" + display(v.Role),
		"- 预算：" + yesUnknown(v.BudgetPresent),
		"- 到岗时间：" + display(v.StartDate),
		"- 负责人：" + display(v.Owner),
		"- 项目负责人：" + display(v.ProjectLead),
		"- 规则变化：" + boolText(v.RuleChange),
		"- 规则类型：" + display(v.RuleType),
		"- 影响范围：" + display(v.AffectedScope),
		"- 缺失信息：" + list(v.MissingFields),
		"- 不确定项：" + list(v.Uncertainties),
		"建议下一步：" + display(v.ProposedNextStep),
	}
	if v.Consequential {
		lines = append(lines, "检测到可能影响规则、候选人状态或外部动作的指令；请回复“确认生效”后再由人工或受控流程执行。本处理器不会执行该变更。")
	}
	if question != "" {
		lines = append(lines, "需要确认："+question)
	}
	return strings.Join(lines, "\n")
}

func display(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "未提供"
	}
	return v
}

func yesUnknown(v bool) string {
	if v {
		return "已提及（数值不在持久化记录中）"
	}
	return "未提供"
}

func boolText(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func list(v []string) string {
	if len(v) == 0 {
		return "无"
	}
	if len(v) > 6 {
		v = v[:6]
	}
	return fmt.Sprintf("%s", strings.Join(v, "、"))
}
