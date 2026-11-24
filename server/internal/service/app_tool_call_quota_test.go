package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type appToolCallQuotaFixture struct {
	pool        *pgxpool.Pool
	quota       AppToolCallQuota
	stub        *entitlementtest.Stub
	workspace   uuid.UUID
	workspaceID pgtype.UUID
	ownerID     pgtype.UUID
	periodStart time.Time
	periodEnd   time.Time
}

func newAppToolCallQuotaFixture(t *testing.T) *appToolCallQuotaFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	workspaceIDString, ownerID, _, _ := seedAttributionFixture(t, pool)
	workspaceID := util.MustParseUUID(workspaceIDString)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	stub := entitlementtest.New()
	f := &appToolCallQuotaFixture{
		pool: pool, stub: stub,
		workspace: uuid.UUID(workspaceID.Bytes), workspaceID: workspaceID, ownerID: util.MustParseUUID(ownerID),
		periodStart: start, periodEnd: start.AddDate(0, 1, 0),
		quota: AppToolCallQuota{
			Queries: db.New(pool), Entitlements: stub, TxStarter: pool, Bus: events.New(),
			now: func() time.Time { return start.Add(12 * time.Hour) },
		},
	}
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM app_tool_call_quota_period WHERE workspace_id = $1`, workspaceID)
		pool.Exec(ctx, `DELETE FROM entitlement_limit_overage WHERE workspace_id = $1`, workspaceID)
		pool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	})
	return f
}

func (f *appToolCallQuotaFixture) decision(action entitlement.Action, limit int, start time.Time) entitlement.Decision {
	end := start.AddDate(0, 1, 0)
	gate := entitlement.Gate{Action: action}
	if action != entitlement.ActionOff {
		gate.Limit = &limit
		gate.PeriodStart, gate.PeriodEnd, gate.ResetAt = &start, &end, &end
	}
	if action == entitlement.ActionEnforce {
		gate.Notifications = &entitlement.NotificationPolicy{OnRejection: entitlement.NotificationFirstRejectionPerLimit}
	}
	return entitlement.Decision{Gate: gate}
}

func (f *appToolCallQuotaFixture) setPolicy(action entitlement.Action, limit int) {
	f.stub.Set(f.workspace, entitlement.GateAppToolCalls, f.decision(action, limit, f.periodStart))
}

// admit is Admit's refusal; the period a call was counted in is checked where
// it matters.
func (f *appToolCallQuotaFixture) admit(ctx context.Context) *connectedapps.CallQuotaExceeded {
	_, refused := f.quota.Admit(ctx, f.workspaceID)
	return refused
}

func (f *appToolCallQuotaFixture) used(t *testing.T) int64 {
	t.Helper()
	return f.usedIn(t, f.periodStart)
}

func (f *appToolCallQuotaFixture) usedIn(t *testing.T, periodStart time.Time) int64 {
	t.Helper()
	var used int64
	err := f.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(used_count), 0) FROM app_tool_call_quota_period WHERE workspace_id = $1 AND period_start = $2`,
		f.workspaceID, periodStart).Scan(&used)
	if err != nil {
		t.Fatalf("read used: %v", err)
	}
	return used
}

func (f *appToolCallQuotaFixture) overage(t *testing.T) (peak, observed, enforced int64) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(), `
		SELECT peak_usage, observed_count, enforced_count FROM entitlement_limit_overage
		WHERE workspace_id = $1 AND gate = 'app_tool_calls'`, f.workspaceID).Scan(&peak, &observed, &enforced)
	if err != nil {
		t.Fatalf("read overage: %v", err)
	}
	return peak, observed, enforced
}

