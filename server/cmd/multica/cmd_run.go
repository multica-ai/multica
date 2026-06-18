package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/multica-ai/multica/server/internal/cli"
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
const localRunReporterQueueSize = 128
const localRunReporterDrainTimeout = 30 * time.Second
const localRunSpoolRetention = 7 * 24 * time.Hour

var localRunMessageRetryBackoffs = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, 3 * time.Second}

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

	spool, err := newLocalRunMessageSpoolForCommand(cmd)
	if err != nil {
		return err
	}
	go syncPendingLocalRunSpool(context.Background(), client, spool, "")

	fmt.Fprintf(os.Stderr, "Multica local run %s started.\n", run.ID)
	reporter := newLocalRunReporterWithSpool(client, run.ID, spool)
	usageReporter := newLocalRunUsageReporter(client, run.ID, localRunUsageDebounce)
	stopHeartbeat := startLocalRunHeartbeat(client, run.ID, localRunHeartbeatInterval)
	startTime := time.Now()
	exitCode, runErr := executeLocalCLIForRun(childArgs, cwd, cliName, localCLIEnv{
		RunID:       run.ID,
		IssueID:     issueRef.ID,
		WorkspaceID: client.WorkspaceID,
		ServerURL:   resolveServerURL(cmd),
		Token:       resolveToken(cmd),
	}, reporter, usageReporter)
	usageReporter.Close()
	reporter.Close()
	stopHeartbeat()
	elapsed := time.Since(startTime)
	activeMs := reporter.ActiveMs()
	if activeMs > 0 {
		fmt.Fprintf(os.Stderr, "Local run finished in %s (active %s, exit %d)\n",
			elapsed.Round(time.Second),
			time.Duration(activeMs)*time.Millisecond,
			exitCode,
		)
	} else {
		fmt.Fprintf(os.Stderr, "Local run finished in %s (exit %d)\n",
			elapsed.Round(time.Second),
			exitCode,
		)
	}
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

type localRunMessagePoster interface {
	PostJSON(ctx context.Context, path string, body any, out any) error
}

type localRunMessageSpooler interface {
	Write(runID string, msg localCLIMessage) error
	Sync(ctx context.Context, client localRunMessagePoster, runID string) (int, int, error)
	Count(runID string) int
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
	client       localRunMessagePoster
	runID        string
	ch           chan localCLIMessage
	done         chan struct{}
	spool        localRunMessageSpooler
	retryBackoff []time.Duration
	drainTimeout time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	seq          atomic.Int64
	mu           sync.Mutex
	closed       bool
	activeMs     int64 // cumulative active time in ms, set by tracker
}

func newLocalRunReporter(client localRunMessagePoster, runID string) *localRunReporter {
	return newLocalRunReporterWithOptions(client, runID, nil, localRunReporterQueueSize, localRunMessageRetryBackoffs, localRunReporterDrainTimeout)
}

func newLocalRunReporterWithSpool(client localRunMessagePoster, runID string, spool localRunMessageSpooler) *localRunReporter {
	return newLocalRunReporterWithOptions(client, runID, spool, localRunReporterQueueSize, localRunMessageRetryBackoffs, localRunReporterDrainTimeout)
}

func newLocalRunReporterWithOptions(client localRunMessagePoster, runID string, spool localRunMessageSpooler, queueSize int, retryBackoff []time.Duration, drainTimeout time.Duration) *localRunReporter {
	if queueSize <= 0 {
		queueSize = localRunReporterQueueSize
	}
	if drainTimeout <= 0 {
		drainTimeout = localRunReporterDrainTimeout
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &localRunReporter{
		client:       client,
		runID:        runID,
		ch:           make(chan localCLIMessage, queueSize),
		done:         make(chan struct{}),
		spool:        spool,
		retryBackoff: append([]time.Duration(nil), retryBackoff...),
		drainTimeout: drainTimeout,
		ctx:          ctx,
		cancel:       cancel,
	}
	go r.loop()
	return r
}

func (r *localRunReporter) SetActiveMs(ms int64) {
	if r != nil {
		r.activeMs = ms
	}
}

func (r *localRunReporter) ActiveMs() int64 {
	if r == nil {
		return 0
	}
	return r.activeMs
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
	msg = r.prepareMessage(msg)
	select {
	case r.ch <- msg:
	default:
		r.spoolMessage(msg)
	}
}

func (r *localRunReporter) prepareMessage(msg localCLIMessage) localCLIMessage {
	msg.Source = strings.TrimSpace(msg.Source)
	msg.SourceKey = strings.TrimSpace(msg.SourceKey)
	if msg.SourceKey == "" {
		seq := r.seq.Add(1)
		if msg.Source == "" {
			msg.Source = "local-run"
		}
		msg.SourceKey = "local-run:" + r.runID + ":seq:" + strconv.FormatInt(seq, 10)
	}
	return msg
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
	timer := time.NewTimer(r.drainTimeout)
	defer timer.Stop()
	select {
	case <-r.done:
	case <-timer.C:
		r.cancel()
		<-r.done
	}
	if r.spool != nil {
		ctx, cancel := context.WithTimeout(context.Background(), r.drainTimeout)
		defer cancel()
		_, remaining, err := r.spool.Sync(ctx, r.client, r.runID)
		if err != nil || remaining > 0 {
			fmt.Fprintf(os.Stderr, "multica: %d local run messages remain queued for later sync; run `multica local-run sync-pending` to retry.\n", remaining)
		}
	}
}

func (r *localRunReporter) loop() {
	defer close(r.done)
	path := "/api/local-runs/" + url.PathEscape(r.runID) + "/messages"
	for msg := range r.ch {
		if err := postLocalRunMessageWithRetry(r.ctx, r.client, path, msg, r.retryBackoff); err != nil {
			r.spoolMessage(msg)
		}
	}
}

func (r *localRunReporter) spoolMessage(msg localCLIMessage) {
	if r.spool == nil {
		fmt.Fprintf(os.Stderr, "multica: failed to sync local run message and no spool is configured\n")
		return
	}
	if err := r.spool.Write(r.runID, msg); err != nil {
		fmt.Fprintf(os.Stderr, "multica: failed to spool local run message: %v\n", err)
	}
}

func postLocalRunMessageWithRetry(ctx context.Context, client localRunMessagePoster, path string, msg localCLIMessage, backoffs []time.Duration) error {
	if client == nil {
		return errors.New("local run message client is nil")
	}
	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := client.PostJSON(attemptCtx, path, msg, nil)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == len(backoffs) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoffs[attempt]):
		}
	}
	return lastErr
}

