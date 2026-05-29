// Package terminal — daemon_bridge.go wires the daemonws hub to the
// terminal broker. Daemon→server frames named cerebro:term_* turn into
// broker session lifecycle calls (Adopt / PushOutput / Exit) so the
// browser-facing AttachWS handler sees the same stream the agent is
// producing on the user's machine.
//
// The bridge is read-mostly today: v1 is display-only and never forwards
// stdin back to the daemon. The StdinChan plumbing is wired so v2 can
// flip on writes without re-touching this surface — a goroutine per
// adopted session drains the broker's stdin channel into outbound
// cerebro:term_stdin frames over the daemonws hub.
package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Frame-type constants for daemon ↔ server interactive-terminal messages.
// These live in the cerebro zone (rather than protocol/events.go) because
// the feature is cerebro-only — upstream multica neither produces nor
// consumes them.
const (
	FrameTermAttach = "cerebro:term_attach"
	FrameTermStdout = "cerebro:term_stdout"
	FrameTermStdin  = "cerebro:term_stdin"
	FrameTermExit   = "cerebro:term_exit"
)

// PresentationModeQuerier is the subset of the cerebro sqlc Queries needed
// to verify a runtime is opted in to interactive presentation. Defined as
// an interface so tests can stub it without standing up Postgres.
type PresentationModeQuerier interface {
	GetAgentRuntimePresentationMode(ctx context.Context, id pgtype.UUID) (string, error)
}

// hubWriter is the subset of *daemonws.Hub the bridge uses to send frames
// back to a daemon. Mirrored as an interface so tests can record sends.
type hubWriter interface {
	DeliverDaemonRuntime(scopeID string, frame []byte, eventID string)
}

// DaemonBridge translates daemon-originated terminal frames into broker
// session lifecycle events. One instance per server process; lookup of an
// adopted session by (runtime_id, task_id) is in-memory.
type DaemonBridge struct {
	broker  *Broker
	hub     hubWriter
	queries PresentationModeQuerier

	mu       sync.Mutex
	adopted  map[string]*Session // key: bridgeKey(runtimeID, taskID)
	stdinWG  sync.WaitGroup      // tracks per-session stdin pumps for test/shutdown
	logger   *slog.Logger
}

// NewDaemonBridge wires the broker, hub, and queries together. queries may
// be nil — in that case the bridge logs and drops every attach (used in
// tests that don't exercise gating).
func NewDaemonBridge(broker *Broker, hub *daemonws.Hub, queries PresentationModeQuerier) *DaemonBridge {
	return newDaemonBridge(broker, hub, queries)
}

// newDaemonBridge accepts the interface form for tests.
func newDaemonBridge(broker *Broker, hub hubWriter, queries PresentationModeQuerier) *DaemonBridge {
	return &DaemonBridge{
		broker:  broker,
		hub:     hub,
		queries: queries,
		adopted: make(map[string]*Session),
		logger:  slog.Default(),
	}
}

func bridgeKey(runtimeID, taskID string) string {
	return runtimeID + "|" + taskID
}

// HandleMessage is the daemonws.MessageHandler entry point. Any frame
// whose Type does not start with the cerebro:term_ prefix is ignored
// (returns nil) so other cerebro features can co-exist on the same seam.
func (b *DaemonBridge) HandleMessage(ctx context.Context, identity daemonws.ClientIdentity, msg protocol.Message) error {
	switch msg.Type {
	case FrameTermAttach:
		return b.handleAttach(ctx, identity, msg.Payload)
	case FrameTermStdout:
		return b.handleStdout(ctx, identity, msg.Payload)
	case FrameTermExit:
		return b.handleExit(ctx, identity, msg.Payload)
	default:
		// Not ours — let any future handler decide what to do.
		return nil
	}
}

// ----- term_attach -----

type termAttachPayload struct {
	TaskID    string `json:"task_id"`
	RuntimeID string `json:"runtime_id"`
	Command   string `json:"command,omitempty"`
}

func (b *DaemonBridge) handleAttach(ctx context.Context, identity daemonws.ClientIdentity, raw json.RawMessage) error {
	var p termAttachPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("term_attach: invalid payload: %w", err)
	}
	if p.RuntimeID == "" || p.TaskID == "" {
		return errors.New("term_attach: runtime_id and task_id required")
	}

	// Authorize the attach against the daemon's connection scope: the
	// runtime_id must be one of identity.RuntimeIDs. The daemonws hub
	// already validates this on heartbeat; we mirror the check so a
	// malformed daemon build can't tee output from a runtime it isn't
	// connected for.
	if !runtimeInIdentity(identity, p.RuntimeID) {
		return fmt.Errorf("term_attach: runtime %s not in identity scope", p.RuntimeID)
	}

	if b.queries != nil {
		mode, err := b.queries.GetAgentRuntimePresentationMode(ctx, parsePgUUID(p.RuntimeID))
		if err != nil {
			b.logger.Debug("term_attach: presentation_mode lookup failed", "runtime_id", p.RuntimeID, "error", err)
			return nil // no-op: don't blow up the daemon WS pump on a transient DB miss
		}
		if mode != "interactive" {
			b.logger.Debug("term_attach: runtime not interactive; ignoring", "runtime_id", p.RuntimeID, "mode", mode)
			return nil
		}
	}

	key := bridgeKey(p.RuntimeID, p.TaskID)
	b.mu.Lock()
	if existing, ok := b.adopted[key]; ok && !existing.closed.Load() {
		b.mu.Unlock()
		b.logger.Debug("term_attach: session already adopted; ignoring duplicate", "runtime_id", p.RuntimeID, "task_id", p.TaskID, "session_id", existing.ID)
		return nil
	}

	s, err := b.broker.Adopt(AdoptOptions{
		RuntimeID:   p.RuntimeID,
		WorkspaceID: identity.WorkspaceID,
		OwnerUserID: identity.UserID,
		TaskID:      p.TaskID,
		Command:     p.Command,
	})
	if err != nil {
		b.mu.Unlock()
		return fmt.Errorf("term_attach: broker adopt failed: %w", err)
	}
	b.adopted[key] = s
	b.mu.Unlock()

	b.logger.Info("term_attach: session adopted",
		"runtime_id", p.RuntimeID,
		"task_id", p.TaskID,
		"session_id", s.ID,
		"command", p.Command,
	)

	// Wire the stdin pump. v1 is read-only, but the channel still has to
	// be drained — leaving it unconsumed would block any browser-side
	// `stdin` frame at the broker's Attach write hook.
	b.stdinWG.Add(1)
	go b.runStdinPump(s, p.RuntimeID, p.TaskID)
	return nil
}

