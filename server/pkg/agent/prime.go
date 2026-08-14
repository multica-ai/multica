package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	primeMaxFrameBytes         = 4 << 20
	primeRequestTimeout        = 30 * time.Second
	primeTerminateGrace        = 3 * time.Second
	primeShutdownTimeout       = 10 * time.Second
	primeUnixSocketPathLimit   = 100
	primeDaemonProtocolVersion = 7
	primeDaemonSchemaID        = "protocol-7-schema-16-1bcb9e7f1a49"
)

type primeBackend struct{ cfg Config }

type primeFrame struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Success bool            `json:"success,omitempty"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`

	Message  json.RawMessage `json:"message,omitempty"`
	Messages []struct {
		Role         string `json:"role"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"messages,omitempty"`
	AssistantMessageEvent *struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

type primeState struct {
	SessionID    string `json:"sessionId"`
	IsStreaming  bool   `json:"isStreaming"`
	MessageCount *int   `json:"messageCount"`
}

type primeStats struct {
	SessionID string `json:"sessionId"`
	Tokens    struct {
		Input, Output, CacheRead, CacheWrite int64
	} `json:"tokens"`
	Cost float64 `json:"cost"`
}

type primeJSONLReader struct{ r *bufio.Reader }

func newPrimeJSONLReader(r io.Reader) *primeJSONLReader {
	return &primeJSONLReader{r: bufio.NewReader(r)}
}

func (r *primeJSONLReader) ReadFrame() ([]byte, error) {
	var out []byte
	for {
		part, err := r.r.ReadSlice('\n')
		if len(out)+len(part) > primeMaxFrameBytes+1 {
			return nil, fmt.Errorf("Prime RPC frame exceeds %d bytes", primeMaxFrameBytes)
		}
		out = append(out, part...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if errors.Is(err, io.EOF) && len(out) > 0 {
				return nil, errors.New("Prime RPC ended with an unterminated JSONL frame")
			}
			return nil, err
		}
	}
	out = out[:len(out)-1]
	out = bytes.TrimSuffix(out, []byte{'\r'})
	if len(out) == 0 {
		return nil, errors.New("Prime RPC emitted an empty frame")
	}
	return out, nil
}

type primeRPC struct {
	in      io.WriteCloser
	frames  <-chan primeFrame
	errs    <-chan error
	writeMu sync.Mutex
	nextID  int
	events  func(primeFrame)
}

func (c *primeRPC) write(command map[string]any) (string, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.nextID++
	id := "multica-" + strconv.Itoa(c.nextID)
	command["id"] = id
	b, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	if _, err := c.in.Write(b); err != nil {
		return "", err
	}
	return id, nil
}

func (c *primeRPC) request(ctx context.Context, command map[string]any) (primeFrame, error) {
	id, err := c.write(command)
	if err != nil {
		return primeFrame{}, err
	}
	timer := time.NewTimer(primeRequestTimeout)
	defer timer.Stop()
	for {
		var frame primeFrame
		select {
		case <-ctx.Done():
			return primeFrame{}, ctx.Err()
		case <-timer.C:
			return primeFrame{}, fmt.Errorf("Prime RPC %s response timeout", command["type"])
		case err := <-c.errs:
			return primeFrame{}, err
		case frame = <-c.frames:
		}
		if frame.Type == "response" {
			if frame.ID != id {
				return primeFrame{}, fmt.Errorf("Prime RPC response correlation mismatch: got %q", frame.ID)
			}
			if frame.Command != command["type"] {
				return primeFrame{}, fmt.Errorf("Prime RPC command mismatch for %s", command["type"])
			}
			if !frame.Success {
				return primeFrame{}, fmt.Errorf("Prime RPC %s failed: %s", frame.Command, sanitizeAgentDiagnostic(frame.Error))
			}
			return frame, nil
		}
		c.events(frame)
	}
}

func pumpPrimeFrames(ctx context.Context, r io.Reader) (<-chan primeFrame, <-chan error) {
	frames, errs := make(chan primeFrame, 8), make(chan error, 1)
	go func() {
		reader := newPrimeJSONLReader(r)
		for {
			b, err := reader.ReadFrame()
			if err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			var f primeFrame
			if err := json.Unmarshal(b, &f); err != nil {
				select {
				case errs <- fmt.Errorf("malformed Prime RPC JSONL frame: %w", err):
				case <-ctx.Done():
				}
				return
			}
			if f.Type == "" {
				select {
				case errs <- errors.New("Prime RPC frame missing type"):
				case <-ctx.Done():
				}
				return
			}
			select {
			case frames <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	return frames, errs
}

func parsePrimeModel(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	provider, model, ok := strings.Cut(value, "/")
	if !ok || strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
		return "", "", errors.New("Prime model must use provider/modelId form")
	}
	return provider, model, nil
}

func validatePrimeArgs(args []string) error {
	if len(args) != 0 {
		return errors.New("Prime Agent is a Multica-managed runtime; extra and custom arguments are not allowed")
	}
	return nil
}

func (b *primeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("Prime prompt must not be empty")
	}
	if err := CheckPrimeAdmission(); err != nil {
		return nil, err
	}
	tmpDir, err := validatePrimeTaskTempDir(b.cfg.Env["TMPDIR"])
	if err != nil {
		return nil, err
	}
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "prime-agent"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("prime-agent executable not found: %w", err)
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 10*time.Second)
	detected, err := DetectPrimeVersion(probeCtx, execPath)
	probeCancel()
	if err != nil {
		return nil, fmt.Errorf("verify Prime Agent version: %w", err)
	}
	if err := CheckPrimeVersion(detected); err != nil {
		return nil, err
	}
	provider, model, err := parsePrimeModel(opts.Model)
	if err != nil {
		return nil, err
	}
	if err := validatePrimeArgs(append(append([]string{}, opts.ExtraArgs...), opts.CustomArgs...)); err != nil {
		return nil, err
	}

	socketPath := filepath.Join(tmpDir, "multica-prime-daemon.sock")
	if err := validateUnusedPrimeSocketPath(tmpDir, socketPath); err != nil {
		return nil, err
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	args := []string{"--mode", "rpc", "--no-extensions", "--daemon-socket", socketPath}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = 10 * time.Second
	cmd.Dir = opts.Cwd
	env := map[string]string{}
	for k, v := range b.cfg.Env {
		env[k] = v
	}
	cmd.Env = buildPrimeEnv(env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// Prime stderr may contain provider diagnostics. Never forward raw bytes to
	// daemon logs; retain only a bounded tail and redact before persistence.
	stderr := newStderrTail(io.Discard, agentStderrTailBytes)
	cmd.Stderr = stderr
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start Prime Agent: %w", err)
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go b.run(runCtx, cancel, cmd, stdin, stdout, stderr, prompt, provider, model, tmpDir, socketPath, opts, msgCh, resCh)
	return &Session{Messages: msgCh, Result: resCh}, nil
}

func validateUnusedPrimeSocketPath(tmpDir, socketPath string) error {
	rel, err := filepath.Rel(tmpDir, socketPath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("Prime daemon socket must be inside the task-private TMPDIR")
	}
	if len([]byte(socketPath)) >= primeUnixSocketPathLimit {
		return fmt.Errorf("Prime daemon socket path is too long; task TMPDIR must keep the socket path below %d bytes", primeUnixSocketPathLimit)
	}
	if _, err := os.Lstat(socketPath); err == nil {
		return errors.New("Prime daemon socket path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Prime daemon socket path: %w", err)
	}
	return nil
}

func validatePrimeTaskTempDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return "", errors.New("Prime Agent requires an explicit absolute task-private TMPDIR")
	}
	info, err := os.Lstat(value)
	if err != nil {
		return "", fmt.Errorf("validate Prime task TMPDIR: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("Prime Agent TMPDIR must be a real directory, not a symlink or file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("Prime Agent TMPDIR %q is not private; permissions must deny group/other access", value)
	}
	return filepath.Clean(value), nil
}

func buildPrimeEnv(extra map[string]string) []string {
	base := make([]string, 0, 16)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP", "LANG",
			"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR":
			base = append(base, entry)
		default:
			if strings.HasPrefix(key, "LC_") {
				base = append(base, entry)
			}
		}
	}
	withExplicit := mergeEnv(base, extra)
	return mergeEnv(withExplicit, map[string]string{
		"PRIME_AGENT_TELEMETRY": "0",
		"DO_NOT_TRACK":          "1",
		"PI_SKIP_VERSION_CHECK": "1",
	})
}

