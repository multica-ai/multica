package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// forgeBlockedArgs are flags the daemon hardcodes for the ForgeCode one-shot
// invocation; user custom_args must not override them.
var forgeBlockedArgs = map[string]blockedArgMode{
	"--prompt":          blockedWithValue, // user prompt is owned by the daemon
	"-p":                blockedWithValue,
	"--conversation-id": blockedWithValue, // daemon pins the conversation id for resume + dump
	"--cid":             blockedWithValue,
	"--directory":       blockedWithValue, // task workdir anchor
	"-C":                blockedWithValue,
}

// forgeBackend implements Backend by spawning `forge -C <cwd> --cid <id> -p
// <prompt>` — ForgeCode's one-shot mode (no TUI).
//
// ForgeCode (forgecode.dev) does NOT emit a streaming JSON/NDJSON event
// protocol like Claude Code or OpenCode. Its `-p` mode renders terminal
// Markdown (with ANSI styling) to stdout, where tool calls, tool results,
// reasoning, and prose are visually interleaved with no reliable machine
// boundary.
//
// Because the live stream cannot be parsed into structured messages, this
// backend uses a two-phase approach:
//
//   - Phase 1 (live): buffer stdout lines and forward a status heartbeat per
//     line so the daemon's idle watchdog sees activity. No text is emitted
//     yet — otherwise tool output (e.g. `multica ... --output json`) would
//     leak into the assistant's prose, which is the original bug.
//   - Phase 2 (post-run): `forge conversation dump <cid>` writes a
//     machine-readable JSON document (to a file in cwd). This is the only
//     authoritative source of structured tool calls / results / reasoning /
//     token usage. The dump's message list is replayed as structured Message
//     events (thinking / tool-use / tool-result / text), mirroring the
//     OpenCode backend's classification. When the dump is unavailable (older
//     build, revoked cid, non-zero exit), the buffered stdout lines are
//     emitted as plain text so the run is never silent.
//
// Model selection: ForgeCode has no --model flag. The active model is set via
// FORGE_SESSION__PROVIDER_ID / FORGE_SESSION__MODEL_ID env vars (or the
// persisted .forge.toml). The daemon translates a "provider/model" value
// (from MULTICA_FORGE_MODEL or the per-agent Model override) into those two
// env vars.
type forgeBackend struct {
	cfg Config
}

