package main

import (
	"context"
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
	Type      string         `json:"type"`
	Tool      string         `json:"tool,omitempty"`
	Content   string         `json:"content,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Output    string         `json:"output,omitempty"`
	Source    string         `json:"source,omitempty"`
	SourceKey string         `json:"source_key,omitempty"`
}

type localCLIUsage struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

const invalidLocalRunMulticaToken = "multica-local-run-token-disabled"
const localRunHeartbeatInterval = 30 * time.Second
const localRunUsageDebounce = 2 * time.Second

var executeLocalCLIForRun = executeLocalCLI

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
	if !supportsLocalRunAgent(cliName) {
		return fmt.Errorf("当前 Agent 尚未支持，敬请期待")
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
	usageReporter := newLocalRunUsageReporter(client, run.ID, localRunUsageDebounce)
	stopHeartbeat := startLocalRunHeartbeat(client, run.ID, localRunHeartbeatInterval)
	exitCode, runErr := executeLocalCLIForRun(childArgs, cwd, cliName, localCLIEnv{
		RunID:     run.ID,
		IssueID:   issueRef.ID,
		ServerURL: resolveServerURL(cmd),
		Token:     resolveToken(cmd),
	}, localRunPrompt(issueRef.ID), reporter, usageReporter)
	usageReporter.Close()
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

func supportsLocalRunAgent(cliName string) bool {
	_, ok := localRunProviderForCLI(cliName)
	return ok
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

type localRunUsagePutter interface {
	PutJSON(ctx context.Context, path string, body any, out any) error
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
	if r == nil || strings.TrimSpace(msg.Content) == "" && msg.Output == "" && msg.Type != "tool_use" && msg.Type != "tool_result" {
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

type localRunUsageReporter struct {
	client   localRunUsagePutter
	runID    string
	debounce time.Duration
	wake     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
	dirty    bool
	usage    map[string]localCLIUsage
}

func newLocalRunUsageReporter(client localRunUsagePutter, runID string, debounce time.Duration) *localRunUsageReporter {
	if debounce <= 0 {
		debounce = localRunUsageDebounce
	}
	r := &localRunUsageReporter{
		client:   client,
		runID:    runID,
		debounce: debounce,
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
		usage:    make(map[string]localCLIUsage),
	}
	go r.loop()
	return r
}

func (r *localRunUsageReporter) Report(u localCLIUsage) {
	if r == nil {
		return
	}
	u.Provider = strings.ToLower(strings.TrimSpace(u.Provider))
	u.Model = strings.TrimSpace(u.Model)
	if u.Model == "" {
		u.Model = "unknown"
	}
	if u.Provider == "" {
		return
	}
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 {
		return
	}
	key := u.Provider + "\x00" + u.Model
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.usage[key] = u
	r.dirty = true
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *localRunUsageReporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		<-r.done
		return
	}
	r.closed = true
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	<-r.done
}

func (r *localRunUsageReporter) loop() {
	defer close(r.done)
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-r.wake:
			r.mu.Lock()
			closed := r.closed
			r.mu.Unlock()
			if closed {
				if timer != nil {
					timer.Stop()
				}
				r.flush()
				return
			}
			if timer == nil {
				timer = time.NewTimer(r.debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(r.debounce)
			}
		case <-timerC:
			r.flush()
			timerC = nil
			timer = nil
		}
	}
}

func (r *localRunUsageReporter) flush() {
	if r.client == nil || r.runID == "" {
		return
	}
	r.mu.Lock()
	if !r.dirty || len(r.usage) == 0 {
		r.mu.Unlock()
		return
	}
	rows := make([]localCLIUsage, 0, len(r.usage))
	for _, u := range r.usage {
		rows = append(rows, u)
	}
	r.dirty = false
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := "/api/local-runs/" + url.PathEscape(r.runID) + "/usage"
	if err := r.client.PutJSON(ctx, path, map[string]any{"usage": rows}, nil); err != nil {
		r.mu.Lock()
		if !r.closed {
			r.dirty = true
		}
		r.mu.Unlock()
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

type localCLIEnv struct {
	RunID     string
	IssueID   string
	ServerURL string
	Token     string
}

func executeLocalCLI(args []string, cwd, cliName string, env localCLIEnv, initialPrompt string, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error) {
	provider, ok := localRunProviderForCLI(cliName)
	if !ok {
		return 1, fmt.Errorf("unsupported local CLI provider %q", cliName)
	}
	return provider.Run(args, cwd, env, initialPrompt, reporter, usageReporter)
}

type localRunPTYOptions struct {
	Args         []string
	Cwd          string
	Env          []string
	InitialStdin string
}

func runLocalRunPTY(opts localRunPTYOptions) (int, error) {
	if len(opts.Args) == 0 {
		return 1, fmt.Errorf("missing local CLI command")
	}
	child := exec.Command(opts.Args[0], opts.Args[1:]...)
	child.Dir = opts.Cwd
	child.Env = opts.Env
	return runLocalRunPTYCommand(child, opts.InitialStdin)
}

func runLocalRunPTYCommand(child *exec.Cmd, initialStdin string) (int, error) {
	ptmx, err := pty.Start(child)
	if err != nil {
		return 1, err
	}
	var closePTYOnce sync.Once
	closePTY := func() {
		closePTYOnce.Do(func() {
			_ = ptmx.Close()
		})
	}
	defer closePTY()

	restore, err := makeStdinRaw()
	if err != nil {
		stopCommand(child)
		return 1, err
	}
	defer restore()
	stopResizeWatch := watchTerminalResize(ptmx)
	defer stopResizeWatch()
	stopSignalForward := forwardSignals(child.Process)
	defer stopSignalForward()

	go func() {
		if initialStdin != "" {
			_, _ = io.WriteString(ptmx, initialStdin)
		}
		_, _ = io.Copy(ptmx, os.Stdin)
	}()

	type waitResult struct {
		exitCode int
		err      error
	}
	waitCh := make(chan waitResult, 1)
	go func() {
		err := child.Wait()
		exitCode := 0
		if child.ProcessState != nil {
			exitCode = child.ProcessState.ExitCode()
		} else if err != nil {
			exitCode = 1
		}
		closePTY()
		waitCh <- waitResult{exitCode: exitCode, err: err}
	}()

	_, _ = io.Copy(os.Stdout, ptmx)
	result := <-waitCh
	return result.exitCode, result.err
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

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(obj[key]); value != "" {
			return value
		}
	}
	return ""
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
