package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	codexAppServerSource       = "codex-app-server"
	codexAppServerStartRetries = 5
	codexProposedPlanInputKind = "codex_proposed_plan"
)

func executeCodexRemoteCLI(args []string, cwd string, env localCLIEnv, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error) {
	if err := validateCodexRemoteArgs(args[1:]); err != nil {
		return 1, err
	}

	appServer, upstreamURL, err := startCodexAppServer(args[0], cwd, env)
	if err != nil {
		return 1, err
	}
	defer stopCodexSidecarCommand(appServer)

	systemPrompt := codexLocalRunSystemPrompt(env.IssueID)
	proxy, err := newCodexRemoteProxy(upstreamURL, reporter, usageReporter, systemPrompt)
	if err != nil {
		return 1, err
	}
	defer proxy.Close(context.Background())

	childArgs := codexRemoteChildArgs(args, proxy.URL())
	child := exec.Command(args[0], childArgs...)
	child.Dir = cwd
	child.Env = localCLIProcessEnv(os.Environ(), env)
	return runLocalRunPTYCommand(child, "")
}

func codexRemoteChildArgs(args []string, remoteURL string) []string {
	return append([]string{"--remote", remoteURL}, args[1:]...)
}

func codexLocalRunSystemPrompt(issueID string) string {
	issueID = strings.TrimSpace(issueID)
	var b strings.Builder
	b.WriteString("Multica local run context:\n")
	b.WriteString("You can read the Multica issue bound to this local run when the user explicitly asks about it. This is context access, not a startup task.\n\n")
	if issueID != "" {
		fmt.Fprintf(&b, "Bound Multica issue ID: %s\n\n", issueID)
		b.WriteString("Read-only commands for this bound issue:\n")
		fmt.Fprintf(&b, "- Get issue details: multica issue get %s --output json\n", issueID)
		fmt.Fprintf(&b, "- Get issue comments: multica issue comment list %s --output json\n\n", issueID)
	} else {
		b.WriteString("No bound Multica issue ID was provided in the local run environment.\n\n")
	}
	b.WriteString("Use those commands only when the user clearly asks about the current or bound Multica issue, issue details, issue status, issue description, task background, issue comments, what was said in comments, or previous discussion in the Multica issue.\n\n")
	b.WriteString("Do not use those commands for ordinary greetings, food, preferences, casual chat, slash commands, exit commands, local command output, or general coding questions that do not mention the Multica issue.\n\n")
	b.WriteString("If the user says comments and clearly means code comments, git commit messages, GitHub PR comments, or GitHub issue comments, do not assume they mean the bound Multica issue.\n\n")
	b.WriteString("After reading the issue or comments, answer only the user's current question. Do not offer next-step menus, ask what to do next, or suggest modifying, assigning, labeling, or changing priority unless explicitly asked.\n\n")
	b.WriteString("For later unrelated questions, answer normally and do not continue summarizing the issue. If the user asks whether you remember issue comments, answer from conversation history when sufficient; read comments again only if fresh details are needed.\n\n")
	b.WriteString("When reading comments, ignore local command pseudo-messages such as local-command-caveat, command-name, and local-command-stdout unless the user explicitly asks about local command output.\n")
	return b.String()
}