func (b *primeBackend) run(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr *stderrTail, prompt, provider, model, tmpDir, socketPath string, opts ExecOptions, msgCh chan Message, resCh chan Result) {
	defer cancel()
	defer close(msgCh)
	defer close(resCh)
	defer releaseProcessGroup(cmd)
	started := time.Now()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	frames, frameErrs := pumpPrimeFrames(ctx, stdout)
	rpc := &primeRPC{in: stdin, frames: frames, errs: frameErrs, events: func(f primeFrame) { mapPrimeEvent(f, msgCh) }}
	var daemonIdentity *primeDaemonIdentity
	cleanup := func() (error, error) {
		_, _ = rpc.write(map[string]any{"type": "abort"})
		_ = stdin.Close()
		var clientErr error
		select {
		case err := <-waitDone:
			clientErr = err
			_ = stdout.Close()
		case <-time.After(primeTerminateGrace):
			signalProcessGroup(cmd, syscall.SIGTERM)
			if !waitProcessGroupGone(cmd, primeTerminateGrace) {
				signalProcessGroup(cmd, syscall.SIGKILL)
			}
			_ = stdout.Close()
			clientErr = <-waitDone
		}
		return clientErr, shutdownPrimeTaskDaemon(tmpDir, socketPath, daemonIdentity, kernelPrimePeerIdentity)
	}
	fail := func(sessionID string, err error, resumeRejected ...bool) {
		clientErr, shutdownErr := cleanup()
		if shutdownErr != nil {
			err = fmt.Errorf("%v; Prime task daemon shutdown failed: %w", err, shutdownErr)
		}
		b.finishPrime(clientErr, stderr, started, sessionID, err, len(resumeRejected) != 0 && resumeRejected[0], resCh)
	}

	stateResp, err := rpc.request(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		fail("", err)
		return
	}
	var state primeState
	if json.Unmarshal(stateResp.Data, &state) != nil || state.SessionID == "" || state.MessageCount == nil || state.IsStreaming {
		fail("", errors.New("Prime RPC get_state compatibility check failed"))
		return
	}
	daemonIdentity, err = observePrimeTaskDaemon(tmpDir, socketPath, kernelPrimePeerIdentity)
	if err != nil {
		fail(state.SessionID, err)
		return
	}
	if opts.ResumeSessionID != "" && state.SessionID != opts.ResumeSessionID {
		fail(state.SessionID, errors.New("Prime RPC resume session mismatch"), true)
		return
	}
	trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: state.SessionID})
	if provider != "" {
		if _, err = rpc.request(ctx, map[string]any{"type": "set_model", "provider": provider, "modelId": model}); err != nil {
			fail(state.SessionID, err)
			return
		}
	}
	if opts.ThinkingLevel != "" {
		if _, err = rpc.request(ctx, map[string]any{"type": "set_thinking_level", "level": opts.ThinkingLevel}); err != nil {
			fail(state.SessionID, err)
			return
		}
	}
	if _, err = rpc.request(ctx, map[string]any{"type": "prompt", "message": prompt}); err != nil {
		fail(state.SessionID, err)
		return
	}
	for {
		var f primeFrame
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case err = <-rpc.errs:
		case f = <-rpc.frames:
		}
		if err != nil {
			break
		}
		if f.Type == "response" {
			err = errors.New("unexpected uncorrelated Prime RPC response")
			break
		}
		mapPrimeEvent(f, msgCh)
		if f.Type == "agent_end" {
			for i := len(f.Messages) - 1; i >= 0; i-- {
				m := f.Messages[i]
				if m.Role == "assistant" && (m.StopReason == "error" || m.StopReason == "aborted") {
					err = fmt.Errorf("Prime Agent turn %s: %s", m.StopReason, sanitizeAgentDiagnostic(m.ErrorMessage))
				}
				if m.Role == "assistant" {
					break
				}
			}
			break
		}
	}
	if err != nil {
		fail(state.SessionID, err)
		return
	}
	last, lastErr := rpc.request(ctx, map[string]any{"type": "get_last_assistant_text"})
	if lastErr != nil {
		fail(state.SessionID, lastErr)
		return
	}
	var textData struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(last.Data, &textData) != nil {
		fail(state.SessionID, errors.New("invalid Prime last-assistant response"))
		return
	}
	output := ""
	if textData.Text != nil {
		output = *textData.Text
	}
	statsResp, statsErr := rpc.request(ctx, map[string]any{"type": "get_session_stats"})
	if statsErr != nil {
		fail(state.SessionID, statsErr)
		return
	}
	var s primeStats
	if json.Unmarshal(statsResp.Data, &s) != nil {
		fail(state.SessionID, errors.New("invalid Prime session-stats response"))
		return
	}
	scaledCost := s.Cost * CostUSDTicksPerUSD
	if s.SessionID != state.SessionID || s.Tokens.Input < 0 || s.Tokens.Output < 0 || s.Tokens.CacheRead < 0 || s.Tokens.CacheWrite < 0 || s.Cost < 0 || math.IsNaN(s.Cost) || math.IsInf(s.Cost, 0) || math.IsNaN(scaledCost) || math.IsInf(scaledCost, 0) || scaledCost < 0 || scaledCost >= float64(math.MaxInt64)-2048 {
		fail(state.SessionID, errors.New("invalid Prime session-stats response"))
		return
	}
	key := opts.Model
	if key == "" {
		key = "prime/default"
	}
	usage := map[string]TokenUsage{key: {InputTokens: s.Tokens.Input, OutputTokens: s.Tokens.Output, CacheReadTokens: s.Tokens.CacheRead, CacheWriteTokens: s.Tokens.CacheWrite, CostUSDTicks: int64(math.Round(scaledCost))}}
	clientErr, shutdownErr := cleanup()
	if shutdownErr != nil {
		b.finishPrime(clientErr, stderr, started, state.SessionID, shutdownErr, false, resCh)
		return
	}
	if clientErr != nil {
		b.finishPrime(clientErr, stderr, started, state.SessionID, errors.New("Prime RPC client exited unsuccessfully"), false, resCh)
		return
	}
	resCh <- Result{Status: "completed", Output: output, SessionID: state.SessionID, Usage: usage, DurationMs: time.Since(started).Milliseconds()}
}

