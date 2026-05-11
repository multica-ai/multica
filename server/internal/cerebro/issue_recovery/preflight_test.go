package issuerecovery

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClassifyRuntimeOrUsageFailure(t *testing.T) {
	got := Classify(CandidateInput{
		Comments: []db.Comment{{Content: "You've hit your org's monthly usage limit"}},
	})

	if got.Category != CategoryRuntimeOrUsageFailure {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryRuntimeOrUsageFailure)
	}
}

func TestClassifyTaskRuntimeFailure(t *testing.T) {
	got := Classify(CandidateInput{
		Tasks: []db.AgentTaskQueue{{
			Status:        "failed",
			FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			Error:         pgtype.Text{String: "runtime went offline", Valid: true},
		}},
	})

	if got.Category != CategoryRuntimeOrUsageFailure {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryRuntimeOrUsageFailure)
	}
}

func TestClassifyUnverifiedAgentDelivery(t *testing.T) {
	got := Classify(CandidateInput{
		IssueStatus: "in_review",
		Comments:    []db.Comment{{Content: "Færdig og leveret."}},
	})

	if got.Category != CategoryUnverifiedAgentDelivery {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryUnverifiedAgentDelivery)
	}
}

func TestClassifyPRReviewDeployQueue(t *testing.T) {
	got := Classify(CandidateInput{
		Comments: []db.Comment{{Content: "PR #127 is ready for /ultrareview before merge."}},
	})

	if got.Category != CategoryPRReviewDeployQueue {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryPRReviewDeployQueue)
	}
}

func TestClassifyBlocker(t *testing.T) {
	got := Classify(CandidateInput{
		Comments: []db.Comment{{Content: "The repo is not in workspace, so the agent cannot checkout the source."}},
	})

	if got.Category != CategoryBlocker {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryBlocker)
	}
}
