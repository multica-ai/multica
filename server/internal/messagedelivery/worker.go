package messagedelivery

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// workerConcurrency bounds the per-process send pool. SKIP LOCKED
	// spreads the same queue across every replica's pool.
	workerConcurrency = 2
	// pollInterval is the idle sweep cadence; Notify coalesces faster.
	pollInterval = time.Second
	// defaultScanEvery is the compensator cadence.
	defaultScanEvery = 30 * time.Second
	// Event bursts retain a trailing pass but cannot start decisions more
	// often than once per second, including immediately after compensation.
	decideMinInterval = time.Second
	// This is overlap, not an assumed transaction timeout. The stable
	// horizon also holds behind every open transaction (see SCANNING.md).
	sourceScanOverlap = time.Minute
	// scanBatchSize bounds each compensator pass.
	scanBatchSize = 200
)

// Run starts the send workers, the compensator and the wakeup listener, and
// blocks until ctx is cancelled. WaitWithTimeout joins it during shutdown.
func (s *Service) Run(ctx context.Context) {
	if s == nil || s.Queries == nil {
		return
	}
	defer close(s.done)

	var wg sync.WaitGroup
	wg.Add(workerConcurrency)
	for range workerConcurrency {
		go func() {
			defer wg.Done()
			s.sendLoop(ctx)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.scanLoop(ctx)
	}()

	wg.Wait()
}

// WaitWithTimeout reports whether the workers exited within timeout.
func (s *Service) WaitWithTimeout(timeout time.Duration) bool {
	if s == nil {
		return true
	}
	done := s.done
	if done == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// Notify hints the send pool that new work exists. Non-blocking and
// lossy-by-design: the poll loop and the compensator are the safety net.
// Enqueue calls this after persisting new decisions.
func (s *Service) Notify() {
	if s == nil {
		return
	}
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// NotifyDecide hints the scan loop that new persisted sources may exist.
// The server assembly subscribes it to autopilot:run_done / inbox:new /
// activity:created / comment:created — wakeups only; the compensation scanner re-derives the
// missing set from persisted rows, so a lost event is always recovered.
func (s *Service) NotifyDecide() {
	if s == nil || s.decideNotify == nil {
		return
	}
	select {
	case s.decideNotify <- struct{}{}:
	default:
	}
}

func (s *Service) sendLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		worked, err := s.ProcessNext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			s.logger().Error("messagedelivery: process delivery", "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
		case <-ticker.C:
		}
	}
}

// ProcessNext claims and processes ONE due delivery. Exported for tests.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	d, err := s.Queries.ClaimDueLabrastroMessageDelivery(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.processClaimed(ctx, d)
	return true, nil
}

// processClaimed runs the pre-send gates, performs the send pass and writes
// the terminal transition. Every gate failure is a recorded decision with a
// stable reason — never a silent drop and never a source the compensator
// will see again.
func (s *Service) processClaimed(ctx context.Context, d db.LabrastroMessageDelivery) {
	v := s.sendDelivery(ctx, d)
	out := &v
	if out.lost {
		// Lease ownership was lost mid-pass (review R9): the row's new
		// owner owns the outcome; this worker writes nothing.
		s.logger().Warn("messagedelivery: abandoned send pass, lease lost mid-flight",
			"delivery_id", util.UUIDToString(d.ID),
			"detail", out.detail,
		)
		return
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outcomeWriteTimeout)
	defer cancel()
	ctx = writeCtx
	switch out.status {
	case DeliveryStatusSent:
		s.completeClaimed(ctx, d, *out)
	case DeliveryStatusCancelled:
		s.cancelClaimed(ctx, d, *out)
	case DeliveryStatusUncertain:
		s.uncertainClaimed(ctx, d, *out)
	case DeliveryStatusFailed:
		if out.errorCode == ErrorCodeSendTransient && d.Attempts+1 < int32(s.maxAttempts()) {
			s.retryClaimed(ctx, d, *out)
			return
		}
		if out.errorCode == ErrorCodeSendTransient {
			out.errorCode = ErrorCodeAttemptsExhausted
			out.detail = "exhausted " + strconv.Itoa(int(d.Attempts+1)) + " attempts; last: " + out.detail
		}
		s.failClaimed(ctx, d, *out)
	default:
		s.failClaimed(ctx, d, sendOutcome{
			status:    DeliveryStatusFailed,
			errorCode: ErrorCodeSendRejected,
			detail:    "unclassified send outcome " + out.status,
		})
	}
}

