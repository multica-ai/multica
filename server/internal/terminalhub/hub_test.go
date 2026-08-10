package terminalhub

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/terminalproto"
)

func TestHubCoalescesTinyReplayFramesWithinBoundedPeerQueue(t *testing.T) {
	h := New(Options{RelayRingBytes: 2 * 1024 * 1024})
	daemon := NewPeer("daemon", "", []string{"runtime"})
	sessionID := uuid.New()
	if _, err := h.RegisterSession(daemon, terminalproto.Message{
		SessionID: sessionID.String(), TaskID: "task", WorkspaceID: "workspace",
		RuntimeID: "runtime", Generation: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= DefaultPeerQueue*4; sequence++ {
		raw, err := terminalproto.EncodeBinary(terminalproto.KindOutput, sessionID, sequence, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		if err := h.PublishDaemonBinary(daemon, raw); err != nil {
			t.Fatal(err)
		}
	}

	browser := NewPeer("browser", "user", nil)
	if _, err := h.AttachBrowser("task", browser, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.Done():
		t.Fatal("healthy replay overflowed the bounded browser queue")
	default:
	}

	var replay terminalproto.BinaryFrame
	for len(browser.Send) > 0 {
		item := <-browser.Send
		if item.MessageType != websocket.BinaryMessage {
			continue
		}
		frame, err := terminalproto.DecodeBinary(item.Data)
		if err != nil {
			t.Fatal(err)
		}
		replay = frame
	}
	if replay.Sequence != uint64(DefaultPeerQueue*4) {
		t.Fatalf("replay sequence = %d, want %d", replay.Sequence, DefaultPeerQueue*4)
	}
	if !bytes.Equal(replay.Payload, bytes.Repeat([]byte("x"), DefaultPeerQueue*4)) {
		t.Fatalf("replay payload bytes = %d, want %d", len(replay.Payload), DefaultPeerQueue*4)
	}
}

func TestHubReplayGapAndControllerLease(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	h := New(Options{RelayRingBytes: 5, LeaseDuration: 30 * time.Second, Now: func() time.Time { return now }})
	daemon := NewPeer("daemon", "", []string{"runtime"})
	h.RegisterDaemon(daemon)
	sessionID := uuid.New()
	meta, err := h.RegisterSession(daemon, terminalproto.Message{Type: "session", SessionID: sessionID.String(), TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime", Provider: "codex", Generation: 1, Cols: 120, Rows: 32})
	if err != nil || !meta.Available {
		t.Fatalf("register = %#v, %v", meta, err)
	}
	for seq, payload := range []string{"aaa", "bbb", "ccc"} {
		raw, err := terminalproto.EncodeBinary(terminalproto.KindOutput, sessionID, uint64(seq+1), []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if err := h.PublishDaemonBinary(daemon, raw); err != nil {
			t.Fatal(err)
		}
	}

	controller := NewPeer("browser-1", "user-1", nil)
	if _, err := h.AttachBrowser("task", controller, 0); err != nil {
		t.Fatal(err)
	}
	seenGap := false
	seenReplay := 0
	for i := 0; i < 4; i++ {
		select {
		case item := <-controller.Send:
			if item.MessageType == websocket.BinaryMessage {
				seenReplay++
			} else if string(item.Data) != "" && bytesContains(item.Data, []byte(`"type":"gap"`)) {
				seenGap = true
			}
		default:
		}
	}
	if !seenGap || seenReplay == 0 {
		t.Fatalf("gap=%v replay=%d", seenGap, seenReplay)
	}

	token, _, err := h.ClaimControl(sessionID, controller)
	if err != nil || token == "" {
		t.Fatalf("claim = %q, %v", token, err)
	}
	observer := NewPeer("browser-2", "user-2", nil)
	if _, err := h.AttachBrowser("task", observer, 3); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.ClaimControl(sessionID, observer); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("observer claim error = %v", err)
	}
	input, _ := terminalproto.EncodeBinary(terminalproto.KindInput, sessionID, 99, []byte("hello"))
	if err := h.ForwardBrowserBinary(observer, input); !errors.Is(err, ErrNotController) {
		t.Fatalf("observer input error = %v", err)
	}
	if err := h.ForwardBrowserBinary(controller, input); err != nil {
		t.Fatal(err)
	}
	if item := <-daemon.Send; item.MessageType != websocket.BinaryMessage {
		t.Fatalf("daemon message type = %d", item.MessageType)
	}

	now = now.Add(31 * time.Second)
	if _, _, err := h.ClaimControl(sessionID, observer); err != nil {
		t.Fatalf("claim after expiry: %v", err)
	}
}

func TestHubRejectsCrossRuntimeSession(t *testing.T) {
	h := New(Options{})
	daemon := NewPeer("daemon", "", []string{"runtime-a"})
	_, err := h.RegisterSession(daemon, terminalproto.Message{SessionID: uuid.NewString(), TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime-b"})
	if !errors.Is(err, ErrInvalidPeer) {
		t.Fatalf("error = %v", err)
	}
}

func TestHubRejectsDuplicateAndOlderGenerations(t *testing.T) {
	h := New(Options{})
	daemon := NewPeer("daemon", "", []string{"runtime"})
	register := func(sessionID uuid.UUID, generation int) error {
		_, err := h.RegisterSession(daemon, terminalproto.Message{
			SessionID: sessionID.String(), TaskID: "task", WorkspaceID: "workspace",
			RuntimeID: "runtime", Generation: generation,
		})
		return err
	}
	activeID := uuid.New()
	if err := register(activeID, 2); err != nil {
		t.Fatal(err)
	}
	if err := register(uuid.New(), 2); !errors.Is(err, ErrGeneration) {
		t.Fatalf("duplicate generation error = %v", err)
	}
	if err := register(uuid.New(), 1); !errors.Is(err, ErrGeneration) {
		t.Fatalf("older generation error = %v", err)
	}
	if err := register(uuid.New(), 3); err != nil {
		t.Fatalf("newer generation = %v", err)
	}
}

func TestHubDisconnectReleasesController(t *testing.T) {
	h := New(Options{})
	daemon := NewPeer("daemon", "", []string{"runtime"})
	sessionID := uuid.New()
	if _, err := h.RegisterSession(daemon, terminalproto.Message{
		SessionID: sessionID.String(), TaskID: "task", WorkspaceID: "workspace",
		RuntimeID: "runtime", Generation: 1,
	}); err != nil {
		t.Fatal(err)
	}
	controller := NewPeer("controller", "user-1", nil)
	observer := NewPeer("observer", "user-2", nil)
	if _, err := h.AttachBrowser("task", controller, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AttachBrowser("task", observer, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.ClaimControl(sessionID, controller); err != nil {
		t.Fatal(err)
	}
	h.UnregisterPeer(controller)
	if _, _, err := h.ClaimControl(sessionID, observer); err != nil {
		t.Fatalf("observer could not claim after disconnect: %v", err)
	}
}

func TestPeerSlowQueueClosesWithoutBlocking(t *testing.T) {
	peer := NewPeer("slow", "user", nil)
	for i := 0; i < DefaultPeerQueue; i++ {
		if !peer.Enqueue(websocket.BinaryMessage, []byte("x")) {
			t.Fatalf("queue closed at %d", i)
		}
	}
	if peer.Enqueue(websocket.BinaryMessage, []byte("overflow")) {
		t.Fatal("overflowing slow peer queue succeeded")
	}
	select {
	case <-peer.Done():
	default:
		t.Fatal("slow peer was not disconnected")
	}
}

func TestHubBoundsBrowserConnectionsPerSession(t *testing.T) {
	h := New(Options{})
	daemon := NewPeer("daemon", "", []string{"runtime"})
	if _, err := h.RegisterSession(daemon, terminalproto.Message{
		SessionID: uuid.NewString(), TaskID: "task", WorkspaceID: "workspace",
		RuntimeID: "runtime", Generation: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < DefaultBrowserLimit; i++ {
		if _, err := h.AttachBrowser("task", NewPeer(fmt.Sprintf("browser-%d", i), "user", nil), 0); err != nil {
			t.Fatalf("attach browser %d: %v", i, err)
		}
	}
	if _, err := h.AttachBrowser("task", NewPeer("overflow", "user", nil), 0); !errors.Is(err, ErrBrowserLimit) {
		t.Fatalf("overflow attach error = %v", err)
	}
}

func bytesContains(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