func validateCodexRemoteArgs(args []string) error {
	for _, arg := range args {
		if arg == "--remote" || strings.HasPrefix(arg, "--remote=") {
			return fmt.Errorf("multica manages Codex --remote automatically; remove %s from the command", arg)
		}
		if arg == "app-server" {
			return fmt.Errorf("multica run manages codex app-server automatically")
		}
		switch arg {
		case "exec", "review":
			return fmt.Errorf("multica run supports interactive Codex sessions only; use codex %s outside multica run", arg)
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
		prepareCodexSidecarCommand(cmd)
		var logs limitedBuffer
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		if err := waitForWebSocket(upstreamURL, 5*time.Second); err != nil {
			lastErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(logs.String()))
			stopCodexSidecarCommand(cmd)
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
	listener    net.Listener
	server      *http.Server
	mapper      *codexAppServerMapper
	mu          sync.Mutex
	conns       map[*websocket.Conn]struct{}
	closing     bool
	wg          sync.WaitGroup
}

func newCodexRemoteProxy(upstreamURL string, reporter *localRunReporter, usageReporter *localRunUsageReporter, developerInstructions string) (*codexRemoteProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mapper := newCodexAppServerMapper(reporter, usageReporter)
	mapper.developerInstructions = developerInstructions
	p := &codexRemoteProxy{
		upstreamURL: upstreamURL,
		listener:    ln,
		mapper:      mapper,
		conns:       make(map[*websocket.Conn]struct{}),
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
	p.mu.Lock()
	p.closing = true
	conns := make([]*websocket.Conn, 0, len(p.conns))
	for conn := range p.conns {
		conns = append(conns, conn)
	}
	p.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
	_ = p.server.Shutdown(ctx)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
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

	if !p.beginConnection(client, upstream) {
		return
	}
	defer p.endConnection(client, upstream)

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

func (p *codexRemoteProxy) beginConnection(conns ...*websocket.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		for _, conn := range conns {
			_ = conn.Close()
		}
		return false
	}
	p.wg.Add(1)
	for _, conn := range conns {
		p.conns[conn] = struct{}{}
	}
	return true
}

func (p *codexRemoteProxy) endConnection(conns ...*websocket.Conn) {
	p.mu.Lock()
	for _, conn := range conns {
		delete(p.conns, conn)
	}
	p.mu.Unlock()
	p.wg.Done()
}

func (p *codexRemoteProxy) copyMessages(src, dst *websocket.Conn, dstWriteMu *sync.Mutex, clientToServer bool) {
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage {
			if clientToServer {
				payload = p.mapper.rewriteClientMessage(payload)
			}
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
	mu                    sync.Mutex
	reporter              *localRunReporter
	usageReporter         *localRunUsageReporter
	developerInstructions string
	pending               map[string]codexPendingRequest
	turnComment           map[string]bool
	deltas                map[string]string
	turnUsage             map[string]localCLIUsage
	usageTotals           map[string]localCLIUsage
	threadModel           map[string]string
	turnModel             map[string]string
	activeThreadID        string
	activeTurnID          string
}

type codexPendingRequest struct {
	method string
	params map[string]any
}

func newCodexAppServerMapper(reporter *localRunReporter, usageReporter *localRunUsageReporter) *codexAppServerMapper {
	return &codexAppServerMapper{
		reporter:      reporter,
		usageReporter: usageReporter,
		pending:       make(map[string]codexPendingRequest),
		turnComment:   make(map[string]bool),
		deltas:        make(map[string]string),
		turnUsage:     make(map[string]localCLIUsage),
		usageTotals:   make(map[string]localCLIUsage),
		threadModel:   make(map[string]string),
		turnModel:     make(map[string]string),
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

func (m *codexAppServerMapper) rewriteClientMessage(payload []byte) []byte {
	if strings.TrimSpace(m.developerInstructions) == "" {
		return payload
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		return payload
	}
	method := stringValue(msg["method"])
	if method != "thread/start" && method != "thread/resume" {
		return payload
	}
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
		msg["params"] = params
	}
	params["developerInstructions"] = appendCodexDeveloperInstructions(
		stringValue(params["developerInstructions"]),
		m.developerInstructions,
	)
	rewritten, err := json.Marshal(msg)
	if err != nil {
		return payload
	}
	return rewritten
}

func appendCodexDeveloperInstructions(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return existing
	}
	if existing == "" {
		return addition
	}
	return existing + "\n\n" + addition
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
		if id := codexRPCID(msg["id"]); id != "" {
			m.recordServerRequest(id, method, params)
		}
		m.handleNotification(method, params)
		return
	}
	rpcErr := nestedMap(msg, "error")
	id := codexRPCID(msg["id"])
	if id == "" {
		if rpcErr != nil {
			m.recordError(firstString(rpcErr, "message", "error"))
		}
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
	if rpcErr != nil {
		m.recordError(firstString(rpcErr, "message", "error"))
		return
	}
	result, _ := msg["result"].(map[string]any)
	switch pending.method {
	case "thread/start":
		m.handleThreadResult(result)
	case "thread/resume":
		m.handleThreadResult(result)
	}
}

func (m *codexAppServerMapper) recordServerRequest(id, method string, params map[string]any) {
	switch method {
	case "item/tool/requestUserInput":
		m.recordUserInputRequest(id, params)
	}
}

func (m *codexAppServerMapper) recordUserInputRequest(id string, params map[string]any) {
	content, input := codexAppServerUserInputRequestContent(params)
	if content == "" {
		return
	}
	threadID := m.threadID(params)
	turnID := m.turnID(params)
	sourceKey := "request:" + id
	if threadID != "" && turnID != "" {
		sourceKey = "thread:" + threadID + ":turn:" + turnID + ":request:" + id
	}
	m.post(localCLIMessage{
		Type:      "text",
		Content:   content,
		Input:     input,
		SourceKey: sourceKey,
	})
}

func (m *codexAppServerMapper) handleNotification(method string, params map[string]any) {
	switch method {
	case "thread/started":
		if thread := nestedMap(params, "thread"); thread != nil {
			m.setActiveThread(stringValue(thread["id"]))
		}
	case "turn/started":
		m.trackTurn(params)
	case "turn/completed":
		m.trackTurn(params)
		m.recordFailedTurn(params)
	case "thread/tokenUsage/updated":
		m.recordThreadTokenUsage(params)
	case "model/rerouted":
		m.recordModelReroute(params)
	case "error":
		m.recordError(firstString(params, "message", "error"))
		if errObj := nestedMap(params, "error"); errObj != nil {
			m.recordError(firstString(errObj, "message", "error"))
		}
	case "item/agentMessage/delta":
		m.recordAgentDelta(params)
	case "item/plan/delta":
		m.recordPlanDelta(params)
	case "item/started":
		m.recordStartedItem(params)
	case "item/completed":
		m.recordCompletedItem(params)
	}
}

func (m *codexAppServerMapper) handleThreadResult(result map[string]any) {
	thread := nestedMap(result, "thread")
	if thread == nil {
		return
	}
	m.setActiveThreadModel(stringValue(thread["id"]), stringValue(result["model"]))
}

func (m *codexAppServerMapper) trackTurn(params map[string]any) {
	threadID := m.threadID(params)
	turn := nestedMap(params, "turn")
	turnID := stringValue(params["turnId"])
	if turnID == "" && turn != nil {
		turnID = stringValue(turn["id"])
	}
	m.mu.Lock()
	if threadID != "" {
		m.activeThreadID = threadID
	}
	if turnID != "" {
		m.activeTurnID = turnID
	}
	m.mu.Unlock()
}

func (m *codexAppServerMapper) recordAgentDelta(params map[string]any) {
	itemID := codexAppServerParamItemID(params)
	delta := stringValue(params["delta"])
	if itemID == "" || delta == "" {
		return
	}
	m.mu.Lock()
	m.deltas[itemID] += delta
	m.mu.Unlock()
}

func (m *codexAppServerMapper) recordPlanDelta(params map[string]any) {
	itemID := codexAppServerParamItemID(params)
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
	threadID := m.threadID(params)
	turnID := m.turnID(params)
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
	case "plan":
		m.recordPlanMessage(threadID, turnID, itemID, item)
	case "commandExecution":
		m.recordCommandResult(threadID, turnID, itemID, item)
	case "fileChange":
		m.recordPatchResult(threadID, turnID, itemID)
	}
}

func (m *codexAppServerMapper) recordStartedItem(params map[string]any) {
	item := nestedMap(params, "item")
	if item == nil {
		return
	}
	threadID := m.threadID(params)
	turnID := m.turnID(params)
	itemID := stringValue(item["id"])
	itemType := stringValue(item["type"])
	if threadID == "" || turnID == "" || itemID == "" || itemType == "" {
		return
	}
	switch itemType {
	case "commandExecution":
		input := map[string]any{}
		if command := stringValue(item["command"]); command != "" {
			input["command"] = command
		}
		m.post(localCLIMessage{
			Type:      "tool_use",
			Tool:      "exec_command",
			Input:     input,
			SourceKey: "thread:" + threadID + ":turn:" + turnID + ":tool:" + itemID + ":start",
		})
	case "fileChange":
		m.post(localCLIMessage{
			Type:      "tool_use",
			Tool:      "patch_apply",
			SourceKey: "thread:" + threadID + ":turn:" + turnID + ":tool:" + itemID + ":start",
		})
	}
}

func (m *codexAppServerMapper) recordUserMessage(threadID, turnID, itemID string, item map[string]any) {
	content := strings.TrimSpace(codexAppServerItemText(item))
	slash, isSlash := parseSlashInput(content)
	commentable := content != "" && (!isSlash || slash.Args != "")
	m.mu.Lock()
	m.turnComment[turnID] = commentable
	m.mu.Unlock()
	if !commentable {
		return
	}
	input := map[string]any{
		"thread_id": threadID,
		"turn_id":   turnID,
		"item_id":   itemID,
	}
	if isSlash {
		input["command"] = true
		input["slash_command"] = slash.Command
		input["slash_args"] = slash.Args
		input["commentable"] = true
	}
	m.post(localCLIMessage{
		Type:      "user_input",
		Content:   content,
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":user:" + itemID,
		Input:     input,
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
	phase := stringValue(item["phase"])
	m.post(localCLIMessage{
		Type:      "text",
		Content:   content,
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":agent:" + itemID + ":text",
	})

	if phase == "final_answer" {
		m.mu.Lock()
		commentable := m.turnComment[turnID]
		m.mu.Unlock()
		if !commentable || isStatusOnly(content) {
			return
		}
		m.post(localCLIMessage{
			Type:      "final",
			Content:   content,
			SourceKey: "thread:" + threadID + ":turn:" + turnID + ":agent:" + itemID + ":final",
		})
	}
}

func (m *codexAppServerMapper) recordPlanMessage(threadID, turnID, itemID string, item map[string]any) {
	content := strings.TrimSpace(codexAppServerItemText(item))
	if content == "" {
		m.mu.Lock()
		content = strings.TrimSpace(m.deltas[itemID])
		m.mu.Unlock()
	}
	if content == "" {
		return
	}
	m.post(localCLIMessage{
		Type:    "text",
		Content: "Proposed Plan\n\n" + content,
		Input: map[string]any{
			"kind":    codexProposedPlanInputKind,
			"item_id": itemID,
		},
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":plan:" + itemID,
	})
}

func (m *codexAppServerMapper) recordCommandResult(threadID, turnID, itemID string, item map[string]any) {
	output := strings.TrimSpace(firstString(item, "aggregatedOutput", "output", "text", "content"))
	m.post(localCLIMessage{
		Type:      "tool_result",
		Tool:      "exec_command",
		Output:    output,
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":tool:" + itemID + ":result",
	})
}

func (m *codexAppServerMapper) recordPatchResult(threadID, turnID, itemID string) {
	m.post(localCLIMessage{
		Type:      "tool_result",
		Tool:      "patch_apply",
		SourceKey: "thread:" + threadID + ":turn:" + turnID + ":tool:" + itemID + ":result",
	})
}

func (m *codexAppServerMapper) recordThreadTokenUsage(params map[string]any) {
	if m.usageReporter == nil {
		return
	}
	threadID := m.threadID(params)
	turnID := stringValue(params["turnId"])
	if turnID == "" {
		turnID = m.turnID(params)
	}
	tokenUsage := nestedMap(params, "tokenUsage")
	if tokenUsage == nil {
		return
	}
	last := nestedMap(tokenUsage, "last")
	if last == nil {
		return
	}
	if threadID == "" || turnID == "" {
		return
	}
	model := m.modelForTurn(threadID, turnID)
	usage := localCLIUsage{
		Provider:         "codex",
		Model:            model,
		InputTokens:      int64Value(firstAny(last, "inputTokens", "input_tokens")),
		OutputTokens:     int64Value(firstAny(last, "outputTokens", "output_tokens")),
		CacheReadTokens:  int64Value(firstAny(last, "cachedInputTokens", "cached_input_tokens")),
		CacheWriteTokens: 0,
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0 {
		return
	}
	turnKey := threadID + "\x00" + turnID
	m.mu.Lock()
	m.turnUsage[turnKey] = usage
	totals := m.recomputeUsageTotalsLocked()
	m.mu.Unlock()
	for _, total := range totals {
		m.usageReporter.Report(total)
	}
}

func (m *codexAppServerMapper) recordModelReroute(params map[string]any) {
	threadID := m.threadID(params)
	turnID := stringValue(params["turnId"])
	model := stringValue(params["toModel"])
	if threadID == "" || turnID == "" || model == "" {
		return
	}
	m.mu.Lock()
	m.turnModel[threadID+"\x00"+turnID] = model
	m.mu.Unlock()
}

func (m *codexAppServerMapper) modelForTurn(threadID, turnID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if model := strings.TrimSpace(m.turnModel[threadID+"\x00"+turnID]); model != "" {
		return model
	}
	if model := strings.TrimSpace(m.threadModel[threadID]); model != "" {
		return model
	}
	return "unknown"
}

func (m *codexAppServerMapper) recomputeUsageTotalsLocked() []localCLIUsage {
	next := make(map[string]localCLIUsage)
	for _, usage := range m.turnUsage {
		key := usage.Provider + "\x00" + usage.Model
		total := next[key]
		if total.Provider == "" {
			total.Provider = usage.Provider
			total.Model = usage.Model
		}
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.CacheReadTokens += usage.CacheReadTokens
		total.CacheWriteTokens += usage.CacheWriteTokens
		next[key] = total
	}
	for key, prev := range m.usageTotals {
		if _, ok := next[key]; !ok {
			next[key] = localCLIUsage{Provider: prev.Provider, Model: prev.Model}
		}
	}
	m.usageTotals = next
	totals := make([]localCLIUsage, 0, len(next))
	for _, total := range next {
		totals = append(totals, total)
	}
	return totals
}

func (m *codexAppServerMapper) recordFailedTurn(params map[string]any) {
	turn := nestedMap(params, "turn")
	if turn == nil || stringValue(turn["status"]) != "failed" {
		return
	}
	errMsg := "codex turn failed"
	if errObj := nestedMap(turn, "error"); errObj != nil {
		if msg := firstString(errObj, "message", "error"); msg != "" {
			errMsg = msg
		}
	}
	m.recordError(errMsg)
}

func (m *codexAppServerMapper) recordError(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	m.post(localCLIMessage{Type: "error", Content: content})
}

func (m *codexAppServerMapper) setActiveThread(threadID string) {
	if threadID == "" {
		return
	}
	m.mu.Lock()
	m.activeThreadID = threadID
	m.activeTurnID = ""
	m.mu.Unlock()
}

func (m *codexAppServerMapper) setActiveThreadModel(threadID, model string) {
	if threadID == "" {
		return
	}
	m.mu.Lock()
	m.activeThreadID = threadID
	m.activeTurnID = ""
	if strings.TrimSpace(model) != "" {
		m.threadModel[threadID] = strings.TrimSpace(model)
	}
	m.mu.Unlock()
}

func (m *codexAppServerMapper) threadID(params map[string]any) string {
	if threadID := stringValue(params["threadId"]); threadID != "" {
		return threadID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeThreadID
}

func (m *codexAppServerMapper) turnID(params map[string]any) string {
	if turnID := stringValue(params["turnId"]); turnID != "" {
		return turnID
	}
	if turn := nestedMap(params, "turn"); turn != nil {
		if turnID := stringValue(turn["id"]); turnID != "" {
			return turnID
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeTurnID
}

func (m *codexAppServerMapper) post(msg localCLIMessage) {
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Output) == "" && msg.Type != "tool_use" && msg.Type != "tool_result" {
		return
	}
	msg.Source = codexAppServerSource
	msg.Content = redact.Text(strings.TrimSpace(msg.Content))
	msg.Output = redact.Text(strings.TrimSpace(msg.Output))
	msg.Input = redactInputMap(msg.Input)
	m.reporter.Post(msg)
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

func firstNestedMap(obj map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if m := nestedMap(obj, key); m != nil {
			return m
		}
	}
	return nil
}

func firstAny(obj map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := obj[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

func int64Value(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

type codexAppServerUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type codexAppServerUserInputQuestion struct {
	ID       string                          `json:"id"`
	Header   string                          `json:"header"`
	Question string                          `json:"question"`
	Options  []codexAppServerUserInputOption `json:"options"`
}

type codexAppServerUserInputParams struct {
	Questions []codexAppServerUserInputQuestion `json:"questions"`
}

func codexAppServerUserInputRequestContent(params map[string]any) (string, map[string]any) {
	if params == nil {
		return "", nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return "", nil
	}
	var parsed codexAppServerUserInputParams
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Questions) == 0 {
		return "", nil
	}
	q := parsed.Questions[0]
	title := strings.TrimSpace(q.Header)
	detail := strings.TrimSpace(q.Question)
	if title == "" {
		title = detail
	}
	if title == "" && detail == "" {
		return "", nil
	}
	content := title
	if detail != "" && detail != title {
		content = title + "\n\n" + detail
	}
	options := make([]string, 0, len(q.Options))
	for _, opt := range q.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = strings.TrimSpace(opt.Description)
		}
		if label != "" {
			options = append(options, label)
		}
	}
	input := map[string]any{
		"kind":        "user_input_request",
		"question_id": q.ID,
	}
	if strings.EqualFold(title, "Proposed Plan") {
		input["kind"] = codexProposedPlanInputKind
	}
	if len(options) > 0 {
		input["options"] = options
	}
	return content, input
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

func codexAppServerParamItemID(params map[string]any) string {
	if itemID := stringValue(params["itemId"]); itemID != "" {
		return itemID
	}
	if item := nestedMap(params, "item"); item != nil {
		return stringValue(item["id"])
	}
	return ""
}