func (s *Service) completeClaimed(ctx context.Context, d db.LabrastroMessageDelivery, out sendOutcome) {
	if _, err := s.Queries.CompleteClaimedLabrastroMessageDelivery(ctx, db.CompleteClaimedLabrastroMessageDeliveryParams{
		ID:         d.ID,
		LeaseToken: d.LeaseToken,
	}); err != nil {
		s.logLeaseLoss("complete", d, err)
		return
	}
	s.logger().Info("messagedelivery: delivery sent",
		"delivery_id", util.UUIDToString(d.ID),
		"run_id", util.UUIDToString(d.RunID),
	)
}

func (s *Service) failClaimed(ctx context.Context, d db.LabrastroMessageDelivery, out sendOutcome) {
	if _, err := s.Queries.FailClaimedLabrastroMessageDelivery(ctx, db.FailClaimedLabrastroMessageDeliveryParams{
		ID:         d.ID,
		LeaseToken: d.LeaseToken,
		ErrorCode:  pgtype.Text{String: out.errorCode, Valid: out.errorCode != ""},
		LastError:  pgtype.Text{String: out.detail, Valid: out.detail != ""},
	}); err != nil {
		s.logLeaseLoss("fail", d, err)
	}
}

func (s *Service) cancelClaimed(ctx context.Context, d db.LabrastroMessageDelivery, out sendOutcome) {
	if _, err := s.Queries.CancelClaimedLabrastroMessageDelivery(ctx, db.CancelClaimedLabrastroMessageDeliveryParams{
		ID:         d.ID,
		LeaseToken: d.LeaseToken,
		ErrorCode:  pgtype.Text{String: out.errorCode, Valid: out.errorCode != ""},
		LastError:  pgtype.Text{String: out.detail, Valid: out.detail != ""},
	}); err != nil {
		s.logLeaseLoss("cancel", d, err)
	}
}

func (s *Service) uncertainClaimed(ctx context.Context, d db.LabrastroMessageDelivery, out sendOutcome) {
	if _, err := s.Queries.UncertainClaimedLabrastroMessageDelivery(ctx, db.UncertainClaimedLabrastroMessageDeliveryParams{
		ID:         d.ID,
		LeaseToken: d.LeaseToken,
		ErrorCode:  pgtype.Text{String: out.errorCode, Valid: out.errorCode != ""},
		LastError:  pgtype.Text{String: out.detail, Valid: out.detail != ""},
	}); err != nil {
		s.logLeaseLoss("uncertain", d, err)
	}
	s.logger().Warn("messagedelivery: delivery outcome uncertain",
		"delivery_id", util.UUIDToString(d.ID),
		"reason_code", out.errorCode,
	)
}

func (s *Service) retryClaimed(ctx context.Context, d db.LabrastroMessageDelivery, out sendOutcome) {
	backoff := backoffFor(int(d.Attempts))
	if _, err := s.Queries.RetryClaimedLabrastroMessageDelivery(ctx, db.RetryClaimedLabrastroMessageDeliveryParams{
		ID:            d.ID,
		LeaseToken:    d.LeaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: s.now().Add(backoff), Valid: true},
		ErrorCode:     pgtype.Text{String: out.errorCode, Valid: out.errorCode != ""},
		LastError:     pgtype.Text{String: out.detail, Valid: out.detail != ""},
	}); err != nil {
		s.logLeaseLoss("retry", d, err)
		return
	}
	s.logger().Warn("messagedelivery: delivery deferred",
		"delivery_id", util.UUIDToString(d.ID),
		"attempt", d.Attempts+1,
		"backoff", backoff.String(),
		"reason_code", out.errorCode,
	)
}

