package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	githubintegration "github.com/multica-ai/multica/server/internal/integrations/github"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCRFlow_v2_CommentedClean_SettleSweeps(t *testing.T) {
	store := newSettleStore()
	stubEvaluateCRPredicate(t, true, true)

	promoteOrBounceCommentedClean(context.Background(), db.New(store), settleTxStarter{store: store}, events.New(), nil, store.pendingRow())

	if store.updatedStatus != githubintegration.StatusStaged {
		t.Fatalf("status = %q, want staged", store.updatedStatus)
	}
	if store.closedOutcome != "completed_clean" || store.closedReason != "commented_clean_settled" {
		t.Fatalf("attempt close = %q/%q, want completed_clean/commented_clean_settled", store.closedOutcome, store.closedReason)
	}
	if store.activityAction != "review_passed" {
		t.Fatalf("activity = %q, want review_passed", store.activityAction)
	}
}

func TestCRFlow_v2_CommentedClean_LateCommentBouncesToResolving(t *testing.T) {
	store := newSettleStore()
	stubEvaluateCRPredicate(t, true, false)

	promoteOrBounceCommentedClean(context.Background(), db.New(store), settleTxStarter{store: store}, events.New(), nil, store.pendingRow())

	if store.updatedStatus != githubintegration.StatusResolving {
		t.Fatalf("status = %q, want resolving", store.updatedStatus)
	}
	if store.closedOutcome != "completed_with_findings" || store.closedReason != "commented_clean_then_dirty_at_settle" {
		t.Fatalf("attempt close = %q/%q, want completed_with_findings/commented_clean_then_dirty_at_settle", store.closedOutcome, store.closedReason)
	}
	if store.activityAction != "review_comments_unresolved" {
		t.Fatalf("activity = %q, want review_comments_unresolved", store.activityAction)
	}
}

func TestCRPostCommentSettleSweeper_StatusRaceSkipsWithoutClosingAttempt(t *testing.T) {
	store := newSettleStore()
	store.statusUpdateErr = pgx.ErrNoRows
	stubEvaluateCRPredicate(t, true, true)

	promoteOrBounceCommentedClean(context.Background(), db.New(store), settleTxStarter{store: store}, events.New(), nil, store.pendingRow())

	if store.updatedStatus != "" {
		t.Fatalf("status = %q, want no committed update", store.updatedStatus)
	}
	if store.closeAttemptCalls != 0 {
		t.Fatalf("close attempt calls = %d, want 0", store.closeAttemptCalls)
	}
	if store.activityAction != "" {
		t.Fatalf("activity = %q, want none", store.activityAction)
	}
}

func stubEvaluateCRPredicate(t *testing.T, noOpenChanges, noUnresolved bool) {
	t.Helper()
	oldEval := evaluateCRPredicate
	oldClient := newCRPredicateClient
	newCRPredicateClient = func(*githubintegration.AppAuth, int64) githubintegration.PRReviewClient {
		return nil
	}
	evaluateCRPredicate = func(context.Context, githubintegration.PRReviewClient, string, string, int, string) (bool, bool, error) {
		return noOpenChanges, noUnresolved, nil
	}
	t.Cleanup(func() {
		evaluateCRPredicate = oldEval
		newCRPredicateClient = oldClient
	})
}

type settleStore struct {
	workspaceID       pgtype.UUID
	issueID           pgtype.UUID
	binding           db.WorkspaceRepoBinding
	updatedStatus     string
	closedOutcome     string
	closedReason      string
	activityAction    string
	statusUpdateErr   error
	closeAttemptCalls int
}

func newSettleStore() *settleStore {
	workspaceID := settleUUID(1)
	issueID := settleUUID(2)
	return &settleStore{
		workspaceID: workspaceID,
		issueID:     issueID,
		binding: db.WorkspaceRepoBinding{
			ID:             settleUUID(3),
			WorkspaceID:    workspaceID,
			RepoFullName:   "acme/repo",
			InstallationID: 123,
			CrBotUsername:  "coderabbitai[bot]",
			Active:         true,
		},
	}
}

func (s *settleStore) pendingRow() db.ListPendingCommentedApprovalsRow {
	return db.ListPendingCommentedApprovalsRow{
		AttemptID:   settleUUID(4),
		IssueID:     s.issueID,
		WorkspaceID: s.workspaceID,
		CrRound:     1,
		PrUrl:       "https://github.com/acme/repo/pull/7",
		PrRepo:      "acme/repo",
		PrNumber:    7,
	}
}

