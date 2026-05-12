package agent

import (
	"strings"
	"testing"
)

func TestLocalizedPlanStageContentChineseRevise(t *testing.T) {
	t.Parallel()

	got := localizedPlanStageContent("zh", "revise", "请补充风险和实施顺序")
	if !strings.Contains(got, "继续停留在计划模式中修改方案") {
		t.Fatalf("expected Chinese revise content, got %q", got)
	}
	if !strings.Contains(got, "请补充风险和实施顺序") {
		t.Fatalf("expected revision request to be preserved, got %q", got)
	}
}

func TestMergeApprovedPlanIntoOutputChinese(t *testing.T) {
	t.Parallel()

	got := mergeApprovedPlanIntoOutput("zh", "第一阶段：梳理方案", "已开始实施第一阶段")
	if !strings.Contains(got, "已批准方案") {
		t.Fatalf("expected approved plan heading, got %q", got)
	}
	if !strings.Contains(got, "执行结果") {
		t.Fatalf("expected execution result heading, got %q", got)
	}
}
