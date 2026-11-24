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
	refreshed map[uuid.UUID]map[entitlement.GateName]entitlement.Decision
	features  map[uuid.UUID]map[entitlement.FeatureName]entitlement.FeatureDecision
	calls     []Call
	refreshes []Call
}

var (
	_ entitlement.Provider        = (*Stub)(nil)
	_ entitlement.Refresher       = (*Stub)(nil)
	_ entitlement.FreshRefresher  = (*Stub)(nil)
	_ entitlement.FeatureProvider = (*Stub)(nil)
)

func New() *Stub {
	return &Stub{
		decisions: make(map[uuid.UUID]map[entitlement.GateName]entitlement.Decision),
		refreshed: make(map[uuid.UUID]map[entitlement.GateName]entitlement.Decision),
		features:  make(map[uuid.UUID]map[entitlement.FeatureName]entitlement.FeatureDecision),
	}
}

// SetRefreshed fixes what RefreshGate returns, standing in for a policy Cloud
// changed after the cached one was read. Once RefreshGate has returned it,
// Gate returns it too, as the refreshed cache would.
func (s *Stub) SetRefreshed(workspaceID uuid.UUID, name entitlement.GateName, decision entitlement.Decision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshed[workspaceID] == nil {
		s.refreshed[workspaceID] = make(map[entitlement.GateName]entitlement.Decision)
	}
	decision.Reason = entitlement.ReasonStub
	s.refreshed[workspaceID][name] = cloneDecision(decision)
}

// RefreshGate returns the decision set with SetRefreshed, or the same
// decision as Gate when none was set.
func (s *Stub) RefreshGate(ctx context.Context, workspaceID uuid.UUID, name entitlement.GateName) entitlement.Decision {
	s.mu.Lock()
	s.refreshes = append(s.refreshes, Call{WorkspaceID: workspaceID, Gate: name})
	if decision, ok := s.refreshed[workspaceID][name]; ok {
		if s.decisions[workspaceID] == nil {
			s.decisions[workspaceID] = make(map[entitlement.GateName]entitlement.Decision)
		}
		s.decisions[workspaceID][name] = decision
		delete(s.refreshed[workspaceID], name)
		s.mu.Unlock()
		return cloneDecision(decision)
	}
	s.mu.Unlock()
	return s.Gate(ctx, workspaceID, name)
}

// RefreshGateNow behaves like RefreshGate: every stub refresh is forced.
func (s *Stub) RefreshGateNow(ctx context.Context, workspaceID uuid.UUID, name entitlement.GateName) entitlement.Decision {
	return s.RefreshGate(ctx, workspaceID, name)
}

// Refreshes lists every RefreshGate and RefreshGateNow call.
func (s *Stub) Refreshes() []Call {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Call(nil), s.refreshes...)
}

// SetFeature fixes one workspace's paid-feature decision. The Reason given is
// kept, so tests can tell a configured observe from a stale downgrade.
func (s *Stub) SetFeature(workspaceID uuid.UUID, name entitlement.FeatureName, decision entitlement.FeatureDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.features[workspaceID] == nil {
		s.features[workspaceID] = make(map[entitlement.FeatureName]entitlement.FeatureDecision)
	}
	if decision.Reason == "" {
		decision.Reason = entitlement.ReasonStub
	}
	s.features[workspaceID][name] = decision
}

func (s *Stub) Feature(_ context.Context, workspaceID uuid.UUID, name entitlement.FeatureName) entitlement.FeatureDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if decision, ok := s.features[workspaceID][name]; ok {
		return decision
	}
	return entitlement.FeatureDecision{Action: entitlement.ActionOff, Reason: entitlement.ReasonStub}
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