func (f *appToolCallQuotaFixture) notices(t *testing.T, recipient pgtype.UUID) int {
	t.Helper()
	var n int
	err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND type = $3`,
		f.workspaceID, recipient, AppToolCallQuotaNoticeType).Scan(&n)
	if err != nil {
		t.Fatalf("count notices: %v", err)
	}
	return n
}

// Without Cloud, or with the gate off, every call runs and nothing is written.
func TestAppToolCallQuotaOffTouchesNothing(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	ctx := context.Background()
	if countedIn, got := (AppToolCallQuota{Queries: f.quota.Queries}).Admit(ctx, f.workspaceID); got != nil || !countedIn.IsZero() {
		t.Fatalf("no entitlements: admit = %v, %+v", countedIn, got)
	}
	f.setPolicy(entitlement.ActionOff, 0)
	for range 3 {
		if got := f.admit(ctx); got != nil {
			t.Fatalf("off: admit = %+v", got)
		}
	}
	var rows int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM app_tool_call_quota_period WHERE workspace_id = $1`, f.workspaceID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("off wrote %d quota rows (err %v), want none", rows, err)
	}
	usage, err := f.quota.Usage(ctx, f.workspaceID)
	if err != nil || usage.Action != entitlement.ActionOff || usage.Used != nil {
		t.Fatalf("off usage = %+v, err = %v", usage, err)
	}
}

// Concurrent calls at the boundary never take the workspace past its limit;
// each refused call is recorded, and owners and admins hear once.
func TestAppToolCallQuotaEnforceNeverExceedsLimit(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	f.setPolicy(entitlement.ActionEnforce, 5)
	adminID := addAppToolCallQuotaMember(t, f, "admin")
	memberID := addAppToolCallQuotaMember(t, f, "member")
	ctx := context.Background()

	const calls = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	var refused []*connectedapps.CallQuotaExceeded
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := f.admit(ctx); got != nil {
				mu.Lock()
				refused = append(refused, got)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(refused) != calls-5 || f.used(t) != 5 {
		t.Fatalf("refused %d, used %d; want %d refused and 5 used", len(refused), f.used(t), calls-5)
	}
	if got := refused[0]; got.Used != 5 || got.Limit != 5 || !got.ResetAt.Equal(f.periodEnd) {
		t.Fatalf("refusal = %+v, want used 5 / limit 5 / reset at %s", got, f.periodEnd)
	}
	if peak, observed, enforced := f.overage(t); peak != 6 || observed != 0 || enforced != calls-5 {
		t.Fatalf("overage peak %d observed %d enforced %d", peak, observed, enforced)
	}
	if f.notices(t, f.ownerID) != 1 || f.notices(t, adminID) != 1 || f.notices(t, memberID) != 0 {
		t.Fatalf("notices: owner %d admin %d member %d; want 1, 1, 0",
			f.notices(t, f.ownerID), f.notices(t, adminID), f.notices(t, memberID))
	}
	usage, err := f.quota.Usage(ctx, f.workspaceID)
	if err != nil || usage.Used == nil || *usage.Used != 5 || usage.Reached == nil || !*usage.Reached {
		t.Fatalf("usage = %+v, err = %v", usage, err)
	}
}

// Observe runs every call, counts it, and records how far over it went.
func TestAppToolCallQuotaObserveCountsPastLimit(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	f.setPolicy(entitlement.ActionObserve, 2)
	ctx := context.Background()
	for i := range 4 {
		countedIn, got := f.quota.Admit(ctx, f.workspaceID)
		if got != nil || !countedIn.Equal(f.periodStart) {
			t.Fatalf("observe call %d = counted in %v, refused %+v; want counted in %v", i, countedIn, got, f.periodStart)
		}
	}
	if f.used(t) != 4 {
		t.Fatalf("used = %d, want 4", f.used(t))
	}
	if peak, observed, enforced := f.overage(t); peak != 4 || observed != 2 || enforced != 0 {
		t.Fatalf("overage peak %d observed %d enforced %d; want 4, 2, 0", peak, observed, enforced)
	}
	if f.notices(t, f.ownerID) != 0 {
		t.Fatal("observe sent a notice")
	}
	usage, err := f.quota.Usage(ctx, f.workspaceID)
	if err != nil || usage.Action != entitlement.ActionObserve || *usage.Used != 4 || usage.Reached != nil {
		t.Fatalf("observe usage = %+v, err = %v", usage, err)
	}
}

// A workspace that upgraded or added seats moments ago is not refused on the
// cached policy, and reaching the new limit notifies again.
func TestAppToolCallQuotaRefreshesBeforeRefusing(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	f.setPolicy(entitlement.ActionEnforce, 1)
	ctx := context.Background()
	if got := f.admit(ctx); got != nil {
		t.Fatalf("first call refused: %+v", got)
	}
	if got := f.admit(ctx); got == nil {
		t.Fatal("second call admitted past limit 1")
	}
	f.stub.SetRefreshed(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 3, f.periodStart))
	for i := range 2 {
		if got := f.admit(ctx); got != nil {
			t.Fatalf("call %d after upgrade refused: %+v", i, got)
		}
	}
	if got := f.admit(ctx); got == nil || got.Limit != 3 {
		t.Fatalf("call past the new limit = %+v, want refused at 3", got)
	}
	if f.used(t) != 3 || f.notices(t, f.ownerID) != 2 {
		t.Fatalf("used %d notices %d; want 3 and one per limit (2)", f.used(t), f.notices(t, f.ownerID))
	}
}

