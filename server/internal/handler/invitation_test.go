package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
)

const invitationTestEmail = "invitation-test@multica.ai"

type stubSeatCapacity struct {
	reserveDecision seatcapacity.Decision
	reserveErr      error
	reserveCalls    int
	releaseCalls    int
	consumeCalls    int
	consumeDecision *seatcapacity.Decision
	consumeErr      error
}

func (s *stubSeatCapacity) Enabled() bool { return true }
func (s *stubSeatCapacity) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (seatcapacity.Decision, error) {
	s.reserveCalls++
	return s.reserveDecision, s.reserveErr
}
func (s *stubSeatCapacity) ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) Consume(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	s.consumeCalls++
	decision := seatcapacity.Decision{Managed: true, Allowed: true}
	if s.consumeDecision != nil {
		decision = *s.consumeDecision
	}
	return decision, s.consumeErr
}
func (s *stubSeatCapacity) Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) Release(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	s.releaseCalls++
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) GetOperation(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{}, nil
}

func useSeatCapacity(t *testing.T, executor seatcapacity.Executor) {
	t.Helper()
	previous := testHandler.SeatCapacity
	testHandler.SeatCapacity = executor
	t.Cleanup(func() {
		testHandler.SeatCapacity = previous
	})
}

func TestDefaultInvitationRateLimits(t *testing.T) {
	limits := DefaultInvitationRateLimits()
	want := SlidingWindowRateLimit{Limit: 6, Window: 24 * time.Hour}
	if limits.Recipient != want {
		t.Fatalf("recipient limit = %+v, want %+v", limits.Recipient, want)
	}
}

type stubInvitationRateLimiter struct {
	allowed     bool
	allowResult *bool
	err         error
	allowErr    error
	retryAfter  time.Duration
	checkKeys   []string
	allowKeys   []string
}

func (l *stubInvitationRateLimiter) Allow(ctx context.Context, key string) bool {
	allowed, _ := l.AllowWithError(ctx, key)
	return allowed
}

func (l *stubInvitationRateLimiter) AllowWithError(_ context.Context, key string) (bool, error) {
	l.allowKeys = append(l.allowKeys, key)
	if l.allowResult != nil {
		return *l.allowResult, l.allowErr
	}
	return l.allowed, l.allowErr
}

func (l *stubInvitationRateLimiter) Check(ctx context.Context, key string) bool {
	allowed, _ := l.CheckWithError(ctx, key)
	return allowed
}

func (l *stubInvitationRateLimiter) CheckWithError(_ context.Context, key string) (bool, error) {
	l.checkKeys = append(l.checkKeys, key)
	return l.allowed, l.err
}

func (l *stubInvitationRateLimiter) RetryAfter(_ context.Context, _ string) time.Duration {
	return l.retryAfter
}

func useInvitationRateLimiters(t *testing.T, limiters InvitationRateLimiters) {
	t.Helper()
	previous := testHandler.InvitationRateLimiters
	testHandler.InvitationRateLimiters = limiters
	t.Cleanup(func() {
		testHandler.InvitationRateLimiters = previous
	})
}

func clearInvitationsForTestWorkspace(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`DELETE FROM workspace_invitation WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID),
	); err != nil {
		t.Fatalf("clear invitations: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_invitation WHERE workspace_id = $1`,
			parseUUID(testWorkspaceID),
		)
	})
}

type rollbackOnCommitTx struct {
	pgx.Tx
}

func (tx rollbackOnCommitTx) Commit(ctx context.Context) error {
	_ = tx.Tx.Rollback(ctx)
	return errors.New("forced commit failure")
}

type rollbackOnCommitTxStarter struct {
	pool *pgxpool.Pool
}

func (s rollbackOnCommitTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return rollbackOnCommitTx{Tx: tx}, nil
}

