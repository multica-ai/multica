package command_test

import (
	"reflect"
	"testing"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	chcommand "github.com/multica-ai/multica/server/internal/channel/command"
)

// TC-command-1 rule-engine rows from STA-37 (source=rule corpus).
func TestRuleMatcher_Corpus_Table(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()

	cases := []struct {
		text       string
		wantKind   chaction.Kind
		wantParams map[string]string
	}{
		{"帮我记一个 登录页加载慢", chaction.KindCreateIssue, map[string]string{"title": "登录页加载慢"}},
		{"创建一个 Issue：导出报错 500", chaction.KindCreateIssue, map[string]string{"title": "导出报错 500"}},
		{"在 [STA-2] 上加一条评论：已找产品确认", chaction.KindAddComment, map[string]string{"issue_key": "STA-2", "comment": "已找产品确认"}},
		{"在 [sta-2] 上加一条评论：已找产品确认", chaction.KindAddComment, map[string]string{"issue_key": "STA-2", "comment": "已找产品确认"}},
		{"STA-12 评论：请补一下截图", chaction.KindAddComment, map[string]string{"issue_key": "STA-12", "comment": "请补一下截图"}},
		{"[STA-2] 到哪了？", chaction.KindQueryProgress, map[string]string{"scope": "issue", "issue_key": "STA-2"}},
		{"sta-1 这个 issue 怎么样了", chaction.KindQueryProgress, map[string]string{"scope": "issue", "issue_key": "STA-1"}},
		{"STA-5 现在状态", chaction.KindQueryProgress, map[string]string{"scope": "issue", "issue_key": "STA-5"}},
		{"我的待办", chaction.KindQueryIssue, map[string]string{}},
		{"把 [STA-2] 标成 done", chaction.KindSetStatus, map[string]string{"issue_key": "STA-2", "status": "done"}},
		{"STA-7 完成了", chaction.KindSetStatus, map[string]string{"issue_key": "STA-7", "status": "done"}},
		{"STA-5 改成 in_progress", chaction.KindSetStatus, map[string]string{"issue_key": "STA-5", "status": "in_progress"}},
		{"删除 [STA-2]", chaction.KindUnsupported, map[string]string{"issue_key": "STA-2"}},
		{"上传一张图给 [STA-2]", chaction.KindUnsupported, map[string]string{"issue_key": "STA-2"}},
		{"在么", chaction.KindUnknown, map[string]string{}},
	}

	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			got, ok := m.Match(tc.text)
			if !ok {
				t.Fatalf("Match(%q): got ok=false, want rule hit", tc.text)
			}
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Confidence != 1 {
				t.Errorf("Confidence = %v, want 1", got.Confidence)
			}
			if got.Source != chaction.SourceRule {
				t.Errorf("Source = %q, want %q", got.Source, chaction.SourceRule)
			}
			if !reflect.DeepEqual(got.Params, tc.wantParams) {
				t.Errorf("Params = %#v, want %#v", got.Params, tc.wantParams)
			}
		})
	}
}

func TestRuleMatcher_CreateIssue_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []string{
		"帮我记一个 BUG：首页白屏",
		"帮我记一个  P99 延迟飙升 ",
		"创建一个 Issue：仅复现在 Safari",
		"创建一个 issue：大小写混用",
		"创建一个Issue：无空格也可以",
	}
	for _, s := range variants {
		if _, ok := m.Match(s); !ok {
			t.Errorf("expected CreateIssue hit for %q", s)
			continue
		}
		got, _ := m.Match(s)
		if got.Kind != chaction.KindCreateIssue {
			t.Errorf("%q: got kind %q", s, got.Kind)
		}
		if got.Params["title"] == "" {
			t.Errorf("%q: empty title param", s)
		}
	}
}

