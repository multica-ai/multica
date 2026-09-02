package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type autopilotQuotaFixture struct {
	pool          *pgxpool.Pool
	queries       *db.Queries
	service       *AutopilotService
	stub          *entitlementtest.Stub
	workspace     uuid.UUID
	workspaceID   pgtype.UUID
	autopilotID   pgtype.UUID
	agentID       pgtype.UUID
	publisherID   pgtype.UUID
	issueID       pgtype.UUID
	periodStart   time.Time
	periodEnd     time.Time
	resetAt       time.Time
	policyLimit   int
	createRunArgs db.CreateAutopilotRunParams
}

type autopilotQuotaMetricsRecorder struct {
	mu        sync.Mutex
	decisions map[string]int
}

func (m *autopilotQuotaMetricsRecorder) RecordAutopilotQuotaDecision(action, source, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decisions == nil {
		m.decisions = make(map[string]int)
	}
	m.decisions[action+"\x00"+source+"\x00"+result]++
}

func (m *autopilotQuotaMetricsRecorder) count(action, source, result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decisions[action+"\x00"+source+"\x00"+result]
}

func newAutopilotQuotaFixture(t *testing.T, action entitlement.Action, limit int) *autopilotQuotaFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	q := db.New(pool)
	workspaceIDString, publisherID, agentID, issueIDString := seedAttributionFixture(t, pool)
	autopilotIDString, _ := seedRunOnlyAutopilot(t, pool, workspaceIDString, agentID, publisherID)
	workspaceID := util.MustParseUUID(workspaceIDString)
	workspace := uuid.UUID(workspaceID.Bytes)
	periodStart := time.Now().UTC().Truncate(time.Second)
	periodEnd := periodStart.Add(37 * time.Hour)
	stub := entitlementtest.New()
	fixture := &autopilotQuotaFixture{
		pool: pool, queries: q, stub: stub, workspace: workspace,
		workspaceID: workspaceID, autopilotID: util.MustParseUUID(autopilotIDString),
		agentID: util.MustParseUUID(agentID), publisherID: util.MustParseUUID(publisherID),
		issueID:     util.MustParseUUID(issueIDString),
		periodStart: periodStart, periodEnd: periodEnd, resetAt: periodEnd,
		policyLimit: limit,
		createRunArgs: db.CreateAutopilotRunParams{
			AutopilotID: util.MustParseUUID(autopilotIDString), Source: "api", Status: "running",
		},
	}
	fixture.service = &AutopilotService{
		Queries: q, TxStarter: pool, Bus: events.New(), Entitlements: stub,
	}
	fixture.setPolicy(action, periodStart, periodEnd, limit)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM autopilot_quota_reservation WHERE workspace_id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id = $1`, workspaceID)
	})
	return fixture
}

func (f *autopilotQuotaFixture) setPolicy(action entitlement.Action, start, end time.Time, limit int) {
	f.stub.Set(f.workspace, entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: action, Limit: &limit,
			PeriodStart: &start, PeriodEnd: &end, ResetAt: &end,
		},
		PolicyRevision: 7, SubscriptionVersion: 11,
	})
}

func (f *autopilotQuotaFixture) setNotificationPolicy(limit int) {
	f.stub.Set(f.workspace, entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: entitlement.ActionEnforce, Limit: &limit,
			PeriodStart: &f.periodStart, PeriodEnd: &f.periodEnd, ResetAt: &f.resetAt,
			Notifications: &entitlement.NotificationPolicy{
				OnRejection: entitlement.NotificationFirstRejectionPerPeriod,
			},
		},
		PolicyRevision: 8, SubscriptionVersion: 11,
	})
}

func addAutopilotQuotaMember(t *testing.T, f *autopilotQuotaFixture, role string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var userID pgtype.UUID
	email := fmt.Sprintf("quota-notice-%s-%d@multica.test", role, time.Now().UnixNano())
	if err := f.pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Quota Notice Member', $1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert quota notice user: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`, f.workspaceID, userID, role); err != nil {
		t.Fatalf("insert quota notice member: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.workspaceID, userID)
		f.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func TestAutopilotQuotaDisabledDoesNotReadQuotaTables(t *testing.T) {
	ctx := context.Background()
	workspace := uuid.New()
	workspaceID := pgtype.UUID{Bytes: workspace, Valid: true}

	t.Run("off", func(t *testing.T) {
		stub := entitlementtest.New()
		stub.Set(workspace, entitlement.GateAutopilotRuns, entitlement.Decision{
			Gate: entitlement.Gate{Action: entitlement.ActionOff},
		})
		svc := &AutopilotService{Entitlements: stub} // Queries intentionally nil
		usage, err := svc.AutopilotQuotaUsage(ctx, workspaceID)
		if err != nil || usage.Enabled {
			t.Fatalf("off usage = %+v, %v; want disabled", usage, err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		stub := entitlementtest.New()
		limit := 2
		stub.Set(workspace, entitlement.GateAutopilotRuns, entitlement.Decision{
			Gate: entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &limit},
		})
		svc := &AutopilotService{Entitlements: stub} // missing interval; Queries intentionally nil
		usage, err := svc.AutopilotQuotaUsage(ctx, workspaceID)
		if err != nil || usage.Enabled {
			t.Fatalf("malformed usage = %+v, %v; want fail-open disabled", usage, err)
		}
	})
}

func TestAutopilotQuotaRejectsUnknownExecutionSource(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	_, _, err := fixture.service.createAutopilotRunWithQuota(
		context.Background(), fixture.workspaceID, "manul", "invalid-source", fixture.createRunArgs,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid autopilot execution source") {
		t.Fatalf("unknown source error = %v, want application validation", err)
	}
}

func TestAutopilotQuotaEnforcesBoundaryAndFinalizesIdempotently(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceIDString, publisherID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotIDString, _ := seedRunOnlyAutopilot(t, pool, workspaceIDString, agentID, publisherID)
	workspaceID := util.MustParseUUID(workspaceIDString)
	autopilotID := util.MustParseUUID(autopilotIDString)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM autopilot_quota_reservation WHERE workspace_id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id = $1`, workspaceID)
	})

	periodStart := time.Now().UTC().Truncate(time.Second)
	periodEnd := periodStart.Add(37 * time.Hour) // opaque Cloud interval, deliberately not a calendar month
	resetAt := periodEnd
	limit := 2
	stub := entitlementtest.New()
	stub.Set(uuid.UUID(workspaceID.Bytes), entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: entitlement.ActionEnforce, Limit: &limit,
			PeriodStart: &periodStart, PeriodEnd: &periodEnd, ResetAt: &resetAt,
		},
		PolicyRevision: 7, SubscriptionVersion: 11,
	})
	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), Entitlements: stub}
	params := db.CreateAutopilotRunParams{
		AutopilotID: autopilotID, Source: "api", Status: "running",
	}

	runs := make([]db.AutopilotRun, 0, limit)
	for i, key := range []string{"boundary-1", "boundary-2"} {
		run, _, err := svc.createAutopilotRunWithQuota(ctx, workspaceID, "api", key, params)
		if err != nil {
			t.Fatalf("admission %d: %v", i+1, err)
		}
		runs = append(runs, run)
	}
	reused, wasReused, err := svc.createAutopilotRunWithQuota(ctx, workspaceID, "api", "boundary-1", params)
	if err != nil || !wasReused || reused.ID.Bytes != runs[0].ID.Bytes {
		t.Fatalf("idempotent reuse = %s, %v; want %s", util.UUIDToString(reused.ID), err, util.UUIDToString(runs[0].ID))
	}
	_, _, err = svc.createAutopilotRunWithQuota(ctx, workspaceID, "api", "boundary-3", params)
	var quotaErr *AutopilotQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("N+1 admission error = %v, want quota exceeded", err)
	}

	usage, err := svc.AutopilotQuotaUsage(ctx, workspaceID)
	if err != nil || usage.Used == nil || usage.Reserved == nil || *usage.Used != 0 || *usage.Reserved != int64(limit) {
		t.Fatalf("reserved usage = %+v, %v", usage, err)
	}
	if got := usage.BlockedCounts["api"]; got != 1 {
		t.Fatalf("blocked API count = %d, want 1", got)
	}
	if _, err := settleAutopilotQuota(ctx, q, runs[0].QuotaReservationID, true); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if _, err := settleAutopilotQuota(ctx, q, runs[0].QuotaReservationID, true); err != nil {
		t.Fatalf("duplicate consume: %v", err)
	}
	if _, err := settleAutopilotQuota(ctx, q, runs[1].QuotaReservationID, false); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Consumed usage is monotonic: later issue cancellation/deletion cannot
	// turn a successfully created issue back into quota.
	if _, err := settleAutopilotQuota(ctx, q, runs[0].QuotaReservationID, false); err != nil {
		t.Fatalf("release consumed reservation: %v", err)
	}
	usage, err = svc.AutopilotQuotaUsage(ctx, workspaceID)
	if err != nil || *usage.Used != 1 || *usage.Reserved != 0 {
		t.Fatalf("final usage = %+v, %v; want one immutable consumed unit", usage, err)
	}
}

func TestAutopilotQuotaFirstRejectionNotifiesBillingManagersOncePerWorkspacePeriod(t *testing.T) {
	const attempts = 10
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 2)
	fixture.setNotificationPolicy(2)
	adminID := addAutopilotQuotaMember(t, fixture, "admin")
	ordinaryMemberID := addAutopilotQuotaMember(t, fixture, "member")
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		if _, _, err := fixture.service.createAutopilotRunWithQuota(
			ctx, fixture.workspaceID, "api", fmt.Sprintf("fill-quota-%d", i), fixture.createRunArgs,
		); err != nil {
			t.Fatalf("fill quota %d: %v", i, err)
		}
	}

	start := make(chan struct{})
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			params := fixture.createRunArgs
			params.Source = "schedule"
			_, _, err := fixture.service.createAutopilotRunWithQuota(
				ctx, fixture.workspaceID, "schedule", fmt.Sprintf("rejection-%d", i), params,
			)
			var quotaErr *AutopilotQuotaExceededError
			if !errors.As(err, &quotaErr) {
				errs <- fmt.Errorf("rejection %d = %v, want quota error", i, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// A different autopilot rejected later in the same billing period must not
	// select a second set of recipients or create duplicate notices.
	secondAutopilotID, _ := seedRunOnlyAutopilot(
		t,
		fixture.pool,
		util.UUIDToString(fixture.workspaceID),
		util.UUIDToString(fixture.agentID),
		util.UUIDToString(ordinaryMemberID),
	)
	secondParams := fixture.createRunArgs
	secondParams.AutopilotID = util.MustParseUUID(secondAutopilotID)
	secondParams.Source = "schedule"
	_, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "schedule", "second-autopilot-rejection", secondParams,
	)
	var quotaErr *AutopilotQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("second autopilot rejection = %v, want quota error", err)
	}

	rows, err := fixture.pool.Query(ctx, `
		SELECT recipient_id FROM inbox_item
		WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`, fixture.workspaceID)
	if err != nil {
		t.Fatalf("list quota rejection recipients: %v", err)
	}
	defer rows.Close()
	recipients := make(map[string]int)
	for rows.Next() {
		var recipientID pgtype.UUID
		if err := rows.Scan(&recipientID); err != nil {
			t.Fatalf("scan quota rejection recipient: %v", err)
		}
		recipients[util.UUIDToString(recipientID)]++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate quota rejection recipients: %v", err)
	}
	wantRecipients := []pgtype.UUID{fixture.publisherID, adminID}
	if len(recipients) != len(wantRecipients) {
		t.Fatalf("quota rejection recipients = %v, want owner and admin", recipients)
	}
	for _, recipientID := range wantRecipients {
		if got := recipients[util.UUIDToString(recipientID)]; got != 1 {
			t.Fatalf("quota rejection recipient %s notices = %d, want one", util.UUIDToString(recipientID), got)
		}
	}
	if got := recipients[util.UUIDToString(ordinaryMemberID)]; got != 0 {
		t.Fatalf("ordinary member received %d quota rejection notices, want none", got)
	}

	period, err := fixture.queries.GetAutopilotQuotaPeriod(ctx, db.GetAutopilotQuotaPeriodParams{
		WorkspaceID: fixture.workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: fixture.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: fixture.periodEnd, Valid: true},
	})
	if err != nil {
		t.Fatalf("load quota period: %v", err)
	}
	if !period.RejectionNotifiedAt.Valid {
		t.Fatal("first rejection notification marker was not persisted")
	}

	var title, noticeKey, severity, body string
	var hasThresholdPercent bool
	if err := fixture.pool.QueryRow(ctx, `
		SELECT details->>'autopilot_title', details->>'notice_key',
		       details ? 'threshold_percent', severity, body
		FROM inbox_item
		WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'
		LIMIT 1`, fixture.workspaceID,
	).Scan(&title, &noticeKey, &hasThresholdPercent, &severity, &body); err != nil {
		t.Fatalf("load first rejection details: %v", err)
	}
	if title == "" || noticeKey != autopilotQuotaFirstRejectionNoticeKey || hasThresholdPercent {
		t.Fatalf("first rejection details = title %q, key %q, threshold field %v", title, noticeKey, hasThresholdPercent)
	}
	if severity != "attention" {
		t.Fatalf("quota rejection severity = %q, want attention", severity)
	}
	if strings.Contains(body, title) {
		t.Fatalf("workspace-level quota rejection body names one autopilot: %q", body)
	}
	if strings.Contains(body, fixture.resetAt.UTC().Format(time.RFC3339)) ||
		!strings.Contains(body, fixture.resetAt.UTC().Format("January 2, 2006 at 15:04 UTC")) {
		t.Fatalf("quota rejection body reset time is not human-readable: %q", body)
	}

	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil {
		t.Fatalf("quota usage: %v", err)
	}
	if got := usage.BlockedCounts["schedule"]; got != attempts+1 {
		t.Fatalf("blocked schedule count = %d, want %d", got, attempts+1)
	}
}

func TestAutopilotQuotaInboxFailurePreservesQuotaRejection(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	fixture.setNotificationPolicy(1)
	adminID := addAutopilotQuotaMember(t, fixture, "admin")
	ctx := context.Background()
	if _, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "inbox-failure-fill", fixture.createRunArgs,
	); err != nil {
		t.Fatalf("fill quota: %v", err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := pgx.Identifier{"test_fail_quota_notice_" + suffix}.Sanitize()
	triggerName := pgx.Identifier{"test_fail_quota_notice_trigger_" + suffix}.Sanitize()
	if _, err := fixture.pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.workspace_id = '%s'::uuid
				AND NEW.type = 'autopilot_quota_exceeded'
				AND NEW.recipient_id = '%s'::uuid THEN
				RAISE EXCEPTION 'injected quota notice failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER %s BEFORE INSERT ON inbox_item
		FOR EACH ROW EXECUTE FUNCTION %s();`,
		functionName, fixture.workspace.String(), util.UUIDToString(adminID), triggerName, functionName)); err != nil {
		t.Fatalf("install inbox failure trigger: %v", err)
	}
	t.Cleanup(func() {
		fixture.pool.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON inbox_item", triggerName))
		fixture.pool.Exec(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})

	params := fixture.createRunArgs
	params.Source = "schedule"
	_, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "schedule", "inbox-failure-rejection", params,
	)
	var quotaErr *AutopilotQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("rejection error = %v, want quota exceeded despite inbox failure", err)
	}
	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil {
		t.Fatalf("quota usage: %v", err)
	}
	if got := usage.BlockedCounts["schedule"]; got != 1 {
		t.Fatalf("blocked schedule count = %d, want committed count 1", got)
	}
	period, err := fixture.queries.GetAutopilotQuotaPeriod(ctx, db.GetAutopilotQuotaPeriodParams{
		WorkspaceID: fixture.workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: fixture.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: fixture.periodEnd, Valid: true},
	})
	if err != nil {
		t.Fatalf("load quota period after notice failure: %v", err)
	}
	if period.RejectionNotifiedAt.Valid {
		t.Fatal("failed Inbox transaction must not persist the notification marker")
	}
	var failedTransactionNotices int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`,
		fixture.workspaceID,
	).Scan(&failedTransactionNotices); err != nil {
		t.Fatalf("count rolled-back quota notices: %v", err)
	}
	if failedTransactionNotices != 0 {
		t.Fatalf("failed multi-recipient transaction persisted %d notices, want zero", failedTransactionNotices)
	}

	if _, err := fixture.pool.Exec(ctx, fmt.Sprintf("DROP TRIGGER %s ON inbox_item", triggerName)); err != nil {
		t.Fatalf("remove inbox failure trigger: %v", err)
	}
	_, _, err = fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "schedule", "inbox-failure-retry", params,
	)
	if !errors.As(err, &quotaErr) {
		t.Fatalf("retry rejection error = %v, want quota exceeded", err)
	}
	var notices int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`,
		fixture.workspaceID,
	).Scan(&notices); err != nil {
		t.Fatalf("count retried quota notices: %v", err)
	}
	if notices != 2 {
		t.Fatalf("retried quota notices = %d, want owner and admin", notices)
	}
}

