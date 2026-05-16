package agentpass

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// fakeQueries is a hand-rolled stub. Tests reach into its fields to set
// up the row that GetActiveAgentPassForAgentIssue will return and to
// observe what MarkAgentPassStatus was called with.
type fakeQueries struct {
	pass        cerebrodb.CerebroAgentPass
	passErr     error
	markCalled  bool
	markStatus  string
	markErr     error
	markPassID  pgtype.UUID
	getCalled   bool
	getArg      cerebrodb.GetActiveAgentPassForAgentIssueParams
	// getByID is used by ValidateChildScope tests.
	getByID    cerebrodb.CerebroAgentPass
	getByIDErr error
	// usageRows and usageErr are used by spend-ceiling tests.
	usageRows []cerebrodb.SumIssueTreeUsageByModelRow
	usageErr  error
}

func (f *fakeQueries) GetActiveAgentPassForAgentIssue(_ context.Context, arg cerebrodb.GetActiveAgentPassForAgentIssueParams) (cerebrodb.CerebroAgentPass, error) {
	f.getCalled = true
	f.getArg = arg
	if f.passErr != nil {
		return cerebrodb.CerebroAgentPass{}, f.passErr
	}
	// Simulate the pass no longer being active after it was marked exhausted.
	if f.markCalled && f.markStatus == StatusExhausted {
		return cerebrodb.CerebroAgentPass{}, pgx.ErrNoRows
	}
	return f.pass, nil
}

func (f *fakeQueries) GetExhaustedAgentPassForAgentIssue(_ context.Context, _ cerebrodb.GetActiveAgentPassForAgentIssueParams) (cerebrodb.CerebroAgentPass, error) {
	// Return the exhausted pass when it has been marked exhausted.
	if f.markCalled && f.markStatus == StatusExhausted {
		p := f.pass
		p.Status = StatusExhausted
		return p, nil
	}
	return cerebrodb.CerebroAgentPass{}, pgx.ErrNoRows
}

func (f *fakeQueries) GetCerebroAgentPass(_ context.Context, _ pgtype.UUID) (cerebrodb.CerebroAgentPass, error) {
	if f.getByIDErr != nil {
		return cerebrodb.CerebroAgentPass{}, f.getByIDErr
	}
	return f.getByID, nil
}

func (f *fakeQueries) MarkAgentPassStatus(_ context.Context, arg cerebrodb.MarkAgentPassStatusParams) (cerebrodb.CerebroAgentPass, error) {
	f.markCalled = true
	f.markStatus = arg.Status
	f.markPassID = arg.ID
	if f.markErr != nil {
		return cerebrodb.CerebroAgentPass{}, f.markErr
	}
	updated := f.pass
	updated.Status = arg.Status
	return updated, nil
}

func (f *fakeQueries) SumIssueTreeUsageByModel(_ context.Context, _ pgtype.UUID) ([]cerebrodb.SumIssueTreeUsageByModelRow, error) {
	if f.usageErr != nil {
		return nil, f.usageErr
	}
	return f.usageRows, nil
}

func uuidValue(b byte) pgtype.UUID {
	var v pgtype.UUID
	for i := range v.Bytes {
		v.Bytes[i] = b
	}
	v.Valid = true
	return v
}

func tsValue(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func mustService(t *testing.T, q Queries, now Now) *Service {
	t.Helper()
	s, err := New(q, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNew_NilQueriesIsRejected(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, nil); !errors.Is(err, ErrNilQueries) {
		t.Fatalf("expected ErrNilQueries, got %v", err)
	}
}

func TestEvaluateForEnqueue_NoPass_Allows(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{passErr: pgx.ErrNoRows}
	s := mustService(t, q, nil)

	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("EvaluateForEnqueue err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("want allow, got %s", res.Decision)
	}
	if res.Pass != nil {
		t.Fatalf("no pass should be returned, got %+v", res.Pass)
	}
	if q.markCalled {
		t.Fatalf("MarkAgentPassStatus must not be called on default-allow path")
	}
}

func TestEvaluateForEnqueue_InvalidIDs_Allow(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{}
	s := mustService(t, q, nil)

	res, err := s.EvaluateForEnqueue(context.Background(), pgtype.UUID{}, uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("invalid agent id must allow (chat path), got %s", res.Decision)
	}
	if q.getCalled {
		t.Fatalf("must not query when ids invalid")
	}

	res, err = s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), pgtype.UUID{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("invalid issue id must allow (chat path), got %s", res.Decision)
	}
}

func TestEvaluateForEnqueue_ActivePassNotExpired_Allows(t *testing.T) {
	t.Parallel()
	clockNow := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:        uuidValue(0xAB),
			AgentID:   uuidValue(0x01),
			IssueID:   uuidValue(0x02),
			Status:    StatusActive,
			ExpiresAt: tsValue(clockNow.Add(1 * time.Hour)),
		},
	}
	s := mustService(t, q, func() time.Time { return clockNow })

	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("active+not-expired must allow, got %s reason=%q", res.Decision, res.DenyReason())
	}
	if res.Pass == nil || res.Pass.ID != q.pass.ID {
		t.Fatalf("must return the matched pass")
	}
	if q.markCalled {
		t.Fatalf("must not mark status on allow path")
	}
}

