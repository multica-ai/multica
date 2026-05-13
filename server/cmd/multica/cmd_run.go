package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/multica-ai/multica/server/pkg/redact"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var runCmd = &cobra.Command{
	Use:   "run <issue> -- <command...>",
	Short: "Run a local CLI command bound to an issue",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runLocalCLI,
}

func init() {
	runCmd.Flags().String("cwd", "", "Working directory for the local command (default: current directory)")
	runCmd.Flags().Bool("no-status-update", false, "Skip automatically moving backlog/todo issues to in_progress")
	runCmd.Flags().String("comments", "thread", "Issue comment mode: thread or off")
}

type localRunResponse struct {
	ID         string `json:"id"`
	IssueID    string `json:"issue_id"`
	CLIName    string `json:"cli_name"`
	ContextDir string `json:"context_dir"`
}

type localCLIMessage struct {
	Type    string         `json:"type"`
	Tool    string         `json:"tool,omitempty"`
	Content string         `json:"content,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
}

const invalidLocalRunMulticaToken = "multica-local-run-token-disabled"
const localRunHeartbeatInterval = 30 * time.Second

func runLocalCLI(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	issueInput := args[0]
	childArgs := args[1:]
	cliName := inferCLIName(childArgs[0])
	if cliName == "" {
		return fmt.Errorf("unable to infer CLI name")
	}

	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	commentsMode, _ := cmd.Flags().GetString("comments")
	if commentsMode != "thread" && commentsMode != "off" {
		return fmt.Errorf("--comments must be thread or off")
	}
	noStatusUpdate, _ := cmd.Flags().GetBool("no-status-update")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issueInput)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var run localRunResponse
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/local-runs", map[string]any{
		"cli_name":         cliName,
		"work_dir":         cwd,
		"comments_mode":    commentsMode,
		"no_status_update": noStatusUpdate,
	}, &run); err != nil {
		return fmt.Errorf("create local run: %w", err)
	}

	if err := client.PatchJSON(context.Background(), "/api/local-runs/"+url.PathEscape(run.ID), map[string]any{
		"status": "running",
	}, nil); err != nil {
		return fmt.Errorf("update local run status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Multica local run %s started.\n", run.ID)
	reporter := newLocalRunReporter(client, run.ID)
	stopHeartbeat := startLocalRunHeartbeat(client, run.ID, localRunHeartbeatInterval)
	exitCode, runErr := executeLocalCLI(childArgs, cwd, cliName, localCLIEnv{
		RunID:     run.ID,
		IssueID:   issueRef.ID,
		ServerURL: resolveServerURL(cmd),
		Token:     resolveToken(cmd),
	}, localRunPrompt(issueRef.ID), reporter)
	reporter.Close()
	stopHeartbeat()
	status := "completed"
	errText := ""
	if runErr != nil || exitCode != 0 {
		status = "failed"
		if runErr != nil {
			errText = runErr.Error()
		}
	}

	reportCtx, reportCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer reportCancel()
	if err := client.PatchJSON(reportCtx, "/api/local-runs/"+url.PathEscape(run.ID), map[string]any{
		"status":    status,
		"exit_code": exitCode,
		"error":     errText,
	}, nil); err != nil {
		return fmt.Errorf("report local run result: %w", err)
	}

	if runErr != nil {
		return runErr
	}
	if exitCode != 0 {
		return fmt.Errorf("local command exited with code %d", exitCode)
	}
	return nil
}

func inferCLIName(command string) string {
	base := filepath.Base(command)
	base = strings.TrimSuffix(base, ".exe")
	return strings.TrimSpace(base)
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func localRunPrompt(issueID string) string {
	return fmt.Sprintf(strings.Join([]string{
		"You are assigned to Multica issue %s.",
		"",
		"This local agent run is starting in bootstrap mode. Your job is to load context and then stay quiet until the user gives you more input.",
		"",
		"Assigned issue ID: %s",
		"",
		"You may use only these Multica CLI commands to read context:",
		"",
		"- `multica issue get %s --output json`",
		"- `multica issue comment list %s --output json`",
		"",
		"Do not use any other `multica` command during bootstrap. Do not create, update, assign, change status, add comments, delete comments, rerun tasks, or list unrelated workspace data.",
		"",
		"Follow the same context scope as the platform agent: read the assigned issue and its comments only. Do not proactively fetch parent issues, child issues, or issues mentioned in text unless the user later explicitly asks for them.",
		"",
		"After loading context, produce no output. Do not summarize the issue, do not say you are ready, and do not post a final answer. Wait silently for the user's next input.",
		"",
	}, "\n"), issueID, issueID, issueID, issueID)
}

type localRunMessagePoster interface {
	PostJSON(ctx context.Context, path string, body any, out any) error
}

type localRunStatusPatcher interface {
	PatchJSON(ctx context.Context, path string, body any, out any) error
}

func startLocalRunHeartbeat(client localRunStatusPatcher, runID string, interval time.Duration) func() {
	if client == nil || runID == "" || interval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		path := "/api/local-runs/" + url.PathEscape(runID)
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = client.PatchJSON(ctx, path, map[string]any{"status": "running"}, nil)
				cancel()
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
}

type localRunReporter struct {
	client localRunMessagePoster
	runID  string
	ch     chan localCLIMessage
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func newLocalRunReporter(client localRunMessagePoster, runID string) *localRunReporter {
	r := &localRunReporter{
		client: client,
		runID:  runID,
		ch:     make(chan localCLIMessage, 128),
		done:   make(chan struct{}),
	}
	go r.loop()
	return r
}

func (r *localRunReporter) Post(msg localCLIMessage) {
	if r == nil || strings.TrimSpace(msg.Content) == "" && msg.Output == "" && msg.Type != "tool_use" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	select {
	case r.ch <- msg:
	default:
	}
}

func (r *localRunReporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.ch)
	r.mu.Unlock()
	<-r.done
}

func (r *localRunReporter) loop() {
	defer close(r.done)
	path := "/api/local-runs/" + url.PathEscape(r.runID) + "/messages"
	for msg := range r.ch {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = r.client.PostJSON(ctx, path, msg, nil)
		cancel()
	}
}

type transcriptStream struct {
	reporter   *localRunReporter
	capture    *terminalTurnCapture
	raw        []byte
	rawScreen  *terminalScreen
	rawVisible string
	line       strings.Builder
	last       time.Time
}

func newTranscriptStream(reporter *localRunReporter, capture *terminalTurnCapture) *transcriptStream {
	return &transcriptStream{reporter: reporter, capture: capture, rawScreen: newTerminalScreen(), last: time.Now()}
}

func (s *transcriptStream) Write(p []byte) (int, error) {
	if s == nil {
		return len(p), nil
	}
	s.raw = append(s.raw, p...)
	if s.capture != nil {
		s.capture.Write(p)
	}
	for _, b := range p {
		if b == '\n' {
			s.flushLine()
			continue
		}
		s.line.WriteByte(b)
	}
	if (len(s.raw) >= 4096 || time.Since(s.last) >= time.Second) && !looksLikePotentialStructuredLine(s.line.String()) {
		s.FlushRaw()
	}
	return len(p), nil
}

func (s *transcriptStream) Flush() {
	if s == nil {
		return
	}
	s.flushLine()
	s.FlushRaw()
}

func (s *transcriptStream) FlushRaw() {
	if s == nil || len(s.raw) == 0 {
		return
	}
	s.flushRawBytes(s.raw)
	s.raw = s.raw[:0]
	s.last = time.Now()
}

func (s *transcriptStream) flushLine() {
	if s == nil || s.line.Len() == 0 {
		return
	}
	rawLine := s.line.String()
	line := strings.TrimSpace(strings.TrimRight(rawLine, "\r"))
	s.line.Reset()
	if msg, ok := parseStructuredMessage(line); ok {
		s.flushRawBeforeStructuredLine(rawLine)
		msg.Content = redact.Text(msg.Content)
		msg.Output = redact.Text(msg.Output)
		msg.Input = redactInputMap(msg.Input)
		if msg.Type == "final" && s.capture != nil {
			if !s.capture.PrepareStructuredFinal() {
				return
			}
		}
		s.reporter.Post(msg)
	}
}

func (s *transcriptStream) flushRawBeforeStructuredLine(rawLine string) {
	if s == nil || len(s.raw) == 0 {
		return
	}
	lineBytes := len([]byte(rawLine)) + 1
	if lineBytes > len(s.raw) {
		s.FlushRaw()
		return
	}
	prior := s.raw[:len(s.raw)-lineBytes]
	if len(prior) > 0 {
		s.flushRawBytes(prior)
	}
	s.raw = s.raw[:0]
	s.last = time.Now()
}

func (s *transcriptStream) flushRawBytes(raw []byte) {
	if s == nil || len(raw) == 0 {
		return
	}
	if s.rawScreen == nil {
		s.rawScreen = newTerminalScreen()
	}
	s.rawScreen.Write(raw)
	visible := s.rawScreen.Text()
	delta := visibleTextDelta(s.rawVisible, visible)
	s.rawVisible = visible
	content := strings.TrimSpace(delta)
	if content == "" || isStatusOnly(content) {
		return
	}
	content = redact.Text(content)
	if content == "" {
		return
	}
	s.reporter.Post(localCLIMessage{Type: "raw", Content: content})
}

func visibleTextDelta(prev, next string) string {
	prev = strings.TrimSpace(prev)
	next = strings.TrimSpace(next)
	if next == "" || next == prev {
		return ""
	}
	if prev != "" && strings.HasPrefix(next, prev) {
		return strings.TrimSpace(strings.TrimPrefix(next, prev))
	}
	return next
}

func looksLikePotentialStructuredLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "{")
}

func redactInputMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = redact.Text(s)
		} else {
			out[k] = v
		}
	}
	return out
}

type localCLIEnv struct {
	RunID     string
	IssueID   string
	ServerURL string
	Token     string
}

func executeLocalCLI(args []string, cwd, cliName string, env localCLIEnv, initialPrompt string, reporter *localRunReporter) (int, error) {
	startTime := time.Now()
	childArgs := args[1:]
	writePromptToPTY := initialPrompt
	if cliName == "codex" && initialPrompt != "" {
		childArgs = append(childArgs, initialPrompt)
		writePromptToPTY = ""
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

	turnCapture := newTerminalTurnCapture(reporter, newProviderTranscriptExtractor(cliName, cwd, startTime))
	turnCapture.StartInitialPrompt(initialPrompt)
	transcript := newTranscriptStream(reporter, turnCapture)
	go func() {
		if writePromptToPTY != "" {
			_, _ = io.WriteString(ptmx, writePromptToPTY)
		}
		_, _ = io.Copy(ptmx, io.TeeReader(os.Stdin, &stdinCapture{reporter: reporter, turns: turnCapture}))
	}()

	_, _ = io.Copy(io.MultiWriter(os.Stdout, transcript), ptmx)
	transcript.Flush()
	turnCapture.Finalize()
	err = child.Wait()
	exitCode := 0
	if child.ProcessState != nil {
		exitCode = child.ProcessState.ExitCode()
	}
	return exitCode, err
}

func localCLIProcessEnv(base []string, env localCLIEnv) []string {
	out := make([]string, 0, len(base)+4)
	for _, entry := range base {
		if strings.HasPrefix(entry, "MULTICA_WORKSPACE_ID=") || strings.HasPrefix(entry, "MULTICA_TOKEN=") {
			continue
		}
		out = append(out, entry)
	}
	set := func(key, value string) {
		if value == "" {
			return
		}
		prefix := key + "="
		for i, entry := range out {
			if strings.HasPrefix(entry, prefix) {
				out[i] = prefix + value
				return
			}
		}
		out = append(out, prefix+value)
	}
	set("MULTICA_RUN_ID", env.RunID)
	set("MULTICA_ISSUE_ID", env.IssueID)
	set("MULTICA_SERVER_URL", env.ServerURL)
	token := env.Token
	if token == "" {
		token = invalidLocalRunMulticaToken
	}
	set("MULTICA_TOKEN", token)
	return out
}

func watchTerminalResize(ptmx *os.File) func() {
	_ = pty.InheritSize(os.Stdin, ptmx)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				_ = pty.InheritSize(os.Stdin, ptmx)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

type structuredAdapter interface {
	ParseLine(line string) (localCLIMessage, bool)
}

type jsonLineAdapter struct{}

func (jsonLineAdapter) ParseLine(line string) (localCLIMessage, bool) {
	return parseJSONLineStructuredMessage(line)
}

type providerAdapter struct {
	name string
	next structuredAdapter
}

func (a providerAdapter) ParseLine(line string) (localCLIMessage, bool) {
	return a.next.ParseLine(line)
}

var defaultStructuredAdapter structuredAdapter = providerAdapter{name: "json-line", next: jsonLineAdapter{}}

func parseStructuredTranscript(raw string) []localCLIMessage {
	var messages []localCLIMessage
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		msg, ok := defaultStructuredAdapter.ParseLine(line)
		if ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

func parseStructuredMessage(line string) (localCLIMessage, bool) {
	return defaultStructuredAdapter.ParseLine(line)
}

func parseJSONLineStructuredMessage(line string) (localCLIMessage, bool) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return localCLIMessage{}, false
	}
	msgType := stringValue(obj["type"])
	if msgType == "" {
		msgType = stringValue(obj["kind"])
	}
	if msgType == "" {
		return localCLIMessage{}, false
	}
	msgType = normalizeStructuredMessageType(msgType)
	if msgType == "" {
		return localCLIMessage{}, false
	}
	msg := localCLIMessage{
		Type:    msgType,
		Tool:    firstString(obj, "tool", "name", "tool_name"),
		Content: firstString(obj, "content", "text", "message", "result"),
		Output:  firstString(obj, "output"),
	}
	if input, ok := obj["input"].(map[string]any); ok {
		msg.Input = input
	}
	return msg, true
}

func normalizeStructuredMessageType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "thinking", "reasoning":
		return "thinking"
	case "tool", "tool_use", "tool_call":
		return "tool_use"
	case "tool_result", "tool_output":
		return "tool_result"
	case "final", "answer", "result":
		return "final"
	case "raw", "text", "error", "assistant", "assistant_message", "agent_message":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "assistant") || strings.TrimSpace(t) == "agent_message" {
			return "text"
		}
		return strings.ToLower(strings.TrimSpace(t))
	default:
		return ""
	}
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(obj[key]); value != "" {
			return value
		}
	}
	return ""
}

func makeStdinRaw() (func(), error) {
	fd := int(os.Stdin.Fd())
	if !isTerminal(fd) {
		return func() {}, nil
	}
	oldState, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, fmt.Errorf("read terminal state: %w", err)
	}
	raw := *oldState
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return nil, fmt.Errorf("set terminal raw mode: %w", err)
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, oldState)
	}, nil
}

func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	return err == nil
}

func forwardSignals(proc *os.Process) func() {
	ch := make(chan os.Signal, 3)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				if proc != nil {
					_ = proc.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