// backoffFor is the automatic retry schedule for transient failures.
func backoffFor(attempt int) time.Duration {
	backoff := time.Second << min(attempt, 5)
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}
	return backoff
}

func (s *Service) logLeaseLoss(operation string, d db.LabrastroMessageDelivery, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		// Lease ownership changed hands; the new owner owns the outcome.
		s.logger().Debug("messagedelivery: lease ownership changed",
			"operation", operation,
			"delivery_id", util.UUIDToString(d.ID),
		)
		return
	}
	s.logger().Error("messagedelivery: lease-guarded write failed",
		"operation", operation,
		"delivery_id", util.UUIDToString(d.ID),
		"error", err,
	)
}

// ---- compensation scan ----

func (s *Service) scanEvery() time.Duration {
	if s.ScanEvery > 0 {
		return s.ScanEvery
	}
	return defaultScanEvery
}

// scanLoop is the compensator: recover expired claims, re-sync stale run
// terminals through the EXISTING sync logic, and decide any persisted
// source (automation run or OL-27 inbox/activity/comment record) that still
// lacks a decision for an enabled route target. A source-event wakeup
// schedules a rate-limited decide pass — a latency hint only.
func (s *Service) scanLoop(ctx context.Context) {
	runScanSchedule(ctx, s.scanEvery(), decideMinInterval, s.decideNotify, func() {
		if err := s.ScanOnce(ctx); err != nil {
			s.logger().Error("messagedelivery: compensation scan", "error", err)
		}
	}, func() { s.decideSourcesOnce(ctx) })
}

// runScanSchedule keeps one trailing event pass. Repeated events never reset
// its deadline, so continuous traffic cannot starve either kind of scan.
func runScanSchedule(ctx context.Context, every, minimum time.Duration, notify <-chan struct{}, scan, decide func()) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var ready <-chan time.Time
	var next time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			if ready == nil {
				timer.Reset(max(0, time.Until(next)))
				ready = timer.C
			}
		case <-ready:
			ready = nil
			decide()
			next = time.Now().Add(minimum)
		case <-ticker.C:
			timer.Stop()
			ready = nil
			scan()
			next = time.Now().Add(minimum)
		}
	}
}

// ScanOnce runs one bounded compensator pass. Exported for tests. Each
// scanner's failure is contained: one class failing must not permanently
// block the others (repair contract §4).
func (s *Service) ScanOnce(ctx context.Context) error {
	var failures []error
	if err := s.requeueExpiredClaims(ctx); err != nil {
		failures = append(failures, err)
	}
	if s.Syncer != nil {
		for scanner, run := range map[string]func(context.Context, scanCursor) (scanCursor, error){
			scannerRunOnlyTask:   s.syncStaleRunOnlyTasks,
			scannerIssueStatus:   s.syncStaleCreateIssueIssues,
			scannerLinkedFailure: s.syncStaleLinkedTaskFailures,
		} {
			if _, err := s.advanceScanner(ctx, scanner, run); err != nil && !errors.Is(err, errScanCursorMoved) {
				failures = append(failures, err)
			}
		}
	}
	if err := s.decideMissing(ctx); err != nil {
		failures = append(failures, err)
	}
	// OL-27: the three persisted personal/team sources advance on their
	// own cursors, independent of the run machinery and of each other —
	// one source class's backlog never holds another's position hostage.
	failures = append(failures, s.decideSourcesErr(ctx)...)
	return errors.Join(failures...)
}

// sourceScannerFuncs returns the scanner name → page function pairs.
func (s *Service) sourceScannerFuncs() map[string]func(context.Context, scanCursor) (scanCursor, error) {
	return map[string]func(context.Context, scanCursor) (scanCursor, error){
		scannerInboxSource:    s.scanInboxSourceDecisions,
		scannerActivitySource: s.scanActivitySourceDecisions,
		scannerCommentSource:  s.scanCommentSourceDecisions,
	}
}

