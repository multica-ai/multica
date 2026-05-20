package main

import (
"strings"
"testing"
)

func TestExtractSummary_ExplicitSummary(t *testing.T) {
got := ExtractSummary("验收 PASS，编译复现通过", "long body content...", "Issue Title", 80)
if got != "验收 PASS，编译复现通过" {
t.Errorf("expected explicit summary, got %q", got)
}
}

func TestExtractSummary_FromBodyConclusionLine(t *testing.T) {
body := "## 实现完成 ✅\n\n`openclaw_weixin` 通知通道已实现\n\n**PR**: https://..."
got := ExtractSummary("", body, "Some Title", 80)
// "实现完成" is a conclusion prefix, "✅" is <3 chars so we get the full normalized line
if got != "实现完成 ✅" {
t.Errorf("expected 实现完成 ✅, got %q", got)
}
}

func TestExtractSummary_FromBodyFirstLine(t *testing.T) {
body := "Fixed the login redirect issue and added tests."
got := ExtractSummary("", body, "Issue Title", 80)
if got != "Fixed the login redirect issue and added tests." {
t.Errorf("expected first line, got %q", got)
}
}

func TestExtractSummary_FallbackToTitle(t *testing.T) {
got := ExtractSummary("", "", "系统级微信通知渠道", 80)
if got != "系统级微信通知渠道" {
t.Errorf("expected title fallback, got %q", got)
}
}

func TestExtractSummary_FallbackDefault(t *testing.T) {
got := ExtractSummary("", "", "", 80)
if got != "查看详情" {
t.Errorf("expected default fallback, got %q", got)
}
}

func TestExtractSummary_Truncation(t *testing.T) {
long := strings.Repeat("测", 100)
got := ExtractSummary(long, "", "", 80)
runes := []rune(got)
if len(runes) > 80 {
t.Errorf("expected max 80 runes, got %d", len(runes))
}
if !strings.HasSuffix(got, "...") {
t.Errorf("expected ... suffix, got %q", got)
}
}

func TestExtractSummary_DoesNotBreakMarkdownLink(t *testing.T) {
body := "修复了 [OPE-918](https://multica.wujieai.com/openharness/issues/OPE-918) 的问题"
got := ExtractSummary("", body, "", 30)
// Should not contain an unclosed link
openBrackets := strings.Count(got, "[")
closeBrackets := strings.Count(got, "]")
if openBrackets > closeBrackets {
t.Errorf("broken markdown link in summary: %q", got)
}
}

func TestExtractSummary_CleansMarkdown(t *testing.T) {
body := "结论：验收 PASS\n\n```go\nfunc main() {}\n```\n\n| col1 | col2 |\n| --- | --- |"
got := ExtractSummary("", body, "", 80)
if strings.Contains(got, "```") || strings.Contains(got, "|") {
t.Errorf("markdown not cleaned: %q", got)
}
if !strings.Contains(got, "验收 PASS") {
t.Errorf("expected conclusion, got %q", got)
}
}

func TestExtractSummary_CleansMentionLinks(t *testing.T) {
body := "[@guodage_dev](mention://agent/abc-123) 完成了任务"
got := ExtractSummary("", body, "", 80)
if strings.Contains(got, "mention://") {
t.Errorf("mention link not cleaned: %q", got)
}
if !strings.Contains(got, "@guodage_dev") {
t.Errorf("expected readable name, got %q", got)
}
}

// --- ShouldRenderCompact tests ---

func TestShouldRenderCompact_EmptyBody(t *testing.T) {
if !ShouldRenderCompact("") {
t.Error("empty body should be compact")
}
}

func TestShouldRenderCompact_ShortBody(t *testing.T) {
if ShouldRenderCompact("验收 PASS") {
t.Error("short body should not be compact")
}
}

func TestShouldRenderCompact_LongBody(t *testing.T) {
long := strings.Repeat("字", 200)
if !ShouldRenderCompact(long) {
t.Error("long body (200 chars) should be compact")
}
}

func TestShouldRenderCompact_ManyLines(t *testing.T) {
body := "line1\nline2\nline3\nline4\nline5"
if !ShouldRenderCompact(body) {
t.Error("5 lines should be compact")
}
}

func TestShouldRenderCompact_CodeBlock(t *testing.T) {
body := "结论：OK\n```go\nfunc main(){}\n```"
if !ShouldRenderCompact(body) {
t.Error("body with code block should be compact")
}
}

func TestShouldRenderCompact_Table(t *testing.T) {
body := "| col1 | col2 |\n| --- | --- |\n| a | b |"
if !ShouldRenderCompact(body) {
t.Error("body with table should be compact")
}
}

