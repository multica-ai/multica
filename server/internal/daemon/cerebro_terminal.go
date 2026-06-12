// CEREBRO-PATCH(daemon-cerebro-terminal): interactive terminal mirror.
//
// When the server claims a task whose runtime has presentation_mode =
// "interactive", the daemon installs a tee on the agent message stream and
// forwards a textual render to the server over the existing daemonws
// connection. The server's DaemonBridge translates those frames into
// broker session activity, and any browser attached to the broker session
// sees the agent's progress live.
//
// v1 is one-way: stdin frames from the browser are dropped at the daemon
// (logged and ignored). The wire protocol already reserves the slot.
package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Cerebro terminal frame type constants. Kept in sync with the server-side
// constants in server/internal/cerebro/terminal/daemon_bridge.go.
const (
	frameTermAttach = "cerebro:term_attach"
	frameTermStdout = "cerebro:term_stdout"
	frameTermExit   = "cerebro:term_exit"

	cerebroTermFlushInterval = 100 * time.Millisecond
	cerebroTermFlushBytes    = 8 * 1024

	// CEREBRO-PATCH(daemon-cerebro-term-buffer): cap the per-task output backlog
	// kept while the WS is down. The wakeup WS flaps every ~30-90 min through
	// Cloudflare (close 1001 "CloudFlare WebSocket proxy restarting"); without a
	// backlog, all agent output produced during the ~30s reconnect gap was
	// dropped and the live terminal went blank. We retain up to this many bytes
	// per task and flush them, in order, once the WS reconnects. The bound stops
	// a chatty agent during a prolonged outage from growing daemon memory without
	// limit — oldest frames are dropped first (TECH-3388).
	cerebroTermBufferMaxBytes = 512 * 1024
)

// enqueueCerebroFrame writes a frame to the daemonws writes channel if a
// connection is live. Non-blocking — a stalled writer must not back up
// agent execution. Returns true if the frame was queued.
func (d *Daemon) enqueueCerebroFrame(frame []byte) bool {
	ch := d.wsWrites.Load()
	if ch == nil {
		return false
	}
	select {
	case *ch <- frame:
		return true
	default:
		// Writer is backed up; drop the frame. The browser sees a gap in
		// the live stream rather than a daemon stall.
		return false
	}
}

// CEREBRO-PATCH(daemon-cerebro-term-tee): per-task terminal sink + attach-frame registry helpers (TECH-3388).
//
// registerCerebroTermSink installs the per-task tee callback. Each interactive
// task owns its own sink keyed by taskID, so concurrent tasks (across slots or
// runtimes) on the same daemon process each mirror to their own broker session
// instead of clobbering a single shared sink (TECH-3388).
func (d *Daemon) registerCerebroTermSink(taskID string, fn func(text string)) {
	d.cerebroTermSinkMu.Lock()
	d.cerebroTermSinks[taskID] = fn
	d.cerebroTermSinkMu.Unlock()
}

// unregisterCerebroTermSink removes one task's sink. Other tasks' sinks are
// untouched, so a finishing task no longer tears down mirroring for the rest.
func (d *Daemon) unregisterCerebroTermSink(taskID string) {
	d.cerebroTermSinkMu.Lock()
	delete(d.cerebroTermSinks, taskID)
	d.cerebroTermSinkMu.Unlock()
}

// dispatchCerebroTerm forwards a rendered message to the named task's sink, if
// one is registered. Called from executeAndDrain on the hot path.
func (d *Daemon) dispatchCerebroTerm(taskID, text string) {
	if text == "" {
		return
	}
	d.cerebroTermSinkMu.RLock()
	fn := d.cerebroTermSinks[taskID]
	d.cerebroTermSinkMu.RUnlock()
	if fn != nil {
		fn(text)
	}
}

// storeCerebroAttach records a task's term_attach frame so a WS reconnect can
// re-emit it. removeCerebroAttach drops it when the task ends.
func (d *Daemon) storeCerebroAttach(taskID string, frame []byte) {
	d.cerebroTermSinkMu.Lock()
	d.cerebroActiveAttaches[taskID] = frame
	d.cerebroTermSinkMu.Unlock()
}

func (d *Daemon) removeCerebroAttach(taskID string) {
	d.cerebroTermSinkMu.Lock()
	delete(d.cerebroActiveAttaches, taskID)
	d.cerebroTermSinkMu.Unlock()
}

// snapshotCerebroAttaches returns a copy of every in-flight attach frame, so
// runTaskWakeupConnection can re-announce all active interactive tasks when the
// WS reconnects (not just the most recent one).
func (d *Daemon) snapshotCerebroAttaches() [][]byte {
	d.cerebroTermSinkMu.RLock()
	defer d.cerebroTermSinkMu.RUnlock()
	out := make([][]byte, 0, len(d.cerebroActiveAttaches))
	for _, f := range d.cerebroActiveAttaches {
		out = append(out, f)
	}
	return out
}