type primeDaemonHello struct {
	Type       string `json:"type"`
	SocketPath string `json:"socketPath"`
	Protocol   struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	} `json:"protocol"`
	SchemaID      string `json:"schemaId"`
	AppVersion    string `json:"appVersion"`
	SupervisorPID int    `json:"supervisorPid"`
	ClientID      string `json:"clientId"`
}

type primeDaemonIdentity struct {
	PID  int
	PGID int
}

func validatePrimeSocket(tmpDir, socketPath string) error {
	if err := validateUnusedPrimeSocketPath(tmpDir, socketPath); err == nil {
		return os.ErrNotExist
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		return fmt.Errorf("inspect Prime daemon socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Prime daemon socket failed private socket validation")
	}
	if err := validatePrimeSocketOwner(info); err != nil {
		return err
	}
	return nil
}

func readPrimeDaemonIdentity(conn net.Conn, socketPath string, peerIdentity func(net.Conn) (int, int, error)) (*primeDaemonIdentity, primeDaemonHello, error) {
	peerPID, peerUID, err := peerIdentity(conn)
	if err != nil || peerPID <= 0 || peerUID != os.Geteuid() {
		return nil, primeDaemonHello{}, errors.New("Prime daemon kernel peer identity check failed")
	}
	peerPGID, err := primeSupervisorIdentity(peerPID)
	if err != nil {
		return nil, primeDaemonHello{}, errors.New("Prime daemon kernel peer process identity could not be verified")
	}
	identity := &primeDaemonIdentity{PID: peerPID, PGID: peerPGID}
	b, err := newPrimeJSONLReader(conn).ReadFrame()
	if err != nil {
		return identity, primeDaemonHello{}, fmt.Errorf("read Prime daemon hello: %w", err)
	}
	var hello primeDaemonHello
	if json.Unmarshal(b, &hello) != nil || hello.Type != "daemon_hello" || hello.Protocol.Name != "prime-agent.daemon" || hello.Protocol.Version != primeDaemonProtocolVersion || hello.SchemaID != primeDaemonSchemaID || hello.AppVersion != PrimeAgentVersion || filepath.Clean(hello.SocketPath) != socketPath || hello.SupervisorPID != peerPID || hello.ClientID == "" {
		return identity, hello, errors.New("Prime daemon hello compatibility or identity check failed")
	}
	return identity, hello, nil
}