// Sanity check: a fresh, live pending invitation must block re-invitation.
func TestCreateInvitation_BlocksWhilePending(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	actor := &stubInvitationRateLimiter{allowed: true}
	workspace := &stubInvitationRateLimiter{allowed: true}
	recipient := &stubInvitationRateLimiter{allowed: true}
	useInvitationRateLimiters(t, InvitationRateLimiters{Actor: actor, Workspace: workspace, Recipient: recipient})

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first invite: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateInvitation(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second invite: expected 409 while still pending, got %d: %s", w2.Code, w2.Body.String())
	}
	for name, calls := range map[string]int{
		"actor checks": len(actor.checkKeys), "workspace checks": len(workspace.checkKeys), "recipient checks": len(recipient.checkKeys),
		"actor allows": len(actor.allowKeys), "workspace allows": len(workspace.allowKeys), "recipient allows": len(recipient.allowKeys),
	} {
		if calls != 1 {
			t.Errorf("%s limiter calls = %d, want 1; a pending retry must not consume budget", name, calls)
		}
	}
}

func TestCreateInvitation_BlocksWhenPurchasedCapacityIsFull(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM seat_capacity_outbox WHERE workspace_id = $1`, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("clear capacity outbox: %v", err)
	}
	capacity := &stubSeatCapacity{reserveDecision: seatcapacity.Decision{
		Managed: true, Allowed: false, Reason: "capacity_full",
	}}
	useSeatCapacity(t, capacity)

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: "capacity-full-invite@multica.ai", Role: "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.CreateInvitation(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "seat_capacity_full" {
		t.Fatalf("code = %q, want seat_capacity_full", body.Code)
	}
	if capacity.reserveCalls != 1 {
		t.Fatalf("reserve calls = %d, want 1", capacity.reserveCalls)
	}
	var invitationCount, outboxCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_invitation WHERE workspace_id = $1 AND invitee_email = $2`, parseUUID(testWorkspaceID), "capacity-full-invite@multica.ai").Scan(&invitationCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM seat_capacity_outbox WHERE workspace_id = $1`, parseUUID(testWorkspaceID)).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if invitationCount != 0 || outboxCount != 0 {
		t.Fatalf("persisted invitation=%d outbox=%d, want both zero", invitationCount, outboxCount)
	}
}

func TestCreateInvitation_BlocksOvercommittedCapacityWithoutOfferingSingleSeatSemantics(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	capacity := &stubSeatCapacity{reserveErr: &seatcapacity.HTTPError{
		StatusCode: http.StatusConflict,
		Code:       "capacity_overcommitted",
	}}
	useSeatCapacity(t, capacity)

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: "capacity-overcommitted-invite@multica.ai", Role: "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.CreateInvitation(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "seat_capacity_overcommitted" {
		t.Fatalf("code = %q, want seat_capacity_overcommitted", body.Code)
	}
	if capacity.reserveCalls != 1 {
		t.Fatalf("reserve calls = %d, want 1", capacity.reserveCalls)
	}
}

func TestCreateInvitation_CompensatesCapacityWhenCommitRollsBack(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	if _, err := testPool.Exec(ctx, `DELETE FROM seat_capacity_outbox WHERE workspace_id = $1`, workspaceID); err != nil {
		t.Fatalf("clear capacity outbox: %v", err)
	}

	capacity := &stubSeatCapacity{reserveDecision: seatcapacity.Decision{Managed: true, Allowed: true}}
	useSeatCapacity(t, capacity)
	previousTxStarter := testHandler.TxStarter
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	t.Cleanup(func() { testHandler.TxStarter = previousTxStarter })

	const email = "capacity-commit-failure@multica.ai"
	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: email, Role: "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.CreateInvitation(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if capacity.reserveCalls != 1 || capacity.releaseCalls != 1 {
		t.Fatalf("reserve calls = %d, release calls = %d; want one each", capacity.reserveCalls, capacity.releaseCalls)
	}

	var invitationCount, outboxCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace_invitation WHERE workspace_id = $1 AND invitee_email = $2`, workspaceID, email).Scan(&invitationCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM seat_capacity_outbox WHERE workspace_id = $1`, workspaceID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if invitationCount != 0 || outboxCount != 0 {
		t.Fatalf("persisted invitation=%d outbox=%d, want both zero after compensation", invitationCount, outboxCount)
	}
}

// Regression for issue #2055: an expired pending invitation must NOT block a
// new invitation to the same email. The stale row should be flipped to
// 'expired' and a fresh pending row should be created.
func TestCreateInvitation_AllowsAfterExpiry(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	ctx := context.Background()

	var staleID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_invitation (
			workspace_id, inviter_id, invitee_email, role, status, created_at, updated_at, expires_at
		)
		VALUES ($1, $2, $3, 'member', 'pending', now() - interval '10 days', now() - interval '10 days', now() - interval '3 days')
		RETURNING id
	`, parseUUID(testWorkspaceID), parseUUID(testUserID), invitationTestEmail).Scan(&staleID); err != nil {
		t.Fatalf("seed expired invitation: %v", err)
	}

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-invite after expiry: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp InvitationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" || resp.ID == staleID {
		t.Fatalf("expected a new invitation row, got id=%q (stale=%q)", resp.ID, staleID)
	}

	var staleStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM workspace_invitation WHERE id = $1`, staleID,
	).Scan(&staleStatus); err != nil {
		t.Fatalf("read stale row: %v", err)
	}
	if staleStatus != "expired" {
		t.Fatalf("expected stale row to be 'expired', got %q", staleStatus)
	}

	var pendingCount int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspace_invitation
		WHERE workspace_id = $1 AND invitee_email = $2 AND status = 'pending'
	`, parseUUID(testWorkspaceID), invitationTestEmail).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected exactly 1 pending invitation after re-invite, got %d", pendingCount)
	}
}

