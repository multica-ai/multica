package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// geminiBackend implements Backend by spawning Google's AGY CLI. AGY print
// mode emits plain stdout today, while AGY hook/transcript integrations emit
// JSONL events. The backend accepts both so local hook wiring can stream real
// agent events without breaking ordinary `agy -p` execution.
type geminiBackend struct {
	cfg Config
}

func (b *geminiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "agy"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("agy executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	hookBridge, err := setupAgyHookBridge(opts.Cwd, b.cfg.Logger)
	if err != nil {
		cancel()
		return nil, err
	}
	args := buildGeminiArgs(prompt, opts, b.cfg.Logger)
	if hookBridge != nil {
		args = append([]string{"--add-dir", hookBridge.rootDir}, args...)
	}

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildGeminiEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		if hookBridge != nil {
			hookBridge.cleanup(b.cfg.Logger)
		}
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[agy:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		if hookBridge != nil {
			hookBridge.cleanup(b.cfg.Logger)
		}
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so scanner.Scan() unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer close(resCh)
		defer close(msgCh)
		defer cancel()
		if hookBridge != nil {
			defer hookBridge.cleanup(b.cfg.Logger)
		}

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)
		var plainStdout strings.Builder
		var stateMu sync.Mutex
		var jsonEventsSeen atomic.Bool
		var jsonTextSeen atomic.Bool
		watchCtx, stopWatch := context.WithCancel(runCtx)
		var watchWG sync.WaitGroup
		var stopWatchOnce sync.Once
		stopWatchers := func() {
			stopWatchOnce.Do(func() {
				stopWatch()
				watchWG.Wait()
			})
		}
		defer stopWatchers()

		handleEvent := func(evt agyStreamEvent) {
			messages := evt.messages()
			eventUsage := evt.extractUsage()
			var hasText bool
			stateMu.Lock()
			if id := evt.sessionID(); id != "" {
				sessionID = id
			}
			mergeAgyUsage(usage, eventUsage)
			for _, msg := range messages {
				if msg.Type == MessageText {
					appendAgyOutput(&output, msg.Content)
					hasText = true
				}
			}
			stateMu.Unlock()

			if len(messages) > 0 {
				jsonEventsSeen.Store(true)
				if hasText {
					jsonTextSeen.Store(true)
				}
			}
			for _, msg := range messages {
				trySend(msgCh, msg)
			}
		}
		if hookBridge != nil {
			watchWG.Add(1)
			go func() {
				defer watchWG.Done()
				watchAgyHookEvents(watchCtx, hookBridge.eventsPath, handleEvent, b.cfg.Logger)
			}()
		}

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
		for scanner.Scan() {
			line := scanner.Text()
			if evt, ok := parseAgyJSONLine(line); ok {
				handleEvent(evt)
				continue
			}
			if hookBridge != nil {
				appendAgyOutput(&plainStdout, line)
				continue
			}
			if jsonEventsSeen.Load() {
				continue
			}
			stateMu.Lock()
			appendAgyOutput(&output, line)
			stateMu.Unlock()
			if line != "" {
				trySend(msgCh, Message{Type: MessageText, Content: line})
			}
		}
		if err := scanner.Err(); err != nil && runCtx.Err() == nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("read agy output: %v", err)
		}

		waitErr := cmd.Wait()
		if hookBridge != nil {
			time.Sleep(300 * time.Millisecond)
			stopWatchers()
		}
		if hookBridge != nil && !jsonTextSeen.Load() && plainStdout.Len() > 0 {
			content := plainStdout.String()
			stateMu.Lock()
			appendAgyOutput(&output, content)
			stateMu.Unlock()
			trySend(msgCh, Message{Type: MessageText, Content: content})
		}
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		}
		stateMu.Lock()
		finalOutput := output.String()
		finalSessionID := sessionID
		finalUsage := usage
		stateMu.Unlock()
		if authErr := agyAuthError(finalOutput); authErr != "" && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = authErr
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  finalSessionID,
			Usage:      finalUsage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func agyAuthError(output string) string {
	normalized := strings.ToLower(output)
	if strings.Contains(normalized, "authentication required") ||
		strings.Contains(normalized, "authentication timed out") {
		return "agy authentication required"
	}
	return ""
}

