package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeHub records DeliverDaemonRuntime calls so tests can assert outgoing
// term_stdin frames. The bridge only invokes the one method, so an
// interface-level fake is enough.
type fakeHub struct {
	mu     sync.Mutex
	frames []fakeFrame
}

type fakeFrame struct {
	scopeID string
	frame   []byte
}

func (f *fakeHub) DeliverDaemonRuntime(scopeID string, frame []byte, _ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, fakeFrame{scopeID: scopeID, frame: append([]byte(nil), frame...)})
}

func (f *fakeHub) seen() []fakeFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeFrame(nil), f.frames...)
}

// fakeQ stubs the presentation-mode lookup. By default returns
// "interactive" so tests don't have to set it.
type fakeQ struct {
	mode string
	err  error
}

func (f *fakeQ) GetAgentRuntimePresentationMode(_ context.Context, _ pgtype.UUID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	mode := f.mode
	if mode == "" {
		mode = "interactive"
	}
	return mode, nil
}

func msgWith(t *testing.T, frameType string, payload any) protocol.Message {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return protocol.Message{Type: frameType, Payload: raw}
}

// TestBridge_AttachThenStdoutThenExit drives the full happy path. After an
// attach frame, a matching stdout frame must reach the broker; the exit
// frame must close the broker session and clean up the bridge state.
func TestBridge_AttachThenStdoutThenExit(t *testing.T) {
	broker := NewBroker()
	hub := &fakeHub{}
	bridge := newDaemonBridge(broker, hub, &fakeQ{mode: "interactive"})

	identity := daemonws.ClientIdentity{
		DaemonID:    "d-1",
		UserID:      "u-1",
		WorkspaceID: "ws-1",
		RuntimeIDs:  []string{"00000000-0000-0000-0000-0000000000aa"},
	}
	runtimeID := identity.RuntimeIDs[0]
	taskID := "task-bridge-1"

	// attach
	err := bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermAttach, map[string]any{
		"task_id":    taskID,
		"runtime_id": runtimeID,
		"command":    "claude",
	}))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	session := broker.GetByRuntime(runtimeID)
	if session == nil {
		t.Fatal("expected GetByRuntime to find an adopted session after attach")
	}
	if !session.External() {
		t.Errorf("expected adopted session, got internal-process")
	}

	// subscribe before stdout so we can observe the chunk
	sub, _, detach := session.Attach()
	t.Cleanup(detach)

	// stdout
	err = bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermStdout, map[string]any{
		"task_id": taskID,
		"data":    base64.StdEncoding.EncodeToString([]byte("agent output\n")),
	}))
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	got := readChunks(t, sub, 1, 2*time.Second)
	if string(got) != "agent output\n" {
		t.Errorf("subscriber got %q, want %q", got, "agent output\n")
	}

	// exit
	err = bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermExit, map[string]any{
		"task_id": taskID,
		"code":    0,
	}))
	if err != nil {
		t.Fatalf("exit: %v", err)
	}
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session not closed after exit frame")
	}
	if got := broker.GetByRuntime(runtimeID); got != nil {
		t.Errorf("GetByRuntime returned %v after exit; want nil", got)
	}
}

// TestBridge_StdoutWithoutAttachIsDropped proves the bridge tolerates a
// stale stdout frame (e.g. arriving after the daemon already sent exit
// for the same task). No panic, no broker side-effect.
func TestBridge_StdoutWithoutAttachIsDropped(t *testing.T) {
	broker := NewBroker()
	bridge := newDaemonBridge(broker, &fakeHub{}, &fakeQ{mode: "interactive"})
	identity := daemonws.ClientIdentity{
		RuntimeIDs: []string{"00000000-0000-0000-0000-0000000000bb"},
	}
	err := bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermStdout, map[string]any{
		"task_id": "task-missing",
		"data":    base64.StdEncoding.EncodeToString([]byte("nope")),
	}))
	if err != nil {
		t.Fatalf("stdout without attach should be silent, got: %v", err)
	}
}

// TestBridge_NonInteractiveRuntimeIsIgnored proves the bridge respects the
// runtime's presentation_mode and never spawns a broker session for a
// headless runtime even if a daemon mistakenly sends term_attach.
func TestBridge_NonInteractiveRuntimeIsIgnored(t *testing.T) {
	broker := NewBroker()
	bridge := newDaemonBridge(broker, &fakeHub{}, &fakeQ{mode: "headless"})
	identity := daemonws.ClientIdentity{
		RuntimeIDs: []string{"00000000-0000-0000-0000-0000000000cc"},
	}
	err := bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermAttach, map[string]any{
		"task_id":    "task-headless",
		"runtime_id": identity.RuntimeIDs[0],
	}))
	if err != nil {
		t.Fatalf("attach should be a silent no-op for headless: %v", err)
	}
	if got := broker.GetByRuntime(identity.RuntimeIDs[0]); got != nil {
		t.Errorf("expected no broker session for headless runtime; got %v", got)
	}
}