func TestAutopilotQuotaNoticeSurvivesDeletedAutopilot(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, fixture.autopilotID); err != nil {
		t.Fatalf("delete autopilot before quota notice: %v", err)
	}

	items, err := fixture.service.createAutopilotQuotaRejectionNotice(
		ctx,
		autopilotQuotaPolicy{
			action:      entitlement.ActionEnforce,
			limit:       1,
			periodStart: fixture.periodStart,
			periodEnd:   fixture.periodEnd,
			resetAt:     fixture.resetAt,
			notifications: &entitlement.NotificationPolicy{
				OnRejection: entitlement.NotificationFirstRejectionPerPeriod,
			},
		},
		fixture.workspaceID,
		"schedule",
		fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("create quota notice after autopilot deletion: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("quota notices after autopilot deletion = %d, want workspace owner", len(items))
	}
	if items[0].Body.String == "" || strings.Contains(items[0].Body.String, "run-only ap") {
		t.Fatalf("deleted-autopilot notice body = %q, want generic fallback", items[0].Body.String)
	}
}

func TestAutopilotQuotaNoticeDoesNotDependOnCreatorAgentOwner(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 0)
	fixture.setNotificationPolicy(0)
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET owner_id = NULL WHERE id = $1`, fixture.agentID); err != nil {
		t.Fatalf("remove autopilot creator agent owner: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		UPDATE autopilot SET created_by_type = 'agent', created_by_id = $2 WHERE id = $1`,
		fixture.autopilotID, fixture.agentID,
	); err != nil {
		t.Fatalf("make autopilot agent-created: %v", err)
	}

	_, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "schedule", "ownerless-agent-rejection", fixture.createRunArgs,
	)
	var quotaErr *AutopilotQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("ownerless-agent rejection = %v, want quota error", err)
	}
	var recipientID pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		SELECT recipient_id FROM inbox_item
		WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`,
		fixture.workspaceID,
	).Scan(&recipientID); err != nil {
		t.Fatalf("load ownerless-agent quota notice: %v", err)
	}
	if recipientID.Bytes != fixture.publisherID.Bytes {
		t.Fatalf("ownerless-agent notice recipient = %s, want workspace owner %s",
			util.UUIDToString(recipientID), util.UUIDToString(fixture.publisherID))
	}
}

func TestAutopilotQuotaNoticeRetriesAfterWorkspaceManagerIsRestored(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 0)
	fixture.setNotificationPolicy(0)
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `
		UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`,
		fixture.workspaceID, fixture.publisherID,
	); err != nil {
		t.Fatalf("remove final workspace manager role: %v", err)
	}

	assertRejected := func(key string) {
		t.Helper()
		_, _, err := fixture.service.createAutopilotRunWithQuota(
			ctx, fixture.workspaceID, "schedule", key, fixture.createRunArgs,
		)
		var quotaErr *AutopilotQuotaExceededError
		if !errors.As(err, &quotaErr) {
			t.Fatalf("quota rejection %q = %v, want quota error", key, err)
		}
	}
	loadPeriod := func() db.AutopilotQuotaPeriod {
		t.Helper()
		period, err := fixture.queries.GetAutopilotQuotaPeriod(ctx, db.GetAutopilotQuotaPeriodParams{
			WorkspaceID: fixture.workspaceID,
			PeriodStart: pgtype.Timestamptz{Time: fixture.periodStart, Valid: true},
			PeriodEnd:   pgtype.Timestamptz{Time: fixture.periodEnd, Valid: true},
		})
		if err != nil {
			t.Fatalf("load quota period: %v", err)
		}
		return period
	}
	countNotices := func() int {
		t.Helper()
		var count int
		if err := fixture.pool.QueryRow(ctx, `
			SELECT count(*) FROM inbox_item
			WHERE workspace_id = $1 AND type = 'autopilot_quota_exceeded'`,
			fixture.workspaceID,
		).Scan(&count); err != nil {
			t.Fatalf("count quota notices: %v", err)
		}
		return count
	}

	assertRejected("no-workspace-manager")
	if period := loadPeriod(); period.RejectionNotifiedAt.Valid {
		t.Fatal("quota notice marker was consumed without a workspace manager")
	}
	if got := countNotices(); got != 0 {
		t.Fatalf("quota notices without a workspace manager = %d, want zero", got)
	}

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`,
		fixture.workspaceID, fixture.publisherID,
	); err != nil {
		t.Fatalf("restore workspace owner: %v", err)
	}
	assertRejected("restored-workspace-manager")
	if period := loadPeriod(); !period.RejectionNotifiedAt.Valid {
		t.Fatal("quota notice marker was not persisted after manager recovery")
	}
	if got := countNotices(); got != 1 {
		t.Fatalf("quota notices after manager recovery = %d, want one", got)
	}
}

