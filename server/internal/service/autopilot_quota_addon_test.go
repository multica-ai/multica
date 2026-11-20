package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// fakeCloudPolicy serves the internal entitlement-policy endpoint with an
// enforced autopilot_runs gate whose limit a test can raise, standing in for
// Cloud after an add-on purchase.
type fakeCloudPolicy struct {
	server      *httptest.Server
	limit       atomic.Int64
	fetches     atomic.Int64
	periodStart time.Time
	periodEnd   time.Time
}

func newFakeCloudPolicy(t *testing.T, f *autopilotQuotaFixture, limit int64) *fakeCloudPolicy {
	t.Helper()
	cloud := &fakeCloudPolicy{periodStart: f.periodStart, periodEnd: f.periodEnd}
	cloud.limit.Store(limit)
	cloud.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cloud.fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version":       entitlement.SchemaVersion,
			"policy_revision":      6,
			"subscription_version": 3,
			"valid_until":          time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
			"valid_for_seconds":    60,
			"gates": map[string]any{
				"autopilot_runs": map[string]any{
					"action":        "enforce",
					"limit":         cloud.limit.Load(),
					"period_start":  cloud.periodStart.Format(time.RFC3339),
					"period_end":    cloud.periodEnd.Format(time.RFC3339),
					"reset_at":      cloud.periodEnd.Format(time.RFC3339),
					"notifications": map[string]any{"on_rejection": entitlement.NotificationFirstRejectionPerLimit},
				},
			},
		})
	}))
	t.Cleanup(cloud.server.Close)
	return cloud
}