// CEREBRO-PATCH(daemon-cerebro-term-buffer): retain term_stdout across WS flaps (TECH-3388).
//
// sendOrBufferTermStdout sends a term_stdout frame live when the WS is up and no
// backlog is pending; otherwise it appends the frame to the task's ordered
// backlog so output produced during a reconnect gap survives instead of being
// dropped. Strict ordering: once a backlog exists, every new frame queues behind
// it until drainCerebroTermBuffer empties it on reconnect, so the browser never
// sees newer output ahead of older.
func (d *Daemon) sendOrBufferTermStdout(taskID string, frame []byte) {
	d.cerebroTermSinkMu.Lock()
	defer d.cerebroTermSinkMu.Unlock()
	if len(d.cerebroTermBuffers[taskID]) == 0 && d.enqueueCerebroFrame(frame) {
		return
	}
	buf := append(d.cerebroTermBuffers[taskID], frame)
	d.cerebroTermBuffers[taskID] = trimCerebroTermBuffer(buf)
}

// drainCerebroTermBuffer flushes a task's buffered output, in order, stopping if
// the WS goes down again (the unsent tail stays buffered for the next reconnect).
// Called from the WS reconnect path after the attach frame is re-emitted.
func (d *Daemon) drainCerebroTermBuffer(taskID string) {
	d.cerebroTermSinkMu.Lock()
	defer d.cerebroTermSinkMu.Unlock()
	buf := d.cerebroTermBuffers[taskID]
	i := 0
	for ; i < len(buf); i++ {
		if !d.enqueueCerebroFrame(buf[i]) {
			break
		}
	}
	if i >= len(buf) {
		delete(d.cerebroTermBuffers, taskID)
	} else {
		d.cerebroTermBuffers[taskID] = buf[i:]
	}
}

// drainAllCerebroTermBuffers flushes every task's backlog on WS reconnect.
func (d *Daemon) drainAllCerebroTermBuffers() {
	d.cerebroTermSinkMu.RLock()
	ids := make([]string, 0, len(d.cerebroTermBuffers))
	for id := range d.cerebroTermBuffers {
		ids = append(ids, id)
	}
	d.cerebroTermSinkMu.RUnlock()
	for _, id := range ids {
		d.drainCerebroTermBuffer(id)
	}
}

// removeCerebroTermBuffer drops a finished task's backlog so it can't leak or be
// re-sent after the task ends.
func (d *Daemon) removeCerebroTermBuffer(taskID string) {
	d.cerebroTermSinkMu.Lock()
	delete(d.cerebroTermBuffers, taskID)
	d.cerebroTermSinkMu.Unlock()
}

// trimCerebroTermBuffer drops the oldest frames until the backlog fits under
// cerebroTermBufferMaxBytes. Dropping oldest-first keeps the most recent agent
// output, which is what a user reopening the terminal cares about most.
func trimCerebroTermBuffer(buf [][]byte) [][]byte {
	total := 0
	for _, f := range buf {
		total += len(f)
	}
	for total > cerebroTermBufferMaxBytes && len(buf) > 1 {
		total -= len(buf[0])
		buf = buf[1:]
	}
	return buf
}

// cerebroAttachTerminal emits cerebro:term_attach for the task and installs
// a buffered tee. The returned teardown emits cerebro:term_exit and clears
// the sink. Safe to call when the WS connection is down — the attach frame
// is just dropped and the sink is still installed (it'll be a no-op until
// reconnect; subsequent stdout will resume the stream cleanly).
func (d *Daemon) cerebroAttachTerminal(_ context.Context, task Task) func() {
	command := ""
	if task.Agent != nil {
		command = task.Agent.Name
	}
	attachFrame, err := marshalCerebroDaemonFrame(frameTermAttach, map[string]any{
		"task_id":    task.ID,
		"runtime_id": task.RuntimeID,
		"command":    command,
	})
	if err != nil {
		d.logger.Debug("cerebro term_attach marshal failed", "error", err, "task_id", task.ID)
	} else {
		// CEREBRO-PATCH(daemon-cerebro-term-reattach): persist frame so runTaskWakeupConnection can re-emit on WS connect.
		d.storeCerebroAttach(task.ID, attachFrame)
		if !d.enqueueCerebroFrame(attachFrame) {
			d.logger.Debug("cerebro term_attach dropped: ws not connected; will retry on reconnect", "task_id", task.ID)
		}
	}

	pump := newCerebroTermPump(d, task.ID, task.RuntimeID)
	d.registerCerebroTermSink(task.ID, func(text string) {
		pump.write(text)
	})

	return func() {
		pump.stop()
		d.unregisterCerebroTermSink(task.ID)
		// CEREBRO-PATCH(daemon-cerebro-term-reattach): clear on task end so WS reconnects don't re-adopt a finished task.
		d.removeCerebroAttach(task.ID)
		// CEREBRO-PATCH(daemon-cerebro-term-buffer): drop any unsent backlog for the finished task.
		d.removeCerebroTermBuffer(task.ID)
		exitFrame, err := marshalCerebroDaemonFrame(frameTermExit, map[string]any{
			"task_id": task.ID,
			"code":    0,
		})
		if err != nil {
			d.logger.Debug("cerebro term_exit marshal failed", "error", err, "task_id", task.ID)
			return
		}
		if !d.enqueueCerebroFrame(exitFrame) {
			d.logger.Debug("cerebro term_exit dropped: ws not connected", "task_id", task.ID)
		}
	}
}