func (b *forgeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "forge"
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("forge executable not found at %q: %w", execPath, err)
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// ForgeCode resumes a prior conversation by reusing its conversation id.
	// When the daemon hands us a prior session id we pin it so the run is a
	// continuation; otherwise generate a fresh UUID the post-run dump can
	// read back.
	convID := opts.ResumeSessionID
	if convID == "" {
		convID = uuid.NewString()
	}

	// Build argv. Order mirrors the documented one-shot invocation:
	//   forge [-C <cwd>] [--cid <convID>] [custom_args] -p <prompt>
	args := []string{}
	if opts.Cwd != "" {
		args = append(args, "-C", opts.Cwd)
	}
	args = append(args, "--cid", convID)
	args = append(args, filterCustomArgs(opts.CustomArgs, forgeBlockedArgs, b.cfg.Logger)...)
	args = append(args, "-p", prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	if opts.Cwd != "" {
		// Override PWD so the child process's discovery root matches the
		// task workdir, mirroring the OpenCode backend's reasoning.
		env = append(env, "PWD="+opts.Cwd)
	}
	// ForgeCode selects the model from FORGE_SESSION__PROVIDER_ID /
	// FORGE_SESSION__MODEL_ID. The Model value (from MULTICA_FORGE_MODEL or
	// the per-agent override) is expected in "provider/model" form.
	if modelVal := strings.TrimSpace(opts.Model); modelVal != "" {
		if provider, model, ok := splitForgeModel(modelVal); ok {
			env = append(env, "FORGE_SESSION__PROVIDER_ID="+provider)
			env = append(env, "FORGE_SESSION__MODEL_ID="+model)
		} else {
			b.cfg.Logger.Warn("forge model value is not provider/model; leaving model to .forge.toml", "model", modelVal)
		}
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("forge stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[forge:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start forge: %w", err)
	}

	b.cfg.Logger.Info("forge started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "conversation_id", convID, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so the scanner unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processStream(stdout, msgCh, convID)

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		status := scanResult.status
		errMsg := scanResult.errMsg
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			status = "timeout"
			errMsg = fmt.Sprintf("forge timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			status = "aborted"
			errMsg = "execution cancelled"
		case exitErr != nil && status == "completed":
			// ForgeCode exits 1 on error (main.rs). A non-zero exit with no
			// preceding error marker is still a failure.
			status = "failed"
			errMsg = fmt.Sprintf("forge exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("forge finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())

		// Phase 2: replay structured messages from the post-run conversation
		// dump. ForgeCode has no structured event stream, so the dump is the
		// only source of tool calls / results / reasoning / token usage. On
		// success, replay the dump's messages as structured Message events
		// (mirroring OpenCode's tool_use / tool_result / text
		// classification). When the dump is unavailable (older build,
		// revoked cid, non-zero exit), fall back to the raw buffered stdout
		// lines as plain text so the run is never silent.
		var output string
		var usage map[string]TokenUsage

		if status == "completed" {
			if dump, derr := b.readConversationDump(execPath, convID, opts.Cwd); derr == nil {
				output = b.replayFromDump(dump.Conversation.Context.Messages, msgCh)
				if u := forgeUsageFromMessages(dump.Conversation.Context.Messages); u != nil {
					key := strings.TrimSpace(opts.Model)
					if key == "" {
						key = "unknown"
					}
					usage = map[string]TokenUsage{key: *u}
				}
			} else {
				b.cfg.Logger.Debug("forge conversation dump unavailable; falling back to raw stdout", "error", derr)
			}
		}

		// Fallback: when no structured output was produced (dump failed or
		// non-completed status), emit the buffered stdout as plain text.
		if output == "" && strings.TrimSpace(scanResult.output) != "" {
			output = scanResult.output
			trySend(msgCh, Message{Type: MessageText, Content: scanResult.output})
		}

		resCh <- Result{
			Status:     status,
			Output:     output,
			Error:      errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  convID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// forgeStreamResult accumulates state from the stdout scan.
type forgeStreamResult struct {
	status string
	errMsg string
	output string
}

// processStream reads ForgeCode's rendered-Markdown stdout line by line.
// Because the live stream has no reliable structure (tool output, reasoning,
// and prose are all interleaved as styled text), lines are buffered for the
// fallback path rather than emitted as text. Each non-empty line forwards a
// status heartbeat so the daemon's idle watchdog sees activity; the first
// heartbeat carries the conversation id so the daemon can pin the resume
// pointer. The authoritative structured output is produced later from the
// post-run conversation dump (see replayFromDump).
func (b *forgeBackend) processStream(r io.Reader, ch chan<- Message, convID string) forgeStreamResult {
	var output strings.Builder
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	firstSignal := true
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		// ForgeCode renders ANSI styling for the terminal. Strip it so the
		// buffered fallback text is readable.
		clean := stripANSI(line)
		if strings.TrimSpace(clean) == "" {
			continue
		}
		// Buffer the ANSI-stripped line. These lines are only replayed as
		// plain text when the structured dump is unavailable.
		output.WriteString(clean)
		output.WriteString("\n")

		// ForgeCode emits no structured event stream, so stdout lines are
		// the only liveness signal. Forward a status heartbeat so the
		// daemon's idle watchdog sees activity (the daemon does not persist
		// status messages). The first heartbeat also carries the
		// conversation id so the daemon can pin the resume pointer.
		if firstSignal {
			trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: convID})
			firstSignal = false
		} else {
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		}
	}

	if err := scanner.Err(); err != nil {
		b.cfg.Logger.Warn("forge stdout scanner error", "error", err)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", err)
		}
	}

	return forgeStreamResult{status: finalStatus, errMsg: finalError, output: output.String()}
}

// readConversationDump runs `forge conversation dump <convID>` and reads the
// resulting JSON document. ForgeCode writes the dump to a
// <timestamp>-dump.json file in cwd (NOT stdout), so existing dump files are
// snapshotted before the command to identify the newly created one, which is
// removed after reading. Returns an error when the dump is unavailable or
// unparseable — the caller treats this as "use the fallback path" rather
// than a run failure.
func (b *forgeBackend) readConversationDump(execPath, convID, cwd string) (*forgeConversationDump, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if cwd == "" {
		return nil, fmt.Errorf("no cwd for conversation dump")
	}

	// Snapshot existing dump files so the new one can be identified.
	existing := map[string]bool{}
	if entries, err := os.ReadDir(cwd); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "-dump.json") {
				existing[e.Name()] = true
			}
		}
	}

	cmd := exec.CommandContext(ctx, execPath, "conversation", "dump", convID)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		b.cfg.Logger.Debug("forge conversation dump failed (non-fatal)", "conversation_id", convID, "error", err, "stderr", strings.TrimSpace(stderr.String()))
		return nil, fmt.Errorf("conversation dump: %w", err)
	}

	// Locate the newly created dump file.
	var dumpPath string
	if entries, err := os.ReadDir(cwd); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "-dump.json") && !existing[e.Name()] {
				dumpPath = filepath.Join(cwd, e.Name())
				break
			}
		}
	}
	if dumpPath == "" {
		return nil, fmt.Errorf("conversation dump produced no file")
	}
	defer os.Remove(dumpPath)

	raw, err := os.ReadFile(dumpPath)
	if err != nil {
		return nil, fmt.Errorf("read dump file: %w", err)
	}

	var doc forgeConversationDump
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse dump json: %w", err)
	}
	return &doc, nil
}

