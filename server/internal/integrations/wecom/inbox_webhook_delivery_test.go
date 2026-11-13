package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type inboxWebhookDeliveryFixture struct {
	*inboxWebhookCardFixture
	membershipErr  error
	hiddenProjects []pgtype.UUID
}

func (f *inboxWebhookDeliveryFixture) ListHiddenProjectIDsForUser(context.Context, db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error) {
	return f.hiddenProjects, nil
}

func (f *inboxWebhookDeliveryFixture) ListPrivateProjectIDs(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	return f.hiddenProjects, nil
}

func (f *inboxWebhookDeliveryFixture) GetMemberByUserAndWorkspace(_ context.Context, p db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	if f.membershipErr != nil {
		return db.Member{}, f.membershipErr
	}
	if p.WorkspaceID != f.workspace.ID || p.UserID != f.user.ID {
		return db.Member{}, pgx.ErrNoRows
	}
	return db.Member{WorkspaceID: p.WorkspaceID, UserID: p.UserID}, nil
}

func deliveryWebhook(t *testing.T) (*InboxWebhook, *inboxWebhookDeliveryFixture, map[string]any) {
	t.Helper()
	w := configuredInboxWebhook(t)
	f := &inboxWebhookDeliveryFixture{inboxWebhookCardFixture: newInboxWebhookCardFixture()}
	w.workspaceID = util.UUIDToString(f.workspace.ID)
	w.memberID = util.UUIDToString(f.user.ID)
	w.q = f
	w.interval = 0
	w.spawn = func(run func()) { run() }
	item := webhookTestItem()
	item["workspace_id"] = w.workspaceID
	item["recipient_id"] = w.memberID
	item["issue_id"] = util.UUIDToString(f.issue.ID)
	item["created_at"] = "2026-11-10T01:02:03Z"
	return w, f, item
}

func webhookReply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestInboxWebhookConcurrentReplaySendsOnce(t *testing.T) {
	w, _, item := deliveryWebhook(t)
	var calls atomic.Int32
	w.client = &http.Client{Transport: inboxWebhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return webhookReply(200, `{"errcode":0}`), nil
	})}
	bus := events.New()
	w.Register(bus)
	private := NewOutbound(nil, nil, nil, nil, WithInboxWebhook(w))
	private.Register(bus)
	e := events.Event{Type: protocol.EventInboxNew, WorkspaceID: w.workspaceID, Payload: map[string]any{"item": item}}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); bus.Publish(e) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent replay sent %d times", calls.Load())
	}
	next := make(map[string]any, len(item))
	for key, value := range item {
		next[key] = value
	}
	next["id"] = "77777777-7777-4777-8777-777777777777"
	e.Payload = map[string]any{"item": next}
	bus.Publish(e)
	if calls.Load() != 2 {
		t.Fatal("different events for the same issue were incorrectly merged")
	}
}

func TestInboxWebhookFailuresNeverFallBackOrLeakCredentials(t *testing.T) {
	for _, failure := range []string{"membership", "card", "network", "rejection", "rate_limit"} {
		t.Run(failure, func(t *testing.T) {
			w, f, item := deliveryWebhook(t)
			var logged strings.Builder
			w.logger = slog.New(slog.NewTextHandler(&logged, nil))
			calls := 0
			if failure == "membership" {
				f.membershipErr = pgx.ErrNoRows
			}
			if failure == "card" {
				f.issueErr = pgx.ErrNoRows
			}
			w.client = &http.Client{Transport: inboxWebhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if failure == "network" {
					return nil, errors.New("secret test-only " + w.endpoint)
				}
				if failure == "rate_limit" {
					return webhookReply(429, "secret test-only"), nil
				}
				return webhookReply(200, `{"errcode":40058,"errmsg":"secret test-only"}`), nil
			})}
			bus := events.New()
			w.Register(bus)
			// Without exclusive routing the private subscriber dereferences a nil DB.
			NewOutbound(nil, nil, nil, nil, WithInboxWebhook(w)).Register(bus)
			bus.Publish(events.Event{Type: protocol.EventInboxNew, WorkspaceID: w.workspaceID, Payload: map[string]any{"item": item}})
			// Bus recovers listener panics, so also exercise this gate directly.
			NewOutbound(nil, nil, nil, nil, WithInboxWebhook(w)).handleInboxNew(events.Event{
				WorkspaceID: w.workspaceID, Payload: map[string]any{"item": item},
			})
			wantCalls := 1
			if failure == "membership" || failure == "card" {
				wantCalls = 0
			}
			if failure == "rate_limit" {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("got %d requests want %d", calls, wantCalls)
			}
			if strings.Contains(logged.String(), "test-only") || strings.Contains(logged.String(), "delivered") || !strings.Contains(logged.String(), "outcome=") {
				t.Fatalf("unsafe or missing failure outcome: %s", logged.String())
			}
		})
	}
}

