package permgate

import (
	"context"
	"errors"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
)

// Status is the single-shot, non-blocking read the repo-checkout poll endpoint
// uses (FIR-2586). Unlike Await it must return immediately with the current
// outcome so the daemon — not the server — owns the wait loop.

func TestStatus_PendingStaysPending(t *testing.T) {
	ap := &fakeApprovals{row: cerebrodb.CerebroApprovalRequest{ID: newUUID(), Status: approvals.StatusPending}}
	g := &Gate{Approvals: ap}

	out, err := g.Status(context.Background(), newUUID(), newUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != OutcomePending {
		t.Fatalf("got %q, want pending", out)
	}
	if ap.getCalls != 1 {
		t.Fatalf("Status must read exactly once (non-blocking), got %d", ap.getCalls)
	}
}

func TestStatus_TerminalOutcomes(t *testing.T) {
	cases := []struct {
		status string
		want   Outcome
	}{
		{approvals.StatusApproved, OutcomeApproved},
		{approvals.StatusRejected, OutcomeRejected},
		{approvals.StatusExpired, OutcomeExpired},
		{approvals.StatusCancelled, OutcomeDenied},
		{approvals.StatusDelegated, OutcomePending}, // still actionable
	}
	for _, c := range cases {
		ap := &fakeApprovals{row: cerebrodb.CerebroApprovalRequest{ID: newUUID(), Status: c.status}}
		g := &Gate{Approvals: ap}
		out, err := g.Status(context.Background(), newUUID(), newUUID())
		if err != nil {
			t.Fatalf("status %q: unexpected error: %v", c.status, err)
		}
		if out != c.want {
			t.Fatalf("status %q: got %q, want %q", c.status, out, c.want)
		}
	}
}

func TestStatus_ReadErrorIsPendingPlusError(t *testing.T) {
	ap := &fakeApprovals{getErr: errors.New("db down")}
	g := &Gate{Approvals: ap}

	out, err := g.Status(context.Background(), newUUID(), newUUID())
	if err == nil {
		t.Fatal("expected error to surface so the daemon retries, got nil")
	}
	if out != OutcomePending {
		t.Fatalf("got %q, want pending on read error (do not fail closed mid-poll)", out)
	}
}

func TestStatus_NilGateErrors(t *testing.T) {
	var g *Gate
	if _, err := g.Status(context.Background(), newUUID(), newUUID()); err == nil {
		t.Fatal("nil gate must error, not allow")
	}
}
