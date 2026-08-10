package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	daemonterminal "github.com/multica-ai/multica/server/internal/daemon/terminal"
	"github.com/multica-ai/multica/server/pkg/terminalproto"
)

const terminalConnectionMaxBackoff = 30 * time.Second

var terminalControlInputID atomic.Uint64

func (d *Daemon) enqueueTerminalEvent(event daemonterminal.Event) {
	if d.terminalEvents == nil {
		return
	}
	if event.Type == "output" {
		select {
		case d.terminalEvents <- event:
		default:
			// The PTYManager's own 8 MiB ring is authoritative during a relay
			// outage. A reconnect replays it by sequence; dropping this transient
			// notification bounds memory without dropping replayable bytes.
		}
		return
	}
	d.terminalEvents <- event
}

func (d *Daemon) terminalConnectionLoop(ctx context.Context) {
	backoff := time.Second
	runtimeSetCh, unsubscribe := d.runtimeSet.Subscribe()
	defer unsubscribe()
	for {
		runtimeIDs := d.allRuntimeIDs()
		connectedFor, err := d.runTerminalConnection(ctx, runtimeIDs, runtimeSetCh)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errRuntimeSetChanged) {
			backoff = time.Second
			continue
		}
		if connectedFor >= 10*time.Second {
			backoff = time.Second
		}
		if err != nil {
			d.logger.Debug("terminal websocket unavailable; structured fallback remains active for new tasks", "error", err, "retry_in", backoff)
		}
		if err := sleepWithContextOrRuntimeChange(ctx, jitterDuration(backoff), runtimeSetCh); err != nil {
			return
		}
		if backoff < terminalConnectionMaxBackoff {
			backoff *= 2
			if backoff > terminalConnectionMaxBackoff {
				backoff = terminalConnectionMaxBackoff
			}
		}
	}
}

func (d *Daemon) runTerminalConnection(ctx context.Context, runtimeIDs []string, runtimeSetCh <-chan struct{}) (time.Duration, error) {
	if len(runtimeIDs) == 0 {
		return 0, errors.New("no runtimes registered for terminal connection")
	}
	wsURL, err := terminalWebSocketURL(d.cfg.ServerBaseURL, runtimeIDs)
	if err != nil {
		return 0, err
	}
	headers := http.Header{}
	if token := d.client.Token(); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	headers.Set("X-Client-Platform", d.client.platform)
	headers.Set("X-Client-Version", d.client.version)
	headers.Set("X-Client-OS", d.client.os)
	headers.Set("X-Client-Capabilities", daemonClientCapabilities())
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	conn, response, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if response != nil && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusNotImplemented) {
			return 0, fmt.Errorf("server does not support terminal-pty-v1: %w", err)
		}
		return 0, err
	}
	connectedAt := time.Now()
	defer conn.Close()
	d.terminalConnected.Store(true)
	defer d.terminalConnected.Store(false)
	readCh := make(chan terminalInbound, 32)
	go readTerminalConnection(conn, readCh)
	if err := writeTerminalJSON(conn, terminalproto.Message{Type: "hello", ProtocolVersion: int(terminalproto.Version)}); err != nil {
		return time.Since(connectedAt), err
	}
	if err := d.registerAndReplayTerminalSessions(conn); err != nil {
		return time.Since(connectedAt), err
	}
	ping := time.NewTicker(50 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return time.Since(connectedAt), ctx.Err()
		case <-runtimeSetCh:
			return time.Since(connectedAt), errRuntimeSetChanged
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return time.Since(connectedAt), err
			}
		case inbound := <-readCh:
			if inbound.err != nil {
				return time.Since(connectedAt), inbound.err
			}
			if err := d.handleTerminalInbound(inbound); err != nil {
				d.logger.Debug("terminal websocket input rejected", "error", err)
			}
		case event := <-d.terminalEvents:
			if err := writeTerminalEvent(conn, event); err != nil {
				return time.Since(connectedAt), err
			}
		}
	}
}

type terminalInbound struct {
	messageType int
	raw         []byte
	err         error
}

func readTerminalConnection(conn *websocket.Conn, out chan<- terminalInbound) {
	conn.SetReadLimit(64 * 1024)
	for {
		messageType, raw, err := conn.ReadMessage()
		out <- terminalInbound{messageType: messageType, raw: raw, err: err}
		if err != nil {
			return
		}
	}
}

