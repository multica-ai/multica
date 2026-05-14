package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	codexAppServerSource       = "codex-app-server"
	codexAppServerStartRetries = 5
)

func shouldUseCodexRemoteRunner(args []string, cliName string) bool {
	if len(args) == 0 {
		return false
	}
	return strings.EqualFold(cliName, "codex") && strings.EqualFold(inferCLIName(args[0]), "codex")
}

func executeCodexRemoteCLI(args []string, cwd string, env localCLIEnv, initialPrompt string, reporter *localRunReporter) (int, error) {
	if err := validateCodexRemoteArgs(args[1:]); err != nil {
		return 1, err
	}

	appServer, upstreamURL, err := startCodexAppServer(args[0], cwd, env)
	if err != nil {
		return 1, err
	}
	defer stopCommand(appServer)

	proxy, err := newCodexRemoteProxy(upstreamURL, reporter, initialPrompt)
	if err != nil {
		return 1, err
	}
	defer proxy.Close(context.Background())

	childArgs := append([]string{"--remote", proxy.URL()}, args[1:]...)
	if strings.TrimSpace(initialPrompt) != "" {
		childArgs = append(childArgs, initialPrompt)
	}
	child := exec.Command(args[0], childArgs...)
	child.Dir = cwd
	child.Env = localCLIProcessEnv(os.Environ(), env)

	ptmx, err := pty.Start(child)
	if err != nil {
		return 1, err
	}
	defer ptmx.Close()
	restore, err := makeStdinRaw()
	if err != nil {
		return 1, err
	}
	defer restore()
	stopResizeWatch := watchTerminalResize(ptmx)
	defer stopResizeWatch()
	stopSignalForward := forwardSignals(child.Process)
	defer stopSignalForward()

	transcript := newStructuredTranscriptStream(reporter)
	go func() {
		_, _ = io.Copy(ptmx, os.Stdin)
	}()

	_, _ = io.Copy(io.MultiWriter(os.Stdout, transcript), ptmx)
	transcript.Flush()
	err = child.Wait()
	exitCode := 0
	if child.ProcessState != nil {
		exitCode = child.ProcessState.ExitCode()
	}
	return exitCode, err
}

func validateCodexRemoteArgs(args []string) error {
	for _, arg := range args {
		if arg == "--remote" || strings.HasPrefix(arg, "--remote=") {
			return fmt.Errorf("multica manages Codex --remote automatically; remove %s from the command", arg)
		}
		if arg == "app-server" {
			return fmt.Errorf("multica run manages codex app-server automatically")
		}
	}
	return nil
}

func startCodexAppServer(command, cwd string, env localCLIEnv) (*exec.Cmd, string, error) {
	var lastErr error
	for i := 0; i < codexAppServerStartRetries; i++ {
		addr, err := reserveLoopbackAddress()
		if err != nil {
			return nil, "", err
		}
		upstreamURL := "ws://" + addr
		cmd := exec.Command(command, "app-server", "--listen", upstreamURL)
		cmd.Dir = cwd
		cmd.Env = localCLIProcessEnv(os.Environ(), env)
		var logs limitedBuffer
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		if err := waitForWebSocket(upstreamURL, 5*time.Second); err != nil {
			lastErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(logs.String()))
			stopCommand(cmd)
			continue
		}
		return cmd, upstreamURL, nil
	}
	if lastErr == nil {
		lastErr = errors.New("codex app-server did not start")
	}
	return nil, "", fmt.Errorf("start codex app-server: %w", lastErr)
}

func reserveLoopbackAddress() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

