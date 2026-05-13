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

	contextDir := filepath.Join(cwd, ".multica", "runs", run.ID)
	if err := writeLocalRunContext(ctx, client, issueRef.ID, run.ID, cliName, cwd, contextDir); err != nil {
		return err
	}
	_ = addMulticaToGitExclude(cwd)

	if err := client.PatchJSON(context.Background(), "/api/local-runs/"+url.PathEscape(run.ID), map[string]any{
		"status":      "running",
		"context_dir": contextDir,
	}, nil); err != nil {
		return fmt.Errorf("update local run context: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Multica local run %s started. Context: %s\n", run.ID, contextDir)
	reporter := newLocalRunReporter(client, run.ID)
	exitCode, runErr := executeLocalCLI(childArgs, cwd, cliName, run.ID, issueRef.ID, contextDir, resolveServerURL(cmd), initialLocalRunPrompt(contextDir), reporter)
	reporter.Close()
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

func writeLocalRunContext(ctx context.Context, client jsonGetter, issueID, runID, cliName, cwd, contextDir string) error {
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return fmt.Errorf("create context dir: %w", err)
	}

	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID), &issue); err != nil {
		return fmt.Errorf("fetch issue context: %w", err)
	}
	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", &comments); err != nil {
		return fmt.Errorf("fetch issue comments: %w", err)
	}
	resources := map[string]any{"issue": issue}
	if workspaceID := stringValue(issue["workspace_id"]); workspaceID != "" {
		var workspace map[string]any
		if err := client.GetJSON(ctx, "/api/workspaces/"+url.PathEscape(workspaceID), &workspace); err == nil {
			resources["workspace"] = workspace
		}
	}
	if projectID := stringValue(issue["project_id"]); projectID != "" {
		var project map[string]any
		if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectID), &project); err == nil {
			resources["project"] = project
		}
		var projectResources map[string]any
		if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectID)+"/resources", &projectResources); err == nil {
			resources["project_resources"] = projectResources
		}
	}

	if err := writeJSONFile(filepath.Join(contextDir, "run.json"), map[string]any{
		"id":          runID,
		"issue_id":    issueID,
		"cli_name":    cliName,
		"work_dir":    cwd,
		"context_dir": contextDir,
		"started_at":  time.Now().Format(time.RFC3339),
	}); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(contextDir, "resources.json"), resources); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contextDir, "issue.md"), []byte(formatIssueMarkdown(issue)), 0o644); err != nil {
		return fmt.Errorf("write issue.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "comments.md"), []byte(formatCommentsMarkdown(comments)), 0o644); err != nil {
		return fmt.Errorf("write comments.md: %w", err)
	}
	return nil
}

type jsonGetter interface {
	GetJSON(ctx context.Context, path string, out any) error
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func formatIssueMarkdown(issue map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", stringValue(issue["title"]))
	fmt.Fprintf(&b, "- ID: `%s`\n", stringValue(issue["id"]))
	fmt.Fprintf(&b, "- Identifier: `%s`\n", stringValue(issue["identifier"]))
	fmt.Fprintf(&b, "- Status: `%s`\n", stringValue(issue["status"]))
	fmt.Fprintf(&b, "- Priority: `%s`\n\n", stringValue(issue["priority"]))
	if desc := stringValue(issue["description"]); desc != "" {
		b.WriteString("## Description\n\n")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	return b.String()
}

func formatCommentsMarkdown(comments []map[string]any) string {
	var b strings.Builder
	b.WriteString("# Comments\n\n")
	for _, c := range comments {
		fmt.Fprintf(&b, "## %s %s\n\n", stringValue(c["author_type"]), stringValue(c["author_id"]))
		if created := stringValue(c["created_at"]); created != "" {
			fmt.Fprintf(&b, "_%s_\n\n", created)
		}
		b.WriteString(stringValue(c["content"]))
		b.WriteString("\n\n")
	}
	return b.String()
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func initialLocalRunPrompt(contextDir string) string {
	return fmt.Sprintf("Before starting, read the Multica issue context in `%s`.\n", contextDir)
}

type localRunMessagePoster interface {
	PostJSON(ctx context.Context, path string, body any, out any) error
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
	reporter *localRunReporter
	capture  *terminalTurnCapture
	raw      strings.Builder
	line     strings.Builder
	last     time.Time
}

func newTranscriptStream(reporter *localRunReporter, capture *terminalTurnCapture) *transcriptStream {
	return &transcriptStream{reporter: reporter, capture: capture, last: time.Now()}
}

func (s *transcriptStream) Write(p []byte) (int, error) {
	if s == nil {
		return len(p), nil
	}
	s.raw.Write(p)
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
	if s.raw.Len() >= 4096 || time.Since(s.last) >= time.Second {
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
	if s == nil || s.raw.Len() == 0 {
		return
	}
	content := redact.Text(s.raw.String())
	s.raw.Reset()
	s.last = time.Now()
	s.reporter.Post(localCLIMessage{Type: "raw", Content: content})
}

func (s *transcriptStream) flushLine() {
	if s == nil || s.line.Len() == 0 {
		return
	}
	line := strings.TrimSpace(strings.TrimRight(s.line.String(), "\r"))
	s.line.Reset()
	if msg, ok := parseStructuredMessage(line); ok {
		s.FlushRaw()
		msg.Content = redact.Text(msg.Content)
		msg.Output = redact.Text(msg.Output)
		msg.Input = redactInputMap(msg.Input)
		if s.capture != nil && (msg.Type == "final" || msg.Type == "text") {
			s.capture.MarkStructuredAssistant()
		}
		s.reporter.Post(msg)
	}
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

func executeLocalCLI(args []string, cwd, cliName, runID, issueID, contextDir, serverURL, initialPrompt string, reporter *localRunReporter) (int, error) {
	startTime := time.Now()
	child := exec.Command(args[0], args[1:]...)
	child.Dir = cwd
	child.Env = append(os.Environ(),
		"MULTICA_RUN_ID="+runID,
		"MULTICA_ISSUE_ID="+issueID,
		"MULTICA_RUN_CONTEXT_DIR="+contextDir,
		"MULTICA_SERVER_URL="+serverURL,
	)

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
	transcript := newTranscriptStream(reporter, turnCapture)
	go func() {
		if initialPrompt != "" {
			_, _ = io.WriteString(ptmx, initialPrompt)
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

func addMulticaToGitExclude(cwd string) error {
	gitDir := filepath.Join(cwd, ".git")
	if st, err := os.Stat(gitDir); err != nil || !st.IsDir() {
		return nil
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	data, _ := os.ReadFile(excludePath)
	if strings.Contains(string(data), ".multica/") {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(".multica/\n")
	return err
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
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
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
