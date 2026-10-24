package daemon

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const mcpProbeTimeout = 15 * time.Second

type mcpProbeOutcome struct {
	Status    string
	ErrorCode string
	Error     string
	ElapsedMs int64
	Tools     []string
}

type mcpServerEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Env     map[string]string `json:"env"`
}

func (d *Daemon) handleMcpProbe(ctx context.Context, runtimeID, requestID string) {
	start := time.Now()
	outcome := mcpProbeOutcome{Status: "failed", ErrorCode: protocol.McpProbeCodeInternal, Error: "probe failed"}
	defer func() {
		outcome.ElapsedMs = time.Since(start).Milliseconds()
		d.reportMcpProbeResult(ctx, runtimeID, requestID, outcome)
	}()

	if d.client == nil {
		outcome.Error = "daemon client is not configured"
		return
	}
	job, err := d.client.GetMcpProbeJob(ctx, runtimeID, requestID)
	if err != nil {
		outcome.ErrorCode = protocol.McpProbeCodeInternal
		outcome.Error = "failed to load probe job"
		d.logger.Warn("mcp probe: load job", "error", err, "request_id", requestID)
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, mcpProbeTimeout)
	defer cancel()
	result, err := probeWorkspaceMcp(probeCtx, job.Config)
	if err != nil {
		outcome.ErrorCode, outcome.Error = classifyMcpProbeError(err)
		return
	}
	outcome.Status = "completed"
	outcome.ErrorCode = ""
	outcome.Error = ""
	outcome.Tools = result
}

func (d *Daemon) reportMcpProbeResult(ctx context.Context, runtimeID, requestID string, outcome mcpProbeOutcome) {
	if d.client == nil {
		return
	}
	body := map[string]any{
		"status":     outcome.Status,
		"error_code": outcome.ErrorCode,
		"error":      outcome.Error,
		"elapsed_ms": outcome.ElapsedMs,
		"tools":      outcome.Tools,
	}
	if err := d.client.ReportMcpProbeResult(ctx, runtimeID, requestID, body); err != nil {
		d.logger.Warn("mcp probe: report failed", "error", err, "request_id", requestID)
	}
}

func probeWorkspaceMcp(ctx context.Context, raw json.RawMessage) ([]string, error) {
	var entry mcpServerEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("%s: invalid server config", protocol.McpProbeCodeHandshake)
	}
	switch mcpProbeTransportOf(entry) {
	case "stdio":
		return probeMcpStdio(ctx, entry)
	case "http":
		return probeMcpHTTP(ctx, entry)
	default:
		return nil, fmt.Errorf("%s: unsupported transport", protocol.McpProbeCodeUnsupportedTransport)
	}
}

func mcpProbeTransportOf(entry mcpServerEntry) string {
	declared := strings.ToLower(strings.TrimSpace(entry.Type))
	if declared != "" {
		switch declared {
		case "local", "stdio":
			return "stdio"
		case "remote", "http", "streamable-http":
			return "http"
		}
		return declared
	}
	if strings.TrimSpace(entry.Command) != "" {
		return "stdio"
	}
	if strings.TrimSpace(entry.URL) != "" {
		return "http"
	}
	return "unknown"
}

func probeMcpStdio(ctx context.Context, entry mcpServerEntry) ([]string, error) {
	command := strings.TrimSpace(entry.Command)
	if command == "" {
		return nil, fmt.Errorf("%s: command is required", protocol.McpProbeCodeCommandNotFound)
	}
	cmd := exec.Command(command, entry.Args...)
	env := os.Environ()
	for k, v := range entry.Env {
		if strings.TrimSpace(k) == "" {
			continue
		}
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: failed to start server", protocol.McpProbeCodeInternal)
	}
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("%s: failed to start server", protocol.McpProbeCodeInternal)
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = io.Discard
	processCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		err := processtree.Run(processCtx, cmd, time.Second)
		_ = stdoutWriter.Close()
		done <- err
	}()
	defer func() {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
	}()

	session := newMcpStdioSession(stdin, stdout)
	tools, handshakeErr := mcpHandshake(ctx, session)
	cancel()
	processErr := <-done
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(processErr, exec.ErrNotFound) || isCommandNotFound(processErr) {
		return nil, fmt.Errorf("%s: command not found", protocol.McpProbeCodeCommandNotFound)
	}
	if processErr != nil && !errors.Is(processErr, context.Canceled) && handshakeErr == nil {
		return nil, fmt.Errorf("%s: server cleanup failed", protocol.McpProbeCodeInternal)
	}
	return tools, handshakeErr
}

func isCommandNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "not found in $path")
}

type mcpTransport interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Notify(ctx context.Context, method string, params any) error
	Close()
}

func mcpHandshake(ctx context.Context, t mcpTransport) ([]string, error) {
	defer t.Close()
	initialized, err := t.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "multica-probe", "version": "1"},
	})
	if err != nil {
		return nil, wrapMcpHandshakeError(ctx, err, "initialize failed")
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(initialized, &initResult); err != nil ||
		(initResult.ProtocolVersion != "2024-11-05" && initResult.ProtocolVersion != "2025-03-26") {
		return nil, fmt.Errorf("%s: unsupported protocol version", protocol.McpProbeCodeHandshake)
	}
	if err := t.Notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return nil, wrapMcpHandshakeError(ctx, err, "initialization failed")
	}
	names := make([]string, 0)
	params := map[string]any{}
	seenCursors := make(map[string]bool)
	for {
		raw, err := t.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, wrapMcpHandshakeError(ctx, err, "tools/list failed")
		}
		var listed struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &listed); err != nil || listed.Tools == nil {
			return nil, fmt.Errorf("%s: tools/list failed", protocol.McpProbeCodeHandshake)
		}
		for _, tool := range listed.Tools {
			if name := strings.TrimSpace(tool.Name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 1000 || seenCursors[listed.NextCursor] {
			return nil, fmt.Errorf("%s: tools/list failed", protocol.McpProbeCodeHandshake)
		}
		if listed.NextCursor == "" {
			break
		}
		seenCursors[listed.NextCursor] = true
		params["cursor"] = listed.NextCursor
	}
	return names, nil
}

type mcpStdioSession struct {
	w   io.WriteCloser
	r   *bufio.Reader
	seq int
}

func newMcpStdioSession(w io.WriteCloser, r io.Reader) *mcpStdioSession {
	return &mcpStdioSession{w: w, r: bufio.NewReader(r)}
}

func (s *mcpStdioSession) Close() {
	_ = s.w.Close()
}

func (s *mcpStdioSession) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.seq++
	id := s.seq
	if err := writeMCPMessage(s.w, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msg, err := readMCPMessage(s.r)
		if err != nil {
			return nil, err
		}
		if msg.Method != "" || string(msg.ID) != strconv.Itoa(id) {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("rpc error %d", msg.Error.Code)
		}
		return msg.Result, nil
	}
}

func (s *mcpStdioSession) Notify(_ context.Context, method string, params any) error {
	return writeMCPMessage(s.w, map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

type mcpRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeMCPMessage(w io.Writer, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}

func readMCPMessage(r *bufio.Reader) (mcpRPCMessage, error) {
	var msg mcpRPCMessage
	var body []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(body)+len(part) > 1<<20 {
			return msg, errors.New("MCP message exceeds size limit")
		}
		body = append(body, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return msg, err
		}
		break
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return msg, err
	}
	return msg, nil
}

func probeMcpHTTP(ctx context.Context, entry mcpServerEntry) ([]string, error) {
	target := strings.TrimSpace(entry.URL)
	if target == "" {
		return nil, fmt.Errorf("%s: initialize failed", protocol.McpProbeCodeHandshake)
	}
	client := &http.Client{
		Timeout: mcpProbeTimeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSHandshakeTimeout: 8 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("MCP endpoint redirects are not supported")
		},
	}
	session := &mcpHTTPSession{client: client, url: target, headers: entry.Headers}
	return mcpHandshake(ctx, session)
}