func TestRuleMatcher_AddComment_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []struct {
		text    string
		wantKey string
	}{
		{"在 [BUG-1] 上加一条评论：复现步骤见附件", "BUG-1"},
		{"在[STA-99]上评论：LGTM", "STA-99"},
		{"STA-3 评论：需要设计稿", "STA-3"},
		{"sta-3 评论：需要设计稿", "STA-3"},
		{"XY-42 评论：ping", "XY-42"},
		{"ZZ-1 评论：hello：world", "ZZ-1"},
	}
	for _, tc := range variants {
		got, ok := m.Match(tc.text)
		if !ok || got.Kind != chaction.KindAddComment {
			t.Fatalf("%q: want AddComment hit", tc.text)
		}
		if got.Params["issue_key"] != tc.wantKey {
			t.Errorf("%q issue_key=%q want %q", tc.text, got.Params["issue_key"], tc.wantKey)
		}
	}
}

func TestRuleMatcher_QueryIssue_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []struct {
		text    string
		wantKey string
	}{
		{"[STA-2] 到哪了", "STA-2"},
		{"STA-9 到哪了？", "STA-9"},
		{"AB-3 现在状态", "AB-3"},
		{"sta-1 这个 issue 怎么样了", "STA-1"},
		{"STA-1 怎么样", "STA-1"},
		{"STA-1 什么情况", "STA-1"},
		{"STA-1 进展怎么样", "STA-1"},
		{"STA-1 状态怎么样", "STA-1"},
	}
	for _, tc := range variants {
		got, ok := m.Match(tc.text)
		if !ok || got.Kind != chaction.KindQueryProgress {
			t.Fatalf("%q: want QueryProgress hit", tc.text)
		}
		if tc.wantKey != "" && got.Params["issue_key"] != tc.wantKey {
			t.Errorf("%q issue_key=%q want %q", tc.text, got.Params["issue_key"], tc.wantKey)
		}
		if got.Params["scope"] != "issue" {
			t.Errorf("%q scope=%q want issue", tc.text, got.Params["scope"])
		}
	}
	for _, text := range []string{"我的待办", "待办列表", "看一下待办", "我有哪些待办"} {
		got, ok := m.Match(text)
		if !ok || got.Kind != chaction.KindQueryIssue || len(got.Params) != 0 {
			t.Fatalf("%q should be empty-param QueryIssue, got ok=%v kind=%q params=%#v", text, ok, got.Kind, got.Params)
		}
	}
}

func TestRuleMatcher_SetStatus_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []struct {
		text       string
		wantStatus string
	}{
		{"把 [STA-2] 标成 todo", "todo"},
		{"把STA-3标成blocked", "blocked"},
		{"STA-9 完成了", "done"},
		{"sta-9 完成了", "done"},
		{"AB-1 改成 done", "done"},
		{"CD-2 改成 in_review", "in_review"},
	}
	for _, tc := range variants {
		got, ok := m.Match(tc.text)
		if !ok || got.Kind != chaction.KindSetStatus {
			t.Fatalf("%q: want SetStatus hit", tc.text)
		}
		if got.Params["status"] != tc.wantStatus {
			t.Errorf("%q status=%q want %q", tc.text, got.Params["status"], tc.wantStatus)
		}
	}
}

func TestRuleMatcher_SetAssignee_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []struct {
		text       string
		wantKey    string
		wantAssign string
	}{
		{"把 [STA-2] 指派给 @张三", "STA-2", "张三"},
		{"把STA-3指派给李四", "STA-3", "李四"},
		{"STA-9 指派给 @王五", "STA-9", "王五"},
		{"sta-9 指派给 @王五", "STA-9", "王五"},
	}
	for _, tc := range variants {
		got, ok := m.Match(tc.text)
		if !ok || got.Kind != chaction.KindSetAssignee {
			t.Fatalf("%q: want SetAssignee hit", tc.text)
		}
		if got.Params["issue_key"] != tc.wantKey {
			t.Errorf("%q issue_key=%q want %q", tc.text, got.Params["issue_key"], tc.wantKey)
		}
		if got.Params["assignee"] != tc.wantAssign {
			t.Errorf("%q assignee=%q want %q", tc.text, got.Params["assignee"], tc.wantAssign)
		}
	}
}