func waitForWebSocket(rawURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, _, err := websocket.DefaultDialer.Dial(rawURL, nil)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func stopCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() < 16*1024 {
		_, _ = b.buf.Write(p)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type codexRemoteProxy struct {
	upstreamURL string
	reporter    *localRunReporter
	listener    net.Listener
	server      *http.Server
	mapper      *codexAppServerMapper
}

func newCodexRemoteProxy(upstreamURL string, reporter *localRunReporter, bootstrapPrompt string) (*codexRemoteProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &codexRemoteProxy{
		upstreamURL: upstreamURL,
		reporter:    reporter,
		listener:    ln,
		mapper:      newCodexAppServerMapper(reporter, bootstrapPrompt),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handle)
	p.server = &http.Server{Handler: mux}
	go func() {
		if err := p.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			reporter.Post(localCLIMessage{
				Type:    "error",
				Content: redact.Text("Codex remote proxy stopped: " + err.Error()),
				Source:  codexAppServerSource,
			})
		}
	}()
	return p, nil
}

func (p *codexRemoteProxy) URL() string {
	return "ws://" + p.listener.Addr().String()
}

func (p *codexRemoteProxy) Close(ctx context.Context) {
	if p == nil || p.server == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	_ = p.server.Shutdown(ctx)
}

func (p *codexRemoteProxy) handle(w http.ResponseWriter, r *http.Request) {
	requestedProtocols := websocket.Subprotocols(r)
	upgrader := websocket.Upgrader{
		Subprotocols: requestedProtocols,
		CheckOrigin:  func(r *http.Request) bool { return true },
	}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()

	header := http.Header{}
	if auth := r.Header.Get("Authorization"); auth != "" {
		header.Set("Authorization", auth)
	}
	dialer := websocket.Dialer{Subprotocols: requestedProtocols}
	upstream, _, err := dialer.Dial(p.upstreamURL, header)
	if err != nil {
		_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, err.Error()), time.Now().Add(time.Second))
		return
	}
	defer upstream.Close()

	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	var clientWriteMu sync.Mutex
	var upstreamWriteMu sync.Mutex
	done := make(chan struct{}, 2)
	go func() {
		p.copyMessages(client, upstream, &upstreamWriteMu, true)
		closeBoth()
		done <- struct{}{}
	}()
	go func() {
		p.copyMessages(upstream, client, &clientWriteMu, false)
		closeBoth()
		done <- struct{}{}
	}()
	<-done
}

func (p *codexRemoteProxy) copyMessages(src, dst *websocket.Conn, dstWriteMu *sync.Mutex, clientToServer bool) {
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage {
			p.mapper.Observe(clientToServer, payload)
		}
		dstWriteMu.Lock()
		err = dst.WriteMessage(messageType, payload)
		dstWriteMu.Unlock()
		if err != nil {
			return
		}
	}
}

type codexAppServerMapper struct {
	mu              sync.Mutex
	reporter        *localRunReporter
	bootstrap       string
	pending         map[string]codexPendingRequest
	turnComment     map[string]bool
	deltas          map[string]string
	currentThread   string
	syncedLifecycle map[string]bool
}

type codexPendingRequest struct {
	method string
	params map[string]any
}

func newCodexAppServerMapper(reporter *localRunReporter, bootstrapPrompt string) *codexAppServerMapper {
	return &codexAppServerMapper{
		reporter:        reporter,
		bootstrap:       bootstrapPrompt,
		pending:         make(map[string]codexPendingRequest),
		turnComment:     make(map[string]bool),
		deltas:          make(map[string]string),
		syncedLifecycle: make(map[string]bool),
	}
}

func (m *codexAppServerMapper) Observe(clientToServer bool, payload []byte) {
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}
	if clientToServer {
		m.observeClientMessage(msg)
		return
	}
	m.observeServerMessage(msg)
}

func (m *codexAppServerMapper) observeClientMessage(msg map[string]any) {
	method := stringValue(msg["method"])
	id := codexRPCID(msg["id"])
	if method == "" || id == "" {
		return
	}
	params, _ := msg["params"].(map[string]any)
	m.mu.Lock()
	m.pending[id] = codexPendingRequest{method: method, params: params}
	m.mu.Unlock()
}

func (m *codexAppServerMapper) observeServerMessage(msg map[string]any) {
	if method := stringValue(msg["method"]); method != "" {
		params, _ := msg["params"].(map[string]any)
		m.handleNotification(method, params)
		return
	}
	id := codexRPCID(msg["id"])
	if id == "" {
		return
	}
	m.mu.Lock()
	pending, ok := m.pending[id]
	if ok {
		delete(m.pending, id)
	}
	m.mu.Unlock()
	if !ok {
		return
	}
	result, _ := msg["result"].(map[string]any)
	switch pending.method {
	case "thread/start":
		m.handleThreadResult("started", pending.params, result)
	case "thread/resume":
		m.handleThreadResult("resumed", pending.params, result)
	}
}

