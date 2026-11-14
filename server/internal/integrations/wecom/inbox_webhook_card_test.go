package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var _ inboxWebhookCardQueries = (*db.Queries)(nil)

type inboxWebhookCardFixture struct {
	workspace db.Workspace
	issue     db.Issue
	comment   db.Comment
	user      db.User
	agent     db.Agent
	statuses  map[string]db.IssueStatus
	issueErr  error
	statusErr error
}

func newInboxWebhookCardFixture() *inboxWebhookCardFixture {
	workspace := util.MustParseUUID("10000000-0000-4000-8000-000000000001")
	issue := util.MustParseUUID("20000000-0000-4000-8000-000000000002")
	comment := util.MustParseUUID("30000000-0000-4000-8000-000000000003")
	user := util.MustParseUUID("40000000-0000-4000-8000-000000000004")
	agent := util.MustParseUUID("50000000-0000-4000-8000-000000000005")
	return &inboxWebhookCardFixture{
		workspace: db.Workspace{ID: workspace, Slug: "demo", IssuePrefix: "MUL"},
		issue:     db.Issue{ID: issue, WorkspaceID: workspace, Number: 42, Title: "修复登录", Status: "cancelled"},
		comment: db.Comment{ID: comment, WorkspaceID: workspace, IssueID: issue, AuthorType: "member", AuthorID: user,
			Content: "真实评论", CreatedAt: pgtype.Timestamptz{Time: time.Date(2026, 11, 10, 3, 4, 5, 0, time.UTC), Valid: true}},
		user:     db.User{ID: user, Name: "张三"},
		agent:    db.Agent{ID: agent, WorkspaceID: workspace, Name: "Agent A"},
		statuses: map[string]db.IssueStatus{"customer_wait": {Key: "customer_wait", Name: "等待客户"}},
	}
}

func (f *inboxWebhookCardFixture) GetIssueInWorkspace(_ context.Context, p db.GetIssueInWorkspaceParams) (db.Issue, error) {
	if f.issueErr != nil {
		return db.Issue{}, f.issueErr
	}
	if p.ID != f.issue.ID || p.WorkspaceID != f.issue.WorkspaceID {
		return db.Issue{}, pgx.ErrNoRows
	}
	return f.issue, nil
}

func (f *inboxWebhookCardFixture) GetWorkspace(_ context.Context, id pgtype.UUID) (db.Workspace, error) {
	if id != f.workspace.ID {
		return db.Workspace{}, pgx.ErrNoRows
	}
	return f.workspace, nil
}

func (f *inboxWebhookCardFixture) GetCommentInWorkspace(_ context.Context, p db.GetCommentInWorkspaceParams) (db.Comment, error) {
	if p.ID != f.comment.ID || p.WorkspaceID != f.comment.WorkspaceID {
		return db.Comment{}, pgx.ErrNoRows
	}
	return f.comment, nil
}

func (f *inboxWebhookCardFixture) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	if id != f.user.ID {
		return db.User{}, pgx.ErrNoRows
	}
	return f.user, nil
}

func (f *inboxWebhookCardFixture) GetAgentInWorkspace(_ context.Context, p db.GetAgentInWorkspaceParams) (db.Agent, error) {
	if p.ID != f.agent.ID || p.WorkspaceID != f.agent.WorkspaceID {
		return db.Agent{}, pgx.ErrNoRows
	}
	return f.agent, nil
}

func (f *inboxWebhookCardFixture) GetIssueStatusEntryByKey(_ context.Context, p db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error) {
	if f.statusErr != nil {
		return db.IssueStatus{}, f.statusErr
	}
	if status, ok := f.statuses[p.Key]; ok && p.WorkspaceID == f.workspace.ID {
		return status, nil
	}
	return db.IssueStatus{}, pgx.ErrNoRows
}

func inboxWebhookCardItem(f *inboxWebhookCardFixture, kind string) map[string]any {
	issueID := util.UUIDToString(f.issue.ID)
	actorID := util.UUIDToString(f.user.ID)
	actorType := "member"
	return map[string]any{
		"workspace_id": util.UUIDToString(f.workspace.ID), "issue_id": &issueID,
		"type": kind, "title": "过期标题", "body": "过期正文",
		"actor_type": &actorType, "actor_id": &actorID, "created_at": "2026-11-10T01:02:03Z",
	}
}

