package connectedapps

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// CallQuota meters the calls a workspace sends to Composio each period (the
// app_tool_calls entitlement gate, MUL-10047). Admit runs right before a call
// is sent and counts it, so a call that then fails upstream still counts. It
// returns the start of the period the call was counted in (zero when it was
// not counted) and, when the call must not run, the refusal. Every failure to
// decide admits. Remote MCP apps and meta tools (listing, searching,
// connection requests) never reach it: they do not run through Composio.
type CallQuota interface {
	Admit(ctx context.Context, workspaceID pgtype.UUID) (countedIn time.Time, refused *CallQuotaExceeded)
}

// CallQuotaExceeded is a refusal: the workspace used its allowance.
type CallQuotaExceeded struct {
	Used    int64
	Limit   int64
	ResetAt time.Time
}

// Message is what the agent, and the approver of a confirmed call, read.
func (e *CallQuotaExceeded) Message() string {
	return fmt.Sprintf("app call limit reached: this workspace has used %d of its %d app calls for this period; the allowance resets at %s. "+
		"Do not retry. Tell the person that this workspace is out of app calls until then, or until it upgrades its plan or adds seats.",
		e.Used, e.Limit, e.ResetAt.UTC().Format("2006-01-02 15:04 UTC"))
}

// overQuota counts a call against the workspace's allowance right before it
// runs, and records and returns the refusal when the allowance is used up.
func (s *Service) overQuota(ctx context.Context, call *toolCall) *CallQuotaExceeded {
	if s.quota == nil {
		return nil
	}
	countedIn, exceeded := s.quota.Admit(ctx, call.workspaceID)
	if exceeded == nil {
		call.meteredPeriod = countedIn
		return nil
	}
	call.outcome, call.err = "quota_exceeded", ""
	s.recordCall(ctx, *call)
	return exceeded
}
