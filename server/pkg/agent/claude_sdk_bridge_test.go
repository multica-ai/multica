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

func TestFindClaudeSDKRootIgnoresDeclaredDependencyWithoutInstall(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"devDependencies":{"@anthropic-ai/claude-agent-sdk":"^0.2.132"}}`), 0o600); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if got := findClaudeSDKRoot(root); got != "" {
		t.Fatalf("declared dependency without installed sdk should not resolve as sdk root, got %q", got)
	}
}

func TestFindClaudeSDKRootResolvesRelativePathToAbsoluteRoot(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@anthropic-ai", "claude-agent-sdk"), 0o755); err != nil {
		t.Fatalf("mkdir sdk root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "@anthropic-ai", "claude-agent-sdk", "package.json"), []byte(`{"name":"@anthropic-ai/claude-agent-sdk"}`), 0o600); err != nil {
		t.Fatalf("write sdk package: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	got := findClaudeSDKRoot(".")
	gotEval, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("eval got: %v", err)
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval root: %v", err)
	}
	if gotEval != rootEval {
		t.Fatalf("findClaudeSDKRoot(.) = %q (eval %q), want %q (eval %q)", got, gotEval, root, rootEval)
	}
}

func TestNormalizeClaudeSDKPermissionMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "default"},
		{in: "default", want: "default"},
		{in: "plan", want: "plan"},
		{in: "acceptEdits", want: "acceptEdits"},
		{in: "unexpected", want: "default"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := normalizeClaudeSDKPermissionMode(tt.in); got != tt.want {
				t.Fatalf("normalizeClaudeSDKPermissionMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
