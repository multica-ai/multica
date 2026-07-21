package loops

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeSkillLister returns a fixed set of workspace skill names, so the
// existence check can be exercised without a database.
type fakeSkillLister struct{ names []string }

func (f fakeSkillLister) ListSkillSummariesByWorkspace(_ context.Context, _ pgtype.UUID) ([]db.ListSkillSummariesByWorkspaceRow, error) {
	rows := make([]db.ListSkillSummariesByWorkspaceRow, 0, len(f.names))
	for _, n := range f.names {
		rows = append(rows, db.ListSkillSummariesByWorkspaceRow{Name: n})
	}
	return rows, nil
}

func TestValidateSkillsExist(t *testing.T) {
	ctx := context.Background()
	ws := pgtype.UUID{}
	lister := fakeSkillLister{names: []string{"dev-runbook", "systematic-debugging"}}

	t.Run("missing plan_skill is rejected with a clear message", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithSkillLister(lister)
		chain := chainWithSkills("dev-runbook", "firtal-sales-channel")
		err := b.validateSkillsExist(ctx, ws, chain)
		if err == nil {
			t.Fatal("expected an error for a non-existent plan_skill, got nil")
		}
		if !strings.Contains(err.Error(), "blocks[1].skill") || !strings.Contains(err.Error(), "firtal-sales-channel") {
			t.Fatalf("error should name the field and skill, got: %v", err)
		}
	})

	t.Run("missing build_skill is rejected", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithSkillLister(lister)
		chain := chainWithSkills("no-such-skill")
		err := b.validateSkillsExist(ctx, ws, chain)
		if err == nil || !strings.Contains(err.Error(), "blocks[0].skill") {
			t.Fatalf("expected a build_skill error, got: %v", err)
		}
	})

	t.Run("all skills present passes", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithSkillLister(lister)
		chain := chainWithSkills("dev-runbook", "systematic-debugging")
		if err := b.validateSkillsExist(ctx, ws, chain); err != nil {
			t.Fatalf("expected no error when every skill exists, got: %v", err)
		}
	})

	t.Run("no lister wired is a no-op", func(t *testing.T) {
		b := &IssueLoopBridge{}
		chain := chainWithSkills("no-such-skill", "also-missing")
		if err := b.validateSkillsExist(ctx, ws, chain); err != nil {
			t.Fatalf("without a lister the check must be skipped, got: %v", err)
		}
	})
}

func chainWithSkills(skills ...string) *Chain {
	blocks := make([]Block, len(skills))
	for i, skill := range skills {
		blocks[i] = Block{ID: fmt.Sprintf("block-%d", i), Type: BlockSession, Skill: skill}
	}
	return &Chain{Version: ChainVersion, Phases: []Phase{{ID: "phase", Blocks: blocks, Limits: PhaseLimits{MaxSteps: 10, MaxRounds: 10, NoProgressStalls: 3}}}}
}