func (d *Daemon) registerAndReplayTerminalSessions(conn *websocket.Conn) error {
	for _, session := range d.terminalManager.Sessions() {
		meta := session.Metadata()
		exitCode := (*int)(nil)
		status := "running"
		select {
		case <-session.Done():
			exit := session.Wait()
			exitCode = &exit.Code
			status = "exited"
		default:
		}
		if err := writeTerminalJSON(conn, terminalproto.Message{Type: "session", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID.String(), TaskID: meta.TaskID, IssueID: meta.IssueID, AgentID: meta.AgentID, WorkspaceID: meta.WorkspaceID, RuntimeID: meta.RuntimeID, DaemonID: meta.DaemonID, Provider: meta.Provider, Mode: "pty", Status: status, StructuredObservation: meta.StructuredObservation, Generation: meta.Generation, Cols: meta.Cols, Rows: meta.Rows, ExitCode: exitCode, ProviderSessionID: meta.ProviderSessionID}); err != nil {
			return err
		}
		replay := session.Replay(0)
		if replay.Gap {
			if err := writeTerminalJSON(conn, terminalproto.Message{Type: "state", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID.String(), Status: status, OldestSeq: replay.OldestSeq, OutputSeq: replay.LatestSeq}); err != nil {
				return err
			}
		}
		for _, chunk := range replay.Chunks {
			if err := writeTerminalEvent(conn, chunk); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Daemon) handleTerminalInbound(inbound terminalInbound) error {
	if inbound.messageType == websocket.BinaryMessage {
		frame, err := terminalproto.DecodeBinary(inbound.raw)
		if err != nil || frame.Kind != terminalproto.KindInput {
			return terminalproto.ErrInvalidFrame
		}
		session, ok := d.terminalManager.Get(frame.SessionID)
		if !ok {
			return daemonterminal.ErrSessionNotFound
		}
		if err := session.WriteInput(frame.Sequence, frame.Payload); err != nil && !errors.Is(err, daemonterminal.ErrDuplicateInput) {
			return err
		}
		return nil
	}
	if inbound.messageType != websocket.TextMessage {
		return nil
	}
	var msg terminalproto.Message
	if err := json.Unmarshal(inbound.raw, &msg); err != nil {
		return err
	}
	if msg.Type == "hello" || msg.Type == "registered" || msg.Type == "pong" || msg.Type == "error" {
		return nil
	}
	sessionID, err := uuid.Parse(msg.SessionID)
	if err != nil {
		return daemonterminal.ErrSessionNotFound
	}
	session, ok := d.terminalManager.Get(sessionID)
	if !ok {
		return daemonterminal.ErrSessionNotFound
	}
	switch msg.Type {
	case "resize":
		return session.Resize(msg.Cols, msg.Rows)
	case "ctrl_c":
		inputID := terminalControlInputID.Add(1) | (uint64(1) << 63)
		return session.CtrlC(inputID)
	default:
		return fmt.Errorf("unsupported terminal control message %q", msg.Type)
	}
}

func writeTerminalEvent(conn *websocket.Conn, event daemonterminal.Event) error {
	if event.Type == "output" {
		raw, err := terminalproto.EncodeBinary(terminalproto.KindOutput, event.SessionID, event.Sequence, event.Payload)
		if err != nil {
			return err
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(websocket.BinaryMessage, raw)
	}
	msg := terminalproto.Message{Type: event.Type, ProtocolVersion: int(terminalproto.Version), SessionID: event.SessionID.String(), TaskID: event.TaskID, IssueID: event.IssueID, AgentID: event.AgentID, WorkspaceID: event.WorkspaceID, RuntimeID: event.RuntimeID, DaemonID: event.DaemonID, Provider: event.Provider, Mode: "pty", Status: event.Status, StructuredObservation: event.StructuredObservation, Generation: event.Generation, Cols: event.Cols, Rows: event.Rows, OutputSeq: event.Sequence, ProviderSessionID: event.ProviderSessionID, ExitCode: event.ExitCode, ExitReason: event.ExitReason}
	return writeTerminalJSON(conn, msg)
}

func writeTerminalJSON(conn *websocket.Conn, msg terminalproto.Message) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(msg)
}

func terminalWebSocketURL(baseURL string, runtimeIDs []string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("terminal server URL must use http, https, ws, or wss")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/daemon/terminal/ws"
	u.RawPath = ""
	ids := append([]string(nil), runtimeIDs...)
	sort.Strings(ids)
	query := u.Query()
	query.Set("runtime_ids", strings.Join(ids, ","))
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String(), nil
}