// decideSourcesErr advances every source scanner, containing each failure.
func (s *Service) decideSourcesErr(ctx context.Context) []error {
	var failures []error
	for scanner, page := range s.sourceScannerFuncs() {
		if _, err := s.advanceScanner(ctx, scanner, page); err != nil && !errors.Is(err, errScanCursorMoved) {
			failures = append(failures, err)
		}
	}
	return failures
}

// decideSourcesOnce runs one bounded decide pass over the persisted
// sources — the body a wakeup or a periodic tick executes.
func (s *Service) decideSourcesOnce(ctx context.Context) {
	if err := s.decideMissing(ctx); err != nil {
		s.logger().Error("messagedelivery: run decide scan", "error", err)
	}
	for _, err := range s.decideSourcesErr(ctx) {
		s.logger().Error("messagedelivery: source decide scan", "error", err)
	}
}

// ---- persistent per-scanner cursors (repair contract §4, review R4) ----

// Scanner names. ONE cursor row per scanner: the source classes advance
// independently, so one class's backlog can never hold another's position
// hostage. The first three are the OL-25 run-side scanners; the last three
// are the OL-27 persisted-source decide scans.
const (
	scannerRunOnlyTask    = "run_only_terminal_task"
	scannerIssueStatus    = "create_issue_issue_status"
	scannerLinkedFailure  = "create_issue_linked_task_failure"
	scannerInboxSource    = "inbox_source_delivery"
	scannerActivitySource = "activity_source_delivery"
	scannerCommentSource  = "comment_source_delivery"
)

const (
	// scanPageLimit bounds one page.
	scanPageLimit = 200
	// scanRowBudgetPerTick is the per-tick work budget in ROWS; when it
	// runs out the position is saved and the NEXT tick continues from it.
	scanRowBudgetPerTick = 20_000
)

// requeueExpiredClaims moves crashed claims to uncertain. The send may
// have left the process, so "uncertain" is the only honest state; manual
// verify-and-retry with the fixed send UUID resolves it.
func (s *Service) requeueExpiredClaims(ctx context.Context) error {
	expired, err := s.Queries.RequeueExpiredLabrastroMessageDeliveryClaims(ctx, db.RequeueExpiredLabrastroMessageDeliveryClaimsParams{
		ErrorCode: pgtype.Text{String: ErrorCodeLeaseExpired, Valid: true},
		LastError: pgtype.Text{String: "send claim expired mid-flight; outcome unknown", Valid: true},
	})
	if err != nil {
		return err
	}
	for _, d := range expired {
		s.logger().Warn("messagedelivery: expired claim parked as uncertain",
			"delivery_id", util.UUIDToString(d.ID),
			"reason_code", ErrorCodeLeaseExpired,
		)
	}
	return nil
}

// scanCursor carries one bounded cycle. The generation guards every completed
// page; a stale replica must reload instead of advancing or resetting it.
type scanCursor struct {
	ts           time.Time
	id           pgtype.UUID
	upperID      pgtype.UUID
	generation   int64
	cycleStarted time.Time
	cycleStable  pgtype.Timestamptz
	// nonempty distinguishes another target page at the same source ID from exhaustion; never persisted.
	nonempty bool
}

var scanCursorStart = scanCursor{id: pgtype.UUID{Valid: true}}
var errScanCursorMoved = errors.New("scan cursor advanced by another replica")

func cursorFromRow(row db.LabrastroMessageScanCursor) scanCursor {
	return scanCursor{ts: row.CursorTs.Time, id: row.CursorID, upperID: row.CycleUpperID,
		generation: row.Generation, cycleStarted: row.CycleStartedAt.Time, cycleStable: row.CycleStableAt}
}