func decodeInboxWebhookCard(t *testing.T, f *inboxWebhookCardFixture, item map[string]any) inboxWebhookCardPayload {
	t.Helper()
	encoded, err := buildInboxWebhookCard(context.Background(), f, item)
	if err != nil {
		t.Fatalf("build card: %v", err)
	}
	var card inboxWebhookCardPayload
	if err := json.Unmarshal(encoded, &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	return card
}

func inboxWebhookRowValue(card inboxWebhookCardPayload, key string) string {
	for _, line := range card.TemplateCard.Lines {
		if line.Key == key {
			return line.Value
		}
	}
	return ""
}
func inboxWebhookTimeRow(card inboxWebhookCardPayload) string {
	if len(card.TemplateCard.Lines) != 4 {
		return ""
	}
	line := card.TemplateCard.Lines[3]
	return line.Key + " " + line.Value
}

func inboxWebhookTransition(card inboxWebhookCardPayload) string {
	return card.TemplateCard.SubTitleText
}

func TestInboxWebhookStatusUsesEventDetails(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "http://multica.local:8080")
	f := newInboxWebhookCardFixture()
	jsonText := `{"from":"todo","to":"in_progress","changed_at":"2026-11-10T02:03:04Z"}`
	for name, details := range map[string]any{
		"raw": json.RawMessage(jsonText), "string": jsonText, "pointer": &jsonText,
		"decoded": map[string]any{"from": "todo", "to": "in_progress", "changed_at": "2026-11-10T02:03:04Z"},
	} {
		t.Run(name, func(t *testing.T) {
			item := inboxWebhookCardItem(f, "status_changed")
			item["details"] = details
			card := decodeInboxWebhookCard(t, f, item)
			if card.MsgType != "template_card" || card.TemplateCard.CardType != "text_notice" {
				t.Fatalf("unexpected payload: %+v", card)
			}
			if card.TemplateCard.MainTitle.Title != "修复登录" || card.TemplateCard.Source.Desc != "MUL-42 状态变更" {
				t.Errorf("unexpected title: %+v", card.TemplateCard.MainTitle)
			}
			if got := inboxWebhookTransition(card); got != "待办 → 进行中" {
				t.Errorf("transition = %q", got)
			}
			if got := inboxWebhookTimeRow(card); got != "变更时间 11-10 10:03（北京时间）" {
				t.Errorf("event time = %q", got)
			}
			if got := card.TemplateCard.Action; got.Type != 1 || got.URL != "http://multica.local:8080/demo/issues/MUL-42" {
				t.Errorf("action = %+v", got)
			}
			if len(card.TemplateCard.Jumps) != 1 || card.TemplateCard.Jumps[0].Title != "查看任务" || card.TemplateCard.Jumps[0].URL != card.TemplateCard.Action.URL {
				t.Fatalf("visible task action must match the card action: %+v", card.TemplateCard.Jumps)
			}
		})
	}
}

func TestInboxWebhookMissingFromAndCustomStatus(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	f := newInboxWebhookCardFixture()
	item := inboxWebhookCardItem(f, "status_changed")
	item["details"] = json.RawMessage(`{"to":"customer_wait"}`)
	card := decodeInboxWebhookCard(t, f, item)
	if got := inboxWebhookTransition(card); got != "未知 → 等待客户" {
		t.Errorf("missing from/custom transition = %q", got)
	}
	if got := inboxWebhookTimeRow(card); got != "通知时间 11-10 09:02（北京时间）" {
		t.Errorf("fallback time must be identified as notification time: %q", got)
	}
	item["details"] = `{"from":"blocked","to":"missing_custom"}`
	card = decodeInboxWebhookCard(t, f, item)
	if got := inboxWebhookTransition(card); got != "阻塞 → missing_custom" {
		t.Errorf("long transition must remain complete outside emphasis: %q", got)
	}
}