// newAutopilotQuotaInstance is one API instance: its own entitlement client
// and policy cache over the shared database.
func newAutopilotQuotaInstance(t *testing.T, f *autopilotQuotaFixture, cloud *fakeCloudPolicy) *AutopilotService {
	t.Helper()
	client, err := entitlement.New(entitlement.Config{
		BaseURL: cloud.server.URL,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("entitlement client: %v", err)
	}
	return &AutopilotService{Queries: f.queries, TxStarter: f.pool, Bus: events.New(), Entitlements: client}
}

func cleanupPolicyChange(t *testing.T, f *autopilotQuotaFixture) {
	t.Helper()
	t.Cleanup(func() { cleanupNotices(t, f) })
}

func cleanupNotices(t *testing.T, f *autopilotQuotaFixture) {
	t.Helper()
	f.pool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`, f.workspaceID)
}

func countAutopilotQuotaNotices(t *testing.T, f *autopilotQuotaFixture) map[string]int {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT recipient_id FROM inbox_item
		WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`, f.workspaceID)
	if err != nil {
		t.Fatalf("list quota notices: %v", err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var recipientID pgtype.UUID
		if err := rows.Scan(&recipientID); err != nil {
			t.Fatalf("scan quota notice: %v", err)
		}
		out[util.UUIDToString(recipientID)]++
	}
	return out
}

func scheduleRun(ctx context.Context, svc *AutopilotService, f *autopilotQuotaFixture, key string) error {
	params := f.createRunArgs
	params.Source = "schedule"
	_, _, err := svc.createAutopilotRunWithQuota(ctx, f.workspaceID, "schedule", key, params)
	return err
}

// Stripe's webhook confirmed the add-on to Cloud while the payer has not
// returned to the app, so no Multica instance has heard of it. Instance B
// cached the old limit a moment ago (well inside RefreshGate's 5 s reuse
// window); its very next scheduled run must still start, without a "limit
// reached" notice.
func TestAutopilotQuotaWebhookConfirmedAddonAdmitsNextRunBeforeReturn(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	cleanupPolicyChange(t, fixture)
	cloud := newFakeCloudPolicy(t, fixture, 1)
	instanceA := newAutopilotQuotaInstance(t, fixture, cloud)
	instanceB := newAutopilotQuotaInstance(t, fixture, cloud)
	ctx := context.Background()

	// Both instances hold the full quota's policy.
	if err := scheduleRun(ctx, instanceB, fixture, "fill-on-b"); err != nil {
		t.Fatalf("fill quota on B: %v", err)
	}
	var quotaErr *AutopilotQuotaExceededError
	if err := scheduleRun(ctx, instanceA, fixture, "reject-on-a"); !errors.As(err, &quotaErr) {
		t.Fatalf("run on A before the add-on = %v, want quota error", err)
	}
	if err := scheduleRun(ctx, instanceB, fixture, "reject-on-b"); !errors.As(err, &quotaErr) {
		t.Fatalf("run on B before the add-on = %v, want quota error", err)
	}
	cleanupNotices(t, fixture)
	fetchesBefore := cloud.fetches.Load()

	// Webhook settles the add-on in Cloud. Nothing happens in Multica.
	cloud.limit.Store(1001)

	if err := scheduleRun(ctx, instanceB, fixture, "after-addon-on-b"); err != nil {
		t.Fatalf("scheduled run on B after the webhook = %v, want admitted", err)
	}
	if err := scheduleRun(ctx, instanceA, fixture, "after-addon-on-a"); err != nil {
		t.Fatalf("scheduled run on A after the webhook = %v, want admitted", err)
	}
	if got := cloud.fetches.Load() - fetchesBefore; got != 2 {
		t.Fatalf("instances fetched the policy %d times after the change, want one refresh each", got)
	}
	if notices := countAutopilotQuotaNotices(t, fixture); len(notices) != 0 {
		t.Fatalf("limit-reached notices after the add-on = %v, want none", notices)
	}
}

// Every rejection asks Cloud for the current policy first, once, and still
// rejects when the limit is really reached; the notice goes out once per
// limit, not once per rejection.
func TestAutopilotQuotaRejectionFetchesThePolicyOnce(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	cleanupPolicyChange(t, fixture)
	cloud := newFakeCloudPolicy(t, fixture, 1)
	instance := newAutopilotQuotaInstance(t, fixture, cloud)
	ctx := context.Background()

	if err := scheduleRun(ctx, instance, fixture, "fill"); err != nil {
		t.Fatalf("fill quota: %v", err)
	}
	fetchesBefore := cloud.fetches.Load()
	for i := range 3 {
		var quotaErr *AutopilotQuotaExceededError
		if err := scheduleRun(ctx, instance, fixture, fmt.Sprintf("reject-%d", i)); !errors.As(err, &quotaErr) {
			t.Fatalf("rejection %d = %v, want quota error", i, err)
		}
	}
	if got := cloud.fetches.Load() - fetchesBefore; got != 3 {
		t.Fatalf("3 rejections fetched the policy %d times, want one each", got)
	}
	total := 0
	for _, n := range countAutopilotQuotaNotices(t, fixture) {
		total += n
	}
	if total == 0 {
		t.Fatal("no limit-reached notice for a real rejection")
	}
	first := total
	if err := scheduleRun(ctx, instance, fixture, "reject-again"); err == nil {
		t.Fatal("run past the limit admitted")
	}
	total = 0
	for _, n := range countAutopilotQuotaNotices(t, fixture) {
		total += n
	}
	if total != first {
		t.Fatalf("notices grew from %d to %d on the same limit", first, total)
	}
}

// When nobody confirmed the add-on in the app (the payer closed the Stripe
// tab and Cloud settled it from the webhook), a rejection still asks for the
// current policy first, so a raised limit admits the run.
func TestAutopilotQuotaRejectionRefreshesThePolicyFromTheStub(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	cleanupPolicyChange(t, fixture)
	if _, _, err := fixture.service.createAutopilotRunWithQuota(
		context.Background(), fixture.workspaceID, "schedule", "fill", fixture.createRunArgs,
	); err != nil {
		t.Fatalf("fill quota: %v", err)
	}
	raised := 1001
	fixture.stub.SetRefreshed(fixture.workspace, entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: entitlement.ActionEnforce, Limit: &raised,
			PeriodStart: &fixture.periodStart, PeriodEnd: &fixture.periodEnd, ResetAt: &fixture.resetAt,
		},
		PolicyRevision: 6, SubscriptionVersion: 11,
	})
	refreshesBefore := len(fixture.stub.Refreshes())
	if _, _, err := fixture.service.createAutopilotRunWithQuota(
		context.Background(), fixture.workspaceID, "schedule", "after-webhook", fixture.createRunArgs,
	); err != nil {
		t.Fatalf("run after an add-on settled by webhook = %v, want admitted", err)
	}
	if got := len(fixture.stub.Refreshes()) - refreshesBefore; got != 1 {
		t.Fatalf("refreshes = %d, want one before rejecting", got)
	}
}

func (f *autopilotQuotaFixture) setPerLimitNotificationPolicy(limit int) {
	f.stub.Set(f.workspace, entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: entitlement.ActionEnforce, Limit: &limit,
			PeriodStart: &f.periodStart, PeriodEnd: &f.periodEnd, ResetAt: &f.resetAt,
			Notifications: &entitlement.NotificationPolicy{
				OnRejection: entitlement.NotificationFirstRejectionPerLimit,
			},
		},
		PolicyRevision: 6, SubscriptionVersion: 11,
	})
}