func TestCreateInvitation_RateLimitChecksEveryGateAfterCapacityReservation(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM seat_capacity_outbox WHERE workspace_id = $1`, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("clear capacity outbox: %v", err)
	}
	capacity := &stubSeatCapacity{reserveDecision: seatcapacity.Decision{Managed: true, Allowed: true}}
	useSeatCapacity(t, capacity)
	actor := &stubInvitationRateLimiter{allowed: false, retryAfter: 10 * time.Minute}
	workspace := &stubInvitationRateLimiter{allowed: true}
	recipient := &stubInvitationRateLimiter{allowed: false, retryAfter: 24 * time.Hour}
	useInvitationRateLimiters(t, InvitationRateLimiters{Actor: actor, Workspace: workspace, Recipient: recipient})

	const rawEmail = "  Invitation-Limited@Multica.AI  "
	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: rawEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.CreateInvitation(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "86400" {
		t.Errorf("Retry-After = %q, want 86400", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Error             string `json:"error"`
		Code              string `json:"code"`
		RetryAfterSeconds int64  `json:"retry_after_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "too many invitation requests" || body.Code != "invitation_rate_limited" || body.RetryAfterSeconds != 86400 {
		t.Errorf("unexpected body: %+v", body)
	}
	if strings.Contains(rec.Body.String(), "actor") || strings.Contains(rec.Body.String(), "workspace") || strings.Contains(rec.Body.String(), "recipient") {
		t.Errorf("response leaked the limiting gate: %s", rec.Body.String())
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(rawEmail))
	checks := []struct {
		name string
		got  []string
		want string
	}{
		{name: "actor", got: actor.checkKeys, want: testUserID},
		{name: "workspace", got: workspace.checkKeys, want: testWorkspaceID},
		{name: "recipient", got: recipient.checkKeys, want: invitationRecipientKey(normalizedEmail)},
	}
	for _, check := range checks {
		if len(check.got) != 1 || check.got[0] != check.want {
			t.Errorf("%s limiter keys = %v, want [%s]", check.name, check.got, check.want)
		}
	}
	if len(recipient.checkKeys) == 1 && (recipient.checkKeys[0] == normalizedEmail || strings.Contains(recipient.checkKeys[0], "@")) {
		t.Errorf("recipient limiter key contains the email instead of a digest: %q", recipient.checkKeys[0])
	}
	for name, calls := range map[string]int{
		"actor": len(actor.allowKeys), "workspace": len(workspace.allowKeys), "recipient": len(recipient.allowKeys),
	} {
		if calls != 0 {
			t.Errorf("%s limiter consumed %d times, want 0 after a rejected check", name, calls)
		}
	}

	var pendingCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workspace_invitation
		WHERE workspace_id = $1 AND invitee_email = $2 AND status = 'pending'
	`, parseUUID(testWorkspaceID), normalizedEmail).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending invitations: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending invitation count = %d, want 0 after rate limit rejection", pendingCount)
	}
	if capacity.reserveCalls != 1 || capacity.releaseCalls != 1 {
		t.Fatalf("capacity calls reserve=%d release=%d, want 1/1", capacity.reserveCalls, capacity.releaseCalls)
	}
	if intents := dbfx.Count(t, `SELECT count(*) FROM seat_capacity_outbox WHERE workspace_id = $1`, parseUUID(testWorkspaceID)); intents != 0 {
		t.Fatalf("capacity intents = %d, want 0 after compensated rate-limit rejection", intents)
	}
}

func TestCreateInvitation_RateLimiterFailureReturnsServiceUnavailable(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	actor := &stubInvitationRateLimiter{allowed: false, retryAfter: 10 * time.Minute}
	workspace := &stubInvitationRateLimiter{allowed: true, err: errors.New("redis unavailable")}
	recipient := &stubInvitationRateLimiter{allowed: true}
	useInvitationRateLimiters(t, InvitationRateLimiters{Actor: actor, Workspace: workspace, Recipient: recipient})
	previousMetrics := testHandler.Metrics
	testHandler.Metrics = obsmetrics.NewBusinessMetrics()
	t.Cleanup(func() {
		testHandler.Metrics = previousMetrics
	})

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: "invitation-limiter-error@multica.ai",
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.CreateInvitation(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "5" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("unexpected retry/cache headers: %v", rec.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "invitation_rate_limiter_unavailable" {
		t.Errorf("code = %q, want invitation_rate_limiter_unavailable", body["code"])
	}
	metricFamily := obsmetrics.GatherForTest(t, testHandler.Metrics)["multica_email_rate_limited_total"]
	for _, metric := range metricFamily.GetMetric() {
		if metric.GetCounter().GetValue() != 0 {
			t.Errorf("rate-limit metric = %v, want 0 when the final response is 503", metric.GetCounter().GetValue())
		}
	}
	for name, calls := range map[string]int{
		"actor checks": len(actor.checkKeys), "workspace checks": len(workspace.checkKeys), "recipient checks": len(recipient.checkKeys),
	} {
		if calls != 1 {
			t.Errorf("%s = %d, want 1 even when another gate errors", name, calls)
		}
	}
	for name, calls := range map[string]int{
		"actor": len(actor.allowKeys), "workspace": len(workspace.allowKeys), "recipient": len(recipient.allowKeys),
	} {
		if calls != 0 {
			t.Errorf("%s limiter consumed %d times, want 0 after a backend error", name, calls)
		}
	}
	var pendingCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workspace_invitation
		WHERE workspace_id = $1 AND invitee_email = 'invitation-limiter-error@multica.ai' AND status = 'pending'
	`, parseUUID(testWorkspaceID)).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending invitations: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending invitation count = %d, want 0 after limiter backend failure", pendingCount)
	}
}