func appendAgyOutput(output *strings.Builder, content string) {
	if output.Len() > 0 {
		output.WriteByte('\n')
	}
	output.WriteString(content)
}

type agyHookBridge struct {
	rootDir    string
	eventsPath string
}

func setupAgyHookBridge(cwd string, logger *slog.Logger) (*agyHookBridge, error) {
	if cwd == "" {
		return nil, nil
	}
	rootDir, err := os.MkdirTemp("", "multica-agy-hooks-*")
	if err != nil {
		return nil, fmt.Errorf("set up AGY hooks: create temporary hook root: %w", err)
	}
	cleanupOnError := func() {
		if err := os.RemoveAll(rootDir); err != nil {
			logger.Debug("AGY hook bridge cleanup failed", "path", rootDir, "error", err)
		}
	}

	agentsDir := filepath.Join(rootDir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: create .agents directory: %w", err)
	}

	eventsPath := filepath.Join(agentsDir, "multica-agy-events.jsonl")
	if err := os.WriteFile(eventsPath, nil, 0o600); err != nil {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: create event stream: %w", err)
	}
	scriptPath := filepath.Join(agentsDir, "multica-agy-hook.sh")
	script := "#!/bin/sh\n" +
		"event_file=" + shellQuote(eventsPath) + "\n" +
		"tmp=\"${event_file}.$$\"\n" +
		"cat > \"$tmp\"\n" +
		"if [ -s \"$tmp\" ]; then\n" +
		"  cat \"$tmp\" >> \"$event_file\"\n" +
		"  printf '\\n' >> \"$event_file\"\n" +
		"fi\n" +
		"if grep -q '\"toolCall\"' \"$tmp\" 2>/dev/null; then\n" +
		"  rm -f \"$tmp\"\n" +
		"  printf '{\"decision\":\"allow\"}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"rm -f \"$tmp\"\n" +
		"printf '{}\\n'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: write hook script: %w", err)
	}

	hooksPath := filepath.Join(agentsDir, "hooks.json")
	hooks := make(map[string]json.RawMessage)
	if data, err := os.ReadFile(hooksPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &hooks); err != nil {
			cleanupOnError()
			return nil, fmt.Errorf("set up AGY hooks: parse existing hooks.json: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: read existing hooks.json: %w", err)
	}

	bridgeConfig, err := json.Marshal(agyHookDefinition(shellQuote(scriptPath)))
	if err != nil {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: marshal hook definition: %w", err)
	}
	hooks["multica-agy-json-stream"] = bridgeConfig
	data, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: marshal hooks.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(hooksPath, data, 0o600); err != nil {
		cleanupOnError()
		return nil, fmt.Errorf("set up AGY hooks: write hooks.json: %w", err)
	}

	logger.Debug("AGY hook bridge installed", "root", rootDir, "hooks", hooksPath, "events", eventsPath)
	return &agyHookBridge{rootDir: rootDir, eventsPath: eventsPath}, nil
}

func (b *agyHookBridge) cleanup(logger *slog.Logger) {
	if b == nil || b.rootDir == "" {
		return
	}
	if err := os.RemoveAll(b.rootDir); err != nil {
		logger.Debug("AGY hook bridge cleanup failed", "path", b.rootDir, "error", err)
	}
}

func agyHookDefinition(command string) map[string]any {
	handler := func() map[string]any {
		return map[string]any{
			"type":    "command",
			"command": command,
			"timeout": 10,
		}
	}
	return map[string]any{
		"PreInvocation":  []map[string]any{handler()},
		"PostInvocation": []map[string]any{handler()},
		"Stop":           []map[string]any{handler()},
		"PreToolUse": []map[string]any{{
			"matcher": "*",
			"hooks":   []map[string]any{handler()},
		}},
		"PostToolUse": []map[string]any{{
			"matcher": "*",
			"hooks":   []map[string]any{handler()},
		}},
	}
}