func TestEvaluateForEnqueue_ActivePassWithoutExpiresAt_Allows(t *testing.T) {
	t.Parallel()
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:      uuidValue(0xAB),
			AgentID: uuidValue(0x01),
			IssueID: uuidValue(0x02),
			Status:  StatusActive,
			// ExpiresAt left as zero pgtype.Timestamptz (Valid=false) — open-ended.
		},
	}
	s := mustService(t, q, nil)

	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("open-ended pass must allow, got %s", res.Decision)
	}
}

func TestEvaluateForEnqueue_ExpiredPass_DeniesAndMarks(t *testing.T) {
	t.Parallel()
	clockNow := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	passID := uuidValue(0xAB)
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:        passID,
			AgentID:   uuidValue(0x01),
			IssueID:   uuidValue(0x02),
			Status:    StatusActive,
			ExpiresAt: tsValue(clockNow.Add(-1 * time.Minute)),
		},
	}
	s := mustService(t, q, func() time.Time { return clockNow })

	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionDenyExpired {
		t.Fatalf("expired pass must deny, got %s", res.Decision)
	}
	if got := res.DenyReason(); got != "agent_pass_expired" {
		t.Fatalf("deny reason = %q, want agent_pass_expired", got)
	}
	if !q.markCalled || q.markStatus != StatusExpired {
		t.Fatalf("must mark pass expired, called=%v status=%q", q.markCalled, q.markStatus)
	}
	if q.markPassID != passID {
		t.Fatalf("mark must target the matched pass id")
	}
	if res.Pass == nil || res.Pass.Status != StatusExpired {
		t.Fatalf("returned pass must reflect expired status")
	}
}

func TestEvaluateForEnqueue_ExpiredPass_MarkRaceTreatedAsDeny(t *testing.T) {
	t.Parallel()
	// Simulates a concurrent revoke moving the row out of 'active' between
	// our lookup and our mark — MarkAgentPassStatus returns ErrNoRows.
	// The gate must still deny, because we already observed expires_at < now.
	clockNow := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:        uuidValue(0xAB),
			AgentID:   uuidValue(0x01),
			IssueID:   uuidValue(0x02),
			Status:    StatusActive,
			ExpiresAt: tsValue(clockNow.Add(-1 * time.Second)),
		},
		markErr: pgx.ErrNoRows,
	}
	s := mustService(t, q, func() time.Time { return clockNow })

	res, _ := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if res.Decision != DecisionDenyExpired {
		t.Fatalf("ErrNoRows on mark must still produce deny, got %s", res.Decision)
	}
}

func TestEvaluateForEnqueue_ExactlyExpiresAt_DeniesAsExpired(t *testing.T) {
	t.Parallel()
	// expires_at == now: !ExpiresAt.After(now) is true, so this is expired
	// (consistent with the "exclusive end" semantics in the docstring).
	clockNow := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:        uuidValue(0xAB),
			AgentID:   uuidValue(0x01),
			IssueID:   uuidValue(0x02),
			Status:    StatusActive,
			ExpiresAt: tsValue(clockNow),
		},
	}
	s := mustService(t, q, func() time.Time { return clockNow })

	res, _ := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if res.Decision != DecisionDenyExpired {
		t.Fatalf("expires_at == now must deny as expired, got %s", res.Decision)
	}
}

func TestEvaluateForEnqueue_LookupErrorTreatedAsAllow(t *testing.T) {
	t.Parallel()
	// Real DB error (not ErrNoRows) is logged at the call site and treated
	// as allow, so a transient hiccup doesn't silently block all enqueues.
	q := &fakeQueries{passErr: errors.New("connection refused")}
	s := mustService(t, q, nil)

	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err == nil {
		t.Fatalf("expected error to be surfaced to caller")
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("lookup error must allow (default-allow on DB hiccup), got %s", res.Decision)
	}
}

func TestDenyReason_Coverage(t *testing.T) {
	t.Parallel()
	if (Result{Decision: DecisionAllow}).DenyReason() != "" {
		t.Fatalf("allow must have empty reason")
	}
	if (Result{Decision: DecisionDenyExpired}).DenyReason() != "agent_pass_expired" {
		t.Fatalf("expired token mismatch")
	}
	if (Result{Decision: DecisionDenyExhausted}).DenyReason() != "agent_pass_spend_exhausted" {
		t.Fatalf("exhausted token mismatch")
	}
}

