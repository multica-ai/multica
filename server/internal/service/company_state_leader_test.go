package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// A leader's run that the Secretary started (a directive or a question the
// Secretary posts) is a delegated run, so the company controls hold it like any
// other agent-started run (spec 13.4.3). A run a person started is admitted.
// Resuming the company releases the held run, so the hold is the pause and not
// the fixture.
func TestPausedCompanyHoldsLeaderRunsStartedBySecretary(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()
	runtime := f.Runtime(t, "leader-runtime", testutil.Cols{"owner_id": f.owner})
	leader := f.Agent(t, "leader-agent", runtime, testutil.Cols{"owner_id": f.owner})

	directive := f.Task(t, leader, testutil.Cols{"runtime_id": runtime, "status": "queued", "originator_source": "delegation"})
	reply := f.Task(t, leader, testutil.Cols{"runtime_id": runtime, "status": "queued", "originator_source": "comment_source"})
	human := f.Task(t, leader, testutil.Cols{"runtime_id": runtime, "status": "queued", "originator_source": "direct_human"})

	refusedBy := func(taskID string) *claimRefusal {
		t.Helper()
		task, err := f.svc.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
		if err != nil {
			t.Fatalf("load task %s: %v", taskID, err)
		}
		refusal, err := f.svc.Tasks.claimRefusalFor(ctx, f.svc.Queries, f.ws, task)
		if err != nil {
			t.Fatalf("claim refusal for %s: %v", taskID, err)
		}
		return refusal
	}

	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStatePaused); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"directive": directive, "reply": reply} {
		r := refusedBy(id)
		if r == nil || r.Reason != VerdictReasonCompanyPaused {
			t.Errorf("paused company: %s run refusal = %+v, want held company_paused", name, r)
		}
	}
	if r := refusedBy(human); r != nil {
		t.Errorf("paused company: a person's run was refused with %q, want admitted", r.Reason)
	}

	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStateRunning); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"directive": directive, "reply": reply} {
		if r := refusedBy(id); r != nil {
			t.Errorf("resumed company: %s run still refused with %q", name, r.Reason)
		}
	}
}