func observePrimeTaskDaemon(tmpDir, socketPath string, peerIdentity func(net.Conn) (int, int, error)) (*primeDaemonIdentity, error) {
	if err := validatePrimeSocket(tmpDir, socketPath); err != nil {
		return nil, fmt.Errorf("observe Prime daemon socket: %w", err)
	}
	deadline := time.Now().Add(primeShutdownTimeout)
	conn, err := net.DialTimeout("unix", socketPath, time.Until(deadline))
	if err != nil {
		return nil, errors.New("connect to task-local Prime daemon for identity observation failed")
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	identity, _, err := readPrimeDaemonIdentity(conn, socketPath, peerIdentity)
	if err != nil && identity != nil {
		return nil, forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, err.Error(), true)
	}
	return identity, err
}

func shutdownPrimeTaskDaemon(tmpDir, socketPath string, identity *primeDaemonIdentity, peerIdentity func(net.Conn) (int, int, error)) error {
	if identity == nil {
		return errors.New("Prime daemon cleanup could not be proven because no authenticated supervisor identity was captured")
	}
	if err := validatePrimeSocket(tmpDir, socketPath); errors.Is(err, os.ErrNotExist) {
		if primeSupervisorGone(identity.PID, identity.PGID) {
			return nil
		}
		return forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, "Prime daemon unlinked its socket while supervisor remained alive", false)
	} else if err != nil {
		return err
	}
	deadline := time.Now().Add(primeShutdownTimeout)
	conn, err := net.DialTimeout("unix", socketPath, time.Until(deadline))
	if err != nil {
		return errors.New("Prime daemon shutdown failed: connect to task-local daemon failed")
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	observed, hello, err := readPrimeDaemonIdentity(conn, socketPath, peerIdentity)
	if err != nil || observed.PID != identity.PID || observed.PGID != identity.PGID {
		cause := "Prime daemon reconnect identity mismatch"
		if err != nil {
			cause = err.Error()
		}
		return forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, cause, true)
	}
	reader := newPrimeJSONLReader(conn)
	id := fmt.Sprintf("multica-shutdown-%d", time.Now().UnixNano())
	envelope := map[string]any{
		"type": "command", "id": id, "clientId": hello.ClientID,
		"protocol": map[string]any{"name": "prime-agent.daemon", "version": primeDaemonProtocolVersion},
		"command":  map[string]any{"id": id, "type": "shutdown", "force": true},
	}
	payload, _ := json.Marshal(envelope)
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, "write shutdown command failed", true)
	}
	frameBytes, err := reader.ReadFrame()
	if err == nil {
		var response struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Command string `json:"command"`
			Success bool   `json:"success"`
		}
		if json.Unmarshal(frameBytes, &response) != nil || response.ID != id || response.Type != "response" || response.Command != "shutdown" || !response.Success {
			return forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, "shutdown response failed correlation or success validation", true)
		}
	} else {
		return forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, "read shutdown response failed", true)
	}
	_ = conn.Close()
	if waitPrimeSupervisorGone(identity.PID, identity.PGID, socketPath, primeTerminateGrace) {
		return nil
	}
	return forceAndVerifyPrimeSupervisor(identity.PID, identity.PGID, socketPath, "graceful shutdown was not proven", false)
}

