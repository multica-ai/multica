package lark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeInboxNotifierQueries struct {
	binding           db.LarkUserBinding
	bindingErr        error
	policy            db.LarkWorkspaceNotificationPolicy
	policyErr         error
	installation      db.LarkInstallation
	installationErr   error
	issue             db.Issue
	issueErr          error
	workspace         db.Workspace
	workspaceErr      error
	bindingCalls      int
	policyCalls       int
	installationCalls int
	issueCalls        int
	workspaceCalls    int
	bindingArgs       []db.GetActiveLarkMemberBindingForWorkspaceUserParams
	installationIDs   []pgtype.UUID
	callSequence      []string
	created           []db.CreateLarkInboxNotificationParams
	createErr         error
}

func (f *fakeInboxNotifierQueries) GetActiveLarkMemberBindingForWorkspaceUser(_ context.Context, arg db.GetActiveLarkMemberBindingForWorkspaceUserParams) (db.LarkUserBinding, error) {
	f.bindingCalls++
	f.bindingArgs = append(f.bindingArgs, arg)
	f.callSequence = append(f.callSequence, "binding")
	return f.binding, f.bindingErr
}

func (f *fakeInboxNotifierQueries) GetLarkWorkspaceNotificationPolicy(_ context.Context, _ pgtype.UUID) (db.LarkWorkspaceNotificationPolicy, error) {
	f.policyCalls++
	f.callSequence = append(f.callSequence, "policy")
	return f.policy, f.policyErr
}

func (f *fakeInboxNotifierQueries) GetLarkInstallation(_ context.Context, id pgtype.UUID) (db.LarkInstallation, error) {
	f.installationCalls++
	f.installationIDs = append(f.installationIDs, id)
	f.callSequence = append(f.callSequence, "installation")
	return f.installation, f.installationErr
}

func (f *fakeInboxNotifierQueries) GetIssue(context.Context, pgtype.UUID) (db.Issue, error) {
	f.issueCalls++
	f.callSequence = append(f.callSequence, "issue")
	return f.issue, f.issueErr
}

func (f *fakeInboxNotifierQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	f.workspaceCalls++
	f.callSequence = append(f.callSequence, "workspace")
	return f.workspace, f.workspaceErr
}

func (f *fakeInboxNotifierQueries) CreateLarkInboxNotification(_ context.Context, arg db.CreateLarkInboxNotificationParams) (db.LarkInboxNotification, error) {
	f.created = append(f.created, arg)
	f.callSequence = append(f.callSequence, "mapping")
	return db.LarkInboxNotification{}, f.createErr
}

type fakeInboxCredentials struct {
	calls int
	err   error
}

func (f *fakeInboxCredentials) DecryptAppSecret(db.LarkInstallation) (string, error) {
	f.calls++
	return "secret", f.err
}

type fakeInboxClient struct {
	APIClient
	configured  bool
	directCalls []SendDirectTextParams
	directID    string
	directErr   error
}

func (f *fakeInboxClient) IsConfigured() bool { return f.configured }

func (f *fakeInboxClient) SendDirectTextMessage(_ context.Context, p SendDirectTextParams) (string, error) {
	f.directCalls = append(f.directCalls, p)
	return f.directID, f.directErr
}