func TestAutopilotQuotaConcurrentAdmissionNeverExceedsLimit(t *testing.T) {
	const attempts = 25
	const limit = 5
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, limit)
	ctx := context.Background()
	start := make(chan struct{})
	unexpected := make(chan error, attempts)
	var admitted atomic.Int64
	var blocked atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, err := fixture.service.createAutopilotRunWithQuota(
				ctx, fixture.workspaceID, "api", fmt.Sprintf("concurrent-%d", i), fixture.createRunArgs,
			)
			if err == nil {
				admitted.Add(1)
				return
			}
			var quotaErr *AutopilotQuotaExceededError
			if errors.As(err, &quotaErr) {
				blocked.Add(1)
				return
			}
			unexpected <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(unexpected)
	for err := range unexpected {
		t.Errorf("unexpected concurrent admission error: %v", err)
	}
	if got := admitted.Load(); got != limit {
		t.Fatalf("admitted = %d, want %d", got, limit)
	}
	if got := blocked.Load(); got != attempts-limit {
		t.Fatalf("blocked = %d, want %d", got, attempts-limit)
	}
	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || usage.Reserved == nil || *usage.Reserved != limit || *usage.Used != 0 {
		t.Fatalf("concurrent usage = %+v, %v; want %d reserved", usage, err, limit)
	}
}