func (b *DaemonBridge) runStdinPump(s *Session, runtimeID, taskID string) {
	defer b.stdinWG.Done()
	ch := s.StdinChan()
	if ch == nil {
		return
	}
	for data := range ch {
		frame, err := marshalCerebroFrame(FrameTermStdin, termStdinPayload{
			TaskID: taskID,
			Data:   base64.StdEncoding.EncodeToString(data),
		})
		if err != nil {
			b.logger.Debug("term_stdin: marshal failed", "error", err, "runtime_id", runtimeID, "task_id", taskID)
			continue
		}
		b.hub.DeliverDaemonRuntime(runtimeID, frame, "")
	}
}

type termStdinPayload struct {
	TaskID string `json:"task_id"`
	Data   string `json:"data"`
}

// ----- term_stdout -----

type termStdoutPayload struct {
	TaskID string `json:"task_id"`
	Data   string `json:"data"`
}

func (b *DaemonBridge) handleStdout(_ context.Context, identity daemonws.ClientIdentity, raw json.RawMessage) error {
	var p termStdoutPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("term_stdout: invalid payload: %w", err)
	}
	if p.TaskID == "" {
		return errors.New("term_stdout: task_id required")
	}
	runtimeID := lookupRuntimeForTask(b, identity, p.TaskID)
	if runtimeID == "" {
		b.logger.Debug("term_stdout: no adopted session for task; dropping", "task_id", p.TaskID)
		return nil
	}
	s := b.lookupSession(runtimeID, p.TaskID)
	if s == nil {
		b.logger.Debug("term_stdout: no adopted session for (runtime, task); dropping", "runtime_id", runtimeID, "task_id", p.TaskID)
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return fmt.Errorf("term_stdout: base64 decode: %w", err)
	}
	s.PushOutput(decoded)
	return nil
}

// ----- term_exit -----

type termExitPayload struct {
	TaskID string `json:"task_id"`
	Code   int    `json:"code"`
	Error  string `json:"error,omitempty"`
}

func (b *DaemonBridge) handleExit(_ context.Context, identity daemonws.ClientIdentity, raw json.RawMessage) error {
	var p termExitPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("term_exit: invalid payload: %w", err)
	}
	if p.TaskID == "" {
		return errors.New("term_exit: task_id required")
	}
	runtimeID := lookupRuntimeForTask(b, identity, p.TaskID)
	if runtimeID == "" {
		b.logger.Debug("term_exit: no adopted session for task; dropping", "task_id", p.TaskID)
		return nil
	}
	s := b.removeSession(runtimeID, p.TaskID)
	if s == nil {
		return nil
	}
	var exitErr error
	if p.Error != "" {
		exitErr = errors.New(p.Error)
	}
	s.Exit(p.Code, exitErr)
	b.logger.Info("term_exit: session closed", "runtime_id", runtimeID, "task_id", p.TaskID, "session_id", s.ID, "code", p.Code)
	return nil
}

// ----- helpers -----

func (b *DaemonBridge) lookupSession(runtimeID, taskID string) *Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.adopted[bridgeKey(runtimeID, taskID)]
}

func (b *DaemonBridge) removeSession(runtimeID, taskID string) *Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := bridgeKey(runtimeID, taskID)
	s := b.adopted[key]
	delete(b.adopted, key)
	return s
}

// lookupRuntimeForTask walks the connection's authorized runtime set and
// returns the runtime ID whose (runtime, task) pair is currently adopted.
// Daemons can hold multiple runtimes per connection, so the task ID alone
// is not unique — the bridge map key is (runtime, task).
func lookupRuntimeForTask(b *DaemonBridge, identity daemonws.ClientIdentity, taskID string) string {
	for _, rid := range identity.RuntimeIDs {
		if rid == "" {
			continue
		}
		if b.lookupSession(rid, taskID) != nil {
			return rid
		}
	}
	return ""
}

func runtimeInIdentity(identity daemonws.ClientIdentity, runtimeID string) bool {
	for _, rid := range identity.RuntimeIDs {
		if rid == runtimeID {
			return true
		}
	}
	return false
}

// parsePgUUID is a best-effort string→pgtype.UUID. Invalid input becomes a
// zero UUID; downstream queries return ErrNoRows in that case, which the
// caller treats as "skip / not interactive".
func parsePgUUID(s string) pgtype.UUID {
	var out pgtype.UUID
	if err := out.Scan(s); err != nil {
		return pgtype.UUID{}
	}
	return out
}

func marshalCerebroFrame(frameType string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(protocol.Message{Type: frameType, Payload: raw})
}
