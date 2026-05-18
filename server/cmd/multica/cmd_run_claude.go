package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/redact"
	"github.com/spf13/cobra"
)

const claudeJSONLSource = "claude-jsonl"

var claudeSessionHookPort int

var claudeSessionHookCmd = &cobra.Command{
	Use:    "__claude-session-hook",
	Hidden: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runClaudeSessionHookForwarder(cmd.Context(), claudeSessionHookPort, os.Stdin)
	},
}

func init() {
	claudeSessionHookCmd.Flags().IntVar(&claudeSessionHookPort, "port", 0, "Claude hook receiver port")
}

func (claudeLocalRunProvider) Run(args []string, cwd string, env localCLIEnv, _ string, reporter *localRunReporter) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("missing Claude command")
	}
	if err := validateClaudeLocalRunArgs(args[1:]); err != nil {
		return 1, err
	}

	tracker := newClaudeTranscriptTracker(reporter, cwd, "", time.Now())
	tracker.Start()
	defer tracker.Close()

	hookServer, err := startClaudeSessionHookServer(tracker.ObserveSessionHook)
	if err != nil {
		return 1, err
	}
	defer hookServer.Close(context.Background())

	settingsPath, cleanupSettings, err := writeClaudeHookSettings(hookServer.Port())
	if err != nil {
		return 1, err
	}
	defer cleanupSettings()

	systemPrompt := claudeLocalRunSystemPrompt(env.IssueID)
	childArgs := claudeLocalRunChildArgs(args, settingsPath, systemPrompt)
	return runProviderPTY(childArgs, cwd, env, "")
}

func claudeLocalRunChildArgs(args []string, settingsPath string, systemPrompt string) []string {
	childArgs := append([]string{args[0]}, args[1:]...)
	childArgs = append(childArgs, "--settings", settingsPath)
	if strings.TrimSpace(systemPrompt) != "" {
		childArgs = append(childArgs, "--append-system-prompt", systemPrompt)
	}
	return childArgs
}

func claudeLocalRunSystemPrompt(issueID string) string {
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

func validateClaudeLocalRunArgs(args []string) error {
	for _, arg := range args {
		if arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			return fmt.Errorf("multica manages Claude --settings automatically; remove %s from the command", arg)
		}
	}
	return nil
}

func runClaudeSessionHookForwarder(ctx context.Context, port int, in io.Reader) error {
	if port <= 0 {
		return fmt.Errorf("missing Claude hook receiver port")
	}
	payload, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		payload = []byte(`{}`)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/hook/session-start"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	return nil
}

type claudeSessionHookPayload struct {
	SessionID      string
	TranscriptPath string
	Cwd            string
	Raw            map[string]any
}

type claudeSessionHookServer struct {
	listener net.Listener
	server   *http.Server
}

func startClaudeSessionHookServer(onSession func(claudeSessionHookPayload)) (*claudeSessionHookServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/hook/session-start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		payload, err := parseClaudeSessionHookPayload(r.Body)
		if err != nil {
			http.Error(w, "invalid hook payload", http.StatusBadRequest)
			return
		}
		onSession(payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	server := &http.Server{Handler: mux}
	out := &claudeSessionHookServer{listener: ln, server: server}
	go func() {
		_ = server.Serve(ln)
	}()
	return out, nil
}

func (s *claudeSessionHookServer) Port() int {
	if s == nil || s.listener == nil {
		return 0
	}
	if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *claudeSessionHookServer) Close(ctx context.Context) {
	if s == nil || s.server == nil {
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
	_ = s.server.Shutdown(ctx)
}

func parseClaudeSessionHookPayload(r io.Reader) (claudeSessionHookPayload, error) {
	var raw map[string]any
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return claudeSessionHookPayload{}, err
	}
	return claudeSessionHookPayload{
		SessionID:      firstString(raw, "session_id", "sessionId"),
		TranscriptPath: firstString(raw, "transcript_path", "transcriptPath"),
		Cwd:            firstString(raw, "cwd", "CWD"),
		Raw:            raw,
	}, nil
}

func writeClaudeHookSettings(port int) (string, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "multica-claude-hooks-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	command := shellQuote(exe) + " __claude-session-hook --port " + strconv.Itoa(port)
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type claudeTranscriptTracker struct {
	reporter         *localRunReporter
	cwd              string
	bootstrap        string
	startedAt        time.Time
	tickerInterval   time.Duration
	mu               sync.Mutex
	sessions         map[string]*claudeTrackedSession
	seen             map[string]bool
	toolByID         map[string]string
	currentTurnReply bool
	done             chan struct{}
	stopped          chan struct{}
	startOnce        sync.Once
	closeOnce        sync.Once
}

type claudeTrackedSession struct {
	sessionID     string
	path          string
	baselineLines int
}

func newClaudeTranscriptTracker(reporter *localRunReporter, cwd, bootstrapPrompt string, startedAt time.Time) *claudeTranscriptTracker {
	return &claudeTranscriptTracker{
		reporter:       reporter,
		cwd:            cwd,
		bootstrap:      bootstrapPrompt,
		startedAt:      startedAt,
		tickerInterval: 500 * time.Millisecond,
		sessions:       make(map[string]*claudeTrackedSession),
		seen:           make(map[string]bool),
		toolByID:       make(map[string]string),
		done:           make(chan struct{}),
		stopped:        make(chan struct{}),
	}
}

func (t *claudeTranscriptTracker) Start() {
	t.startOnce.Do(func() {
		go func() {
			defer close(t.stopped)
			ticker := time.NewTicker(t.tickerInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					t.Sync()
				case <-t.done:
					t.Sync()
					return
				}
			}
		}()
	})
}

func (t *claudeTranscriptTracker) Close() {
	t.closeOnce.Do(func() {
		close(t.done)
		<-t.stopped
	})
}

func (t *claudeTranscriptTracker) ObserveSessionHook(payload claudeSessionHookPayload) {
	path := strings.TrimSpace(payload.TranscriptPath)
	sessionID := strings.TrimSpace(payload.SessionID)
	if path == "" && sessionID != "" {
		path = filepath.Join(claudeProjectDir(t.cwd), sessionID+".jsonl")
	}
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		base := payload.Cwd
		if base == "" {
			base = t.cwd
		}
		path = filepath.Join(base, path)
	}
	key := path
	baseline := countJSONLLines(path)
	t.mu.Lock()
	t.sessions[key] = &claudeTrackedSession{
		sessionID:     sessionID,
		path:          path,
		baselineLines: baseline,
	}
	t.mu.Unlock()
	t.Sync()
}