func TestShouldDeliverInboxNotification(t *testing.T) {
	validIssue := pgtype.UUID{Valid: true}
	defaults := DefaultNotificationEvents()

	tests := []struct {
		name    string
		item    inboxNotificationItem
		enabled []string
		want    bool
	}{
		{name: "default event", item: inboxNotificationItem{recipientType: "member", issueID: validIssue, typ: "mentioned"}, enabled: defaults, want: true},
		{name: "optional event enabled", item: inboxNotificationItem{recipientType: "member", issueID: validIssue, typ: "status_changed"}, enabled: []string{"status_changed"}, want: true},
		{name: "optional event absent from defaults", item: inboxNotificationItem{recipientType: "member", issueID: validIssue, typ: "status_changed"}, enabled: defaults, want: false},
		{name: "empty enabled list", item: inboxNotificationItem{recipientType: "member", typ: "quick_create_failed"}, enabled: nil, want: false},
		{name: "missing issue allowed", item: inboxNotificationItem{recipientType: "member", typ: "quick_create_failed"}, enabled: []string{"quick_create_failed"}, want: true},
		{name: "agent recipient", item: inboxNotificationItem{recipientType: "agent", typ: "quick_create_failed"}, enabled: []string{"quick_create_failed"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDeliverInboxNotification(tt.item, tt.enabled); got != tt.want {
				t.Fatalf("shouldDeliverInboxNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInboxNotifierQuickCreateFailedWithoutIssueSendsWithoutMapping(t *testing.T) {
	queries, credentials, client, notifier := newInboxNotifierHarness([]string{"quick_create_failed"})

	notifier.Handle(context.Background(), inboxPayload("quick_create_failed", "member", ""))

	if len(client.directCalls) != 1 {
		t.Fatalf("direct message calls = %d, want 1", len(client.directCalls))
	}
	if strings.Contains(client.directCalls[0].Text, "Issue：") || strings.Contains(client.directCalls[0].Text, "回复此飞书消息") {
		t.Fatalf("issue-less text contains issue-only content: %q", client.directCalls[0].Text)
	}
	for _, want := range []string{"Multica 收件箱通知", "标题：通知标题", "类型：快速创建失败", "级别：需要关注", "通知正文"} {
		if !strings.Contains(client.directCalls[0].Text, want) {
			t.Fatalf("issue-less text %q does not contain %q", client.directCalls[0].Text, want)
		}
	}
	if len(queries.created) != 0 {
		t.Fatalf("mapping calls = %d, want 0", len(queries.created))
	}
	if queries.issueCalls != 0 || queries.workspaceCalls != 0 {
		t.Fatalf("issue/workspace calls = %d/%d, want 0/0", queries.issueCalls, queries.workspaceCalls)
	}
	if credentials.calls != 1 {
		t.Fatalf("credential calls = %d, want 1", credentials.calls)
	}
}

func TestInboxNotifierIssueLinkedSavesMapping(t *testing.T) {
	queries, _, client, notifier := newInboxNotifierHarness([]string{"mentioned"})
	queries.issue = db.Issue{Title: "数据库标题", Number: 42, WorkspaceID: mustInboxUUID("00000000-0000-0000-0000-000000000002")}
	queries.workspace = db.Workspace{IssuePrefix: "MUL"}

	notifier.Handle(context.Background(), inboxPayload("mentioned", "member", "00000000-0000-0000-0000-000000000005"))

	if len(client.directCalls) != 1 || !strings.Contains(client.directCalls[0].Text, "Issue：MUL-42") || !strings.Contains(client.directCalls[0].Text, "回复此飞书消息") {
		t.Fatalf("issue-linked direct messages = %#v", client.directCalls)
	}
	if len(queries.created) != 1 || !queries.created[0].IssueID.Valid {
		t.Fatalf("created mappings = %#v, want one issue-linked mapping", queries.created)
	}
}

func TestInboxNotifierSendFailureDoesNotSaveMapping(t *testing.T) {
	queries, _, client, notifier := newInboxNotifierHarness([]string{"mentioned"})
	client.directErr = errors.New("send failed")

	notifier.Handle(context.Background(), inboxPayload("mentioned", "member", "00000000-0000-0000-0000-000000000005"))

	if len(client.directCalls) != 1 {
		t.Fatalf("direct message calls = %d, want 1", len(client.directCalls))
	}
	if len(queries.created) != 0 {
		t.Fatalf("mapping calls = %d, want 0", len(queries.created))
	}
}

func TestInboxNotifierMappingFailureDoesNotRetrySend(t *testing.T) {
	queries, _, client, notifier := newInboxNotifierHarness([]string{"mentioned"})
	queries.createErr = errors.New("mapping failed")

	notifier.Handle(context.Background(), inboxPayload("mentioned", "member", "00000000-0000-0000-0000-000000000005"))

	if len(client.directCalls) != 1 {
		t.Fatalf("direct message calls = %d, want 1", len(client.directCalls))
	}
	if len(queries.created) != 1 {
		t.Fatalf("mapping calls = %d, want 1", len(queries.created))
	}
}

func TestInboxNotifierDisabledEventStopsAfterWorkspacePolicy(t *testing.T) {
	queries, credentials, client, notifier := newInboxNotifierHarness(DefaultNotificationEvents())

	notifier.Handle(context.Background(), inboxPayload("status_changed", "member", "00000000-0000-0000-0000-000000000005"))

	if queries.policyCalls != 1 || queries.bindingCalls != 0 || queries.installationCalls != 0 {
		t.Fatalf("policy/binding/installation calls = %d/%d/%d, want 1/0/0", queries.policyCalls, queries.bindingCalls, queries.installationCalls)
	}
	if got := strings.Join(queries.callSequence, ","); got != "policy" {
		t.Fatalf("query call sequence = %q, want %q", got, "policy")
	}
	if credentials.calls != 0 || queries.issueCalls != 0 || queries.workspaceCalls != 0 || len(client.directCalls) != 0 || len(queries.created) != 0 {
		t.Fatalf("disabled event continued: creds=%d issue=%d workspace=%d send=%d mapping=%d", credentials.calls, queries.issueCalls, queries.workspaceCalls, len(client.directCalls), len(queries.created))
	}
}

func TestInboxNotifierGates(t *testing.T) {
	t.Run("non-member stops before database", func(t *testing.T) {
		queries, _, client, notifier := newInboxNotifierHarness([]string{"mentioned"})
		notifier.Handle(context.Background(), inboxPayload("mentioned", "agent", "00000000-0000-0000-0000-000000000005"))
		if queries.bindingCalls != 0 || len(client.directCalls) != 0 {
			t.Fatalf("non-member reached database or client")
		}
	})

	t.Run("unconfigured client stops before database", func(t *testing.T) {
		queries, _, client, notifier := newInboxNotifierHarness([]string{"mentioned"})
		client.configured = false
		notifier.Handle(context.Background(), inboxPayload("mentioned", "member", "00000000-0000-0000-0000-000000000005"))
		if queries.bindingCalls != 0 {
			t.Fatalf("unconfigured client database calls = %d, want 0", queries.bindingCalls)
		}
	})

	t.Run("credential failure prevents send", func(t *testing.T) {
		_, credentials, client, notifier := newInboxNotifierHarness([]string{"mentioned"})
		credentials.err = errors.New("decrypt failed")
		notifier.Handle(context.Background(), inboxPayload("mentioned", "member", "00000000-0000-0000-0000-000000000005"))
		if len(client.directCalls) != 0 {
			t.Fatalf("credential failure send calls = %d, want 0", len(client.directCalls))
		}
	})
}

func TestFormatInboxNotificationTextUsesChineseLabels(t *testing.T) {
	item := inboxNotificationItem{typ: "task_failed", severity: "action_required", title: "创建 issue 2 测试"}
	got := formatInboxNotificationText("JTT-4", item)
	want := strings.Join([]string{
		"Multica 收件箱通知",
		"Issue：JTT-4",
		"标题：创建 issue 2 测试",
		"类型：执行任务失败",
		"级别：需要处理",
		"",
		"回复此飞书消息，即可在对应 issue 下发表评论。",
	}, "\n")
	if got != want {
		t.Fatalf("formatInboxNotificationText() = %q, want %q", got, want)
	}
}

func TestFormatInboxNotificationTextWithoutIssue(t *testing.T) {
	item := inboxNotificationItem{typ: "autopilot_paused", severity: "attention", title: "自动化暂停", body: "请检查配置"}
	got := formatInboxNotificationText("", item)
	if strings.Contains(got, "Issue：") || strings.Contains(got, "回复此飞书消息") {
		t.Fatalf("issue-less text contains issue-only content: %q", got)
	}
}

func TestInboxNotificationTypeLabel(t *testing.T) {
	wants := map[string]string{
		"issue_assigned": "issue 已分配", "unassigned": "issue 已取消分配", "assignee_changed": "负责人已变更",
		"mentioned": "有人提及你", "new_comment": "新评论", "reaction_added": "新表情回应",
		"status_changed": "状态已变更", "priority_changed": "优先级已变更", "start_date_changed": "开始日期已变更",
		"due_date_changed": "截止日期已变更", "task_failed": "执行任务失败", "quick_create_failed": "快速创建失败",
		"quick_create_done": "快速创建完成", "autopilot_paused": "自动化已暂停", "issue_subscribed": "已订阅新 issue",
	}
	for _, typ := range SupportedNotificationEvents() {
		t.Run(typ, func(t *testing.T) {
			got := inboxNotificationTypeLabel(typ)
			if got == "" {
				t.Fatalf("inboxNotificationTypeLabel(%q) is empty", typ)
			}
			if got == typ {
				t.Fatalf("inboxNotificationTypeLabel(%q) returned raw enum", typ)
			}
			want, ok := wants[typ]
			if !ok {
				t.Fatalf("catalog event %q has no expected Chinese label", typ)
			}
			if got != want {
				t.Fatalf("inboxNotificationTypeLabel(%q) = %q, want %q", typ, got, want)
			}
		})
	}
}

func TestInboxNotificationSeverityLabel(t *testing.T) {
	tests := map[string]string{"info": "普通", "action_required": "需要处理", "attention": "需要关注"}
	for severity, want := range tests {
		t.Run(severity, func(t *testing.T) {
			if got := inboxNotificationSeverityLabel(severity); got != want {
				t.Fatalf("inboxNotificationSeverityLabel(%q) = %q, want %q", severity, got, want)
			}
		})
	}
}

func newInboxNotifierHarness(enabled []string) (*fakeInboxNotifierQueries, *fakeInboxCredentials, *fakeInboxClient, *InboxNotifier) {
	queries := &fakeInboxNotifierQueries{
		binding: db.LarkUserBinding{InstallationID: mustInboxUUID("00000000-0000-0000-0000-000000000003"), LarkOpenID: "ou_member"},
		installation: db.LarkInstallation{
			ID: mustInboxUUID("00000000-0000-0000-0000-000000000003"), AppID: "app",
		},
		policy: db.LarkWorkspaceNotificationPolicy{EventTypes: enabled},
	}
	credentials := &fakeInboxCredentials{}
	client := &fakeInboxClient{configured: true, directID: "om_message"}
	return queries, credentials, client, &InboxNotifier{Queries: queries, Credentials: credentials, Client: client}
}

func inboxPayload(typ, recipientType, issueID string) map[string]any {
	item := map[string]any{
		"id": "00000000-0000-0000-0000-000000000001", "workspace_id": "00000000-0000-0000-0000-000000000002",
		"recipient_id": "00000000-0000-0000-0000-000000000004", "recipient_type": recipientType,
		"type": typ, "severity": "attention", "title": "通知标题", "body": "通知正文",
	}
	if issueID != "" {
		item["issue_id"] = issueID
	}
	return map[string]any{"item": item}
}

func mustInboxUUID(value string) pgtype.UUID {
	uuid, ok := parseInboxUUID(value)
	if !ok {
		panic("invalid test UUID")
	}
	return uuid
}