func TestInboxWebhookEnglishLocaleAndNotifiedTime(t *testing.T) {
	restoreLocale(t, LocaleEn)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	f := newInboxWebhookCardFixture()
	item := inboxWebhookCardItem(f, "status_changed")
	item["details"] = `{"to":"done"}`
	card := decodeInboxWebhookCard(t, f, item)
	if card.TemplateCard.Source.Desc != "MUL-42 Status changed" || inboxWebhookTransition(card) != "Unknown → Done" {
		t.Errorf("English status: %+v", card.TemplateCard)
	}
	if got := inboxWebhookTimeRow(card); got != "Time Notified 11-10 01:02 UTC" {
		t.Errorf("fallback time = %q", got)
	}
	item["details"] = `{"from":"todo","to":"done","changed_at":"2026-11-10T02:03:04Z"}`
	card = decodeInboxWebhookCard(t, f, item)
	if got := inboxWebhookTimeRow(card); got != "Time Changed 11-10 02:03 UTC" {
		t.Errorf("changed time = %q", got)
	}
}

func TestInboxWebhookLongStatusNamesRemainDistinct(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	f := newInboxWebhookCardFixture()
	before, after := strings.Repeat("状态", 13)+"甲", strings.Repeat("状态", 13)+"乙"
	f.statuses["before"] = db.IssueStatus{Name: before}
	f.statuses["after"] = db.IssueStatus{Name: after}
	item := inboxWebhookCardItem(f, "status_changed")
	item["details"] = map[string]string{"from": "before", "to": "after"}
	card := decodeInboxWebhookCard(t, f, item)
	if got := inboxWebhookTransition(card); got != before+" → "+after {
		t.Fatalf("distinct long status names were lost: %q", got)
	}
}

func TestInboxWebhookCommentUsesCurrentRecord(t *testing.T) {
	for _, tc := range []struct {
		name, authorType, wantAuthor string
		locale                       Locale
	}{
		{"member", "member", "张三", LocaleZhHans},
		{"agent", "agent", "Agent A", LocaleEn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreLocale(t, tc.locale)
			t.Setenv("WECOM_APP_URL", "https://app.example.test/")
			f := newInboxWebhookCardFixture()
			if tc.authorType == "agent" {
				f.comment.AuthorType, f.comment.AuthorID = "agent", f.agent.ID
			}
			item := inboxWebhookCardItem(f, "new_comment")
			item["details"] = map[string]any{"comment_id": util.UUIDToString(f.comment.ID)}
			card := decodeInboxWebhookCard(t, f, item)
			wantTime := "评论时间 11-10 11:04（北京时间）"
			if tc.locale == LocaleEn {
				wantTime = "Time Commented 11-10 03:04 UTC"
			}
			if got := inboxWebhookTimeRow(card); got != wantTime {
				t.Errorf("comment time = %q, want %q", got, wantTime)
			}
			if card.TemplateCard.SubTitleText != "真实评论" {
				t.Errorf("comment body = %q", card.TemplateCard.SubTitleText)
			}
		})
	}
}

func TestInboxWebhookCommentFailClosed(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, scenario := range []string{"missing ID", "unknown comment", "other issue", "other workspace", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			f := newInboxWebhookCardFixture()
			item := inboxWebhookCardItem(f, "new_comment")
			id := util.UUIDToString(f.comment.ID)
			switch scenario {
			case "missing ID":
				id = ""
			case "unknown comment":
				id = "90000000-0000-4000-8000-000000000009"
			case "other issue":
				f.comment.IssueID = util.MustParseUUID("90000000-0000-4000-8000-000000000009")
			case "other workspace":
				f.comment.WorkspaceID = util.MustParseUUID("90000000-0000-4000-8000-000000000009")
			case "deleted":
				f.comment.DeletedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			}
			item["details"] = map[string]any{"comment_id": id}
			card, err := buildInboxWebhookCard(context.Background(), f, item)
			if err == nil || len(card) != 0 {
				t.Fatalf("unavailable comment produced card %s, error %v", card, err)
			}
		})
	}
}

