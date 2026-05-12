package daemon

import (
	"testing"
	"time"
)

func TestWatchdogSkipBeforeFirstHeartbeat(t *testing.T) {
	d := &Daemon{}

	if d.stalledGap(time.Second) {
		t.Fatal("watchdog flagged stall before first heartbeat")
	}
}

func TestWatchdogDetectsStall(t *testing.T) {
	interval := 10 * time.Second
	d := &Daemon{}
	d.lastHeartbeatAt.Store(time.Now().Add(-3 * interval).UnixNano())

	if !d.stalledGap(2 * interval) {
		t.Fatal("watchdog did not flag stale heartbeat")
	}
}

func TestLastHeartbeatAtStampedOnWSAck(t *testing.T) {
	d := &Daemon{
		wsHBLastAck: make(map[string]time.Time),
	}

	d.recordWSHeartbeatAck("rid-1")

	if d.lastHeartbeatAt.Load() == 0 {
		t.Fatal("expected WS heartbeat ack to stamp lastHeartbeatAt")
	}
}