func forceAndVerifyPrimeSupervisor(pid, pgid int, socketPath, cause string, failClosed bool) error {
	if err := forcePrimeSupervisor(pid, pgid); err != nil {
		return fmt.Errorf("%s; targeted Prime supervisor termination failed: %w", cause, err)
	}
	if !waitPrimeSupervisorGone(pid, pgid, socketPath, primeTerminateGrace) {
		return fmt.Errorf("%s; targeted Prime supervisor termination was not proven", cause)
	}
	if failClosed {
		return errors.New(cause)
	}
	return nil
}

func waitPrimeSupervisorGone(pid, pgid int, socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, socketErr := os.Lstat(socketPath)
		if errors.Is(socketErr, os.ErrNotExist) && primeSupervisorGone(pid, pgid) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	_, socketErr := os.Lstat(socketPath)
	return errors.Is(socketErr, os.ErrNotExist) && primeSupervisorGone(pid, pgid)
}

func mapPrimeEvent(f primeFrame, ch chan Message) {
	switch f.Type {
	case "agent_start":
		trySend(ch, Message{Type: MessageStatus, Status: "running"})
	case "message_update":
		if f.AssistantMessageEvent != nil && f.AssistantMessageEvent.Delta != "" {
			t := MessageText
			if f.AssistantMessageEvent.Type == "thinking_delta" {
				t = MessageThinking
			}
			if f.AssistantMessageEvent.Type == "text_delta" || f.AssistantMessageEvent.Type == "thinking_delta" {
				trySend(ch, Message{Type: t, Content: f.AssistantMessageEvent.Delta})
			}
		}
	case "tool_execution_start":
		var input map[string]any
		_ = json.Unmarshal(f.Args, &input)
		trySend(ch, Message{Type: MessageToolUse, Tool: f.ToolName, CallID: f.ToolCallID, Input: input})
	case "tool_execution_end":
		out := sanitizeAgentDiagnostic(string(f.Result))
		if len(out) > 8192 {
			out = out[:8192] + "…"
		}
		trySend(ch, Message{Type: MessageToolResult, Tool: f.ToolName, CallID: f.ToolCallID, Output: out})
	case "auto_retry_start":
		trySend(ch, Message{Type: MessageStatus, Status: "retrying"})
	case "extension_error":
		trySend(ch, Message{Type: MessageError, Content: "Prime Agent extension failed"})
	}
}

func (b *primeBackend) finishPrime(waitErr error, stderr *stderrTail, started time.Time, sessionID string, err error, resumeRejected bool, resCh chan Result) {
	status := "failed"
	if errors.Is(err, context.Canceled) {
		status = "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status = "timeout"
	}
	msg := sanitizeAgentDiagnostic(err.Error())
	if waitErr != nil {
		msg = withAgentStderr(msg, "Prime Agent", sanitizeAgentDiagnostic(stderr.Tail()))
	}
	resCh <- Result{Status: status, Error: msg, SessionID: sessionID, DurationMs: time.Since(started).Milliseconds(), ResumeRejected: resumeRejected}
}
