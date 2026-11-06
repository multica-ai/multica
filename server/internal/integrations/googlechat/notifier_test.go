package googlechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	notifierWorkspaceID = "00000000-0000-4000-8000-000000000001"
	notifierRecipientID = "00000000-0000-4000-8000-000000000002"
	notifierIssueID     = "00000000-0000-4000-8000-000000000003"
)

type fakeNotifierQueries struct {
	memberErr     error
	preferenceErr error
	preferences   map[string]string
	workspace     db.Workspace
	issue         db.Issue
}

func (f *fakeNotifierQueries) GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memberErr
}

func (f *fakeNotifierQueries) GetNotificationPreference(context.Context, db.GetNotificationPreferenceParams) (db.NotificationPreference, error) {
	if f.preferenceErr != nil {
		return db.NotificationPreference{}, f.preferenceErr
	}
	raw, _ := json.Marshal(f.preferences)
	return db.NotificationPreference{Preferences: raw}, nil
}

func (f *fakeNotifierQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, nil
}

func (f *fakeNotifierQueries) GetIssue(context.Context, pgtype.UUID) (db.Issue, error) {
	return f.issue, nil
}

type recordingSender struct {
	mu      sync.Mutex
	url     string
	message string
	block   <-chan struct{}
	sent    chan struct{}
	err     error
}

func (s *recordingSender) Send(_ context.Context, webhookURL, message string) error {
	if s.sent != nil {
		close(s.sent)
	}
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.url = webhookURL
	s.message = message
	return s.err
}

func notifierEvent(recipientType string) events.Event {
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: notifierWorkspaceID,
		Payload: map[string]any{"item": map[string]any{
			"workspace_id":   notifierWorkspaceID,
			"recipient_type": recipientType,
			"recipient_id":   notifierRecipientID,
			"issue_id":       notifierIssueID,
			"title":          "Checkout needs review",
			"body":           "Please check <totals>",
		}},
	}
}

func TestNotifierDeliversConfiguredMemberNotification(t *testing.T) {
	t.Parallel()

	workspaceID := util.MustParseUUID(notifierWorkspaceID)
	queries := &fakeNotifierQueries{
		preferences: map[string]string{"google_chat_webhook": testWebhookURL},
		workspace: db.Workspace{
			ID:          workspaceID,
			Name:        "Acme",
			Slug:        "acme",
			IssuePrefix: "ACME",
		},
		issue: db.Issue{
			ID:          util.MustParseUUID(notifierIssueID),
			WorkspaceID: workspaceID,
			Number:      12,
		},
	}
	sender := &recordingSender{}
	notifier := NewNotifier(queries, sender, "https://app.example.com", slog.Default())
	notifier.spawn = func(run func()) { run() }
	bus := events.New()
	notifier.Register(bus)

	bus.Publish(notifierEvent("member"))

	if sender.url != testWebhookURL {
		t.Fatalf("webhook URL = %q", sender.url)
	}
	want := "*1 new notification in Acme*\n" +
		"• <https://app.example.com/acme/issues/ACME-12|ACME-12: Checkout needs review> — Please check &lt;totals&gt;"
	if sender.message != want {
		t.Fatalf("message =\n%s\nwant\n%s", sender.message, want)
	}
}

func TestNotifierOmitsIssueLinkWhenAppURLIsEmpty(t *testing.T) {
	t.Parallel()

	workspaceID := util.MustParseUUID(notifierWorkspaceID)
	sender := &recordingSender{}
	notifier := NewNotifier(&fakeNotifierQueries{
		preferences: map[string]string{"google_chat_webhook": testWebhookURL},
		workspace: db.Workspace{
			ID:          workspaceID,
			Name:        "Acme",
			Slug:        "acme",
			IssuePrefix: "ACME",
		},
		issue: db.Issue{
			ID:          util.MustParseUUID(notifierIssueID),
			WorkspaceID: workspaceID,
			Number:      12,
		},
	}, sender, "", slog.Default())
	notifier.spawn = func(run func()) { run() }
	bus := events.New()
	notifier.Register(bus)

	bus.Publish(notifierEvent("member"))

	want := "*1 new notification in Acme*\n" +
		"• ACME-12: Checkout needs review — Please check &lt;totals&gt;"
	if sender.message != want {
		t.Fatalf("message =\n%s\nwant\n%s", sender.message, want)
	}
}

func TestNotifierSkipsAgentsFormerMembersAndMissingPreferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		recipientType string
		memberErr     error
		preferenceErr error
	}{
		{name: "agent recipient", recipientType: "agent"},
		{name: "former member", recipientType: "member", memberErr: pgx.ErrNoRows},
		{name: "no preference row", recipientType: "member", preferenceErr: pgx.ErrNoRows},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sender := &recordingSender{}
			notifier := NewNotifier(&fakeNotifierQueries{
				memberErr:     test.memberErr,
				preferenceErr: test.preferenceErr,
			}, sender, "", slog.Default())
			notifier.spawn = func(run func()) { run() }
			bus := events.New()
			notifier.Register(bus)
			bus.Publish(notifierEvent(test.recipientType))
			if sender.message != "" {
				t.Fatalf("unexpected delivery: %q", sender.message)
			}
		})
	}
}

func TestNotifierNeverBlocksPublish(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{})
	sender := &recordingSender{block: release, sent: started}
	workspaceID := util.MustParseUUID(notifierWorkspaceID)
	notifier := NewNotifier(&fakeNotifierQueries{
		preferences: map[string]string{"google_chat_webhook": testWebhookURL},
		workspace: db.Workspace{
			ID:          workspaceID,
			Name:        "Acme",
			Slug:        "acme",
			IssuePrefix: "ACME",
		},
		issue: db.Issue{
			ID:          util.MustParseUUID(notifierIssueID),
			WorkspaceID: workspaceID,
			Number:      12,
		},
	}, sender, "https://app.example.com", slog.Default())
	bus := events.New()
	notifier.Register(bus)

	published := make(chan struct{})
	go func() {
		bus.Publish(notifierEvent("member"))
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on Google Chat delivery")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}
	close(release)
}

func TestNotifierNeverLogsWebhookCredentials(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	workspaceID := util.MustParseUUID(notifierWorkspaceID)
	sender := &recordingSender{
		err: errors.New("dial tcp: connection refused"),
	}
	notifier := NewNotifier(&fakeNotifierQueries{
		preferences: map[string]string{"google_chat_webhook": testWebhookURL},
		workspace: db.Workspace{
			ID:          workspaceID,
			Name:        "Acme",
			Slug:        "acme",
			IssuePrefix: "ACME",
		},
		issue: db.Issue{
			ID:          util.MustParseUUID(notifierIssueID),
			WorkspaceID: workspaceID,
			Number:      12,
		},
	}, sender, "https://app.example.com", logger)
	notifier.spawn = func(run func()) { run() }
	bus := events.New()
	notifier.Register(bus)

	bus.Publish(notifierEvent("member"))

	output := logs.String()
	if strings.Contains(output, "test-key") || strings.Contains(output, "test-token") {
		t.Fatalf("log exposed webhook credentials: %s", output)
	}
	if !strings.Contains(output, "space_id=AAAA_123") {
		t.Fatalf("log omitted safe space id: %s", output)
	}
	if !strings.Contains(output, "connection refused") {
		t.Fatalf("log omitted transport cause: %s", output)
	}
}
