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
	case "http", "sse":
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
		case "sse":
			return "sse"
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
	cmd := exec.CommandContext(ctx, command, entry.Args...)
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: failed to start server", protocol.McpProbeCodeInternal)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || isCommandNotFound(err) {
			return nil, fmt.Errorf("%s: command not found", protocol.McpProbeCodeCommandNotFound)
		}
		return nil, fmt.Errorf("%s: failed to start server", protocol.McpProbeCodeInternal)
	}
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	session := newMcpStdioSession(stdin, stdout)
	return mcpHandshake(ctx, session)
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
	_, err := t.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "multica-probe", "version": "1"},
	})
	if err != nil {
		return nil, wrapMcpHandshakeError(ctx, err, "initialize failed")
	}
	_ = t.Notify(ctx, "notifications/initialized", map[string]any{})
	raw, err := t.Call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, wrapMcpHandshakeError(ctx, err, "tools/list failed")
	}
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, fmt.Errorf("%s: tools/list failed", protocol.McpProbeCodeHandshake)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		if name := strings.TrimSpace(tool.Name); name != "" {
			names = append(names, name)
		}
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
		if msg.Method != "" && msg.ID == nil {
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
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(raw)); err != nil {
		return err
	}
	_, err = w.Write(raw)
	return err
}

func readMCPMessage(r *bufio.Reader) (mcpRPCMessage, error) {
	var msg mcpRPCMessage
	first, err := r.ReadByte()
	if err != nil {
		return msg, err
	}
	if err := r.UnreadByte(); err != nil {
		return msg, err
	}
	var body []byte
	if first == '{' {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return msg, err
		}
		body = bytes.TrimSpace(line)
	} else {
		headers := map[string]string{}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return msg, err
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
		n, err := strconv.Atoi(headers["content-length"])
		if err != nil || n < 0 || n > 1<<20 {
			return msg, fmt.Errorf("invalid content-length")
		}
		body = make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return msg, err
		}
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
			TLSHandshakeTimeout: 8 * time.Second,
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

func (s *mcpHTTPSession) Close() {}

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return sseJSONData(body)
	}
	return body, nil
}

func sseJSONData(raw []byte) ([]byte, error) {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			return []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), nil
		}
	}
	return nil, fmt.Errorf("%s: initialize failed", protocol.McpProbeCodeHandshake)
}

func wrapMcpHandshakeError(ctx context.Context, err error, fallback string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
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