func TestRuleMatcher_SetPriority_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []struct {
		text         string
		wantKey      string
		wantPriority string
	}{
		{"把 [STA-2] 改优先级 high", "STA-2", "high"},
		{"把STA-3改优先级urgent", "STA-3", "urgent"},
		{"STA-9 改优先级 medium", "STA-9", "medium"},
	}
	for _, tc := range variants {
		got, ok := m.Match(tc.text)
		if !ok || got.Kind != chaction.KindSetPriority {
			t.Fatalf("%q: want SetPriority hit", tc.text)
		}
		if got.Params["issue_key"] != tc.wantKey {
			t.Errorf("%q issue_key=%q want %q", tc.text, got.Params["issue_key"], tc.wantKey)
		}
		if got.Params["priority"] != tc.wantPriority {
			t.Errorf("%q priority=%q want %q", tc.text, got.Params["priority"], tc.wantPriority)
		}
	}
}

func TestRuleMatcher_SetLabel_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	variants := []struct {
		text      string
		wantKey   string
		wantLabel string
		wantOp    string
	}{
		{"把 [STA-2] 加标签 bug", "STA-2", "bug", "add"},
		{"STA-3 加标签 feature", "STA-3", "feature", "add"},
		{"把 [STA-4] 去掉标签 bug", "STA-4", "bug", "remove"},
		{"STA-5 去掉标签 duplicate", "STA-5", "duplicate", "remove"},
	}
	for _, tc := range variants {
		got, ok := m.Match(tc.text)
		if !ok || got.Kind != chaction.KindSetLabel {
			t.Fatalf("%q: want SetLabel hit", tc.text)
		}
		if got.Params["issue_key"] != tc.wantKey {
			t.Errorf("%q issue_key=%q want %q", tc.text, got.Params["issue_key"], tc.wantKey)
		}
		if got.Params["label"] != tc.wantLabel {
			t.Errorf("%q label=%q want %q", tc.text, got.Params["label"], tc.wantLabel)
		}
		if got.Params["op"] != tc.wantOp {
			t.Errorf("%q op=%q want %q", tc.text, got.Params["op"], tc.wantOp)
		}
	}
}

func TestRuleMatcher_Unknown_Variants(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	for _, s := range []string{"在吗", "你好", "Hello", "Hi", "您好"} {
		got, ok := m.Match(s)
		if !ok || got.Kind != chaction.KindUnknown {
			t.Errorf("%q: want Unknown, got ok=%v kind=%v", s, ok, got.Kind)
		}
	}
}

// Negatives: must not match any rule (chat semantic resolver may handle these).
func TestRuleMatcher_NoHit_Negatives(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	negatives := []string{
		// chatter / ambiguous (≥5)
		"今天食堂不好吃",
		"STA-2 好像有点问题但我不知道该怎么说",
		"随便聊聊",
		"把这个 PR 合并一下",
		"回忆一下上周的需求",
		// looks like issue key but wrong verb context
		"STA-2",
		"[STA-2]",
		// partial command stems
		"创建一个",
		"评论一下",
		"完成了吗",
		// destructive followed by extra words (must not match unsupported delete)
		"删除 STA-2 的所有评论",
		// upload without image cue
		"给 STA-2 发个文件",
		// todo-ish but not exact
		"我的待办清单",
		// whitespace-only handled via TrimSpace → empty
		"   ",
	}
	for _, s := range negatives {
		if _, ok := m.Match(s); ok {
			t.Errorf("expected no rule hit for %q", s)
		}
	}
}

func TestRuleMatcher_EmptyString_NoHit(t *testing.T) {
	t.Parallel()
	if _, ok := chcommand.NewRuleMatcher().Match(""); ok {
		t.Fatal("empty text should not hit")
	}
}

// Negatives for new intents: missing params / no issue identifier.
func TestRuleMatcher_NewIntents_NegativeCases(t *testing.T) {
	t.Parallel()
	m := chcommand.NewRuleMatcher()
	negatives := []string{
		// SetAssignee: missing assignee
		"把 STA-1 指派给",
		"指派给 @张三",
		// SetPriority: missing priority value
		"STA-1 改优先级",
		"改优先级 high",
		// SetLabel: missing label
		"STA-1 加标签",
		"加标签 bug",
		"STA-1 去掉标签",
		"去掉标签 bug",
	}
	for _, s := range negatives {
		if _, ok := m.Match(s); ok {
			t.Errorf("expected no rule hit for %q", s)
		}
	}
}