// --- Spend-ceiling tests (JEH-1327 milestone 3) ---

// int8Value wraps an int64 as a valid pgtype.Int8 (spend ceiling field).
func int8Value(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: true} }

func TestEvaluateForEnqueue_NoCeiling_NoSpendInfo(t *testing.T) {
	t.Parallel()
	// A pass without SpendCeilingMicros set should produce no SpendInfo regardless
	// of what's in the usage table.
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:      uuidValue(0xAB),
			AgentID: uuidValue(0x01),
			IssueID: uuidValue(0x02),
			Status:  StatusActive,
			// SpendCeilingMicros left as zero pgtype.Int8 (Valid=false).
		},
		usageRows: []cerebrodb.SumIssueTreeUsageByModelRow{
			{Model: "claude-sonnet-4-6", InputTokens: 1_000_000},
		},
	}
	s := mustService(t, q, nil)
	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("want allow, got %s", res.Decision)
	}
	if res.SpendInfo != nil {
		t.Fatalf("no ceiling → SpendInfo must be nil, got %+v", res.SpendInfo)
	}
}

func TestEvaluateForEnqueue_SpendBelowWarn_Allows(t *testing.T) {
	t.Parallel()
	// Ceiling 1 USD = 1_000_000 micros. 0 tokens → 0 spend → below 70 %.
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:                 uuidValue(0xAB),
			AgentID:            uuidValue(0x01),
			IssueID:            uuidValue(0x02),
			Status:             StatusActive,
			SpendCeilingMicros: int8Value(1_000_000),
		},
	}
	s := mustService(t, q, nil)
	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("zero spend must allow, got %s", res.Decision)
	}
	if res.SpendInfo == nil {
		t.Fatalf("ceiling set → SpendInfo must be non-nil")
	}
	if res.SpendInfo.Level != SpendLevelOK {
		t.Fatalf("want SpendLevelOK, got %s", res.SpendInfo.Level)
	}
}

func TestEvaluateForEnqueue_SpendAtWarn_AllowsWithWarnLevel(t *testing.T) {
	t.Parallel()
	// claude-sonnet-4-6: input $3/M tokens. 1M tokens = 300 cents = 3_000_000 micros.
	// Ceiling: 4_000_000 micros ($4). 3_000_000/4_000_000 = 75 % → warn.
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:                 uuidValue(0xAB),
			AgentID:            uuidValue(0x01),
			IssueID:            uuidValue(0x02),
			Status:             StatusActive,
			SpendCeilingMicros: int8Value(4_000_000),
		},
		usageRows: []cerebrodb.SumIssueTreeUsageByModelRow{
			{Model: "claude-sonnet-4-6", InputTokens: 1_000_000},
		},
	}
	s := mustService(t, q, nil)
	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("warn threshold must still allow, got %s", res.Decision)
	}
	if res.SpendInfo == nil || res.SpendInfo.Level != SpendLevelWarn {
		t.Fatalf("want SpendLevelWarn, got %v", res.SpendInfo)
	}
	if q.markCalled {
		t.Fatalf("must not mark pass at warn level")
	}
}

func TestEvaluateForEnqueue_SpendAtDegrade_AllowsWithDegradeLevel(t *testing.T) {
	t.Parallel()
	// 1M input tokens @ sonnet = 300 cents = 3_000_000 micros.
	// Ceiling: 3_200_000 micros. 3_000_000/3_200_000 = 93.75 % → degrade.
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:                 uuidValue(0xAB),
			AgentID:            uuidValue(0x01),
			IssueID:            uuidValue(0x02),
			Status:             StatusActive,
			SpendCeilingMicros: int8Value(3_200_000),
		},
		usageRows: []cerebrodb.SumIssueTreeUsageByModelRow{
			{Model: "claude-sonnet-4-6", InputTokens: 1_000_000},
		},
	}
	s := mustService(t, q, nil)
	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("degrade threshold must still allow, got %s", res.Decision)
	}
	if res.SpendInfo == nil || res.SpendInfo.Level != SpendLevelDegrade {
		t.Fatalf("want SpendLevelDegrade, got %v", res.SpendInfo)
	}
	if q.markCalled {
		t.Fatalf("must not mark pass at degrade level")
	}
}