func TestInboxWebhookCardBudgetAndLink(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	f := newInboxWebhookCardFixture()
	f.issue.Title = strings.Repeat("标题", 80)
	f.comment.Content = strings.Repeat("评论内容", 80)
	f.user.Name = strings.Repeat("作者", 40)
	item := inboxWebhookCardItem(f, "new_comment")
	item["details"] = json.RawMessage(`{"comment_id":"` + util.UUIDToString(f.comment.ID) + `"}`)
	card := decodeInboxWebhookCard(t, f, item)
	if got := card.TemplateCard.MainTitle.Title; !utf8.ValidString(got) || utf8.RuneCountInString(got) > 26 || !strings.HasPrefix(card.TemplateCard.Source.Desc, "MUL-42 ") {
		t.Errorf("title = %q", got)
	}
	if got := card.TemplateCard.SubTitleText; !utf8.ValidString(got) || utf8.RuneCountInString(got) != 112 || !strings.HasSuffix(got, "…") {
		t.Errorf("summary = %q", got)
	}
	if utf8.RuneCountInString(inboxWebhookTimeRow(card)) > 30 {
		t.Errorf("card exceeds display budgets: %+v", card.TemplateCard)
	}
	if got := card.TemplateCard.Action.URL; got != "https://app.example.test/demo/issues/MUL-42" {
		t.Errorf("issue link = %q", got)
	}
}

func TestInboxWebhookAppURL(t *testing.T) {
	for _, tc := range []struct {
		name, wecom, multica, frontend, want string
	}{
		{"priority", "https://first.test/", "https://second.test", "https://third.test", "https://first.test"},
		{"http and port", "http://web.example.test:8080", "", "", "http://web.example.test:8080"},
		{"fallback invalid scheme", "ftp://bad.test", "https://second.test", "", "https://second.test"},
		{"fallback invalid user", "https://secret@bad.test", "http://second.test:8080/", "", "http://second.test:8080"},
		{"fallback invalid query", "https://bad.test/?token=secret", "", "https://third.test", "https://third.test"},
		{"reject fragment", "https://bad.test/#x", "", "", ""},
		{"reject empty fragment", "https://bad.test/#", "", "", ""},
		{"reject empty query", "https://bad.test/?", "", "", ""},
		{"reject path", "https://bad.test/path", "", "", ""},
		{"reject invalid port", "http://example.test:99999", "", "", ""},
		{"reject missing host", "https:///path", "", "", ""},
		{"reject loopback ipv6", "http://[::1]:8080", "", "", ""},
		{"reject loopback ipv4", "http://127.0.0.1:18898", "", "", ""},
		{"reject localhost", "http://localhost:18898", "", "", ""},
		{"reject localhost domain", "http://preview.localhost", "", "", ""},
		{"reject unspecified", "http://0.0.0.0:18898", "", "", ""},
		{"reject compact ipv4", "http://127.1", "", "", ""},
		{"reject decimal ipv4", "http://2130706433", "", "", ""},
		{"reject decimal unspecified", "http://0", "", "", ""},
		{"reject hex ipv4", "http://0x7f000001", "", "", ""},
		{"reject octal ipv4", "http://0177.0.0.1", "", "", ""},
		{"reject hex component", "http://0x7f.0.0.1", "", "", ""},
		{"canonical remote ipv4", "http://192.0.2.10:8080", "", "", "http://192.0.2.10:8080"},
		{"fallback from local override", "http://127.0.0.1:18898", "http://web.example.test", "", "http://web.example.test"},
		{"fallback from numeric override", "http://127.1", "http://web.example.test", "", "http://web.example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WECOM_APP_URL", tc.wecom)
			t.Setenv("MULTICA_APP_URL", tc.multica)
			t.Setenv("FRONTEND_ORIGIN", tc.frontend)
			if got := inboxWebhookAppURL(); got != tc.want {
				t.Errorf("origin = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInboxWebhookFailsSafely(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	f := newInboxWebhookCardFixture()
	item := inboxWebhookCardItem(f, "status_changed")
	item["details"] = `{"from":"todo","to":"done"}`
	for _, name := range []string{"WECOM_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		t.Setenv(name, "")
	}
	if card, err := buildInboxWebhookCard(context.Background(), f, item); err == nil || len(card) != 0 || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("missing origin: card=%s err=%v", card, err)
	}
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, field := range []string{"workspace_id", "issue_id"} {
		missing := inboxWebhookCardItem(f, "status_changed")
		missing[field] = ""
		if card, err := buildInboxWebhookCard(context.Background(), f, missing); err == nil || len(card) != 0 {
			t.Fatalf("missing %s: card=%s err=%v", field, card, err)
		}
	}
	item["workspace_id"] = "90000000-0000-4000-8000-000000000009"
	if card, err := buildInboxWebhookCard(context.Background(), f, item); err == nil || len(card) != 0 {
		t.Fatalf("cross-workspace issue: card=%s err=%v", card, err)
	}
	item = inboxWebhookCardItem(f, "status_changed")
	item["details"] = `{"from":"customer_wait","to":"done"}`
	f.statusErr = errors.New("sensitive secret")
	if card, err := buildInboxWebhookCard(context.Background(), f, item); err == nil || len(card) != 0 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("sensitive error: card=%s err=%v", card, err)
	}
}

func TestInboxWebhookCardUsesBalancedTextAndEventColor(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, tc := range []struct {
		kind, to string
		color    int
	}{
		{"status_changed", "blocked", 2}, {"status_changed", "done", 3},
		{"status_changed", "in_progress", 1}, {"status_changed", "custom", 1},
		{"new_comment", "", 3},
	} {
		t.Run(tc.kind+tc.to, func(t *testing.T) {
			f := newInboxWebhookCardFixture()
			f.issue.Title = "修复企微状态通知：显示前后状态并附任务 Web 链接"
			item := inboxWebhookCardItem(f, tc.kind)
			if tc.kind == "status_changed" {
				item["details"] = map[string]string{"from": "in_progress", "to": tc.to}
			} else {
				item["details"] = map[string]string{"comment_id": util.UUIDToString(f.comment.ID)}
			}
			card := decodeInboxWebhookCard(t, f, item)
			if card.TemplateCard.Source.DescColor != tc.color {
				t.Errorf("color = %d", card.TemplateCard.Source.DescColor)
			}
			if card.TemplateCard.MainTitle.Title != "修复企微状态通知：显示前后状态并附任务 Web 链接" {
				t.Errorf("title = %q", card.TemplateCard.MainTitle.Title)
			}
			raw, err := buildInboxWebhookCard(context.Background(), f, item)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "horizontal_content_list") || len(card.TemplateCard.Lines) != 4 {
				t.Fatal("task metadata must use aligned labeled rows")
			}
			if strings.Contains(string(raw), "emphasis_content") {
				t.Fatal("giant emphasis text must not be emitted")
			}
			if tc.kind == "status_changed" && card.TemplateCard.SubTitleText == "" {
				t.Fatal("status must use regular body text")
			}
		})
	}
}