func (m *codexAppServerMapper) handleNotification(method string, params map[string]any) {
	switch method {
	case "thread/started":
		if thread := nestedMap(params, "thread"); thread != nil {
			m.recordThreadLifecycle("started", thread, nil)
		}
	case "turn/started":
		m.recordTurnLifecycle("started", params)
	case "turn/completed":
		m.recordTurnLifecycle("completed", params)
	case "item/agentMessage/delta":
		m.recordAgentDelta(params)
	case "item/completed":
		m.recordCompletedItem(params)
	}
}

func (m *codexAppServerMapper) handleThreadResult(action string, reqParams, result map[string]any) {
	thread := nestedMap(result, "thread")
	if thread == nil {
		return
	}
	m.recordThreadLifecycle(action, thread, reqParams)
}

func (m *codexAppServerMapper) recordThreadLifecycle(action string, thread, reqParams map[string]any) {
	threadID := stringValue(thread["id"])
	if threadID == "" {
		return
	}
	source := stringValue(thread["sessionStartSource"])
	if source == "" && reqParams != nil {
		source = stringValue(reqParams["sessionStartSource"])
	}
	m.mu.Lock()
	m.currentThread = threadID
	m.mu.Unlock()
	content := "Codex thread " + action
	if source != "" {
		content += " (" + source + ")"
	}
	m.postLifecycle("thread:"+threadID+":"+action, content, map[string]any{
		"thread_id":  threadID,
		"session_id": stringValue(thread["sessionId"]),
		"path":       stringValue(thread["path"]),
		"cwd":        stringValue(thread["cwd"]),
		"source":     source,
	})
}

func (m *codexAppServerMapper) recordTurnLifecycle(action string, params map[string]any) {
	threadID := stringValue(params["threadId"])
	turn := nestedMap(params, "turn")
	turnID := stringValue(params["turnId"])
	if turnID == "" && turn != nil {
		turnID = stringValue(turn["id"])
	}
	if threadID == "" || turnID == "" {
		return
	}
	input := map[string]any{"thread_id": threadID, "turn_id": turnID}
	if turn != nil {
		input["status"] = stringValue(turn["status"])
		input["duration_ms"] = turn["durationMs"]
		input["error"] = turn["error"]
	}
	m.postLifecycle("thread:"+threadID+":turn:"+turnID+":"+action, "Codex turn "+action, input)
}

func (m *codexAppServerMapper) recordAgentDelta(params map[string]any) {
	itemID := stringValue(params["itemId"])
	delta := stringValue(params["delta"])
	if itemID == "" || delta == "" {
		return
	}
	m.mu.Lock()
	m.deltas[itemID] += delta
	m.mu.Unlock()
}

func (m *codexAppServerMapper) recordCompletedItem(params map[string]any) {
	item := nestedMap(params, "item")
	if item == nil {
		return
	}
	threadID := stringValue(params["threadId"])
	turnID := stringValue(params["turnId"])
	itemID := stringValue(item["id"])
	itemType := stringValue(item["type"])
	if threadID == "" || turnID == "" || itemID == "" || itemType == "" {
		return
	}
	switch itemType {
	case "userMessage":
		m.recordUserMessage(threadID, turnID, itemID, item)
	case "agentMessage":
		m.recordAgentMessage(threadID, turnID, itemID, item)
	case "commandExecution", "localShellCommand", "toolCall", "functionCall":
		m.recordToolItem(threadID, turnID, itemID, item)
	}
}

func (m *codexAppServerMapper) recordUserMessage(threadID, turnID, itemID string, item map[string]any) {
	content := strings.TrimSpace(codexAppServerItemText(item))
	commentable := content != "" && !m.isBootstrap(content) && !isSlashInput(content)
	m.mu.Lock()
	m.turnComment[turnID] = commentable
	m.mu.Unlock()
	if !commentable {
		return
	}
	m.post(localCLIMessage{
		Type:      "user_input",
		Content:   content,
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":user:" + itemID,
		Input: map[string]any{
			"thread_id": threadID,
			"turn_id":   turnID,
			"item_id":   itemID,
		},
	})
}

