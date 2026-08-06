package wecom

import (
	"testing"
	"time"
)

func TestOutboundWakeRegisterReturnsReceivableChannel(t *testing.T) {
	r := NewOutboundWakeRegistry()
	ch := r.Register("inst-1")

	r.Wake("inst-1")

	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected wake on registered channel")
	}
}

func TestOutboundWakeRegisterIsIdempotent(t *testing.T) {
	r := NewOutboundWakeRegistry()
	ch1 := r.Register("inst-1")
	ch2 := r.Register("inst-1")
	if ch1 != ch2 {
		t.Fatal("expected repeated Register to return the same channel")
	}
}

func TestOutboundWakeCoalesce(t *testing.T) {
	r := NewOutboundWakeRegistry()
	ch := r.Register("inst-1")

	r.Wake("inst-1")
	r.Wake("inst-1")

	select {
	case <-ch:
	default:
		t.Fatal("expected first coalesced wake")
	}

	select {
	case <-ch:
		t.Fatal("expected only one wake after coalescing")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOutboundWakeMissingIDIsNoOp(t *testing.T) {
	r := NewOutboundWakeRegistry()
	r.Wake("missing")
}

func TestOutboundWakeUnregisterThenWakeIsNoOp(t *testing.T) {
	r := NewOutboundWakeRegistry()
	ch := r.Register("inst-1")
	r.Unregister("inst-1")
	r.Wake("inst-1")

	select {
	case <-ch:
		t.Fatal("expected no wake after unregister")
	case <-time.After(50 * time.Millisecond):
	}
}