func TestAdmitInvitation_RejectedActorDoesNotConsumeWorkspaceBudget(t *testing.T) {
	limits := InvitationRateLimits{
		Actor:     SlidingWindowRateLimit{Limit: 1, Window: time.Hour},
		Workspace: SlidingWindowRateLimit{Limit: 2, Window: time.Hour},
		Recipient: SlidingWindowRateLimit{Limit: 10, Window: time.Hour},
	}
	h := *testHandler
	h.InvitationRateLimiters = NewMemoryInvitationRateLimiters(limits)

	admit := func(actorID, email string) bool {
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/workspace-a/members", nil)
		return h.admitInvitation(httptest.NewRecorder(), req, actorID, "workspace-a", email)
	}

	if !admit("actor-a", "first@multica.ai") {
		t.Fatal("first invitation was unexpectedly rejected")
	}
	for i := 0; i < 3; i++ {
		if admit("actor-a", fmt.Sprintf("rejected-%d@multica.ai", i)) {
			t.Fatalf("actor-limited invitation %d was unexpectedly admitted", i)
		}
	}
	if !admit("actor-b", "second@multica.ai") {
		t.Fatal("actor-limited retries consumed the shared workspace budget")
	}
}

func TestAdmitInvitation_RejectedActorDoesNotConsumeRecipientBudget(t *testing.T) {
	limits := InvitationRateLimits{
		Actor:     SlidingWindowRateLimit{Limit: 1, Window: time.Hour},
		Workspace: SlidingWindowRateLimit{Limit: 10, Window: time.Hour},
		Recipient: SlidingWindowRateLimit{Limit: 2, Window: time.Hour},
	}
	h := *testHandler
	h.InvitationRateLimiters = NewMemoryInvitationRateLimiters(limits)

	admit := func(actorID, workspaceID string) bool {
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/members", nil)
		return h.admitInvitation(httptest.NewRecorder(), req, actorID, workspaceID, "shared@multica.ai")
	}

	if !admit("actor-a", "workspace-a") {
		t.Fatal("first invitation was unexpectedly rejected")
	}
	if admit("actor-a", "workspace-a") {
		t.Fatal("actor-limited retry was unexpectedly admitted")
	}
	if !admit("actor-b", "workspace-b") {
		t.Fatal("actor-limited retry consumed the global recipient budget")
	}
}