type mcpHTTPSession struct {
	client    *http.Client
	url       string
	headers   map[string]string
	sessionID string
	seq       int
}

func (s *mcpHTTPSession) Close() {
	defer s.client.CloseIdleConnections()
	if s.sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.url, nil)
	if err != nil {
		return
	}
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Mcp-Session-Id", s.sessionID)
	if resp, err := s.client.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

func (s *mcpHTTPSession) Notify(ctx context.Context, method string, params any) error {
	_, err := s.post(ctx, map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	return err
}

func (s *mcpHTTPSession) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.seq++
	raw, err := s.post(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      s.seq,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	var msg mcpRPCMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	if msg.Error != nil {
		return nil, fmt.Errorf("rpc error %d", msg.Error.Code)
	}
	if msg.JSONRPC != "2.0" || string(msg.ID) != strconv.Itoa(s.seq) || len(msg.Result) == 0 {
		return nil, errors.New("invalid MCP response")
	}
	return msg.Result, nil
}

func (s *mcpHTTPSession) post(ctx context.Context, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: initialize failed", protocol.McpProbeCodeHandshake)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	if s.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", s.sessionID)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		s.sessionID = sid
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%s: unauthorized", protocol.McpProbeCodeUnauthorized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: initialize failed", protocol.McpProbeCodeHandshake)
	}
	if request, ok := payload.(map[string]any); ok && request["id"] == nil {
		return nil, nil
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readMcpSSEResponse(resp.Body, s.seq)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, errors.New("MCP response exceeds size limit")
	}
	return body, nil
}

func readMcpSSEResponse(r io.Reader, id int) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, (1<<20)+1))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		} else if line == "" && len(data) > 0 {
			raw := []byte(strings.Join(data, "\n"))
			data = nil
			var msg mcpRPCMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				return nil, err
			}
			if msg.Method == "" && string(msg.ID) == strconv.Itoa(id) {
				return raw, nil
			}
		}
	}
	return nil, errors.New("MCP stream ended before the response")
}

func wrapMcpHandshakeError(ctx context.Context, err error, fallback string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || isTLSError(err) {
		return fmt.Errorf("%s: tls handshake failed", protocol.McpProbeCodeTLS)
	}
	msg := err.Error()
	for _, code := range []string{
		protocol.McpProbeCodeCommandNotFound,
		protocol.McpProbeCodeUnauthorized,
		protocol.McpProbeCodeTLS,
		protocol.McpProbeCodeUnsupportedTransport,
		protocol.McpProbeCodeHandshake,
		protocol.McpProbeCodeTimeout,
		protocol.McpProbeCodeInternal,
	} {
		if strings.HasPrefix(msg, code+":") {
			return err
		}
	}
	return fmt.Errorf("%s: %s", protocol.McpProbeCodeHandshake, fallback)
}

func classifyMcpProbeError(err error) (string, string) {
	if err == nil {
		return protocol.McpProbeCodeInternal, "probe failed"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return protocol.McpProbeCodeTimeout, "probe timed out"
	}
	msg := err.Error()
	for _, code := range []string{
		protocol.McpProbeCodeCommandNotFound,
		protocol.McpProbeCodeUnauthorized,
		protocol.McpProbeCodeTLS,
		protocol.McpProbeCodeUnsupportedTransport,
		protocol.McpProbeCodeHandshake,
		protocol.McpProbeCodeTimeout,
		protocol.McpProbeCodeInternal,
	} {
		if strings.HasPrefix(msg, code+":") {
			return code, handlerSanitizeProbeMessage(strings.TrimSpace(strings.TrimPrefix(msg, code+":")))
		}
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || isTLSError(err) {
		return protocol.McpProbeCodeTLS, "tls handshake failed"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return protocol.McpProbeCodeTimeout, "probe timed out"
	}
	return protocol.McpProbeCodeInternal, "probe failed"
}

func isTLSError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls") || strings.Contains(msg, "x509") || strings.Contains(msg, "certificate")
}

func handlerSanitizeProbeMessage(msg string) string {
	if msg == "" {
		return "probe failed"
	}
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}