// cerebroTermPump batches text chunks into cerebro:term_stdout frames so a
// chatty agent doesn't fire one frame per token. Flushes on byte ceiling
// or interval, whichever happens first.
type cerebroTermPump struct {
	d         *Daemon
	taskID    string
	runtimeID string

	mu      sync.Mutex
	buf     strings.Builder
	stopCh  chan struct{}
	stopped bool
}

func newCerebroTermPump(d *Daemon, taskID, runtimeID string) *cerebroTermPump {
	p := &cerebroTermPump{
		d:         d,
		taskID:    taskID,
		runtimeID: runtimeID,
		stopCh:    make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *cerebroTermPump) write(text string) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.buf.WriteString(text)
	shouldFlush := p.buf.Len() >= cerebroTermFlushBytes
	p.mu.Unlock()
	if shouldFlush {
		p.flush()
	}
}

func (p *cerebroTermPump) flush() {
	p.mu.Lock()
	if p.buf.Len() == 0 {
		p.mu.Unlock()
		return
	}
	payload := p.buf.String()
	p.buf.Reset()
	p.mu.Unlock()

	frame, err := marshalCerebroDaemonFrame(frameTermStdout, map[string]any{
		"task_id": p.taskID,
		"data":    base64.StdEncoding.EncodeToString([]byte(payload)),
	})
	if err != nil {
		p.d.logger.Debug("cerebro term_stdout marshal failed", "error", err, "task_id", p.taskID)
		return
	}
	// CEREBRO-PATCH(daemon-cerebro-term-buffer): buffer (don't drop) when the WS
	// is down so output produced during a Cloudflare reconnect gap survives and
	// is flushed in order on reconnect (TECH-3388).
	p.d.sendOrBufferTermStdout(p.taskID, frame)
}

func (p *cerebroTermPump) run() {
	t := time.NewTicker(cerebroTermFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.flush()
		case <-p.stopCh:
			p.flush()
			return
		}
	}
}

func (p *cerebroTermPump) stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	p.mu.Unlock()
	close(p.stopCh)
}

// formatMessageAsText renders an agent.Message for an interactive terminal
// view. Mirrors the message-type taxonomy that executeAndDrain already
// switches on: text and thinking are emitted inline, tool calls/results
// are bracketed with markers so a terminal pane can tell them apart even
// without colour. The format is deliberately plain — xterm.js, plain
// strings, no ANSI yet.
func formatMessageAsText(msg agent.Message) string {
	switch msg.Type {
	case agent.MessageText:
		if msg.Content == "" {
			return ""
		}
		return msg.Content
	case agent.MessageThinking:
		if msg.Content == "" {
			return ""
		}
		return "[thinking] " + msg.Content + "\n"
	case agent.MessageToolUse:
		args := ""
		if len(msg.Input) > 0 {
			if buf, err := json.Marshal(msg.Input); err == nil {
				args = " " + string(buf)
			}
		}
		return fmt.Sprintf("[tool-use] %s%s\n", msg.Tool, args)
	case agent.MessageToolResult:
		output := msg.Output
		if len(output) > 1024 {
			output = output[:1024] + "…"
		}
		return fmt.Sprintf("[tool-result] %s\n%s\n", msg.Tool, output)
	case agent.MessageStatus:
		if msg.Status == "" {
			return ""
		}
		return fmt.Sprintf("[status] %s\n", msg.Status)
	case agent.MessageError:
		if msg.Content == "" {
			return ""
		}
		return fmt.Sprintf("[error] %s\n", msg.Content)
	case agent.MessageLog:
		if msg.Content == "" {
			return ""
		}
		return fmt.Sprintf("[log] %s\n", msg.Content)
	default:
		return ""
	}
}

func marshalCerebroDaemonFrame(frameType string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(protocol.Message{Type: frameType, Payload: raw})
}