type localRunMessageSpool struct {
	dir string
}

type localRunSpoolEntry struct {
	RunID     string          `json:"run_id"`
	Message   localCLIMessage `json:"message"`
	CreatedAt time.Time       `json:"created_at"`
}

type localRunSpoolFile struct {
	path  string
	entry localRunSpoolEntry
}

func newLocalRunMessageSpoolForCommand(cmd *cobra.Command) (*localRunMessageSpool, error) {
	dir, err := cli.StateDirForInstance(resolveProfile(cmd), resolveConfigPath(cmd))
	if err != nil {
		return nil, err
	}
	return &localRunMessageSpool{dir: filepath.Join(dir, "local-run-spool")}, nil
}

func (s *localRunMessageSpool) Write(runID string, msg localCLIMessage) error {
	if s == nil {
		return nil
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run ID is required")
	}
	msg.Source = strings.TrimSpace(msg.Source)
	msg.SourceKey = strings.TrimSpace(msg.SourceKey)
	if msg.SourceKey == "" {
		return fmt.Errorf("source_key is required")
	}
	if msg.Source == "" {
		msg.Source = "local-run"
	}
	dir := filepath.Join(s.dir, safeSpoolName(runID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create local run spool dir: %w", err)
	}
	entry := localRunSpoolEntry{RunID: runID, Message: msg, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode local run spool entry: %w", err)
	}
	name := safeSpoolName(msg.SourceKey) + ".json"
	path := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".spool-*.tmp")
	if err != nil {
		return fmt.Errorf("create local run spool temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write local run spool temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close local run spool temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod local run spool temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename local run spool entry: %w", err)
	}
	return nil
}

func (s *localRunMessageSpool) Sync(ctx context.Context, client localRunMessagePoster, runID string) (int, int, error) {
	return syncPendingLocalRunSpool(ctx, client, s, runID)
}

func (s *localRunMessageSpool) Count(runID string) int {
	entries, err := s.entries(runID)
	if err != nil {
		return 0
	}
	return len(entries)
}

func syncPendingLocalRunSpool(ctx context.Context, client localRunMessagePoster, spool *localRunMessageSpool, runID string) (int, int, error) {
	if spool == nil {
		return 0, 0, nil
	}
	entries, err := spool.entries(runID)
	if err != nil {
		return 0, 0, err
	}
	sent := 0
	var lastErr error
	for _, file := range entries {
		select {
		case <-ctx.Done():
			return sent, len(entries) - sent, ctx.Err()
		default:
		}
		path := "/api/local-runs/" + url.PathEscape(file.entry.RunID) + "/messages"
		if err := postLocalRunMessageWithRetry(ctx, client, path, file.entry.Message, localRunMessageRetryBackoffs); err != nil {
			lastErr = err
			continue
		}
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			lastErr = err
			continue
		}
		sent++
	}
	remaining := spool.Count(runID)
	return sent, remaining, lastErr
}

func (s *localRunMessageSpool) entries(runID string) ([]localRunSpoolFile, error) {
	if s == nil {
		return nil, nil
	}
	root := s.dir
	if strings.TrimSpace(runID) != "" {
		root = filepath.Join(root, safeSpoolName(runID))
	}
	var entries []localRunSpoolFile
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var entry localRunSpoolEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return err
		}
		if time.Since(entry.CreatedAt) > localRunSpoolRetention {
			_ = os.Remove(path)
			return nil
		}
		entries = append(entries, localRunSpoolFile{path: path, entry: entry})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].entry.RunID == entries[j].entry.RunID {
			return entries[i].entry.CreatedAt.Before(entries[j].entry.CreatedAt)
		}
		return entries[i].entry.RunID < entries[j].entry.RunID
	})
	return entries, nil
}

func safeSpoolName(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
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
	RunID       string
	IssueID     string
	WorkspaceID string
	ServerURL   string
	Token       string
}

func executeLocalCLI(args []string, cwd, cliName string, env localCLIEnv, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error) {
	provider, ok := localRunProviderForCLI(cliName)
	if !ok {
		return 1, fmt.Errorf("unsupported local CLI provider %q", cliName)
	}
	return provider.Run(args, cwd, env, reporter, usageReporter)
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
	out := make([]string, 0, len(base)+5)
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
	set("MULTICA_WORKSPACE_ID", env.WorkspaceID)
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