func TestEvaluateForEnqueue_SpendExhausted_DeniesAndMarks(t *testing.T) {
	t.Parallel()
	// 1M input tokens @ sonnet = 300 cents = 3_000_000 micros.
	// Ceiling: 2_000_000 micros ($2). 3_000_000 >= 2_000_000 → exhausted.
	passID := uuidValue(0xAB)
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:                 passID,
			AgentID:            uuidValue(0x01),
			IssueID:            uuidValue(0x02),
			Status:             StatusActive,
			SpendCeilingMicros: int8Value(2_000_000),
		},
		usageRows: []cerebrodb.SumIssueTreeUsageByModelRow{
			{Model: "claude-sonnet-4-6", InputTokens: 1_000_000},
		},
	}
	s := mustService(t, q, nil)
	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Decision != DecisionDenyExhausted {
		t.Fatalf("exhausted must deny, got %s", res.Decision)
	}
	if got := res.DenyReason(); got != "agent_pass_spend_exhausted" {
		t.Fatalf("deny reason = %q, want agent_pass_spend_exhausted", got)
	}
	if !q.markCalled || q.markStatus != StatusExhausted {
		t.Fatalf("must mark pass exhausted, called=%v status=%q", q.markCalled, q.markStatus)
	}
	if q.markPassID != passID {
		t.Fatalf("mark must target the matched pass id")
	}
	if res.SpendInfo == nil || res.SpendInfo.Level != SpendLevelExhausted {
		t.Fatalf("want SpendLevelExhausted in SpendInfo, got %v", res.SpendInfo)
	}
}

func TestEvaluateForEnqueue_SpendQueryError_Allows(t *testing.T) {
	t.Parallel()
	// A transient DB error on the spend query must not block the task.
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:                 uuidValue(0xAB),
			AgentID:            uuidValue(0x01),
			IssueID:            uuidValue(0x02),
			Status:             StatusActive,
			SpendCeilingMicros: int8Value(1_000_000),
		},
		usageErr: fmt.Errorf("connection refused"),
	}
	s := mustService(t, q, nil)
	res, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err == nil {
		t.Fatalf("expected error to be surfaced")
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("spend query error must allow (fail-open), got %s", res.Decision)
	}
}

func TestEvaluateForEnqueue_ExhaustedPass_SecondEnqueueAlsoBlocked(t *testing.T) {
	t.Parallel()
	// First enqueue crosses 100 % → DenyExhausted and pass is marked exhausted.
	// Second enqueue must also be denied even though GetActiveAgentPassForAgentIssue
	// now returns ErrNoRows (the pass is no longer 'active').
	//
	// 1M input tokens @ sonnet = 300 cents = 3_000_000 micros.
	// Ceiling: 2_000_000 micros → 150 % → exhausted.
	passID := uuidValue(0xAB)
	q := &fakeQueries{
		pass: cerebrodb.CerebroAgentPass{
			ID:                 passID,
			AgentID:            uuidValue(0x01),
			IssueID:            uuidValue(0x02),
			Status:             StatusActive,
			SpendCeilingMicros: int8Value(2_000_000),
		},
		usageRows: []cerebrodb.SumIssueTreeUsageByModelRow{
			{Model: "claude-sonnet-4-6", InputTokens: 1_000_000},
		},
	}
	s := mustService(t, q, nil)

	// First enqueue: crosses 100 %, pass marked exhausted.
	res1, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("first enqueue err=%v", err)
	}
	if res1.Decision != DecisionDenyExhausted {
		t.Fatalf("first enqueue: want DenyExhausted, got %s", res1.Decision)
	}
	if !q.markCalled || q.markStatus != StatusExhausted {
		t.Fatalf("first enqueue: pass must be marked exhausted")
	}

	// Second enqueue: active pass gone, fallback lookup must still deny.
	res2, err := s.EvaluateForEnqueue(context.Background(), uuidValue(0x01), uuidValue(0x02))
	if err != nil {
		t.Fatalf("second enqueue err=%v", err)
	}
	if res2.Decision != DecisionDenyExhausted {
		t.Fatalf("second enqueue: want DenyExhausted, got %s (exhausted pass must persist block)", res2.Decision)
	}
	if got := res2.DenyReason(); got != "agent_pass_spend_exhausted" {
		t.Fatalf("second enqueue deny reason = %q, want agent_pass_spend_exhausted", got)
	}
}

func TestClassifySpendLevel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spent, ceiling int64
		want           SpendLevel
	}{
		{0, 1_000_000, SpendLevelOK},
		{699_999, 1_000_000, SpendLevelOK},   // 69.9 %
		{700_000, 1_000_000, SpendLevelWarn},  // 70 %
		{899_999, 1_000_000, SpendLevelWarn},  // 89.9 %
		{900_000, 1_000_000, SpendLevelDegrade}, // 90 %
		{999_999, 1_000_000, SpendLevelDegrade}, // 99.9 %
		{1_000_000, 1_000_000, SpendLevelExhausted}, // 100 %
		{1_500_000, 1_000_000, SpendLevelExhausted}, // 150 %
	}
	for _, c := range cases {
		got := classifySpendLevel(c.spent, c.ceiling)
		if got != c.want {
			t.Errorf("classifySpendLevel(%d, %d) = %s, want %s", c.spent, c.ceiling, got, c.want)
		}
	}
}
