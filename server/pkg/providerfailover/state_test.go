package providerfailover

import "testing"

func TestOwningStates(t *testing.T) {
	t.Parallel()
	owning := map[HandoffState]bool{
		StatePending:    true,
		StateDispatched: true,
		StateCompleted:  true,
		StateFailed:     false,
		StateShadow:     false,
		StateDeclined:   false,
	}
	for s, want := range owning {
		if got := s.IsOwning(); got != want {
			t.Errorf("%s.IsOwning() = %v, want %v", s, got, want)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	t.Parallel()
	terminal := map[HandoffState]bool{
		StatePending:    false,
		StateDispatched: false,
		StateCompleted:  true,
		StateFailed:     true,
		StateShadow:     true,
		StateDeclined:   true,
	}
	for s, want := range terminal {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", s, got, want)
		}
	}
}

func TestTransitions(t *testing.T) {
	t.Parallel()
	legal := [][2]HandoffState{
		{StatePending, StateDispatched},
		{StatePending, StateFailed},
		{StateDispatched, StateCompleted},
		{StateDispatched, StateFailed},
	}
	for _, tr := range legal {
		if !CanTransition(tr[0], tr[1]) {
			t.Errorf("CanTransition(%s, %s) = false, want true", tr[0], tr[1])
		}
	}
	illegal := [][2]HandoffState{
		{StateShadow, StatePending},
		{StateDeclined, StateDispatched},
		{StateFailed, StateCompleted},
		{StateCompleted, StateDispatched},
		{StatePending, StatePending},
	}
	for _, tr := range illegal {
		if CanTransition(tr[0], tr[1]) {
			t.Errorf("CanTransition(%s, %s) = true, want false", tr[0], tr[1])
		}
	}
}
