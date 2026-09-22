package knowledge

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/pkg/llm"
)

func TestKnowledgeRetryPolicyHonorsAttemptsAndStatuses(t *testing.T) {
	job := &workerJob{Job: Job{Attempt: 1}}
	if !shouldRetryKnowledgeJob(job, &llm.HTTPError{StatusCode: http.StatusTooManyRequests}) {
		t.Fatal("429 was not retryable")
	}
	if !shouldRetryKnowledgeJob(job, &llm.HTTPError{StatusCode: http.StatusBadGateway}) {
		t.Fatal("5xx was not retryable")
	}
	if shouldRetryKnowledgeJob(&workerJob{Job: Job{Attempt: 3}}, &llm.HTTPError{StatusCode: http.StatusBadGateway}) {
		t.Fatal("retry policy exceeded the attempt limit")
	}
	if shouldRetryKnowledgeJob(job, &llm.HTTPError{StatusCode: http.StatusBadRequest}) {
		t.Fatal("4xx deterministic failure was retryable")
	}
	if shouldRetryKnowledgeJob(job, &llm.HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: 16 * time.Minute, RetryAfterSet: true}) {
		t.Fatal("retry-after beyond the scheduling limit was retryable")
	}
	if !shouldRetryKnowledgeJob(job, context.DeadlineExceeded) {
		t.Fatal("deadline exceeded was not retryable")
	}
}

func TestKnowledgeRetryDelayPrefersRetryAfter(t *testing.T) {
	job := &workerJob{Job: Job{Attempt: 2}}
	if got := knowledgeRetryDelayForError(job, &llm.HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: 7 * time.Second, RetryAfterSet: true}); got != 7*time.Second {
		t.Fatalf("retry-after delay = %v", got)
	}
	if got := knowledgeRetryDelayForError(job, errors.New("temporary")); got != 2*time.Minute {
		t.Fatalf("fallback delay = %v", got)
	}
}

func TestKnowledgeProviderErrorKeepsStableRetryCode(t *testing.T) {
	err := knowledgeProviderError(&llm.HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: time.Second, RetryAfterSet: true}, "provider failed")
	if err.Code != "provider_rate_limited" || !err.Retryable || err.Status != http.StatusTooManyRequests {
		t.Fatalf("provider error = %+v", err)
	}
	if !errors.Is(err, err.Err) {
		t.Fatal("provider cause was not retained")
	}
}

func TestCleanupKeepsRetryingAfterNormalAttemptBudget(t *testing.T) {
	workspaceCleanup := &workerJob{Job: Job{Stage: "cleanup", Attempt: 99}, Input: map[string]any{"scope": "workspace"}}
	baseCleanup := &workerJob{Job: Job{Stage: "cleanup", Attempt: 99}, Input: map[string]any{"scope": "base"}}
	documentCleanup := &workerJob{Job: Job{Stage: "cleanup", Attempt: 99}, Input: map[string]any{"scope": "document"}}
	retryable := retryableKnowledge(http.StatusServiceUnavailable, "cleanup_unavailable", "retry", nil)
	if !shouldRetryKnowledgeJob(workspaceCleanup, retryable) {
		t.Fatal("workspace cleanup stopped retrying after the normal attempt budget")
	}
	if !shouldRetryKnowledgeJob(baseCleanup, retryable) || !shouldRetryKnowledgeJob(documentCleanup, retryable) {
		t.Fatal("ordinary cleanup stopped retrying after the normal attempt budget")
	}
}

type failJobDBTX struct {
	statements []string
}

func (d *failJobDBTX) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	d.statements = append(d.statements, sql)
	if len(d.statements) == 1 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (*failJobDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (*failJobDBTX) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func TestFailJobDoesNotUpdateVersionAfterLeaseLoss(t *testing.T) {
	db := &failJobDBTX{}
	service := &Service{db: db}
	job := &workerJob{
		Job: Job{ID: "11111111-1111-4111-8111-111111111111", Stage: "parse", DocumentVersionID: stringPtr("22222222-2222-4222-8222-222222222222")},
	}
	if err := service.failJob(context.Background(), job, "parse_failed", errors.New("stale worker")); err != nil {
		t.Fatalf("failJob returned error: %v", err)
	}
	if len(db.statements) != 1 {
		t.Fatalf("failJob executed %d statements after lease loss, want only the fenced job update", len(db.statements))
	}
}

func stringPtr(value string) *string { return &value }