func TestAutopilotQuotaObserveToEnforceAndOffFinalization(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionObserve, 1)
	metrics := &autopilotQuotaMetricsRecorder{}
	fixture.service.QuotaMetrics = metrics
	ctx := context.Background()
	runs := make([]db.AutopilotRun, 0, 2)
	for i := 0; i < 2; i++ {
		run, _, err := fixture.service.createAutopilotRunWithQuota(
			ctx, fixture.workspaceID, "api", fmt.Sprintf("observe-%d", i), fixture.createRunArgs,
		)
		if err != nil {
			t.Fatalf("observe admission %d: %v", i, err)
		}
		runs = append(runs, run)
	}
	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || usage.Action != string(entitlement.ActionObserve) || *usage.Reserved != 2 || usage.Reached != nil {
		t.Fatalf("observe usage = %+v, %v; want two reservations", usage, err)
	}
	if got := metrics.count("observe", "api", "would_block"); got != 1 {
		t.Fatalf("observed would-block metric = %d, want 1", got)
	}

	fixture.setPolicy(entitlement.ActionEnforce, fixture.periodStart, fixture.periodEnd, 1)
	_, _, err = fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "enforce-after-observe", fixture.createRunArgs,
	)
	var quotaErr *AutopilotQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("enforce after observe error = %v, want quota exceeded", err)
	}

	// Switching the current policy off must not strand reservations created
	// under observe/enforce. Terminal finalization relies on the persisted slot.
	fixture.setPolicy(entitlement.ActionOff, fixture.periodStart, fixture.periodEnd, 1)
	for _, run := range runs {
		if _, err := fixture.service.failAutopilotRun(ctx, db.UpdateAutopilotRunFailedParams{
			ID: run.ID, FailureReason: pgtype.Text{String: "test cleanup", Valid: true},
		}); err != nil {
			t.Fatalf("finalize while policy off: %v", err)
		}
	}
	fixture.setPolicy(entitlement.ActionEnforce, fixture.periodStart, fixture.periodEnd, 1)
	usage, err = fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || *usage.Used != 0 || *usage.Reserved != 0 {
		t.Fatalf("usage after off finalization = %+v, %v; want zero", usage, err)
	}
	if _, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "reopened-after-off", fixture.createRunArgs,
	); err != nil {
		t.Fatalf("admission after reopening policy: %v", err)
	}
}