// Per limit, a rejection at a raised limit notifies again; concurrent
// rejections at one limit notify once.
func TestAutopilotQuotaPerLimitNoticeRearmsAfterTheLimitGrows(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	cleanupPolicyChange(t, fixture)
	fixture.setPerLimitNotificationPolicy(1)
	ctx := context.Background()
	owner := util.UUIDToString(fixture.publisherID)

	rejectConcurrently := func(prefix string, attempts int) {
		t.Helper()
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, attempts)
		for i := range attempts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var quotaErr *AutopilotQuotaExceededError
				if err := scheduleRun(ctx, fixture.service, fixture, fmt.Sprintf("%s-%d", prefix, i)); !errors.As(err, &quotaErr) {
					errs <- fmt.Errorf("%s %d = %v, want quota error", prefix, i, err)
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	}

	if err := scheduleRun(ctx, fixture.service, fixture, "fill-1"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	rejectConcurrently("at-limit-1", 8)
	if got := countAutopilotQuotaNotices(t, fixture)[owner]; got != 1 {
		t.Fatalf("notices at limit 1 = %d, want one", got)
	}

	// An add-on raises the limit; the workspace runs out again.
	fixture.setPerLimitNotificationPolicy(2)
	if err := scheduleRun(ctx, fixture.service, fixture, "fill-2"); err != nil {
		t.Fatalf("run after the add-on: %v", err)
	}
	rejectConcurrently("at-limit-2", 8)
	if got := countAutopilotQuotaNotices(t, fixture)[owner]; got != 2 {
		t.Fatalf("notices after the limit grew = %d, want one more", got)
	}

	// The legacy per-period policy keeps one notice for the whole period.
	fixture.setNotificationPolicy(2)
	rejectConcurrently("per-period", 3)
	if got := countAutopilotQuotaNotices(t, fixture)[owner]; got != 2 {
		t.Fatalf("per-period notices = %d, want no more", got)
	}
}

func TestAutopilotQuotaNoticeReachesTheAutopilotCreator(t *testing.T) {
	cases := []struct {
		name string
		// creator returns the autopilot creator and the recipients expected
		// besides the owner.
		setup func(t *testing.T, f *autopilotQuotaFixture) (creator pgtype.UUID, want []pgtype.UUID)
	}{
		{
			name: "admin and member creator",
			setup: func(t *testing.T, f *autopilotQuotaFixture) (pgtype.UUID, []pgtype.UUID) {
				admin := addAutopilotQuotaMember(t, f, "admin")
				creator := addAutopilotQuotaMember(t, f, "member")
				return creator, []pgtype.UUID{admin, creator}
			},
		},
		{
			name: "creator is an admin",
			setup: func(t *testing.T, f *autopilotQuotaFixture) (pgtype.UUID, []pgtype.UUID) {
				creator := addAutopilotQuotaMember(t, f, "admin")
				return creator, []pgtype.UUID{creator}
			},
		},
		{
			name: "creator left the workspace",
			setup: func(t *testing.T, f *autopilotQuotaFixture) (pgtype.UUID, []pgtype.UUID) {
				admin := addAutopilotQuotaMember(t, f, "admin")
				creator := addAutopilotQuotaMember(t, f, "member")
				if _, err := f.pool.Exec(context.Background(),
					`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.workspaceID, creator); err != nil {
					t.Fatalf("remove creator: %v", err)
				}
				return creator, []pgtype.UUID{admin}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 0)
			cleanupPolicyChange(t, fixture)
			fixture.setPerLimitNotificationPolicy(0)
			creator, extra := tc.setup(t, fixture)
			if _, err := fixture.pool.Exec(context.Background(),
				`UPDATE autopilot SET created_by_type = 'member', created_by_id = $2 WHERE id = $1`,
				fixture.autopilotID, creator); err != nil {
				t.Fatalf("set autopilot creator: %v", err)
			}
			var quotaErr *AutopilotQuotaExceededError
			if err := scheduleRun(context.Background(), fixture.service, fixture, "rejected"); !errors.As(err, &quotaErr) {
				t.Fatalf("rejection = %v, want quota error", err)
			}
			got := countAutopilotQuotaNotices(t, fixture)
			want := append([]pgtype.UUID{fixture.publisherID}, extra...)
			if len(got) != len(want) {
				t.Fatalf("recipients = %v, want %d recipients", got, len(want))
			}
			for _, id := range want {
				if got[util.UUIDToString(id)] != 1 {
					t.Fatalf("recipient %s notices = %d, want one (all: %v)", util.UUIDToString(id), got[util.UUIDToString(id)], got)
				}
			}
		})
	}
}
