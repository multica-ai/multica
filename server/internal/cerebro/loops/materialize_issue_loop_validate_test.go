package loops

import (
	"context"
	"errors"
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

// fakeEvalBindingLister returns a fixed set of gates bound to the workflow, so
// the save-time eval check can be exercised without a database.
type fakeEvalBindingLister struct{ bindings []EvalBindingKey }

func (fakeEvalBindingLister) ListMonitorAdvisoryEvalKeys(_ context.Context, _ pgtype.UUID) ([]string, error) {
	return nil, nil
}

func (f fakeEvalBindingLister) ListEvalBindingKeys(_ context.Context, _ pgtype.UUID) ([]EvalBindingKey, error) {
	return f.bindings, nil
}

func TestValidateEvalBindingsExist(t *testing.T) {
	ctx := context.Background()
	wf := pgtype.UUID{}
	lister := fakeEvalBindingLister{bindings: []EvalBindingKey{
		{EvalKey: "delivery-quality", Phase: "delivery"},
		{EvalKey: "drift-watch", Phase: "monitor"},
	}}

	t.Run("an eval_key with no gate on this workflow is rejected at save time", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithMonitorEvalBindingLister(lister)
		err := b.validateEvalBindingsExist(ctx, wf, chainWithEvals(evalBlock("no-such-gate", "delivery")))
		if err == nil {
			t.Fatal("expected an error for an unbound eval_key, got nil")
		}
		if !strings.Contains(err.Error(), "blocks[0].eval_key") || !strings.Contains(err.Error(), "no-such-gate") {
			t.Fatalf("error should name the field and the key, got: %v", err)
		}
		if !strings.Contains(err.Error(), "Workflow gates") {
			t.Fatalf("error should tell the user where to bind the gate, got: %v", err)
		}
	})

	t.Run("a bound key at the wrong phase is rejected", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithMonitorEvalBindingLister(lister)
		// delivery-quality exists, but only as a delivery gate.
		err := b.validateEvalBindingsExist(ctx, wf, chainWithEvals(evalBlock("delivery-quality", "plan")))
		if err == nil || !strings.Contains(err.Error(), "plan") {
			t.Fatalf("expected a phase-specific error, got: %v", err)
		}
	})

	t.Run("an empty eval_phase defaults to delivery, matching the runner", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithMonitorEvalBindingLister(lister)
		if err := b.validateEvalBindingsExist(ctx, wf, chainWithEvals(evalBlock("delivery-quality", ""))); err != nil {
			t.Fatalf("an empty phase must resolve as delivery, got: %v", err)
		}
	})

	t.Run("every eval block bound passes", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithMonitorEvalBindingLister(lister)
		chain := chainWithEvals(evalBlock("delivery-quality", "delivery"), evalBlock("drift-watch", "monitor"))
		if err := b.validateEvalBindingsExist(ctx, wf, chain); err != nil {
			t.Fatalf("expected no error when every gate is bound, got: %v", err)
		}
	})

	t.Run("a chain without eval blocks never queries the bindings", func(t *testing.T) {
		b := (&IssueLoopBridge{}).WithMonitorEvalBindingLister(explodingEvalBindingLister{})
		if err := b.validateEvalBindingsExist(ctx, wf, chainWithSkills("dev-runbook")); err != nil {
			t.Fatalf("expected no error and no lookup, got: %v", err)
		}
	})

	t.Run("no lister wired is a no-op", func(t *testing.T) {
		b := &IssueLoopBridge{}
		if err := b.validateEvalBindingsExist(ctx, wf, chainWithEvals(evalBlock("anything", "delivery"))); err != nil {
			t.Fatalf("without a lister the check must be skipped, got: %v", err)
		}
	})
}

// explodingEvalBindingLister fails the test if it is ever consulted.
type explodingEvalBindingLister struct{}

func (explodingEvalBindingLister) ListMonitorAdvisoryEvalKeys(_ context.Context, _ pgtype.UUID) ([]string, error) {
	return nil, nil
}

func (explodingEvalBindingLister) ListEvalBindingKeys(_ context.Context, _ pgtype.UUID) ([]EvalBindingKey, error) {
	return nil, errors.New("bindings must not be queried for a chain without eval blocks")
}

func evalBlock(key, phase string) Block {
	return Block{ID: "eval", Type: BlockEval, EvalKey: key, EvalPhase: phase}
}

func chainWithEvals(blocks ...Block) *Chain {
	for i := range blocks {
		blocks[i].ID = fmt.Sprintf("block-%d", i)
	}
	return &Chain{Version: ChainVersion, Phases: []Phase{{ID: "phase", Blocks: blocks, Limits: PhaseLimits{MaxSteps: 10, MaxRounds: 10, NoProgressStalls: 3}}}}
}

func chainWithSkills(skills ...string) *Chain {
	blocks := make([]Block, len(skills))
	for i, skill := range skills {
		blocks[i] = Block{ID: fmt.Sprintf("block-%d", i), Type: BlockSession, Skill: skill}
	}
	return &Chain{Version: ChainVersion, Phases: []Phase{{ID: "phase", Blocks: blocks, Limits: PhaseLimits{MaxSteps: 10, MaxRounds: 10, NoProgressStalls: 3}}}}
}