func TestAutopilotQuotaConsumedCreateIssueCannotBeRefunded(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 2)
	ctx := context.Background()

	cancelled, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "consumed-then-cancelled", fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("admit cancelled run: %v", err)
	}
	if _, err := settleAutopilotQuota(ctx, fixture.queries, cancelled.QuotaReservationID, true); err != nil {
		t.Fatalf("consume cancelled run: %v", err)
	}
	if _, err := fixture.service.failAutopilotRun(ctx, db.UpdateAutopilotRunFailedParams{
		ID: cancelled.ID, FailureReason: pgtype.Text{String: "issue cancelled", Valid: true},
	}); err != nil {
		t.Fatalf("fail consumed run: %v", err)
	}

	deleted, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "consumed-then-deleted", fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("admit deleted run: %v", err)
	}
	deleted, err = fixture.queries.UpdateAutopilotRunIssueCreated(ctx, db.UpdateAutopilotRunIssueCreatedParams{
		ID: deleted.ID, IssueID: fixture.issueID,
	})
	if err != nil {
		t.Fatalf("link issue-created run: %v", err)
	}
	if _, err := settleAutopilotQuota(ctx, fixture.queries, deleted.QuotaReservationID, true); err != nil {
		t.Fatalf("consume deleted run: %v", err)
	}
	if err := fixture.service.FailAutopilotRunsByIssue(ctx, fixture.issueID); err != nil {
		t.Fatalf("fail runs before issue deletion: %v", err)
	}

	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || *usage.Used != 2 || *usage.Reserved != 0 {
		t.Fatalf("usage after cancellation/deletion = %+v, %v; want two consumed units", usage, err)
	}
}

