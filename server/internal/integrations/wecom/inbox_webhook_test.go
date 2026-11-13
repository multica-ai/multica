package wecom

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const webhookTestWorkspace = "33333333-3333-4333-8333-333333333333"
const webhookTestMember = "44444444-4444-4444-8444-444444444444"
const webhookTestInbox = "55555555-5555-4555-8555-555555555555"

func configuredInboxWebhook(t *testing.T) *InboxWebhook {
	t.Helper()
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WEBHOOK_URL", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-only")
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WORKSPACE_ID", webhookTestWorkspace)
	t.Setenv("MULTICA_WECOM_NOTIFICATION_MEMBER_ID", webhookTestMember)
	t.Setenv("WECOM_APP_URL", "https://multica.example")
	w, err := NewInboxWebhookFromEnv(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || w == nil {
		t.Fatalf("configure webhook: %v", err)
	}
	return w
}

func webhookTestItem() map[string]any {
	return map[string]any{
		"id": webhookTestInbox, "workspace_id": webhookTestWorkspace,
		"recipient_type": "member", "recipient_id": webhookTestMember,
		"type": "status_changed", "details": json.RawMessage(`{"from":"todo","to":"in_progress"}`),
	}
}

func TestInboxWebhookConfigurationIsAllOrNothing(t *testing.T) {
	for _, name := range []string{"MULTICA_WECOM_NOTIFICATION_WEBHOOK_URL", "MULTICA_WECOM_NOTIFICATION_WORKSPACE_ID", "MULTICA_WECOM_NOTIFICATION_MEMBER_ID"} {
		t.Setenv(name, "")
	}
	if w, err := NewInboxWebhookFromEnv(nil, nil); err != nil || w != nil {
		t.Fatalf("unset configuration = %v, %v", w != nil, err)
	}
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WEBHOOK_URL", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-only")
	if w, err := NewInboxWebhookFromEnv(nil, nil); err == nil || w != nil {
		t.Fatal("partial configuration must not take over private notifications")
	}
	configuredInboxWebhook(t)
	t.Setenv("MULTICA_WECOM_NOTIFICATION_MEMBER_ID", "invalid")
	if w, err := NewInboxWebhookFromEnv(nil, nil); err == nil || w != nil {
		t.Fatal("invalid recipient configuration was accepted")
	}
}

func TestInboxWebhookOnlyRedirectsTheConfiguredMemberAndTypes(t *testing.T) {
	w := configuredInboxWebhook(t)
	for _, tc := range []struct {
		field string
		value any
		want  bool
	}{
		{"type", "status_changed", true}, {"type", "new_comment", true},
		{"type", "mentioned", true}, {"type", "comment_added", true}, {"type", "reaction_added", true}, {"type", "task_failed", true}, {"type", "start_date_changed", true}, {"type", "due_date_changed", true}, {"type", "priority_changed", true}, {"type", "assignee_changed", true}, {"type", "unassigned", true}, {"type", "issue_assigned", true}, {"type", "task_completed", false},
		{"workspace_id", webhookTestMember, false}, {"recipient_id", webhookTestWorkspace, false},
		{"recipient_type", "agent", false},
	} {
		item := webhookTestItem()
		item[tc.field] = tc.value
		if got := w.Matches(item); got != tc.want {
			t.Errorf("%s=%v matched=%v, want %v", tc.field, tc.value, got, tc.want)
		}
	}
	var disabled *InboxWebhook
	if disabled.Matches(webhookTestItem()) {
		t.Fatal("disabled route matched")
	}
}

func TestInboxWebhookOwnsSnapshotAndDeduplicatesBeforeSpawning(t *testing.T) {
	w := configuredInboxWebhook(t)
	starts := 0
	w.spawn = func(func()) { starts++ }
	bus := events.New()
	w.Register(bus)
	item := webhookTestItem()
	e := events.Event{Type: protocol.EventInboxNew, WorkspaceID: webhookTestWorkspace, Payload: map[string]any{"item": item}}
	bus.Publish(e)
	bus.Publish(e)
	item["title"] = "changed after publish"
	if starts != 1 || len(w.queue) != 1 {
		t.Fatalf("starts=%d queue=%d; one event must be admitted once", starts, len(w.queue))
	}
	if _, exists := w.queue[0]["title"]; exists {
		t.Fatal("queued event retained the publisher's mutable map")
	}
	var details map[string]string
	raw, _ := json.Marshal(w.queue[0]["details"])
	if err := json.Unmarshal(raw, &details); err != nil || details["from"] != "todo" {
		t.Fatalf("event details lost in queued snapshot: %s", raw)
	}
	e.WorkspaceID = webhookTestMember
	item["id"] = "66666666-6666-4666-8666-666666666666"
	bus.Publish(e)
	if len(w.queue) != 1 {
		t.Fatal("cross-workspace envelope was admitted")
	}
}

func TestInboxWebhookQueueFullIsObservableWithoutRememberingUnqueuedEvent(t *testing.T) {
	w := configuredInboxWebhook(t)
	var logged strings.Builder
	w.logger = slog.New(slog.NewTextHandler(&logged, nil))
	w.spawn = func(func()) {}
	for i := 0; i < inboxWebhookQueueLimit; i++ {
		w.enqueue(strings.Repeat("x", i+1), webhookTestItem())
	}
	w.enqueue("overflow", webhookTestItem())
	if len(w.queue) != inboxWebhookQueueLimit || w.seen["overflow"] || !strings.Contains(logged.String(), "queue_full") {
		t.Fatal("queue overflow was unbounded, silently lost, or remembered as delivered")
	}
}

func TestInboxWebhookRedirectedEventsNeverEnterPrivateDelivery(t *testing.T) {
	w := configuredInboxWebhook(t)
	// A nil query/sender would fail if the original private route is entered.
	o := NewOutbound(nil, nil, nil, nil, WithInboxWebhook(w))
	for kind := range copyFor(DefaultLocale).InboxTypeLabels {
		item := webhookTestItem()
		item["type"] = kind
		o.handleInboxNew(events.Event{WorkspaceID: webhookTestWorkspace, Payload: map[string]any{"item": item}})
	}
}