func (s *settleStore) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (s *settleStore) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (s *settleStore) QueryRow(_ context.Context, query string, args ...interface{}) pgx.Row {
	switch {
	case strings.Contains(query, "-- name: GetRepoBindingByRepo"):
		return settleRow([]any{
			s.binding.ID, s.binding.WorkspaceID, s.binding.RepoFullName,
			s.binding.InstallationID, s.binding.CrBotUsername, s.binding.Active,
			s.binding.CreatedAt, s.binding.UpdatedAt, s.binding.CrRequired,
		})
	case strings.Contains(query, "-- name: UpdateIssueStatusIfCurrent"):
		if s.statusUpdateErr != nil {
			return settleErrRow{err: s.statusUpdateErr}
		}
		s.updatedStatus = args[0].(string)
		return settleRow(settleIssueValues(db.Issue{
			ID:          s.issueID,
			WorkspaceID: s.workspaceID,
			Title:       "CR Loop",
			Status:      s.updatedStatus,
			Priority:    "medium",
			CreatorType: "system",
			Number:      42,
		}))
	case strings.Contains(query, "-- name: CloseCRReviewAttempt"):
		s.closeAttemptCalls++
		s.closedOutcome = args[2].(pgtype.Text).String
		s.closedReason = args[3].(pgtype.Text).String
		return settleRow(settleAttemptValues(db.CrReviewAttempt{
			ID:            settleUUID(4),
			IssueID:       s.issueID,
			WorkspaceID:   s.workspaceID,
			CrRound:       args[1].(int32),
			Outcome:       args[2].(pgtype.Text),
			OutcomeReason: args[3].(pgtype.Text),
		}))
	case strings.Contains(query, "-- name: CreateActivity"):
		s.activityAction = args[4].(string)
		return settleRow([]any{
			settleUUID(5), s.workspaceID, s.issueID, args[2].(pgtype.Text),
			pgtype.UUID{}, args[4].(string), args[5].([]byte), pgtype.Timestamptz{},
		})
	case strings.Contains(query, "-- name: CreateComment"):
		return settleRow([]any{
			settleUUID(6), s.issueID, "system", pgtype.UUID{}, args[4].(string), "system",
			pgtype.Timestamptz{}, pgtype.Timestamptz{}, pgtype.UUID{}, s.workspaceID,
			pgtype.UUID{}, pgtype.Timestamptz{},
		})
	default:
		return settleErrRow{err: errors.New("unexpected query: " + firstSettleLine(query))}
	}
}

type settleTxStarter struct{ store *settleStore }

func (s settleTxStarter) Begin(context.Context) (pgx.Tx, error) { return settleTx{store: s.store}, nil }

type settleTx struct{ store *settleStore }

func (s settleTx) Begin(context.Context) (pgx.Tx, error) { return s, nil }
func (s settleTx) Commit(context.Context) error          { return nil }
func (s settleTx) Rollback(context.Context) error        { return nil }
func (s settleTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}
func (s settleTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (s settleTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (s settleTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare")
}
func (s settleTx) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	return s.store.Exec(ctx, query, args...)
}
func (s settleTx) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	return s.store.Query(ctx, query, args...)
}
func (s settleTx) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	return s.store.QueryRow(ctx, query, args...)
}
func (s settleTx) Conn() *pgx.Conn { return nil }

type settleRow []any

func (r settleRow) Scan(dest ...interface{}) error {
	if len(dest) != len(r) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if r[i] == nil {
			continue
		}
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Pointer || dv.IsNil() {
			return errors.New("scan destination is not a pointer")
		}
		v := reflect.ValueOf(r[i])
		if !v.Type().AssignableTo(dv.Elem().Type()) {
			return errors.New("scan value type mismatch")
		}
		dv.Elem().Set(v)
	}
	return nil
}

type settleErrRow struct{ err error }

func (r settleErrRow) Scan(...interface{}) error { return r.err }

func settleIssueValues(i db.Issue) []any {
	return []any{
		i.ID, i.WorkspaceID, i.Title, i.Description, i.Status, i.Priority,
		i.AssigneeType, i.AssigneeID, i.CreatorType, i.CreatorID,
		i.ParentIssueID, i.AcceptanceCriteria, i.ContextRefs, i.Position,
		i.DueDate, i.CreatedAt, i.UpdatedAt, i.Number, i.ProjectID,
		i.OriginType, i.OriginID, i.FirstExecutedAt, i.PrUrl, i.PrNumber,
		i.PrRepo, i.EstimateMinutes, i.PhaseState,
	}
}

func settleAttemptValues(i db.CrReviewAttempt) []any {
	return []any{
		i.ID, i.IssueID, i.WorkspaceID, i.CrRound, i.PrUrl, i.HeadSha,
		i.StartedAt, i.ReviewSubmittedAt, i.ReviewState, i.FindingsCount,
		i.Outcome, i.OutcomeReason, i.ClosedAt, i.FirstSignalAt, i.FirstSignalKind,
	}
}

func settleUUID(seed byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed}, Valid: true}
}

func firstSettleLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