func (t *claudeTranscriptTracker) Sync() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, session := range t.sessions {
		t.syncSessionLocked(session)
	}
}

func (t *claudeTranscriptTracker) syncSessionLocked(session *claudeTrackedSession) {
	file, err := os.Open(session.path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		key := t.claudeLineKey(session, raw, lineNo)
		if t.seen[key] {
			continue
		}
		t.seen[key] = true
		if t.shouldSkipHistoricalLine(raw, lineNo, session.baselineLines) {
			continue
		}
		t.mapLineLocked(session, raw, key)
	}
}

func (t *claudeTranscriptTracker) shouldSkipHistoricalLine(raw map[string]any, lineNo, baselineLines int) bool {
	if ts := claudeLineTimestamp(raw); !ts.IsZero() {
		return ts.Before(t.startedAt.Add(-1 * time.Second))
	}
	return baselineLines > 0 && lineNo <= baselineLines
}

func (t *claudeTranscriptTracker) mapLineLocked(session *claudeTrackedSession, raw map[string]any, key string) {
	switch stringValue(raw["type"]) {
	case "user":
		t.mapUserLineLocked(session, raw, key)
	case "assistant":
		t.mapAssistantLineLocked(session, raw, key)
	case "result":
		t.mapResultLineLocked(session, raw, key)
	}
}

func (t *claudeTranscriptTracker) mapUserLineLocked(session *claudeTrackedSession, raw map[string]any, key string) {
	if isTrue(raw["isMeta"]) {
		return
	}
	msg := nestedMap(raw, "message")
	blocks := claudeContentBlocks(msg["content"])
	if len(blocks) == 0 {
		if content := strings.TrimSpace(stringValue(msg["content"])); content != "" {
			if isClaudeLocalCommandUserContent(content) {
				return
			}
			t.recordClaudeUserPromptLocked(session, content, key)
		}
		return
	}
	for _, block := range blocks {
		if stringValue(block["type"]) != "tool_result" {
			continue
		}
		toolUseID := firstString(block, "tool_use_id", "toolUseId", "id")
		tool := t.toolByID[toolUseID]
		if tool == "" {
			tool = "tool"
		}
		output := claudeToolResultContentString(block["content"])
		if output == "" {
			output = firstString(block, "text")
		}
		t.post(localCLIMessage{
			Type:      "tool_result",
			Tool:      tool,
			Output:    output,
			SourceKey: t.sourceKey(session, key, "tool-result:"+toolUseID),
			Input: map[string]any{
				"tool_use_id": toolUseID,
			},
		})
	}
}

func (t *claudeTranscriptTracker) recordClaudeUserPromptLocked(session *claudeTrackedSession, content, key string) {
	content = strings.TrimSpace(content)
	commentable := content != "" && !t.isBootstrap(content) && !isSlashInput(content)
	t.currentTurnReply = commentable
	if !commentable {
		return
	}
	t.post(localCLIMessage{
		Type:      "user_input",
		Content:   content,
		SourceKey: t.sourceKey(session, key, "user"),
		Input: map[string]any{
			"session_id": session.sessionID,
		},
	})
}

