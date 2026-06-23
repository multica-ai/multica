package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/redact"
)

const agyJSONLSource = "agy-jsonl"

type agyLocalRunProvider struct{}

func (agyLocalRunProvider) Name() string { return "agy" }

func (agyLocalRunProvider) Run(args []string, cwd string, env localCLIEnv, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("missing agy command")
	}
	if err := validateAgyLocalRunArgs(args[1:]); err != nil {
		return 1, err
	}

	startedAt := time.Now()
	tracker := newAgyTranscriptTracker(reporter, usageReporter, startedAt)
	tracker.Start()
	defer tracker.Close()

	childArgs := agyLocalRunChildArgs(args)
	return runProviderPTY(childArgs, cwd, env, "")
}

// validateAgyLocalRunArgs rejects flags that multica manages automatically.
func validateAgyLocalRunArgs(args []string) error {
	for _, arg := range args {
		// -i / --prompt-interactive would re-enter interactive mode; the PTY
		// already provides an interactive session, so passing it is redundant
		// and could confuse the CLI's argument parsing.
		if arg == "-i" || arg == "--prompt-interactive" {
			return fmt.Errorf("multica run already starts agy interactively; remove %s from the command", arg)
		}
	}
	return nil
}

func agyLocalRunChildArgs(args []string) []string {
	return append([]string{args[0]}, args[1:]...)
}

// agyLocalRunSystemPrompt is reserved for future use when agy supports
// injecting runtime context (similar to Claude's --append-system-prompt).
// Today the Antigravity CLI has no equivalent flag; the issue ID is already
// available to agy via the MULTICA_ISSUE_ID environment variable.
func agyLocalRunSystemPrompt(issueID string) string {
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Multica local run context:\n")
	fmt.Fprintf(&b, "Bound Multica issue ID: %s\n\n", issueID)
	fmt.Fprintf(&b, "- Get issue details: multica issue get %s --output json\n", issueID)
	fmt.Fprintf(&b, "- Get issue comments: multica issue comment list %s --output json\n", issueID)
	return b.String()
}

// ── Transcript tracker ──────────────────────────────────────────────────────

// agyTurnIdleFlush is how long the transcript must be idle before we assume
// the model has finished its turn and flush the buffered final message.
// 8 seconds gives long-running tools (compilation, large test suites, file
// searches) enough time to complete without triggering a premature flush.
const agyTurnIdleFlush = 8 * time.Second

// agyRediscoverInterval is how often the tracker re-scans the brain directory
// for a newer transcript file. This handles the case where agy creates a new
// session AFTER the tracker's initial discovery — without periodic re-scan the
// tracker stays locked to the old transcript and never sees the new session's
// entries.
const agyRediscoverInterval = 5 * time.Second

type agyTranscriptTracker struct {
	reporter      *localRunReporter
	usageReporter *localRunUsageReporter
	startedAt     time.Time
	ticker        *time.Ticker
	mu            sync.Mutex
	transcript    string    // discovered transcript path
	lastModTime   time.Time // ModTime of transcript at last sync
	lastActivity  time.Time // last time new entries were read
	lastDiscovery time.Time // last time we scanned for transcript files
	seen          map[int]bool
	totalActiveMs int64
	// Turn tracking: buffer the last model response per turn and only post
	// it as "final" when the turn is detected as complete.
	// Turn completion is detected by:
	// 1. Next USER_INPUT arrives (same as Claude's currentTurnReply pattern)
	// 2. Transcript idle for agyTurnIdleFlush (approximates stop_reason == "end_turn")
	pendingFinal          *localCLIMessage
	pendingFinalStep      int
	lastUserStep          int  // step_index of the most recent USER_INPUT
	lastStepHadToolCalls  bool // true if the previous MODEL step dispatched tool calls
	done             chan struct{}
	stopped          chan struct{}
	startOnce        sync.Once
	closeOnce        sync.Once
}

func newAgyTranscriptTracker(reporter *localRunReporter, usageReporter *localRunUsageReporter, startedAt time.Time) *agyTranscriptTracker {
	return &agyTranscriptTracker{
		reporter:      reporter,
		usageReporter: usageReporter,
		startedAt:     startedAt,
		ticker:        time.NewTicker(500 * time.Millisecond),
		seen:          make(map[int]bool),
		done:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}
}

func (t *agyTranscriptTracker) Start() {
	t.startOnce.Do(func() {
		go func() {
			defer close(t.stopped)
			for {
				select {
				case <-t.ticker.C:
					t.sync()
				case <-t.done:
					t.sync()
					return
				}
			}
		}()
	})
}

func (t *agyTranscriptTracker) Close() {
	t.closeOnce.Do(func() {
		close(t.done)
		<-t.stopped
		t.ticker.Stop()
		t.mu.Lock()
		t.flushPendingFinalLocked()
		t.mu.Unlock()
		if t.reporter != nil {
			t.reporter.SetActiveMs(t.totalActiveMs)
		}
	})
}