func TestInboxWebhookCardKeepsActualTaskName(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, tc := range []struct{ input, want string }{
		{"Fix login: retry failures", "Fix login: retry failures"},
		{"修复登录", "修复登录"},
		{"  修复  登录：重试失败\n详情  ", "修复 登录：重试失败 详情"},
		{"", "MUL-42"},
	} {
		for _, kind := range []string{"status_changed", "new_comment"} {
			f := newInboxWebhookCardFixture()
			f.issue.Title = tc.input
			item := inboxWebhookCardItem(f, kind)
			if kind == "new_comment" {
				item["details"] = map[string]string{"comment_id": util.UUIDToString(f.comment.ID)}
			}
			card := decodeInboxWebhookCard(t, f, item)
			if got := card.TemplateCard.MainTitle.Title; got != tc.want {
				t.Errorf("%s title(%q) = %q, want %q", kind, tc.input, got, tc.want)
			}
		}
	}
}

func TestInboxWebhookCardIncludesIssueIdentifierAndName(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, kind := range []string{"status_changed", "new_comment"} {
		f := newInboxWebhookCardFixture()
		f.workspace.IssuePrefix, f.issue.Number, f.issue.Title = "AIMS", 122, "修复任务通知：带上名称和编号"
		item := inboxWebhookCardItem(f, kind)
		if kind == "new_comment" {
			item["details"] = map[string]string{"comment_id": util.UUIDToString(f.comment.ID)}
		}
		card := decodeInboxWebhookCard(t, f, item)
		if !strings.HasPrefix(card.TemplateCard.Source.Desc, "AIMS-122 ") || card.TemplateCard.MainTitle.Title != f.issue.Title {
			t.Errorf("%s must include actual identifier and task name: %+v", kind, card.TemplateCard)
		}
		if !strings.HasSuffix(card.TemplateCard.Action.URL, "/issues/AIMS-122") {
			t.Errorf("wrong task link: %q", card.TemplateCard.Action.URL)
		}
	}
}

