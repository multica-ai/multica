package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// droidBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. --output-format pins the
// stream-json transport this backend depends on, --input-format and
// --session-id / -s / --fork are owned by Multica via ExecOptions.
var droidBlockedArgs = map[string]blockedArgMode{
	"-o":              blockedWithValue,
	"--output-format": blockedWithValue,
	"--input-format":  blockedWithValue,
	"-s":              blockedWithValue,
	"--session-id":    blockedWithValue,
	"--fork":          blockedWithValue,
}

// droidBackend implements Backend by spawning `droid exec` with
// stream-json output. Unlike Hermes/Kimi/Kiro, droid does NOT speak
// the Agent Client Protocol — its native streaming format is its own
// NDJSON schema (see droidStreamEvent below).
//
// Each backend invocation is a fresh `droid exec` process; multi-turn
// continuation happens via ExecOptions.ResumeSessionID → `-s <id>`.
type droidBackend struct {
	cfg Config
}

// droidStreamEvent is one parsed line from `droid exec --output-format
// stream-json`. Only the union of fields we consume is modeled; unknown
// fields are ignored.
type droidStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Text      string `json:"text,omitempty"`
	FinalText string `json:"finalText,omitempty"`
	ID        string `json:"id,omitempty"`
	// tool_call fields (droid uses toolName/parameters, not name/input)
	ToolName   string          `json:"toolName,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	// tool_result fields (droid puts output under "value", not "output";
	// the parent tool_call id is repeated under "id" rather than a
	// separate tool_use_id field)
	Value   string `json:"value,omitempty"`
	IsError bool   `json:"isError,omitempty"`
	// completion fields
	NumTurns int `json:"numTurns,omitempty"`
	Usage    *struct {
		InputTokens         int64 `json:"input_tokens"`
		OutputTokens        int64 `json:"output_tokens"`
		CacheReadTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage,omitempty"`
}

func (b *droidBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "droid"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("droid executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{"exec", "--output-format", "stream-json", "--auto", "medium"}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.Model != "" {
		// Multica's shared Claude/GPT catalogs use a different ID convention
		// than droid (e.g. "claude-sonnet-4.6" with dots and an optional
		// "anthropic/" prefix vs droid's "claude-sonnet-4-6" with dashes).
		// Normalize before passing through so an agent configured against
		// the general catalog still routes to a valid droid model id.
		normalized := normalizeDroidModelID(opts.Model)
		if normalized != opts.Model {
			b.cfg.Logger.Info("droid: normalized model id",
				"requested", opts.Model,
				"normalized", normalized,
			)
		}
		args = append(args, "--model", normalized)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-s", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, droidBlockedArgs, b.cfg.Logger)...)

	// Final positional: the prompt itself. droid exec takes prompt as
	// a positional argument; SystemPrompt is folded in by daemon.go
	// (providerNeedsInlineSystemPrompt is true for droid).
	userText := prompt
	if opts.SystemPrompt != "" {
		userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}
	args = append(args, userText)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args_count", len(args))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("droid stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("droid stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start droid: %w", err)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(newLogWriter(b.cfg.Logger, "[droid:stderr] "), stderr)
	}()

	b.cfg.Logger.Info("droid exec started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		var outputMu sync.Mutex
		var outputBuf strings.Builder
		var sessionID string
		var finalUsage TokenUsage
		var finalModel string
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var ev droidStreamEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				b.cfg.Logger.Warn("droid: unparseable stream line", "line", line, "err", err)
				continue
			}
			if ev.SessionID != "" && sessionID == "" {
				sessionID = ev.SessionID
			}

			switch ev.Type {
			case "system":
				// init: capture model + session, nothing to emit to UI
				if ev.Subtype == "init" {
					b.cfg.Logger.Info("droid session opened", "session_id", ev.SessionID, "model", droidModelFromEvent(line))
				}
			case "message":
				if ev.Role == "assistant" && ev.Text != "" {
					outputMu.Lock()
					outputBuf.WriteString(ev.Text)
					outputMu.Unlock()
					trySend(msgCh, Message{Type: MessageText, Content: ev.Text})
				}
				// role=user is an echo of our own prompt — skip.
			case "reasoning":
				// Pre-tool / pre-answer thinking. Droid emits these as
				// standalone events with `text`; surface them as the
				// canonical MessageThinking so the UI shows agent
				// reasoning the same way it does for Claude/Codex.
				if ev.Text != "" {
					trySend(msgCh, Message{Type: MessageThinking, Content: ev.Text})
				}
			case "tool_call":
				input := map[string]any{}
				if len(ev.Parameters) > 0 {
					_ = json.Unmarshal(ev.Parameters, &input)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   droidToolNameFromTitle(ev.ToolName),
					CallID: ev.ID,
					Input:  input,
				})
			case "tool_result":
				// `id` repeats the matching tool_call's id (droid does
				// not emit a separate tool_use_id field).
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: ev.ID,
					Output: ev.Value,
				})
			case "completion":
				if ev.FinalText != "" {
					outputMu.Lock()
					if outputBuf.Len() == 0 {
						outputBuf.WriteString(ev.FinalText)
					}
					outputMu.Unlock()
				}
				if ev.Usage != nil {
					finalUsage = TokenUsage{
						InputTokens:     ev.Usage.InputTokens,
						OutputTokens:    ev.Usage.OutputTokens,
						CacheReadTokens: ev.Usage.CacheReadTokens,
					}
				}
			case "error":
				finalStatus = "failed"
				if ev.Text != "" {
					finalError = ev.Text
				} else {
					finalError = "droid emitted an error event"
				}
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("droid: stdout scan error", "err", err)
		}

		<-stderrDone

		// If the context expired mid-stream, surface a precise reason
		// even if droid happened to exit cleanly.
		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("droid timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled && finalStatus == "completed" {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		}

		outputMu.Lock()
		finalOutput := outputBuf.String()
		outputMu.Unlock()

		var usageMap map[string]TokenUsage
		if finalUsage.InputTokens > 0 || finalUsage.OutputTokens > 0 || finalUsage.CacheReadTokens > 0 {
			model := opts.Model
			if model == "" {
				model = finalModel
			}
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: finalUsage}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("droid finished",
			"pid", cmd.Process.Pid,
			"status", finalStatus,
			"duration", duration.Round(time.Millisecond).String(),
			"session_id", sessionID,
			"output_bytes", len(finalOutput),
		)

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// droidModelFromEvent pulls a "model" field out of a raw JSON line
// without re-parsing the full event — used only for the init log line
// so we can record which model the session opened with.
func droidModelFromEvent(line string) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal([]byte(line), &m)
	return m.Model
}