func TestShouldRenderCompact_FourLinesOrLess(t *testing.T) {
body := "line1\nline2\nline3\nline4"
if ShouldRenderCompact(body) {
t.Error("4 lines without complex content should be detail")
}
}

// --- ResolveRenderMode tests ---

func TestResolveRenderMode_CompactPref(t *testing.T) {
got := ResolveRenderMode(RenderModeCompact, RenderModeAuto, "short body")
if got != RenderModeCompact {
t.Errorf("expected compact, got %s", got)
}
}

func TestResolveRenderMode_DetailPref(t *testing.T) {
got := ResolveRenderMode(RenderModeDetail, RenderModeAuto, strings.Repeat("x", 500))
if got != RenderModeDetail {
t.Errorf("expected detail, got %s", got)
}
}

func TestResolveRenderMode_AutoShortBody(t *testing.T) {
got := ResolveRenderMode(RenderModeAuto, RenderModeAuto, "OK")
if got != RenderModeDetail {
t.Errorf("expected detail for short body in auto, got %s", got)
}
}

func TestResolveRenderMode_AutoLongBody(t *testing.T) {
got := ResolveRenderMode(RenderModeAuto, RenderModeAuto, strings.Repeat("字", 200))
if got != RenderModeCompact {
t.Errorf("expected compact for long body in auto, got %s", got)
}
}

// --- BuildCompactIMNotification tests ---

func TestBuildCompactIMNotification_TaskCompleted(t *testing.T) {
got := BuildCompactIMNotification(
"task_completed",
"guodage_tester_opus4.6",
"OPE-1005",
"https://multica.wujieai.com/openharness/issues/OPE-1005",
"验收 PASS，编译复现通过，Fork 特性保留",
)
expected := "[任务完成] guodage_tester_opus4.6 [OPE-1005](https://multica.wujieai.com/openharness/issues/OPE-1005): 验收 PASS，编译复现通过，Fork 特性保留"
if got != expected {
t.Errorf("unexpected compact notification:\n  got:  %q\n  want: %q", got, expected)
}
}

func TestBuildCompactIMNotification_TaskFailed(t *testing.T) {
got := BuildCompactIMNotification(
"task_failed",
"guodage_dev_opus4.6",
"OPE-918",
"https://multica.wujieai.com/openharness/issues/OPE-918",
"Gitee webhook 测试阻塞，缺真实 payload",
)
expected := "[任务失败] guodage_dev_opus4.6 [OPE-918](https://multica.wujieai.com/openharness/issues/OPE-918): Gitee webhook 测试阻塞，缺真实 payload"
if got != expected {
t.Errorf("unexpected compact notification:\n  got:  %q\n  want: %q", got, expected)
}
}

func TestBuildCompactIMNotification_Mentioned(t *testing.T) {
got := BuildCompactIMNotification(
"mentioned",
"张三",
"OPE-100",
"https://multica.wujieai.com/openharness/issues/OPE-100",
"请帮忙看一下这个 Bug",
)
if !strings.HasPrefix(got, "[被@]") {
t.Errorf("expected [被@] prefix, got %q", got)
}
if !strings.Contains(got, "[OPE-100](") {
t.Errorf("expected issue link in compact notification, got %q", got)
}
}

func TestBuildCompactIMNotification_NoSummary(t *testing.T) {
got := BuildCompactIMNotification(
"task_completed",
"agent",
"OPE-1",
"https://example.com/issues/OPE-1",
"",
)
if strings.HasSuffix(got, ": ") {
t.Errorf("should not end with colon-space when no summary: %q", got)
}
}

func TestBuildCompactIMNotification_NoActorName(t *testing.T) {
got := BuildCompactIMNotification(
"task_completed",
"",
"OPE-1",
"https://example.com/issues/OPE-1",
"done",
)
if strings.Contains(got, "  ") {
t.Errorf("should not have double space for empty actor: %q", got)
}
}

func TestBuildCompactIMNotification_IssueIdentifierEmbedsLink(t *testing.T) {
got := BuildCompactIMNotification(
"task_completed",
"agent",
"OPE-1005",
"https://multica.wujieai.com/openharness/issues/OPE-1005",
"summary",
)
// Issue link must be embedded in the identifier text, not appended as bare URL
if strings.Contains(got, "https://multica") && !strings.Contains(got, "[OPE-1005](https://") {
t.Errorf("issue link should be embedded in identifier, not bare: %q", got)
}
// No bare link at the end
if strings.HasSuffix(got, "https://multica.wujieai.com/openharness/issues/OPE-1005") {
t.Errorf("should not have bare link at end: %q", got)
}
}