func TestInboxWebhookOtherTaskNotificationsKeepDetails(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, tc := range []struct{ kind, label string }{
		{"issue_assigned", "任务指派"}, {"unassigned", "取消指派"},
		{"assignee_changed", "指派人变更"}, {"mentioned", "提及你"},
		{"priority_changed", "优先级变更"}, {"due_date_changed", "截止日期变更"},
		{"start_date_changed", "开始日期变更"}, {"task_failed", "task 失败"},
		{"reaction_added", "表情反应"},
	} {
		f := newInboxWebhookCardFixture()
		item := inboxWebhookCardItem(f, tc.kind)
		item["body"] = "需要查看的通知摘要"
		card := decodeInboxWebhookCard(t, f, item)
		want := "需要查看的通知摘要"
		switch tc.kind {
		case "issue_assigned":
			want = "指派给：未知"
		case "unassigned":
			want = "取消指派：未知"
		case "assignee_changed":
			want = "未指派 → 未指派"
		case "mentioned":
			want = "任务描述中提及你\n需要查看的通知摘要"
		}
		if card.TemplateCard.Source.Desc != "MUL-42 "+tc.label || card.TemplateCard.MainTitle.Title != f.issue.Title || card.TemplateCard.SubTitleText != want {
			t.Errorf("%s lost task or event details: %+v", tc.kind, card.TemplateCard)
		}
	}
	for _, tc := range []struct {
		kind    string
		details any
		want    string
	}{
		{"priority_changed", map[string]string{"from": "low", "to": "high"}, "优先级变更 · low → high"},
		{"reaction_added", map[string]string{"emoji": "👍"}, "表情反应 · 👍"},
	} {
		f := newInboxWebhookCardFixture()
		item := inboxWebhookCardItem(f, tc.kind)
		item["body"], item["details"] = "", tc.details
		if got := decodeInboxWebhookCard(t, f, item).TemplateCard.SubTitleText; got != tc.want {
			t.Errorf("%s details=%q", tc.kind, got)
		}
	}
}

func TestInboxWebhookMentionAndReactionRejectUnavailableComments(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	for _, kind := range []string{"mentioned", "reaction_added"} {
		f := newInboxWebhookCardFixture()
		f.comment.DeletedAt.Valid = true
		item := inboxWebhookCardItem(f, kind)
		item["details"] = map[string]string{"comment_id": util.UUIDToString(f.comment.ID)}
		if raw, err := buildInboxWebhookCard(context.Background(), f, item); err == nil || len(raw) != 0 {
			t.Errorf("%s exposed deleted comment", kind)
		}
	}
}

func TestInboxWebhookAssignmentFormatsEventPeople(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	f := newInboxWebhookCardFixture()
	member, agent := util.UUIDToString(f.user.ID), util.UUIDToString(f.agent.ID)
	for _, tc := range []struct {
		kind    string
		details map[string]string
		want    string
	}{
		{"issue_assigned", map[string]string{"new_assignee_type": "agent", "new_assignee_id": agent}, "指派给：Agent A"},
		{"unassigned", map[string]string{"prev_assignee_type": "member", "prev_assignee_id": member}, "取消指派：张三"},
		{"assignee_changed", map[string]string{"prev_assignee_type": "member", "prev_assignee_id": member, "new_assignee_type": "agent", "new_assignee_id": agent}, "张三 → Agent A"},
		{"assignee_changed", map[string]string{"prev_assignee_type": "member", "prev_assignee_id": member}, "张三 → 未指派"},
		{"issue_assigned", nil, "指派给：张三"},
		{"unassigned", nil, "取消指派：张三"},
		{"issue_assigned", map[string]string{"new_assignee_type": "member", "new_assignee_id": "invalid-private-id"}, "指派给：未知"},
	} {
		item := inboxWebhookCardItem(f, tc.kind)
		item["recipient_type"], item["recipient_id"], item["details"] = "member", member, tc.details
		if got := decodeInboxWebhookCard(t, f, item).TemplateCard.SubTitleText; got != tc.want {
			t.Errorf("%s=%q want %q", tc.kind, got, tc.want)
		}
	}
	restoreLocale(t, LocaleEn)
	item := inboxWebhookCardItem(f, "issue_assigned")
	item["details"] = map[string]string{"new_assignee_type": "agent", "new_assignee_id": agent}
	if got := decodeInboxWebhookCard(t, f, item).TemplateCard.SubTitleText; got != "Assigned to: Agent A" {
		t.Errorf("English assignment=%q", got)
	}
}