func TestAdmitInvitation_AllowsBoundedOvershootWhenGateFillsAfterCheck(t *testing.T) {
	allowDenied := false
	actor := &stubInvitationRateLimiter{allowed: true, allowResult: &allowDenied}
	workspace := &stubInvitationRateLimiter{allowed: true}
	recipient := &stubInvitationRateLimiter{allowed: true}
	h := *testHandler
	h.InvitationRateLimiters = InvitationRateLimiters{Actor: actor, Workspace: workspace, Recipient: recipient}

	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/workspace-a/members", nil)
	if !h.admitInvitation(httptest.NewRecorder(), req, "actor-a", "workspace-a", "recipient@multica.ai") {
		t.Fatal("concurrent fill rejected after a different gate may already have consumed")
	}
	for name, calls := range map[string]int{
		"actor checks": len(actor.checkKeys), "workspace checks": len(workspace.checkKeys), "recipient checks": len(recipient.checkKeys),
		"actor allows": len(actor.allowKeys), "workspace allows": len(workspace.allowKeys), "recipient allows": len(recipient.allowKeys),
	} {
		if calls != 1 {
			t.Errorf("%s = %d, want 1", name, calls)
		}
	}
}

func TestAdmitInvitation_AllowsBoundedOvershootWhenBackendFailsAfterChecks(t *testing.T) {
	actor := &stubInvitationRateLimiter{allowed: true}
	workspace := &stubInvitationRateLimiter{allowed: true, allowErr: errors.New("redis unavailable")}
	recipient := &stubInvitationRateLimiter{allowed: true}
	h := *testHandler
	h.InvitationRateLimiters = InvitationRateLimiters{Actor: actor, Workspace: workspace, Recipient: recipient}

	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/workspace-a/members", nil)
	rec := httptest.NewRecorder()
	if !h.admitInvitation(rec, req, "actor-a", "workspace-a", "recipient@multica.ai") {
		t.Fatalf("backend failed after successful checks: request was rejected with %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Retry-After") != "" {
		t.Fatalf("backend failure after checks wrote an error response: headers=%v body=%s", rec.Header(), rec.Body.String())
	}
	for name, calls := range map[string]int{
		"actor checks": len(actor.checkKeys), "workspace checks": len(workspace.checkKeys), "recipient checks": len(recipient.checkKeys),
		"actor allows": len(actor.allowKeys), "workspace allows": len(workspace.allowKeys), "recipient allows": len(recipient.allowKeys),
	} {
		if calls != 1 {
			t.Errorf("%s = %d, want 1", name, calls)
		}
	}
}

func TestCreateInvitation_RouteRequiresAdminRole(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	ctx := context.Background()
	const memberEmail = "invitation-route-member@multica.ai"
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, memberEmail)
	var memberUserID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Invitation Route Member', $1) RETURNING id`, memberEmail).Scan(&memberUserID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})

	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Post("/members", testHandler.CreateInvitation)
		})
	})

	call := func(userID, inviteeEmail string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		if err := json.NewEncoder(&body).Encode(CreateMemberRequest{Email: inviteeEmail, Role: "member"}); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", &body)
		req.Header.Set("X-User-ID", userID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := call(memberUserID, "invitation-route-rejected@multica.ai"); rec.Code != http.StatusForbidden {
		t.Errorf("member status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := call(testUserID, fmt.Sprintf("invitation-route-owner-%s@multica.ai", testWorkspaceID)); rec.Code != http.StatusCreated {
		t.Errorf("owner status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}