// droidKnownModelIDs is the set of droid-native model IDs we ship with
// the static catalog (see droidStaticModels in models.go). Used by
// normalizeDroidModelID to short-circuit when the caller already passed
// a valid id, and to recognize a successful claude-* dot→dash conversion.
var droidKnownModelIDs = map[string]struct{}{
	"claude-opus-4-7":            {},
	"claude-opus-4-7-fast":       {},
	"claude-opus-4-6":            {},
	"claude-opus-4-6-fast":       {},
	"claude-opus-4-5-20251101":   {},
	"claude-sonnet-4-6":          {},
	"claude-sonnet-4-5-20250929": {},
	"claude-haiku-4-5-20251001":  {},
	"gpt-5.5":                    {},
	"gpt-5.5-fast":               {},
	"gpt-5.5-pro":                {},
	"gpt-5.4":                    {},
	"gpt-5.4-fast":               {},
	"gpt-5.4-mini":               {},
	"gpt-5.3-codex":              {},
	"gpt-5.3-codex-fast":         {},
	"gpt-5.2":                    {},
	"gpt-5.2-codex":              {},
	"gemini-3.1-pro-preview":     {},
	"gemini-3.5-flash":           {},
	"gemini-3-flash-preview":     {},
	"glm-5.1":                    {},
	"kimi-k2.6":                  {},
	"kimi-k2.5":                  {},
	"deepseek-v4-pro":            {},
	"minimax-m2.7":               {},
	"minimax-m2.5":               {},
}

// normalizeDroidModelID maps a model id coming from Multica's shared
// model catalogs (which use dots for claude versions and an optional
// "<provider>/" prefix) onto droid's native id format. Unknown ids pass
// through unchanged — droid will reject them and surface the canonical
// "Available built-in models:" list to the user.
func normalizeDroidModelID(id string) string {
	if _, ok := droidKnownModelIDs[id]; ok {
		return id
	}
	// Strip a leading "<provider>/" prefix (anthropic/, openai/, google/, …).
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	if _, ok := droidKnownModelIDs[id]; ok {
		return id
	}
	// Claude-family ids in droid use dashes for version numbers
	// ("claude-sonnet-4-6") where the shared catalog uses dots
	// ("claude-sonnet-4.6"). GPT/Gemini/Kimi/MiniMax keep dots in
	// droid too, so only apply this rewrite to claude- ids.
	if strings.HasPrefix(id, "claude-") {
		candidate := strings.ReplaceAll(id, ".", "-")
		if _, ok := droidKnownModelIDs[candidate]; ok {
			return candidate
		}
	}
	return id
}

// droidToolNameFromTitle normalizes Factory.ai droid's built-in tool
// names (from the stream-json init event's `tools` list) into the
// canonical Multica names used by the UI.
func droidToolNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	if idx := strings.Index(t, ":"); idx > 0 {
		t = strings.TrimSpace(t[:idx])
	}
	switch t {
	case "Read":
		return "read_file"
	case "Create":
		return "write_file"
	case "Edit", "ApplyPatch":
		return "edit_file"
	case "Execute":
		return "terminal"
	case "Grep":
		return "search_files"
	case "Glob":
		return "glob"
	case "LS":
		return "list_files"
	case "WebSearch":
		return "web_search"
	case "FetchUrl":
		return "web_fetch"
	case "TodoWrite":
		return "todo_write"
	case "AskUser":
		return "ask_user"
	case "Task":
		return "task"
	}
	// Fallback: snake_case the original.
	lower := strings.ToLower(t)
	return strings.ReplaceAll(lower, " ", "_")
}