// TestBridge_UnknownFrameTypeIsPassthrough proves the bridge stays out of
// the way of non-cerebro:term_ frames so other handlers can co-exist on
// the same MessageHandler seam.
func TestBridge_UnknownFrameTypeIsPassthrough(t *testing.T) {
	bridge := newDaemonBridge(NewBroker(), &fakeHub{}, &fakeQ{mode: "interactive"})
	err := bridge.HandleMessage(context.Background(), daemonws.ClientIdentity{}, protocol.Message{Type: "other:event", Payload: []byte(`{}`)})
	if err != nil {
		t.Errorf("unknown frame should return nil, got %v", err)
	}
}

// TestBridge_AttachRejectsRuntimeNotInIdentity proves a daemon can't tee
// output from a runtime its connection isn't authorized for.
func TestBridge_AttachRejectsRuntimeNotInIdentity(t *testing.T) {
	broker := NewBroker()
	bridge := newDaemonBridge(broker, &fakeHub{}, &fakeQ{mode: "interactive"})
	identity := daemonws.ClientIdentity{
		RuntimeIDs: []string{"00000000-0000-0000-0000-0000000000dd"},
	}
	err := bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermAttach, map[string]any{
		"task_id":    "task-x",
		"runtime_id": "00000000-0000-0000-0000-0000000000ee", // not in identity
	}))
	if err == nil {
		t.Fatal("expected error when runtime is outside identity scope")
	}
	if got := broker.GetByRuntime("00000000-0000-0000-0000-0000000000ee"); got != nil {
		t.Errorf("expected no broker session for unauthorized runtime; got %v", got)
	}
}

// attachFor is a small helper for the disconnect tests below.
func attachFor(t *testing.T, bridge *DaemonBridge, identity daemonws.ClientIdentity, taskID string) {
	t.Helper()
	if err := bridge.HandleMessage(context.Background(), identity, msgWith(t, FrameTermAttach, map[string]any{
		"task_id":    taskID,
		"runtime_id": identity.RuntimeIDs[0],
	})); err != nil {
		t.Fatalf("attach %s: %v", taskID, err)
	}
}

// TestBridge_DisconnectClosesSessionAfterGrace proves a truly-dead daemon's
// adopted session is closed once the grace elapses with no reconnect.
func TestBridge_DisconnectClosesSessionAfterGrace(t *testing.T) {
	broker := NewBroker()
	bridge := newDaemonBridge(broker, &fakeHub{}, &fakeQ{mode: "interactive"})
	bridge.disconnectGrace = 50 * time.Millisecond
	identity := daemonws.ClientIdentity{
		WorkspaceID: "ws-1",
		RuntimeIDs:  []string{"00000000-0000-0000-0000-0000000000aa"},
	}
	attachFor(t, bridge, identity, "task-dc-1")
	session := broker.GetByRuntime(identity.RuntimeIDs[0])
	if session == nil {
		t.Fatal("expected adopted session before disconnect")
	}

	bridge.OnDaemonDisconnect(identity)
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session not closed after grace elapsed without reconnect")
	}
}

// TestBridge_ReconnectDuringGraceKeepsSession is the TECH-3388 regression: the
// daemon WS flaps and re-announces the task within the grace window. The live
// session must survive (the duplicate attach bumps reattachGen, so the reaper
// skips the close).
func TestBridge_ReconnectDuringGraceKeepsSession(t *testing.T) {
	broker := NewBroker()
	bridge := newDaemonBridge(broker, &fakeHub{}, &fakeQ{mode: "interactive"})
	bridge.disconnectGrace = 200 * time.Millisecond
	identity := daemonws.ClientIdentity{
		WorkspaceID: "ws-1",
		RuntimeIDs:  []string{"00000000-0000-0000-0000-0000000000bb"},
	}
	attachFor(t, bridge, identity, "task-rc-1")
	session := broker.GetByRuntime(identity.RuntimeIDs[0])
	if session == nil {
		t.Fatal("expected adopted session before disconnect")
	}

	// Daemon drops, then reconnects (re-announces the same task) before grace.
	bridge.OnDaemonDisconnect(identity)
	time.Sleep(40 * time.Millisecond)
	attachFor(t, bridge, identity, "task-rc-1") // duplicate attach = reconnect

	// Wait past the original grace; the session must still be live.
	time.Sleep(300 * time.Millisecond)
	if session.closed.Load() {
		t.Fatal("session was closed despite a reconnect within the grace window")
	}
	if got := broker.GetByRuntime(identity.RuntimeIDs[0]); got != session {
		t.Fatal("expected the same live session to remain adopted after reconnect")
	}
}