func (m *codexAppServerMapper) recordAgentMessage(threadID, turnID, itemID string, item map[string]any) {
	content := strings.TrimSpace(codexAppServerItemText(item))
	if content == "" {
		m.mu.Lock()
		content = strings.TrimSpace(m.deltas[itemID])
		m.mu.Unlock()
	}
	if content == "" {
		return
	}
	msgType := "text"
	if stringValue(item["phase"]) == "final_answer" {
		msgType = "final"
		m.mu.Lock()
		commentable := m.turnComment[turnID]
		m.mu.Unlock()
		if !commentable || isStatusOnly(content) {
			return
		}
	}
	m.post(localCLIMessage{
		Type:      msgType,
		Content:   content,
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":agent:" + itemID,
		Input: map[string]any{
			"thread_id": threadID,
			"turn_id":   turnID,
			"item_id":   itemID,
			"phase":     stringValue(item["phase"]),
		},
	})
}

func (m *codexAppServerMapper) recordToolItem(threadID, turnID, itemID string, item map[string]any) {
	tool := firstString(item, "name", "tool", "type")
	input := map[string]any{
		"thread_id": threadID,
		"turn_id":   turnID,
		"item_id":   itemID,
	}
	for _, key := range []string{"command", "cwd", "status", "exitCode"} {
		if v, ok := item[key]; ok {
			input[toSnake(key)] = v
		}
	}
	output := strings.TrimSpace(firstString(item, "output", "text", "content"))
	m.post(localCLIMessage{
		Type:      "tool_use",
		Tool:      tool,
		Input:     input,
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":tool:" + itemID + ":complete",
	})
	if output != "" {
		m.post(localCLIMessage{
			Type:      "tool_result",
			Tool:      tool,
			Output:    output,
			SourceKey: "thread:" + threadID + ":turn:" + turnID + ":tool:" + itemID + ":result",
		})
	}
}

func (m *codexAppServerMapper) postLifecycle(key, content string, input map[string]any) {
	m.mu.Lock()
	if m.syncedLifecycle[key] {
		m.mu.Unlock()
		return
	}
	m.syncedLifecycle[key] = true
	m.mu.Unlock()
	m.post(localCLIMessage{
		Type:      "event",
		Content:   content,
		Input:     input,
		SourceKey: key,
	})
}

func (m *codexAppServerMapper) post(msg localCLIMessage) {
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Output) == "" && msg.Type != "tool_use" {
		return
	}
	msg.Source = codexAppServerSource
	msg.Content = redact.Text(strings.TrimSpace(msg.Content))
	msg.Output = redact.Text(strings.TrimSpace(msg.Output))
	msg.Input = redactInputMap(msg.Input)
	m.reporter.Post(msg)
}

func (m *codexAppServerMapper) isBootstrap(content string) bool {
	if strings.TrimSpace(m.bootstrap) == "" {
		return false
	}
	candidate := normalizeProviderText(content)
	bootstrap := normalizeProviderText(m.bootstrap)
	if candidate == "" || bootstrap == "" {
		return false
	}
	if candidate == bootstrap {
		return true
	}
	return strings.Contains(candidate, "You are assigned to Multica issue") &&
		strings.Contains(candidate, "Assigned issue ID:")
}

func codexRPCID(v any) string {
	switch id := v.(type) {
	case string:
		return id
	case float64:
		return fmt.Sprintf("%.0f", id)
	default:
		return ""
	}
}

func nestedMap(obj map[string]any, key string) map[string]any {
	if obj == nil {
		return nil
	}
	v, _ := obj[key].(map[string]any)
	return v
}

func codexAppServerItemText(item map[string]any) string {
	if text := firstString(item, "text", "message", "content"); text != "" {
		return text
	}
	content, _ := item["content"].([]any)
	var parts []string
	for _, entry := range content {
		switch v := entry.(type) {
		case string:
			parts = append(parts, v)
		case map[string]any:
			if text := firstString(v, "text", "message", "content"); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func toSnake(s string) string {
	switch s {
	case "exitCode":
		return "exit_code"
	default:
		return s
	}
}