func TestInboxWebhookRetryRechecksRevocationAndCommentDeletion(t *testing.T) {
	for _, change := range []string{"membership_revoked", "comment_deleted", "project_access_revoked"} {
		t.Run(change, func(t *testing.T) {
			w, f, item := deliveryWebhook(t)
			item["type"] = "new_comment"
			item["details"] = json.RawMessage(`{"comment_id":"` + util.UUIDToString(f.comment.ID) + `"}`)
			calls := 0
			w.client = &http.Client{Transport: inboxWebhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if change == "membership_revoked" {
					f.membershipErr = pgx.ErrNoRows
				} else if change == "comment_deleted" {
					f.comment.DeletedAt.Valid = true
				} else {
					f.issue.ProjectID = util.MustParseUUID("88888888-8888-4888-8888-888888888888")
					f.hiddenProjects = []pgtype.UUID{f.issue.ProjectID}
				}
				return webhookReply(429, ""), nil
			})}
			w.deliver(item)
			if calls != 1 {
				t.Fatalf("retry leaked a revoked or deleted notification: requests=%d", calls)
			}
		})
	}
}

func TestInboxWebhookQueuedPrivateProjectRevocationStopsSend(t *testing.T) {
	w, f, item := deliveryWebhook(t)
	w.spawn = func(func()) {}
	f.issue.ProjectID = util.MustParseUUID("88888888-8888-4888-8888-888888888888")
	w.handle(events.Event{WorkspaceID: w.workspaceID, Payload: map[string]any{"item": item}})
	f.hiddenProjects = []pgtype.UUID{f.issue.ProjectID}
	calls := 0
	w.client = &http.Client{Transport: inboxWebhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return webhookReply(200, `{"errcode":0}`), nil
	})}
	w.drain()
	if calls != 0 {
		t.Fatal("queued notification escaped the current private-project access check")
	}
}

func TestInboxWebhookAssignmentAndMentionRouteExclusivelyToGroup(t *testing.T) {
	restoreLocale(t, LocaleZhHans)
	for _, kind := range []string{"issue_assigned", "unassigned", "mentioned", "assignee_changed", "priority_changed", "due_date_changed", "start_date_changed", "task_failed", "reaction_added"} {
		t.Run(kind, func(t *testing.T) {
			w, _, item := deliveryWebhook(t)
			item["type"], item["body"], item["details"] = kind, "group-only notification", nil
			calls := 0
			w.client = &http.Client{Transport: inboxWebhookRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var card inboxWebhookCardPayload
				if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
					t.Fatal(err)
				}
				want := "group-only notification"
				switch kind {
				case "issue_assigned":
					want = "指派给：张三"
				case "unassigned":
					want = "取消指派：张三"
				case "assignee_changed":
					want = "未指派 → 未指派"
				case "mentioned":
					want = "任务描述中提及你\ngroup-only notification"
				}
				if card.TemplateCard.SubTitleText != want {
					t.Errorf("queued body lost: %+v", card.TemplateCard)
				}
				return webhookReply(200, `{"errcode":0}`), nil
			})}
			e := events.Event{WorkspaceID: w.workspaceID, Payload: map[string]any{"item": item}}
			w.handle(e)
			if calls != 1 {
				t.Fatalf("group requests=%d, want 1", calls)
			}
			// A nil DB panics if this escapes into private delivery.
			NewOutbound(nil, nil, nil, nil, WithInboxWebhook(w)).handleInboxNew(e)
		})
	}
}