func TestAutopilotQuotaTerminalUpdateNeedsNoTransactionStarter(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionOff, 1)
	ctx := context.Background()
	run, err := fixture.queries.CreateAutopilotRun(ctx, fixture.createRunArgs)
	if err != nil {
		t.Fatalf("create off-path run: %v", err)
	}
	serviceWithoutTx := &AutopilotService{Queries: fixture.queries}
	updated, err := serviceWithoutTx.completeAutopilotRun(ctx, db.UpdateAutopilotRunCompletedParams{ID: run.ID})
	if err != nil {
		t.Fatalf("complete off-path run: %v", err)
	}
	if updated.Status != "completed" || updated.QuotaReservationID.Valid {
		t.Fatalf("completed off-path run = %+v", updated)
	}
}

func TestAutopilotQuotaSettlementStaysInOriginalPeriod(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	ctx := context.Background()
	run, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "cross-period", fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("admit first-period run: %v", err)
	}
	secondStart := fixture.periodEnd
	secondEnd := secondStart.Add(19 * time.Hour)
	fixture.setPolicy(entitlement.ActionEnforce, secondStart, secondEnd, 1)
	if _, err := fixture.service.completeAutopilotRun(ctx, db.UpdateAutopilotRunCompletedParams{ID: run.ID}); err != nil {
		t.Fatalf("complete after period rollover: %v", err)
	}

	first, err := fixture.queries.GetAutopilotQuotaPeriod(ctx, db.GetAutopilotQuotaPeriodParams{
		WorkspaceID: fixture.workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: fixture.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: fixture.periodEnd, Valid: true},
	})
	if err != nil || first.UsedCount != 1 || first.ReservedCount != 0 {
		t.Fatalf("first period = %+v, %v; want one consumed", first, err)
	}
	second, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || *second.Used != 0 || *second.Reserved != 0 {
		t.Fatalf("second period usage = %+v, %v; want zero", second, err)
	}
	if _, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "second-period", fixture.createRunArgs,
	); err != nil {
		t.Fatalf("admit second-period run: %v", err)
	}
}

