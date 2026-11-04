package service

import (
	"context"
	"testing"
)

// A paused company holds an agent's automatic publish (spec 4.5). The draft waits
// for a person, and nothing is written to the governance log for it. After the
// resume the next draft publishes, and the default recorder writes its entry.
func TestPausedCompanyHoldsAutomaticPublishUntilResume(t *testing.T) {
	p := newPolicyFixture(t)
	e := p.e
	ctx := context.Background()
	t.Cleanup(func() {
		e.pool.Exec(context.Background(), `DELETE FROM governance_event WHERE workspace_id = $1`, e.wsID)
	})
	if _, err := e.svc.SetPublishPolicy(ctx, e.ws, e.user, PublishScopeBase, "engineering", PublishModeAuto); err != nil {
		t.Fatal(err)
	}
	css := &CompanyStateService{Queries: e.q, TxStarter: e.pool}
	actor := GovernanceActor{Type: GovernanceActorMember, ID: e.user}
	countAutoPublish := func() int {
		t.Helper()
		var n int
		if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM governance_event WHERE workspace_id = $1 AND kind = $2`,
			e.ws, GovernanceKindAutoPublish).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if _, err := css.Set(ctx, e.ws, actor, CompanyStatePaused); err != nil {
		t.Fatal(err)
	}
	if held := p.draft("Held while paused"); held.State != "draft" {
		t.Fatalf("a paused company published an automatic draft: state %q", held.State)
	}
	if n := countAutoPublish(); n != 0 {
		t.Fatalf("a held publish left %d auto_publish entries", n)
	}

	if _, err := css.Set(ctx, e.ws, actor, CompanyStateRunning); err != nil {
		t.Fatal(err)
	}
	if published := p.draft("Published after resume"); published.State != "published" {
		t.Fatalf("after resume the automatic draft is %q, want published", published.State)
	}
	if n := countAutoPublish(); n != 1 {
		t.Fatalf("the publish after resume recorded %d auto_publish entries, want 1", n)
	}
}