// The next period counts from zero.
func TestAppToolCallQuotaResetsWithThePeriod(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	f.setPolicy(entitlement.ActionEnforce, 1)
	ctx := context.Background()
	f.admit(ctx)
	if got := f.admit(ctx); got == nil {
		t.Fatal("call past limit admitted")
	}
	f.stub.Set(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 1, f.periodEnd))
	if got := f.admit(ctx); got != nil {
		t.Fatalf("first call of the next period refused: %+v", got)
	}
}

// Right after the month turns, the cached policy still names the month that
// is over. The call counts in the new month, not the old one; a Cloud that
// still sends the old month admits without counting.
func TestAppToolCallQuotaRereadsAPolicyWhosePeriodEnded(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	ctx := context.Background()
	previous := f.periodStart.AddDate(0, -1, 0)
	f.stub.Set(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 1, previous))
	f.quota.now = func() time.Time { return f.periodStart.Add(30 * time.Second) }

	if countedIn, got := f.quota.Admit(ctx, f.workspaceID); got != nil || !countedIn.IsZero() {
		t.Fatalf("stale policy, nothing newer: admit = %v, %+v; want uncounted", countedIn, got)
	}
	if f.usedIn(t, previous) != 0 || f.used(t) != 0 {
		t.Fatalf("stale policy counted: previous %d, current %d; want none", f.usedIn(t, previous), f.used(t))
	}

	f.stub.SetRefreshed(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 1, f.periodStart))
	if countedIn, got := f.quota.Admit(ctx, f.workspaceID); got != nil || !countedIn.Equal(f.periodStart) {
		t.Fatalf("first call of the month = counted in %v, refused %+v", countedIn, got)
	}
	if f.usedIn(t, previous) != 0 || f.used(t) != 1 {
		t.Fatalf("used previous %d, current %d; want 0 and 1", f.usedIn(t, previous), f.used(t))
	}
}

// Billing opened in the first minute of a month, before any call refreshed the
// cached policy, shows the new month, not the one that is over.
func TestAppToolCallQuotaUsageRereadsAPolicyWhosePeriodEnded(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	ctx := context.Background()
	previous := f.periodStart.AddDate(0, -1, 0)
	f.stub.Set(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 5, previous))
	f.quota.now = func() time.Time { return previous.Add(time.Hour) }
	f.admit(ctx)
	f.admit(ctx)

	f.quota.now = func() time.Time { return f.periodStart.Add(30 * time.Second) }
	f.stub.SetRefreshed(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 5, f.periodStart))
	usage, err := f.quota.Usage(ctx, f.workspaceID)
	if err != nil || usage.Used == nil || *usage.Used != 0 || usage.ResetAt == nil || !usage.ResetAt.Equal(f.periodEnd) {
		t.Fatalf("usage at the start of the month = %+v, err = %v; want 0 used, resetting %v", usage, err, f.periodEnd)
	}
}

// staleRefreshProvider answers RefreshGate from a policy fetched moments ago,
// as the Cloud client does inside its reuse window; only RefreshGateNow sees
// a seat bought since.
type staleRefreshProvider struct {
	cached, current entitlement.Decision
}