func TestAutopilotQuotaWorkspaceIsolation(t *testing.T) {
	first := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	second := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	ctx := context.Background()
	if _, _, err := first.service.createAutopilotRunWithQuota(
		ctx, first.workspaceID, "api", "isolated", first.createRunArgs,
	); err != nil {
		t.Fatalf("first workspace admission: %v", err)
	}
	_, _, err := first.service.createAutopilotRunWithQuota(
		ctx, first.workspaceID, "api", "isolated-over-limit", first.createRunArgs,
	)
	var quotaErr *AutopilotQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("first workspace over-limit error = %v", err)
	}
	if _, _, err := second.service.createAutopilotRunWithQuota(
		ctx, second.workspaceID, "api", "isolated", second.createRunArgs,
	); err != nil {
		t.Fatalf("second workspace admission leaked first usage: %v", err)
	}
}

func TestAutopilotQuotaReconcilerSettlesTerminalOnlyOnce(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 2)
	ctx := context.Background()
	terminal, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "reconcile-terminal", fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("admit terminal run: %v", err)
	}
	active, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "reconcile-active", fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("admit active run: %v", err)
	}
	// Simulate a crash after the run reached terminal state but before the old
	// two-step finalizer settled its reservation.
	if _, err := fixture.queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{ID: terminal.ID}); err != nil {
		t.Fatalf("seed terminal crash window: %v", err)
	}

	type reconcileResult struct {
		settled int
		err     error
	}
	results := make(chan reconcileResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			settled, err := fixture.service.ReconcileAutopilotQuotaReservations(
				ctx, time.Now().Add(time.Minute), time.Now().Add(-6*time.Hour), 10,
			)
			results <- reconcileResult{settled: settled, err: err}
		}()
	}
	total := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("reconcile replica %d: %v", i, result.err)
		}
		total += result.settled
	}
	if total != 1 {
		t.Fatalf("replicas reported %d settlements, want exactly 1", total)
	}
	activeAfter, err := fixture.queries.GetAutopilotRun(ctx, active.ID)
	if err != nil || activeAfter.Status != "running" {
		t.Fatalf("active run after reconcile = %+v, %v; want running", activeAfter, err)
	}
	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || *usage.Used != 1 || *usage.Reserved != 1 {
		t.Fatalf("reconciled usage = %+v, %v; want one used/one reserved", usage, err)
	}
}