func (t *claudeTranscriptTracker) mapAssistantLineLocked(session *claudeTrackedSession, raw map[string]any, key string) {
	msg := nestedMap(raw, "message")
	var textParts []string
	for idx, block := range claudeContentBlocks(msg["content"]) {
		switch stringValue(block["type"]) {
		case "text":
			text := strings.TrimSpace(firstString(block, "text", "content"))
			if text != "" {
				textParts = append(textParts, text)
			}
		case "thinking":
			thinking := strings.TrimSpace(firstString(block, "text", "content", "thinking"))
			if thinking != "" {
				t.post(localCLIMessage{
					Type:      "thinking",
					Content:   thinking,
					SourceKey: t.sourceKey(session, key, "thinking:"+strconv.Itoa(idx)),
				})
			}
		case "tool_use":
			toolUseID := firstString(block, "id", "tool_use_id", "toolUseId")
			tool := firstString(block, "name", "tool")
			if tool != "" && toolUseID != "" {
				t.toolByID[toolUseID] = tool
			}
			input, _ := block["input"].(map[string]any)
			t.post(localCLIMessage{
				Type:      "tool_use",
				Tool:      tool,
				Input:     input,
				SourceKey: t.sourceKey(session, key, "tool-use:"+toolUseID),
			})
		}
	}
	content := strings.TrimSpace(strings.Join(textParts, "\n\n"))
	if content == "" {
		return
	}
	t.post(localCLIMessage{
		Type:      "text",
		Content:   content,
		SourceKey: t.sourceKey(session, key, "text"),
	})
	if t.currentTurnReply && claudeStopReason(msg) == "end_turn" && !isStatusOnly(content) {
		t.post(localCLIMessage{
			Type:      "final",
			Content:   content,
			SourceKey: t.sourceKey(session, key, "final"),
		})
		t.currentTurnReply = false
	}
}

func isClaudeLocalCommandUserContent(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	for _, prefix := range []string{
		"<local-command-caveat>",
		"<command-name>",
		"<local-command-stdout>",
	} {
		if strings.HasPrefix(content, prefix) {
			return true
		}
	}
	return false
}

func claudeStopReason(msg map[string]any) string {
	return firstString(msg, "stop_reason", "stopReason")
}

func (t *claudeTranscriptTracker) mapResultLineLocked(session *claudeTrackedSession, raw map[string]any, key string) {
	if isTrue(raw["is_error"]) || isTrue(raw["isError"]) {
		content := firstString(raw, "error", "message", "result")
		if content == "" {
			content = "claude turn failed"
		}
		t.post(localCLIMessage{
			Type:      "error",
			Content:   content,
			SourceKey: t.sourceKey(session, key, "error"),
		})
	}
}

func (t *claudeTranscriptTracker) post(msg localCLIMessage) {
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Output) == "" && msg.Type != "tool_use" && msg.Type != "tool_result" {
		return
	}
	msg.Source = claudeJSONLSource
	msg.Content = redact.Text(strings.TrimSpace(msg.Content))
	msg.Output = redact.Text(strings.TrimSpace(msg.Output))
	msg.Input = redactInputMap(msg.Input)
	t.reporter.Post(msg)
}

func (t *claudeTranscriptTracker) sourceKey(session *claudeTrackedSession, lineKey, suffix string) string {
	sessionID := session.sessionID
	if sessionID == "" {
		sessionID = filepath.Base(session.path)
	}
	return "claude:session:" + sessionID + ":line:" + lineKey + ":" + suffix
}

func (t *claudeTranscriptTracker) claudeLineKey(session *claudeTrackedSession, raw map[string]any, lineNo int) string {
	if uuid := firstString(raw, "uuid", "message_uuid", "messageUuid"); uuid != "" {
		return uuid
	}
	if leaf := firstString(raw, "leafUuid", "leaf_uuid"); leaf != "" {
		if summary := stringValue(raw["summary"]); summary != "" {
			return leaf + ":summary:" + summary
		}
		return leaf
	}
	return filepath.Base(session.path) + ":" + strconv.Itoa(lineNo)
}

func (t *claudeTranscriptTracker) isBootstrap(content string) bool {
	if strings.TrimSpace(t.bootstrap) == "" {
		return false
	}
	candidate := normalizeCapturedUserText(content)
	bootstrap := normalizeCapturedUserText(t.bootstrap)
	if candidate == "" || bootstrap == "" {
		return false
	}
	if candidate == bootstrap {
		return true
	}
	return strings.Contains(candidate, "You are assigned to Multica issue") &&
		strings.Contains(candidate, "Assigned issue ID:")
}

func claudeContentBlocks(content any) []map[string]any {
	switch v := content.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				out = append(out, block)
			}
		}
		return out
	default:
		return nil
	}
}

func claudeToolResultContentString(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func claudeLineTimestamp(raw map[string]any) time.Time {
	value := firstString(raw, "timestamp", "created_at", "createdAt")
	if value == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func countJSONLLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}

var claudeProjectPathRe = regexp.MustCompile(`[^a-zA-Z0-9-]`)

func claudeProjectDir(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = filepath.Clean(cwd)
	}
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, ".claude")
		}
	}
	projectID := claudeProjectPathRe.ReplaceAllString(abs, "-")
	return filepath.Join(root, "projects", projectID)
}

func isTrue(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}