// replayFromDump walks the dump's message list and forwards structured
// Message events onto ch, mirroring the OpenCode backend's classification:
//
//   - assistant reasoning_details  → MessageThinking
//   - assistant tool_calls        → MessageToolUse
//   - assistant content           → MessageText
//   - tool results                → MessageToolResult
//
// System and user turns are skipped (they are the daemon-authored prompt,
// not agent output). Returns the accumulated assistant text for Result.Output.
func (b *forgeBackend) replayFromDump(msgs []forgeDumpMessage, ch chan<- Message) string {
	var output strings.Builder
	for _, m := range msgs {
		if m.Text != nil {
			if !strings.EqualFold(m.Text.Role, "assistant") {
				continue
			}
			// Reasoning → thinking.
			for _, r := range m.Text.ReasoningDetails {
				if strings.TrimSpace(r.Text) != "" {
					trySend(ch, Message{Type: MessageThinking, Content: r.Text})
				}
			}
			// Tool calls → tool-use.
			for _, tc := range m.Text.ToolCalls {
				trySend(ch, Message{
					Type:   MessageToolUse,
					Tool:   tc.Name,
					CallID: tc.CallID,
					Input:  tc.Arguments,
				})
			}
			// Prose → text.
			if strings.TrimSpace(m.Text.Content) != "" {
				output.WriteString(m.Text.Content)
				trySend(ch, Message{Type: MessageText, Content: m.Text.Content})
			}
			continue
		}
		if m.Tool != nil {
			var sb strings.Builder
			for _, v := range m.Tool.Output.Values {
				sb.WriteString(v.Text)
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				Tool:   m.Tool.Name,
				CallID: m.Tool.CallID,
				Output: sb.String(),
			})
		}
	}
	return output.String()
}

