package agent

import (
	"os"
	"path/filepath"
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

func TestFindClaudeSDKRootRequiresMulticaInstallNotTaskRepo(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	taskRepo := filepath.Join(base, "task-repo")
	if err := os.MkdirAll(taskRepo, 0o755); err != nil {
		t.Fatalf("mkdir task repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskRepo, "package.json"), []byte(`{"name":"customer-repo"}`), 0o600); err != nil {
		t.Fatalf("write task repo package: %v", err)
	}
	if got := findClaudeSDKRoot(taskRepo); got != "" {
		t.Fatalf("task repo without Claude SDK should not resolve as SDK root, got %q", got)
	}

	installRoot := filepath.Join(base, "multica")
	if err := os.MkdirAll(filepath.Join(installRoot, "node_modules", "@anthropic-ai", "claude-agent-sdk"), 0o755); err != nil {
		t.Fatalf("mkdir sdk root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "node_modules", "@anthropic-ai", "claude-agent-sdk", "package.json"), []byte(`{"name":"@anthropic-ai/claude-agent-sdk"}`), 0o600); err != nil {
		t.Fatalf("write sdk package: %v", err)
	}
	if got := findClaudeSDKRoot(installRoot); got != installRoot {
		t.Fatalf("findClaudeSDKRoot() = %q, want %q", got, installRoot)
	}
}