// Each page is persisted only AFTER its sync calls finish. Crashes replay at
// most the unfinished page; shutdown never resets a partially scanned cycle.
func (s *Service) advanceScanner(ctx context.Context, scanner string, page func(context.Context, scanCursor) (scanCursor, error)) (scanCursor, error) {
	cur, err := s.loadCursor(ctx, scanner)
	if err != nil {
		return cur, err
	}
	for budget := scanRowBudgetPerTick; budget > 0; budget -= scanPageLimit {
		last, err := page(ctx, cur)
		if err != nil {
			return cur, err
		}
		if last.id == cur.id && !last.nonempty {
			// Exhausted this fixed bound. The next tick freezes a new bound.
			if cur.cycleStable.Valid {
				cur.ts = cur.cycleStable.Time
				cur.cycleStable = pgtype.Timestamptz{}
			}
			cur.id, cur.upperID = scanCursorStart.id, pgtype.UUID{}
			return s.saveCursor(ctx, scanner, cur)
		}
		cur, err = s.saveCursor(ctx, scanner, last)
		if err != nil {
			return cur, err
		}
	}
	return cur, nil
}

func (s *Service) loadCursor(ctx context.Context, scanner string) (scanCursor, error) {
	return s.loadCursorWithBound(ctx, scanner, func() (pgtype.UUID, error) {
		if scope := sourceScopeForScanner(scanner); scope != "" {
			return s.Queries.GetLabrastroMessageSourceScanUpperBound(ctx, scope)
		}
		return s.Queries.GetLabrastroMessageScanUpperBound(ctx, scanner == scannerIssueStatus)
	})
}

// loadCursorWithBound is loadCursor with an injectable cycle upper bound —
// the OL-27 source scanners freeze their bound over their own source table.
func (s *Service) loadCursorWithBound(ctx context.Context, scanner string, upperBound func() (pgtype.UUID, error)) (scanCursor, error) {
	row, err := s.Queries.GetLabrastroMessageScanCursor(ctx, scanner)
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = s.Queries.InitLabrastroMessageScanCursor(ctx, scanner)
		if errors.Is(err, pgx.ErrNoRows) {
			row, err = s.Queries.GetLabrastroMessageScanCursor(ctx, scanner)
		}
	}
	if err != nil {
		return scanCursorStart, err
	}
	cur := cursorFromRow(row)
	isSource := sourceScopeForScanner(scanner) != ""
	if cur.upperID.Valid && cur.id.Valid && (!isSource || cur.cycleStable.Valid) {
		return cur, nil
	}
	cur.cycleStarted = s.now()
	if isSource {
		horizon, err := s.Queries.GetLabrastroMessageSourceScanHorizon(ctx)
		if err != nil {
			return cur, err
		}
		cur.cycleStarted, cur.cycleStable = horizon.ScanThrough.Time, horizon.StableAt
		s.warnSourceHorizon(scanner, horizon.ScanThrough.Time, horizon.StableAt.Time)
	}
	upper, err := upperBound()
	if err != nil {
		return cur, err
	}
	cur.id, cur.upperID = scanCursorStart.id, upper
	return s.saveCursor(ctx, scanner, cur)
}

// sourceScopeForScanner maps a source scanner name onto the route scope
// (and source table) it scans.
func sourceScopeForScanner(scanner string) string {
	switch scanner {
	case scannerInboxSource:
		return RouteSourceInbox
	case scannerActivitySource:
		return RouteSourceActivity
	case scannerCommentSource:
		return RouteSourceComment
	}
	return ""
}