// sync discovers the transcript file (if needed) and reads new entries.
func (t *agyTranscriptTracker) sync() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Periodically re-discover to catch a new agy session whose transcript
	// didn't exist (or wasn't the newest) at initial discovery time.
	if t.transcript == "" || time.Since(t.lastDiscovery) > agyRediscoverInterval {
		oldTranscript := t.transcript
		t.discoverTranscriptLocked()
		t.lastDiscovery = time.Now()
		if t.transcript == "" {
			return
		}
		// Switched to a different transcript file (new agy session).
		// Flush any buffered final from the old session, clear the seen
		// map so the new file's entries are processed, and reset lastModTime
		// so the full file is read on this sync.
		if oldTranscript != "" && t.transcript != oldTranscript {
			t.flushPendingFinalLocked()
			t.seen = make(map[int]bool)
			t.lastModTime = time.Time{}
			t.lastActivity = time.Time{}
		}
	}

	// Only re-read if the file has been modified since last sync.
	info, err := os.Stat(t.transcript)
	if err != nil {
		// File gone — force re-discovery on next sync.
		t.transcript = ""
		return
	}
	if !info.ModTime().After(t.lastModTime) {
		// File unchanged. If a pending final has been buffered and the
		// transcript has been idle long enough, the model is done.
		// Flush now instead of waiting for the next user input.
		if t.pendingFinal != nil && !t.lastActivity.IsZero() && time.Since(t.lastActivity) >= agyTurnIdleFlush {
			t.flushPendingFinalLocked()
		}
		return
	}
	t.lastModTime = info.ModTime()
	t.readNewEntriesLocked()
}

// discoverTranscriptLocked finds the most recently modified transcript.jsonl
// across all brain directories. No startedAt filter — the conversation may
// pre-date this run if agy resumed an existing session.
//
// Unlike earlier versions, this does NOT pre-populate the seen map. The
// startedAt filter in mapEntry already skips historical entries, and leaving
// the seen map empty allows re-discovery to work correctly when agy creates a
// new session after the initial discovery.
func (t *agyTranscriptTracker) discoverTranscriptLocked() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	brainDir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return
	}

	var newestPath string
	var newestMod time.Time
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(brainDir, entry.Name(), ".system_generated", "logs", "transcript.jsonl")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newestPath = path
		}
	}
	if newestPath != "" {
		t.transcript = newestPath
		t.lastModTime = newestMod
	}
}

// readNewEntriesLocked reads new JSONL lines and posts messages.
func (t *agyTranscriptTracker) readNewEntriesLocked() {
	file, err := os.Open(t.transcript)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	foundNew := false
	for scanner.Scan() {
		lineNo++
		if t.seen[lineNo] {
			continue
		}
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var entry agyTranscriptEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue
		}
		if entry.StepIndex < 0 {
			continue
		}
		t.seen[lineNo] = true
		foundNew = true
		t.mapEntry(&entry)
	}
	if foundNew {
		t.lastActivity = time.Now()
	}
}

func (t *agyTranscriptTracker) mapEntry(entry *agyTranscriptEntry) {
	var parsedTime time.Time
	if ts := entry.CreatedAt; ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			parsedTime = parsed
		}
	}

	// Skip historical entries to prevent duplicate posts and noise in resume sessions.
	if !parsedTime.IsZero() && parsedTime.Before(t.startedAt.Add(-2*time.Second)) {
		return
	}

	// Track active time from timestamps.
	if !parsedTime.IsZero() && parsedTime.After(t.startedAt) {
		// Approximate active time: time from first user input to last model response.
		if entry.Source == "MODEL" && entry.Type == "PLANNER_RESPONSE" {
			activeMs := parsedTime.Sub(t.startedAt).Milliseconds()
			if activeMs > t.totalActiveMs {
				t.totalActiveMs = activeMs
			}
		}
	}

	switch entry.Source {
	case "USER_EXPLICIT":
		t.mapUserInput(entry)
	case "MODEL":
		t.mapModelResponse(entry)
	}
}

func (t *agyTranscriptTracker) mapUserInput(entry *agyTranscriptEntry) {
	// Flush the previous turn's final response before starting a new turn.
	t.flushPendingFinalLocked()
	t.lastUserStep = entry.StepIndex
	// Reset tool-call chain tracking: a new user turn starts fresh.
	t.lastStepHadToolCalls = false

	content := agyExtractUserContent(entry.Content)
	if content == "" {
		return
	}
	slash, isSlash := parseSlashInput(content)
	commentable := content != "" && (!isSlash || slash.Args != "")
	if !commentable {
		return
	}
	input := map[string]any{}
	if isSlash {
		input["command"] = true
		input["slash_command"] = slash.Command
		input["slash_args"] = slash.Args
		input["commentable"] = true
	}
	t.post(localCLIMessage{
		Type:      "user_input",
		Content:   content,
		SourceKey: "agy:step:" + strconv.Itoa(entry.StepIndex) + ":user",
		Input:     input,
	})
}

