package service

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// goBackground runs fn so Close can wait for it; false when the service closed.
func (s *Service) goBackground(fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		fn()
	}()
	return true
}

func (s *Service) maintain() {
	every := s.opts.maintenanceEvery
	if every == 0 {
		every = maintenanceInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
		case <-s.maintenanceKick:
		}
		s.runMaintenance()
	}
}

// runMaintenance checks everything that depends on the passing of time.
func (s *Service) runMaintenance() {
	s.sweepFocus()
	s.sweepIdle()
	s.spares.check()
}

func (s *Service) kickMaintenance() {
	select {
	case s.maintenanceKick <- struct{}{}:
	default:
	}
}

type evictionCandidate struct {
	id    string
	r     runner
	state idleProcess
	since time.Time
}

// sweepIdle stops processes past their idle limit, then the least recently used
// ones over maxIdleProcesses, those that never served a turn first. It never
// rejects anything: running conversations do not count, and a send to an evicted
// conversation resumes it.
func (s *Service) sweepIdle() {
	now := s.opts.Now()
	s.mu.Lock()
	runners := make(map[string]runner, len(s.runners))
	for id, r := range s.runners {
		runners[id] = r
	}
	s.mu.Unlock()

	idle := 0
	var candidates []evictionCandidate
	for id, r := range runners {
		state, ok := r.IdleProcess()
		if !ok {
			continue
		}
		idle++
		if !state.evictable || s.pinned(id, state) {
			continue
		}
		since := state.since
		s.focus.mu.Lock()
		if ended := s.focus.endedAt[id]; ended.After(since) {
			since = ended
		}
		s.focus.mu.Unlock()
		limit, reason := sentIdleLimit, timing.ReasonIdleSent
		if state.preheated {
			limit, reason = preheatedIdleLimit, timing.ReasonIdlePreheated
		}
		if now.Sub(since) >= limit {
			if s.evict(id, r, state, now.Sub(since), reason) {
				idle--
			}
			continue
		}
		candidates = append(candidates, evictionCandidate{id: id, r: r, state: state, since: since})
	}
	if idle <= maxIdleProcesses {
		return
	}
	slices.SortFunc(candidates, func(a, b evictionCandidate) int {
		if a.state.preheated != b.state.preheated {
			if a.state.preheated {
				return -1
			}
			return 1
		}
		return a.since.Compare(b.since)
	})
	for _, c := range candidates {
		if idle <= maxIdleProcesses {
			return
		}
		if s.evict(c.id, c.r, c.state, now.Sub(c.since), timing.ReasonOverLimit) {
			idle--
		}
	}
}

// pinned reports whether a process must stay although it is idle: someone is
// looking at the conversation, a question waits for an answer, or its Computer
// Use runtime is alive, whose variables releasing the process would discard.
func (s *Service) pinned(id string, state idleProcess) bool {
	s.focus.mu.Lock()
	focused := s.focus.conversations[id] != nil
	s.focus.mu.Unlock()
	if focused || s.computer.Alive(fmt.Sprintf("%d/%s", state.scopeID, id)) {
		return true
	}
	s.questions.mu.Lock()
	defer s.questions.mu.Unlock()
	for _, q := range s.questions.waiting {
		if q.conversationID == id {
			return true
		}
	}
	return false
}

func (s *Service) evict(id string, r runner, state idleProcess, idleFor time.Duration, reason string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if !r.Evict(ctx) {
		return false
	}
	s.traces.process(id, state.provider).MarkDuration(timing.StageRunnerEvicted, reason, idleFor)
	s.opts.Logger.Info("local conversations: stopped an idle process", "conversation_id", id, "reason", reason)
	return true
}
