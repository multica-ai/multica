// Package entitlementtest provides a deterministic Provider for tests of
// future entitlement consumers. It never contacts Multica Cloud.
package entitlementtest

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/entitlement"
)

type Call struct {
	WorkspaceID uuid.UUID
	Gate        entitlement.GateName
}

type Stub struct {
	mu        sync.RWMutex
	decisions map[uuid.UUID]map[entitlement.GateName]entitlement.Decision
	calls     []Call
}

var _ entitlement.Provider = (*Stub)(nil)

func New() *Stub {
	return &Stub{decisions: make(map[uuid.UUID]map[entitlement.GateName]entitlement.Decision)}
}

func (s *Stub) Set(workspaceID uuid.UUID, name entitlement.GateName, decision entitlement.Decision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.decisions[workspaceID] == nil {
		s.decisions[workspaceID] = make(map[entitlement.GateName]entitlement.Decision)
	}
	decision.Reason = entitlement.ReasonStub
	s.decisions[workspaceID][name] = cloneDecision(decision)
}

func (s *Stub) Gate(_ context.Context, workspaceID uuid.UUID, name entitlement.GateName) entitlement.Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{WorkspaceID: workspaceID, Gate: name})
	if decision, ok := s.decisions[workspaceID][name]; ok {
		return cloneDecision(decision)
	}
	return entitlement.Decision{
		Gate:   entitlement.Gate{Action: entitlement.ActionOff},
		Reason: entitlement.ReasonStub,
	}
}

func (s *Stub) Calls() []Call {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Call(nil), s.calls...)
}

func cloneDecision(in entitlement.Decision) entitlement.Decision {
	out := in
	if in.Gate.Limit != nil {
		value := *in.Gate.Limit
		out.Gate.Limit = &value
	}
	if in.Gate.PeriodStart != nil {
		value := *in.Gate.PeriodStart
		out.Gate.PeriodStart = &value
	}
	if in.Gate.PeriodEnd != nil {
		value := *in.Gate.PeriodEnd
		out.Gate.PeriodEnd = &value
	}
	if in.Gate.ResetAt != nil {
		value := *in.Gate.ResetAt
		out.Gate.ResetAt = &value
	}
	return out
}