func TestAutopilotQuotaReconcilerReleasesOnlyAbandonedManualOrAPIRuns(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 4)
	ctx := context.Background()
	create := func(source, key string) db.AutopilotRun {
		t.Helper()
		params := fixture.createRunArgs
		params.Source = source
		run, _, err := fixture.service.createAutopilotRunWithQuota(
			ctx, fixture.workspaceID, source, key, params,
		)
		if err != nil {
			t.Fatalf("admit %s partial run: %v", source, err)
		}
		return run
	}

	staleManual := create("manual", "stale-manual")
	taskBackedAPI := create("api", "task-backed-api")
	staleWebhook := create("webhook", "stale-webhook")
	freshAPI := create("api", "fresh-api")
	agent, err := fixture.queries.GetAgent(ctx, fixture.agentID)
	if err != nil {
		t.Fatalf("load task-backed run agent: %v", err)
	}
	if _, err := fixture.queries.CreateAutopilotTask(ctx, db.CreateAutopilotTaskParams{
		AgentID:              fixture.agentID,
		RuntimeID:            agent.RuntimeID,
		AutopilotRunID:       taskBackedAPI.ID,
		OriginatorUserID:     fixture.publisherID,
		AccountableUserID:    fixture.publisherID,
		OriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		TriggerEvidenceKind:  pgtype.Text{String: "autopilot_run", Valid: true},
		TriggerEvidenceRefID: taskBackedAPI.ID,
	}); err != nil {
		t.Fatalf("create unlinked task evidence: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		UPDATE autopilot_quota_reservation
		SET created_at = now() - interval '24 hours'
		WHERE id = ANY($1::uuid[])`, []uuid.UUID{
		uuid.UUID(staleManual.QuotaReservationID.Bytes),
		uuid.UUID(taskBackedAPI.QuotaReservationID.Bytes),
		uuid.UUID(staleWebhook.QuotaReservationID.Bytes),
	}); err != nil {
		t.Fatalf("age partial reservations: %v", err)
	}

	now := time.Now()
	settled, err := fixture.service.ReconcileAutopilotQuotaReservations(
		ctx, now.Add(-10*time.Minute), now.Add(-6*time.Hour), 10,
	)
	if err != nil || settled != 1 {
		t.Fatalf("reconcile abandoned partial runs = %d, %v; want one", settled, err)
	}

	manualAfter, err := fixture.queries.GetAutopilotRun(ctx, staleManual.ID)
	if err != nil || manualAfter.Status != "failed" {
		t.Fatalf("stale manual run = %+v, %v; want failed", manualAfter, err)
	}
	for name, run := range map[string]db.AutopilotRun{
		"durable webhook": staleWebhook,
		"task-backed api": taskBackedAPI,
		"fresh api":       freshAPI,
	} {
		after, err := fixture.queries.GetAutopilotRun(ctx, run.ID)
		if err != nil || after.Status != "running" {
			t.Fatalf("%s run = %+v, %v; want running", name, after, err)
		}
	}
	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || usage.Reserved == nil || *usage.Reserved != 3 || *usage.Used != 0 {
		t.Fatalf("usage after partial recovery = %+v, %v; want three reservations", usage, err)
	}
}

func TestAutopilotQuotaIdempotencyRecoversOrphanedReservation(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionEnforce, 1)
	ctx := context.Background()
	first, _, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "orphaned-key", fixture.createRunArgs,
	)
	if err != nil {
		t.Fatalf("first admission: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM autopilot_run WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("remove quota-linked run: %v", err)
	}
	recovered, reused, err := fixture.service.createAutopilotRunWithQuota(
		ctx, fixture.workspaceID, "api", "orphaned-key", fixture.createRunArgs,
	)
	if err != nil || reused || recovered.ID.Bytes == first.ID.Bytes {
		t.Fatalf("orphan recovery = run %s reused=%v err=%v", util.UUIDToString(recovered.ID), reused, err)
	}
	usage, err := fixture.service.AutopilotQuotaUsage(ctx, fixture.workspaceID)
	if err != nil || *usage.Used != 0 || *usage.Reserved != 1 {
		t.Fatalf("orphan-recovered usage = %+v, %v; want one reservation", usage, err)
	}
}

func TestAutopilotQuotaSchedulePersistsSkippedRun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceIDString, publisherID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotIDString, _ := seedRunOnlyAutopilot(t, pool, workspaceIDString, agentID, publisherID)
	workspaceID := util.MustParseUUID(workspaceIDString)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM autopilot_quota_reservation WHERE workspace_id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id = $1`, workspaceID)
	})

	start := time.Now().UTC().Truncate(time.Second)
	end := start.Add(13 * time.Hour)
	limit := 0
	stub := entitlementtest.New()
	stub.Set(uuid.UUID(workspaceID.Bytes), entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: entitlement.ActionEnforce, Limit: &limit,
			PeriodStart: &start, PeriodEnd: &end, ResetAt: &end,
		},
	})
	autopilot, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotIDString))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	var triggerID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, timezone, created_by_type, created_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'UTC', 'member', $2) RETURNING id`, autopilot.ID, publisherID).Scan(&triggerID); err != nil {
		t.Fatalf("create schedule trigger: %v", err)
	}
	svc := &AutopilotService{
		Queries: q, TxStarter: pool, Bus: events.New(),
		TaskSvc:      &TaskService{Queries: q, TxStarter: pool, Bus: events.New()},
		Entitlements: stub,
	}
	run, err := svc.DispatchAutopilotForPlan(ctx, autopilot, triggerID, "schedule", nil, start.Add(time.Minute))
	if err != nil {
		t.Fatalf("scheduled dispatch: %v", err)
	}
	if run.Status != "skipped" || !run.ReasonCode.Valid || run.ReasonCode.String != "quota_exceeded" {
		t.Fatalf("scheduled run = status %q reason %q, want skipped/quota_exceeded", run.Status, run.ReasonCode.String)
	}
}