func watchAgyHookEvents(ctx context.Context, eventsPath string, handleEvent func(agyStreamEvent), logger *slog.Logger) {
	var watchedMu sync.Mutex
	watchedTranscripts := make(map[string]struct{})
	var transcriptWG sync.WaitGroup
	defer transcriptWG.Wait()

	tailJSONLFile(ctx, eventsPath, func(line string) {
		evt, ok := parseAgyJSONLine(line)
		if !ok {
			return
		}
		handleEvent(evt)
		transcriptPath := strings.TrimSpace(evt.TranscriptPath)
		if transcriptPath == "" {
			return
		}
		watchedMu.Lock()
		if _, ok := watchedTranscripts[transcriptPath]; ok {
			watchedMu.Unlock()
			return
		}
		watchedTranscripts[transcriptPath] = struct{}{}
		watchedMu.Unlock()

		logger.Debug("AGY transcript stream discovered", "path", transcriptPath)
		transcriptWG.Add(1)
		go func() {
			defer transcriptWG.Done()
			tailJSONLFile(ctx, transcriptPath, func(line string) {
				if transcriptEvt, ok := parseAgyJSONLine(line); ok {
					handleEvent(transcriptEvt)
				}
			}, logger)
		}()
	}, logger)
}

func tailJSONLFile(ctx context.Context, path string, handleLine func(string), logger *slog.Logger) {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		f, err := os.Open(path)
		if err == nil {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				_ = f.Close()
				logger.Debug("AGY JSONL tail seek failed", "path", path, "error", err)
			} else {
				reader := bufio.NewReader(f)
				for {
					chunk, readErr := reader.ReadString('\n')
					if len(chunk) > 0 {
						offset += int64(len(chunk))
						line := strings.TrimRight(chunk, "\r\n")
						if strings.TrimSpace(line) != "" {
							handleLine(line)
						}
					}
					if readErr == io.EOF {
						break
					}
					if readErr != nil {
						logger.Debug("AGY JSONL tail read failed", "path", path, "error", readErr)
						break
					}
				}
				_ = f.Close()
			}
		} else if !os.IsNotExist(err) {
			logger.Debug("AGY JSONL tail open failed", "path", path, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type agyStreamEvent struct {
	StepIndex             *int            `json:"step_index,omitempty"`
	Source                string          `json:"source,omitempty"`
	Type                  string          `json:"type,omitempty"`
	Status                string          `json:"status,omitempty"`
	Role                  string          `json:"role,omitempty"`
	Content               string          `json:"content,omitempty"`
	Text                  string          `json:"text,omitempty"`
	Thinking              string          `json:"thinking,omitempty"`
	Message               string          `json:"message,omitempty"`
	ConversationID        string          `json:"conversationId,omitempty"`
	ConversationIDAlt     string          `json:"conversation_id,omitempty"`
	SessionID             string          `json:"session_id,omitempty"`
	State                 string          `json:"state,omitempty"`
	AgentState            string          `json:"agentState,omitempty"`
	Model                 string          `json:"model,omitempty"`
	Cwd                   string          `json:"cwd,omitempty"`
	TranscriptPath        string          `json:"transcriptPath,omitempty"`
	ArtifactDirectoryPath string          `json:"artifactDirectoryPath,omitempty"`
	WorkspacePaths        []string        `json:"workspacePaths,omitempty"`
	ToolCalls             []agyToolCall   `json:"tool_calls,omitempty"`
	Error                 json.RawMessage `json:"error,omitempty"`
	Usage                 json.RawMessage `json:"usage,omitempty"`
	Metadata              json.RawMessage `json:"metadata,omitempty"`
	TokenUsage            json.RawMessage `json:"tokenUsage,omitempty"`
	TokenUsageAlt         json.RawMessage `json:"token_usage,omitempty"`
}

type agyToolCall struct {
	ID         string          `json:"id,omitempty"`
	CallID     string          `json:"call_id,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

func parseAgyJSONLine(line string) (agyStreamEvent, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return agyStreamEvent{}, false
	}

	var evt agyStreamEvent
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&evt); err != nil {
		return agyStreamEvent{}, false
	}
	if !evt.looksLikeAgyEvent() {
		return agyStreamEvent{}, false
	}
	return evt, true
}

func (e agyStreamEvent) looksLikeAgyEvent() bool {
	if e.StepIndex != nil || e.Source != "" || len(e.ToolCalls) > 0 {
		return true
	}
	if e.Status != "" && e.Type != "" {
		return true
	}
	if e.ConversationID != "" || e.ConversationIDAlt != "" || e.SessionID != "" {
		return true
	}
	if e.TranscriptPath != "" || e.ArtifactDirectoryPath != "" || len(e.WorkspacePaths) > 0 {
		return true
	}
	if e.State != "" || e.AgentState != "" || e.Cwd != "" || e.Model != "" {
		return true
	}
	if len(e.Usage) > 0 || len(e.Metadata) > 0 || len(e.TokenUsage) > 0 || len(e.TokenUsageAlt) > 0 {
		return true
	}
	if strings.EqualFold(e.Type, "message") && strings.EqualFold(e.Role, "assistant") {
		return true
	}
	return false
}

func (e agyStreamEvent) sessionID() string {
	for _, value := range []string{e.ConversationID, e.ConversationIDAlt, e.SessionID} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (e agyStreamEvent) messages() []Message {
	var messages []Message
	if state := firstNonEmpty(e.State, e.AgentState); state != "" {
		messages = append(messages, Message{Type: MessageStatus, Status: state, SessionID: e.sessionID()})
	}
	if e.Thinking != "" {
		messages = append(messages, Message{Type: MessageThinking, Content: e.Thinking, SessionID: e.sessionID()})
	}
	for i, call := range e.ToolCalls {
		messages = append(messages, Message{
			Type:      MessageToolUse,
			Tool:      firstNonEmpty(call.Name, call.Tool),
			CallID:    call.callID(e, i),
			Input:     rawObject(firstNonEmptyRaw(call.Args, call.Input, call.Parameters)),
			SessionID: e.sessionID(),
		})
	}
	if content := e.assistantText(); content != "" {
		messages = append(messages, Message{Type: MessageText, Content: content, SessionID: e.sessionID()})
	}
	if content := e.toolResultText(); content != "" {
		messages = append(messages, Message{
			Type:      MessageToolResult,
			CallID:    e.eventCallID(),
			Output:    content,
			SessionID: e.sessionID(),
		})
	}
	if errText := e.errorText(); errText != "" {
		messages = append(messages, Message{Type: MessageError, Content: errText, SessionID: e.sessionID()})
	}
	return messages
}

func (e agyStreamEvent) assistantText() string {
	content := firstNonEmpty(e.Content, e.Text)
	if content == "" {
		return ""
	}
	if strings.EqualFold(e.Type, "message") && strings.EqualFold(e.Role, "assistant") {
		return content
	}
	if strings.EqualFold(e.Source, "MODEL") && strings.EqualFold(e.Type, "PLANNER_RESPONSE") && len(e.ToolCalls) == 0 {
		return content
	}
	if strings.EqualFold(e.Type, "MODEL_RESPONSE") || strings.EqualFold(e.Type, "ASSISTANT_MESSAGE") {
		return content
	}
	return ""
}

func (e agyStreamEvent) toolResultText() string {
	if e.Content == "" || e.assistantText() != "" {
		return ""
	}
	typ := strings.ToUpper(e.Type)
	switch typ {
	case "RUN_COMMAND", "LIST_DIRECTORY", "SEARCH_WEB", "READ_FILE", "WRITE_FILE", "GENERIC", "TOOL_RESULT":
		return e.Content
	default:
		if strings.Contains(typ, "TOOL") && !strings.Contains(typ, "CALL") {
			return e.Content
		}
		return ""
	}
}

func (e agyStreamEvent) errorText() string {
	if strings.Contains(strings.ToUpper(e.Type), "ERROR") {
		if e.Message != "" {
			return e.Message
		}
		if e.Content != "" {
			return e.Content
		}
	}
	if len(e.Error) == 0 {
		return ""
	}
	var message string
	if err := json.Unmarshal(e.Error, &message); err == nil {
		return message
	}
	var obj struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(e.Error, &obj); err == nil {
		return firstNonEmpty(obj.Message, obj.Error)
	}
	return ""
}

func (e agyStreamEvent) eventCallID() string {
	if e.StepIndex == nil {
		return "agy-step"
	}
	return fmt.Sprintf("agy-step-%d", *e.StepIndex)
}

func (c agyToolCall) callID(e agyStreamEvent, index int) string {
	if id := firstNonEmpty(c.CallID, c.ID, c.ToolID); id != "" {
		return id
	}
	if e.StepIndex == nil {
		return fmt.Sprintf("agy-tool-%d", index)
	}
	return fmt.Sprintf("agy-step-%d-tool-%d", *e.StepIndex, index)
}

func rawObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&obj); err == nil {
		return obj
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil && text != "" {
		return map[string]any{"value": text}
	}
	return map[string]any{"raw": string(raw)}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(value) > 0 && string(value) != "null" {
			return value
		}
	}
	return nil
}

func (e agyStreamEvent) extractUsage() map[string]TokenUsage {
	usage := make(map[string]TokenUsage)
	mergeAgyUsage(usage, extractAgyUsageRaw(e.Usage, "agy"))
	mergeAgyUsage(usage, extractAgyUsageRaw(e.Metadata, "agy"))
	mergeAgyUsage(usage, extractAgyUsageRaw(e.TokenUsage, "agy"))
	mergeAgyUsage(usage, extractAgyUsageRaw(e.TokenUsageAlt, "agy"))
	if len(usage) == 0 {
		return nil
	}
	return usage
}

func mergeAgyUsage(dst map[string]TokenUsage, src map[string]TokenUsage) {
	for model, add := range src {
		if model == "" {
			model = "agy"
		}
		current := dst[model]
		current.InputTokens += add.InputTokens
		current.OutputTokens += add.OutputTokens
		current.CacheReadTokens += add.CacheReadTokens
		current.CacheWriteTokens += add.CacheWriteTokens
		dst[model] = current
	}
}

func extractAgyUsageRaw(raw json.RawMessage, fallbackModel string) map[string]TokenUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil
	}
	return extractAgyUsageValue(value, fallbackModel)
}

func extractAgyUsageValue(value any, fallbackModel string) map[string]TokenUsage {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, nestedKey := range []string{"tokenUsage", "token_usage", "usage"} {
		if nested, ok := obj[nestedKey]; ok {
			if usage := extractAgyUsageValue(nested, fallbackModel); len(usage) > 0 {
				return usage
			}
		}
	}
	if models, ok := obj["models"].(map[string]any); ok {
		result := make(map[string]TokenUsage)
		for model, raw := range models {
			mergeAgyUsage(result, extractAgyUsageValue(raw, model))
		}
		return result
	}
	if usage, ok := usageFromMap(obj); ok {
		model := stringField(obj, "model", "modelName", "model_name", "activeModel", "active_model")
		if model == "" {
			model = fallbackModel
		}
		return map[string]TokenUsage{model: usage}
	}

	result := make(map[string]TokenUsage)
	for model, raw := range obj {
		if nested, ok := raw.(map[string]any); ok {
			mergeAgyUsage(result, extractAgyUsageValue(nested, model))
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func usageFromMap(obj map[string]any) (TokenUsage, bool) {
	usage := TokenUsage{
		InputTokens:      intField(obj, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens"),
		OutputTokens:     intField(obj, "outputTokens", "output_tokens", "completionTokens", "completion_tokens"),
		CacheReadTokens:  intField(obj, "cacheReadTokens", "cache_read_tokens", "cachedTokens", "cached_tokens", "cacheRead", "cached"),
		CacheWriteTokens: intField(obj, "cacheWriteTokens", "cache_write_tokens"),
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0 {
		return TokenUsage{}, false
	}
	return usage, true
}

func intField(obj map[string]any, names ...string) int64 {
	for _, name := range names {
		switch v := obj[name].(type) {
		case json.Number:
			n, _ := v.Int64()
			return n
		case float64:
			return int64(v)
		case int64:
			return v
		case string:
			var parsed json.Number = json.Number(v)
			n, _ := parsed.Int64()
			return n
		}
	}
	return 0
}

func stringField(obj map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := obj[name].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ── Arg builder ──

// buildGeminiArgs assembles the argv for a one-shot AGY invocation.
//
// Flags:
//
//	--add-dir <cwd>      explicitly mount the daemon workspace for AGY's project
//	                      context; Execute prepends a temp hook root separately
//	-p / --prompt         non-interactive prompt (the user's task)
//	--dangerously-skip-permissions
//	                      auto-approve all tool executions
//	--conversation <id>   resume a previous session (if provided)
//
// geminiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var geminiBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,  // non-interactive prompt
	"--print":                        blockedWithValue,  // non-interactive prompt
	"--prompt":                       blockedWithValue,  // alias for --print
	"-i":                             blockedWithValue,  // interactive prompt mode
	"--prompt-interactive":           blockedWithValue,  // interactive prompt mode
	"-c":                             blockedStandalone, // continue mode
	"--continue":                     blockedStandalone, // continue mode
	"--conversation":                 blockedWithValue,  // daemon-managed resume
	"--dangerously-skip-permissions": blockedStandalone, // auto-approve tool use
	"--yolo":                         blockedStandalone, // legacy Gemini spelling
	"-m":                             blockedWithValue,  // legacy Gemini model flag; AGY uses settings
	"-r":                             blockedWithValue,  // legacy Gemini resume flag
	"-o":                             blockedWithValue,  // legacy Gemini output format
	"--output-format":                blockedWithValue,  // legacy Gemini output format
}

func buildGeminiArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	var args []string
	if opts.Cwd != "" {
		args = append(args, "--add-dir", opts.Cwd)
	}
	args = append(args,
		"-p", prompt,
		"--dangerously-skip-permissions",
	)
	if opts.Model != "" {
		logger.Warn("AGY CLI does not expose a model selection flag; ignoring runtime model override", "model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, geminiBlockedArgs, logger)...)
	return args
}

// buildGeminiEnv wraps buildEnv and defaults GEMINI_CLI_TRUST_WORKSPACE=true so
// legacy Gemini CLI folder-trust gates do not fail every headless daemon
// invocation with exit code 55 (FatalUntrustedWorkspaceError). AGY stores its
// persistent settings under `~/.gemini/antigravity-cli/settings.json` and the
// daemon supplies AGY's headless permission bypass on the command line, but this
// environment default remains harmless and preserves compatibility for older
// Gemini installations and user-pinned MULTICA_GEMINI_PATH values.
//
// If the caller explicitly sets the same key in cfg.Env it wins, preserving the
// ability to opt back into the check.
func buildGeminiEnv(extra map[string]string) []string {
	const trustKey = "GEMINI_CLI_TRUST_WORKSPACE"
	if _, ok := extra[trustKey]; ok {
		return buildEnv(extra)
	}
	merged := make(map[string]string, len(extra)+1)
	for k, v := range extra {
		merged[k] = v
	}
	merged[trustKey] = "true"
	return buildEnv(merged)
}