// forgeUsageFromMessages extracts token usage from a dump's message list.
// ForgeCode reports cumulative counters per assistant turn, so the latest
// non-zero entry is the authoritative total. Returns nil when no usable
// counters are found.
func forgeUsageFromMessages(msgs []forgeDumpMessage) *TokenUsage {
	var u TokenUsage
	found := false
	for _, m := range msgs {
		if m.Usage == nil {
			continue
		}
		in := m.Usage.PromptTokens.Actual
		out := m.Usage.CompletionTokens.Actual
		cached := m.Usage.CachedTokens.Actual
		if in == 0 && out == 0 && cached == 0 {
			continue
		}
		// Counters are cumulative across the conversation, so the latest
		// non-zero entry is the authoritative total.
		u.InputTokens = in
		u.OutputTokens = out
		u.CacheReadTokens = cached
		found = true
	}
	if !found {
		return nil
	}
	return &u
}

// ── JSON types for `forge conversation dump <id>` ──

// forgeConversationDump is the top-level shape written to the
// <timestamp>-dump.json file by `forge conversation dump <id>`.
type forgeConversationDump struct {
	Conversation forgeDumpConversation `json:"conversation"`
}

type forgeDumpConversation struct {
	ID      string           `json:"id"`
	Context forgeDumpContext `json:"context"`
}

type forgeDumpContext struct {
	Messages []forgeDumpMessage `json:"messages"`
}

// forgeDumpMessage is a tagged union: exactly one of Text or Tool is
// populated. Text messages carry the system/user/assistant turns (with
// tool_calls and reasoning_details on assistant turns); tool messages carry
// the tool-result side of a tool_call. Usage is attached to assistant text
// turns as a sibling of Text.
type forgeDumpMessage struct {
	Text  *forgeDumpText  `json:"text,omitempty"`
	Tool  *forgeDumpTool  `json:"tool,omitempty"`
	Usage *forgeDumpUsage `json:"usage,omitempty"`
}

type forgeDumpText struct {
	Role             string               `json:"role"`
	Content          string               `json:"content"`
	ToolCalls        []forgeDumpToolCall  `json:"tool_calls,omitempty"`
	ReasoningDetails []forgeDumpReasoning `json:"reasoning_details,omitempty"`
	Model            string               `json:"model,omitempty"`
}

type forgeDumpToolCall struct {
	Name      string         `json:"name"`
	CallID    string         `json:"call_id"`
	Arguments map[string]any `json:"arguments"`
}

type forgeDumpReasoning struct {
	Text string `json:"text"`
}

type forgeDumpTool struct {
	Name   string          `json:"name"`
	CallID string          `json:"call_id"`
	Output forgeDumpOutput `json:"output"`
}

type forgeDumpOutput struct {
	IsError bool                 `json:"is_error"`
	Values  []forgeDumpToolValue `json:"values"`
}

type forgeDumpToolValue struct {
	Text string `json:"text"`
}

// forgeDumpUsage holds per-message token counters. Counters are cumulative
// across the conversation, so the last non-zero entry is the authoritative
// total.
type forgeDumpUsage struct {
	PromptTokens     forgeDumpTokenCount `json:"prompt_tokens"`
	CompletionTokens forgeDumpTokenCount `json:"completion_tokens"`
	CachedTokens     forgeDumpTokenCount `json:"cached_tokens"`
}

type forgeDumpTokenCount struct {
	Actual int64 `json:"actual"`
}

// splitForgeModel splits a "provider/model" string (e.g.
// "anthropic/claude-sonnet-4-20250514") into the provider id and model id
// ForgeCode expects. Returns ok=false when there is no slash, so the caller
// can leave model selection to the persisted .forge.toml.
func splitForgeModel(s string) (provider, model string, ok bool) {
	idx := strings.Index(s, "/")
	if idx <= 0 || idx >= len(s)-1 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}

// ansiEscape matches CSI and OSC ANSI escape sequences emitted by terminal
// renderers (ForgeCode's stream_renderer uses SGR codes for color/style).
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][AB012]`)

// stripANSI removes ANSI escape sequences from s so the forwarded text is
// readable outside a terminal. ForgeCode's one-shot mode renders styled
// Markdown; without stripping, the Multica UI shows raw escape bytes.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