func (p staleRefreshProvider) Gate(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision {
	return p.cached
}

func (p staleRefreshProvider) RefreshGate(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision {
	return p.cached
}

func (p staleRefreshProvider) RefreshGateNow(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision {
	return p.current
}

// A seat bought moments ago admits the call even when the policy was
// refreshed just before the purchase.
func TestAppToolCallQuotaRefreshSkipsTheReuseWindow(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	ctx := context.Background()
	f.quota.Entitlements = staleRefreshProvider{
		cached:  f.decision(entitlement.ActionEnforce, 1, f.periodStart),
		current: f.decision(entitlement.ActionEnforce, 2, f.periodStart),
	}
	for i := range 2 {
		if got := f.admit(ctx); got != nil {
			t.Fatalf("call %d after the seat purchase refused: %+v", i, got)
		}
	}
	if f.used(t) != 2 {
		t.Fatalf("used = %d, want 2", f.used(t))
	}
}

// Managers hear once per limit in a period, also when the limit goes back to
// one they were already told about (seats removed, then added back).
func TestAppToolCallQuotaNotifiesEachLimitOncePerPeriod(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	ctx := context.Background()
	f.setPolicy(entitlement.ActionEnforce, 1)
	f.admit(ctx)
	if got := f.admit(ctx); got == nil {
		t.Fatal("call past limit 1 admitted")
	}
	f.stub.SetRefreshed(f.workspace, entitlement.GateAppToolCalls, f.decision(entitlement.ActionEnforce, 2, f.periodStart))
	f.admit(ctx)
	if got := f.admit(ctx); got == nil || got.Limit != 2 {
		t.Fatalf("call past limit 2 = %+v, want refused", got)
	}
	f.setPolicy(entitlement.ActionEnforce, 1)
	if got := f.admit(ctx); got == nil || got.Limit != 1 {
		t.Fatalf("call back at limit 1 = %+v, want refused", got)
	}
	if n := f.notices(t, f.ownerID); n != 2 {
		t.Fatalf("owner notices = %d, want one each for limits 1 and 2", n)
	}
}

func TestAppToolCallQuotaMemberUsage(t *testing.T) {
	f := newAppToolCallQuotaFixture(t)
	ctx := context.Background()
	var agentID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, f.workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load agent: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM app_tool_call WHERE workspace_id = $1`, f.workspaceID)
	})
	inPeriod := f.periodStart.Add(time.Hour)
	next := f.periodEnd
	for _, c := range []struct {
		by        pgtype.UUID
		outcome   string
		countedIn *time.Time
		at        time.Time
	}{
		{f.ownerID, "ok", &f.periodStart, inPeriod},
		{f.ownerID, "error", &f.periodStart, inPeriod},
		{f.ownerID, "approved", &f.periodStart, inPeriod},
		{f.ownerID, "ok", &f.periodStart, f.periodEnd.Add(time.Second)}, // counted at the end of the month, finished after it
		{f.ownerID, "error", nil, inPeriod},                             // an approved call that failed its checks before running
		{f.ownerID, "ok", nil, inPeriod},                                // a Remote MCP call
		{f.ownerID, "blocked", nil, inPeriod},
		{f.ownerID, "quota_exceeded", nil, inPeriod},
		{pgtype.UUID{}, "ok", &f.periodStart, inPeriod},
		{f.ownerID, "ok", &next, f.periodEnd.Add(time.Hour)},
	} {
		if _, err := f.pool.Exec(ctx, `
			INSERT INTO app_tool_call (workspace_id, agent_id, triggered_by, account_scope, toolkit_slug, tool_slug, action_class, outcome, metered_period_start, created_at)
			VALUES ($1, $2, $3, 'personal', 'gmail', 'GMAIL_SEND_EMAIL', 'send', $4, $5, $6)`,
			f.workspaceID, agentID, c.by, c.outcome, c.countedIn, c.at); err != nil {
			t.Fatalf("insert call: %v", err)
		}
	}
	got, err := f.quota.MemberUsage(ctx, f.workspaceID, f.periodStart)
	if err != nil {
		t.Fatalf("member usage: %v", err)
	}
	byUser := map[string]int64{}
	for _, m := range got {
		byUser[util.UUIDToString(m.UserID)] = m.Calls
	}
	if len(got) != 2 || byUser[util.UUIDToString(f.ownerID)] != 4 || byUser[""] != 1 {
		t.Fatalf("member usage = %+v, want owner 4 and no-person 1", got)
	}
}

func addAppToolCallQuotaMember(t *testing.T, f *appToolCallQuotaFixture, role string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var userID pgtype.UUID
	email := "app-quota-" + role + "-" + uuid.NewString() + "@multica.test"
	if err := f.pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('App Quota Member', $1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`, f.workspaceID, userID, role); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.workspaceID, userID)
		f.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}