func (t *agyTranscriptTracker) mapModelResponse(entry *agyTranscriptEntry) {
	hasToolCalls := len(entry.ToolCalls) > 0

	if entry.Content != "" {
		t.post(localCLIMessage{
			Type:      "text",
			Content:   entry.Content,
			SourceKey: "agy:step:" + strconv.Itoa(entry.StepIndex) + ":text",
		})
	}

	// Post tool calls (for run log display only; tool_use is never an issue comment).
	for i, tc := range entry.ToolCalls {
		tool := tc.Name
		if tool == "" {
			tool = "unknown"
		}
		t.post(localCLIMessage{
			Type:      "tool_use",
			Tool:      tool,
			Input:     tc.Args,
			SourceKey: "agy:step:" + strconv.Itoa(entry.StepIndex) + ":tool:" + strconv.Itoa(i),
		})
	}

	// Determine whether this step is a genuine user-facing reply.
	//
	// A PLANNER_RESPONSE step is a candidate for a final Issue comment only when:
	//   1. It has no outgoing tool calls (not dispatching more tools)
	//   2. The PREVIOUS model step did NOT have tool calls (not processing tool output)
	//
	// Condition 2 is the critical fix: after a tool executes, the very next model
	// step contains the raw tool output in its Content field (test logs, git diffs,
	// command stdout, etc.). That step has tool_calls=[] — exactly like a real reply —
	// but its content is machine output, not a user-facing message.
	// Skipping it here prevents tool output from being posted as an Issue comment.
	//
	// This mirrors Claude's stop_reason=="end_turn" signal: we identify the end of a
	// tool-execution chain rather than individual steps.
	isFinalCandidate := !hasToolCalls &&
		!t.lastStepHadToolCalls &&
		entry.Content != "" &&
		!isStatusOnly(entry.Content)

	if isFinalCandidate {
		msg := localCLIMessage{
			Type:      "final",
			Content:   entry.Content,
			SourceKey: "agy:step:" + strconv.Itoa(entry.StepIndex) + ":final",
		}
		t.pendingFinal = &msg
		t.pendingFinalStep = entry.StepIndex
	}

	// Update the flag for the next step.
	t.lastStepHadToolCalls = hasToolCalls
}

// flushPendingFinalLocked posts the buffered final message if any.
func (t *agyTranscriptTracker) flushPendingFinalLocked() {
	if t.pendingFinal != nil {
		t.post(*t.pendingFinal)
		t.pendingFinal = nil
		t.pendingFinalStep = 0
	}
}

func (t *agyTranscriptTracker) post(msg localCLIMessage) {
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Output) == "" && msg.Type != "tool_use" && msg.Type != "tool_result" {
		return
	}
	msg.Source = agyJSONLSource
	msg.Content = redact.Text(strings.TrimSpace(msg.Content))
	msg.Output = redact.Text(strings.TrimSpace(msg.Output))
	msg.Input = redactInputMap(msg.Input)
	t.reporter.Post(msg)
}

// ── Transcript JSONL types ──────────────────────────────────────────────────

type agyTranscriptEntry struct {
	StepIndex int                `json:"step_index"`
	Source    string             `json:"source"`
	Type      string             `json:"type"`
	Status    string             `json:"status"`
	CreatedAt string             `json:"created_at"`
	Content   string             `json:"content"`
	Thinking  string             `json:"thinking"`
	ToolCalls []agyTranscriptTool `json:"tool_calls"`
}

type agyTranscriptTool struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// agyExtractUserContent extracts the user's actual request from the agy
// transcript format, which wraps it in <USER_REQUEST> tags and appends
// metadata blocks.
func agyExtractUserContent(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Extract content between <USER_REQUEST> and </USER_REQUEST>.
	const openTag = "<USER_REQUEST>"
	const closeTag = "</USER_REQUEST>"
	start := strings.Index(raw, openTag)
	if start >= 0 {
		end := strings.Index(raw[start:], closeTag)
		if end >= 0 {
			return strings.TrimSpace(raw[start+len(openTag) : start+end])
		}
	}
	// Fallback: strip known metadata blocks.
	lines := strings.Split(raw, "\n")
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "<ADDITIONAL_METADATA>") ||
			strings.HasPrefix(line, "<USER_SETTINGS_CHANGE>") ||
			strings.HasPrefix(line, "</ADDITIONAL_METADATA>") ||
			strings.HasPrefix(line, "</USER_SETTINGS_CHANGE>") {
			continue
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}