func (s *Service) saveCursor(ctx context.Context, scanner string, cur scanCursor) (scanCursor, error) {
	row, err := s.Queries.SaveLabrastroMessageScanCursor(ctx, db.SaveLabrastroMessageScanCursorParams{
		Scanner: scanner, CursorTs: pgtype.Timestamptz{Time: cur.ts, Valid: true},
		CursorID: cur.id, CycleUpperID: cur.upperID,
		CycleStartedAt:     pgtype.Timestamptz{Time: cur.cycleStarted, Valid: true},
		CycleStableAt:      cur.cycleStable,
		ExpectedGeneration: cur.generation,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, errScanCursorMoved
	}
	if err != nil {
		return cur, err
	}
	return cursorFromRow(row), nil
}

// syncStaleRunOnlyTasks finds run_only tasks whose run never heard about
// their terminal state and feeds them to the EXISTING sync logic. This
// module runs no second state machine — it reuses the upstream one.
func (s *Service) syncStaleRunOnlyTasks(ctx context.Context, cur scanCursor) (scanCursor, error) {
	tasks, err := s.Queries.ListStaleRunOnlyAutopilotTasks(ctx, db.ListStaleRunOnlyAutopilotTasksParams{
		AfterID: cur.id,
		UpperID: cur.upperID,
		Limit:   scanPageLimit,
	})
	if err != nil {
		return cur, err
	}
	for _, task := range tasks {
		s.Syncer.SyncRunFromTask(ctx, task)
	}
	if len(tasks) == 0 {
		return cur, nil
	}
	last := tasks[len(tasks)-1]
	cur.id = last.ID
	return cur, nil
}

func (s *Service) syncStaleCreateIssueIssues(ctx context.Context, cur scanCursor) (scanCursor, error) {
	issues, err := s.Queries.ListStaleCreateIssueAutopilotIssues(ctx, db.ListStaleCreateIssueAutopilotIssuesParams{
		AfterID: cur.id,
		UpperID: cur.upperID,
		Limit:   scanPageLimit,
	})
	if err != nil {
		return cur, err
	}
	for _, issue := range issues {
		s.Syncer.SyncRunFromIssue(ctx, issue)
	}
	if len(issues) == 0 {
		return cur, nil
	}
	last := issues[len(issues)-1]
	cur.id = last.ID
	return cur, nil
}

// syncStaleLinkedTaskFailures covers the third persisted source class
// (review R12): create_issue tasks that reached a completed or failed state
// through their ISSUE link while the run stayed active. The historical name
// and cursor key stay stable. SyncRunFromLinkedIssueTask checks the dispatch
// chain, so unrelated tasks and earlier retry attempts cannot settle the run.
func (s *Service) syncStaleLinkedTaskFailures(ctx context.Context, cur scanCursor) (scanCursor, error) {
	tasks, err := s.Queries.ListStaleLinkedIssueTaskFailures(ctx, db.ListStaleLinkedIssueTaskFailuresParams{
		AfterID: cur.id,
		UpperID: cur.upperID,
		Limit:   scanPageLimit,
	})
	if err != nil {
		return cur, err
	}
	for _, task := range tasks {
		s.Syncer.SyncRunFromLinkedIssueTask(ctx, task)
	}
	if len(tasks) == 0 {
		return cur, nil
	}
	last := tasks[len(tasks)-1]
	cur.id = last.ID
	return cur, nil
}

// decideMissing freezes a decision for every (terminal run, enabled route
// target) pair the candidate query still finds. Decisions — sends,
// suppressions and cancellations alike — shrink the set, so the scan is a
// full missing-set sweep whose cost decays as sources are judged.
func (s *Service) decideMissing(ctx context.Context) error {
	_, err := s.enqueueRunCandidates(ctx, pgtype.UUID{}, scanBatchSize)
	return err
}

func (s *Service) enqueueRunCandidates(ctx context.Context, runID pgtype.UUID, limit int32) (int, error) {
	candidates, err := s.Queries.ListLabrastroMessageDeliveryCandidateRoutes(ctx, db.ListLabrastroMessageDeliveryCandidateRoutesParams{RunID: runID, Limit: limit})
	if err != nil {
		return 0, err
	}
	decided := 0
	for _, c := range candidates {
		run := c.AutopilotRun
		run.TaskID = c.SourceTaskID
		in := s.decisionInputFromSource(c.Autopilot, c.LabrastroMessageRoute, sourceFactsFromRun(run))
		n, err := s.decideDelivery(ctx, c.Autopilot, in)
		if err != nil {
			return decided, err
		}
		decided += n
		if n > 0 {
			s.Notify()
		}
	}
	return decided, nil
}