func TestInboxWebhookMentionFormatsCommentAndDescription(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	t.Setenv("WECOM_APP_URL", "https://app.example.test")
	f := newInboxWebhookCardFixture()
	text := "[@张三](mention://member/" + util.UUIDToString(f.user.ID) + ") 请查看 [MUL-42](mention://issue/" + util.UUIDToString(f.issue.ID) + ")"
	f.comment.Content, f.issue.Description = text, pgtype.Text{String: text, Valid: true}
	item := inboxWebhookCardItem(f, "mentioned")
	item["details"] = map[string]string{"comment_id": util.UUIDToString(f.comment.ID)}
	if got := decodeInboxWebhookCard(t, f, item).TemplateCard.SubTitleText; got != "评论中提及你\n张三 请查看 MUL-42" {
		t.Errorf("comment mention=%q", got)
	}
	item["body"], item["details"] = nil, nil
	if got := decodeInboxWebhookCard(t, f, item).TemplateCard.SubTitleText; got != "任务描述中提及你\n张三 请查看 MUL-42" {
		t.Errorf("description mention=%q", got)
	}
	f.issue.Description.String = strings.Repeat("长内容", 100)
	if got := decodeInboxWebhookCard(t, f, item).TemplateCard.SubTitleText; utf8.RuneCountInString(got) > 112 || !strings.HasSuffix(got, "…") {
		t.Errorf("mention budget=%q", got)
	}
}

func TestInboxWebhookCardUsesReadableLabeledTaskFields(t *testing.T) {
	for _, tc := range []struct {
		locale                                        Locale
		priorityLabel, dueLabel, actorLabel, priority string
	}{
		{LocaleZhHans, "优先级", "截止时间", "操作者", "普通"},
		{LocaleEn, "Prio.", "Due", "By", "Normal"},
	} {
		restoreLocale(t, tc.locale)
		t.Setenv("WECOM_APP_URL", "https://app.example.test")
		f := newInboxWebhookCardFixture()
		f.issue.Priority = "medium"
		f.issue.DueDate = pgtype.Date{Time: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), Valid: true}
		card := decodeInboxWebhookCard(t, f, inboxWebhookCardItem(f, "status_changed"))
		if card.TemplateCard.MainTitle.Desc != "" {
			t.Error("metadata must not crowd the title")
		}
		for key, want := range map[string]string{tc.priorityLabel: tc.priority, tc.dueLabel: "2026-11-12", tc.actorLabel: "张三"} {
			if got := inboxWebhookRowValue(card, key); got != want {
				t.Errorf("%s=%q want %q", key, got, want)
			}
		}
		for _, line := range card.TemplateCard.Lines {
			if utf8.RuneCountInString(line.Key) > 5 || utf8.RuneCountInString(line.Value) > 26 {
				t.Errorf("metadata exceeds native card budget: %+v", line)
			}
		}
		f.issue.DueDate.Valid = false
		card = decodeInboxWebhookCard(t, f, inboxWebhookCardItem(f, "status_changed"))
		if inboxWebhookRowValue(card, tc.dueLabel) != "-" {
			t.Error("missing due date must stay unavailable")
		}
	}
}

func TestInboxWebhookExcerptKeepsParagraphAndBulletStructure(t *testing.T) {
	got := inboxWebhookExcerpt("  说明\r\n\r\n\r\n - 现象：层级丢失\r\n - 预期：子任务缩进  ", 112)
	if got != "说明\n\n- 现象：层级丢失\n- 预期：子任务缩进" {
		t.Errorf("excerpt=%q", got)
	}
	if got := inboxWebhookExcerpt(strings.Repeat("正文", 100), 112); utf8.RuneCountInString(got) != 112 || !strings.HasSuffix(got, "…") {
		t.Errorf("excerpt budget=%q", got)
	}
}
