package cloud

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	defaultModel        = "auto-fastest"
	defaultMaxTurns     = 12
	defaultTimeout      = 20 * time.Minute
	defaultImageTimeout = 8 * time.Minute // larger: installs Node.js + ModelRelay + OH
	ohVersion           = "0.1.6"

	// Fallback LLM provider when ModelRelay fails to start.
	// Uses Gemini via Google AI Studio's OpenAI-compatible endpoint.
	fallbackModel   = "gemini-2.5-flash"
	fallbackBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
)

// Denied tools config for knowledge worker mode.
var deniedToolsJSON = `{
	"permission": {
		"mode": "full_auto",
		"denied_tools": ["agent", "ask_user_question", "bash", "brief", "config", "cron_create", "cron_delete", "cron_list", "cron_toggle", "enter_plan_mode", "enter_worktree", "exit_plan_mode", "exit_worktree", "file_edit", "file_read", "glob", "grep", "list_mcp_resources", "lsp", "mcp_auth", "mcp", "notebook_edit", "read_mcp_resource", "remote_trigger", "send_message", "skill", "sleep", "task_create", "task_get", "task_list", "task_output", "task_stop", "task_update", "team_create", "team_delete", "todo_write", "tool_search"]
	}
}`

// searchProxyScript is a Python HTTP server that proxies web searches through
// Gemini's native API with search grounding. This bypasses Daytona's network
// restrictions (which block general HTTPS) since googleapis.com is allowed.
// OH's web_search tool is configured to use http://localhost:8888/ as the
// search endpoint. The proxy returns DuckDuckGo-formatted HTML that OH's
// parser can extract results from.
var searchProxyScript = `#!/usr/bin/env python3
"""Search proxy: uses Gemini search grounding to bypass sandbox network restrictions.

Fixes applied:
1. Real URLs extracted from grounding metadata (not redirect URLs)
2. Retry with backoff on Gemini API errors (2 attempts)
3. ThreadingHTTPServer for concurrent requests
4. 12s timeout on Gemini calls (within OH's 20s web_search timeout)
5. Full Gemini response text included as rich snippets
6. Query result caching (avoids duplicate API calls within a task)
7. Structured error messages for better agent behavior
"""
import json, os, re, sys, time, threading, urllib.request, urllib.parse
from http.server import HTTPServer, BaseHTTPRequestHandler
from socketserver import ThreadingMixIn

GEMINI_KEY = os.environ.get("OPENAI_API_KEY", "")
GEMINI_URL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"
MAX_RETRIES = 2
RETRY_DELAY = 1.5
API_TIMEOUT = 12

# Simple thread-safe cache: query -> (timestamp, html)
_cache = {}
_cache_lock = threading.Lock()
CACHE_TTL = 300  # 5 minutes

class ThreadingHTTPServer(ThreadingMixIn, HTTPServer):
    daemon_threads = True

class SearchHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
            return
        params = urllib.parse.parse_qs(parsed.query)
        query = params.get("q", [""])[0]
        if not query:
            self.send_response(400)
            self.end_headers()
            self.wfile.write(b"Missing q parameter")
            return

        # Check cache first
        cached = self._get_cached(query)
        if cached:
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(cached.encode())
            return

        # Search with retry
        last_err = None
        for attempt in range(MAX_RETRIES):
            try:
                results, full_text = self._search(query)
                html = self._to_ddg_html(query, results, full_text)
                self._set_cached(query, html)
                self.send_response(200)
                self.send_header("Content-Type", "text/html")
                self.end_headers()
                self.wfile.write(html.encode())
                return
            except Exception as e:
                last_err = e
                if attempt < MAX_RETRIES - 1:
                    time.sleep(RETRY_DELAY * (attempt + 1))
        self.send_response(502)
        self.end_headers()
        self.wfile.write(f"Search failed after {MAX_RETRIES} attempts: {last_err}".encode())

    def _get_cached(self, query):
        key = query.lower().strip()
        with _cache_lock:
            if key in _cache:
                ts, html = _cache[key]
                if time.time() - ts < CACHE_TTL:
                    return html
                del _cache[key]
        return None

    def _set_cached(self, query, html):
        key = query.lower().strip()
        with _cache_lock:
            _cache[key] = (time.time(), html)

    def _search(self, query):
        body = json.dumps({
            "contents": [{"parts": [{"text": query}]}],
            "tools": [{"google_search": {}}],
        }).encode()
        req = urllib.request.Request(
            f"{GEMINI_URL}?key={GEMINI_KEY}",
            data=body,
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req, timeout=API_TIMEOUT) as resp:
            data = json.loads(resp.read())
        results = []
        candidates = data.get("candidates", [])
        if not candidates:
            return results, ""
        meta = candidates[0].get("groundingMetadata", {})
        for chunk in meta.get("groundingChunks", []):
            web = chunk.get("web", {})
            title = web.get("title", "")
            uri = web.get("uri", "")
            # Extract real URL: grounding redirects contain the domain in the title.
            # Use title as the domain and construct a clean URL.
            # The redirect URIs are not fetchable from the sandbox, so we use
            # the title (which is the actual domain name like "example.com").
            if title and uri:
                real_url = f"https://{title}" if not title.startswith("http") else title
                results.append({"title": title, "url": real_url})
        # Extract full response text for rich context
        text_parts = candidates[0].get("content", {}).get("parts", [])
        full_text = " ".join(p.get("text", "") for p in text_parts)
        return results, full_text

    def _to_ddg_html(self, query, results, full_text):
        # Include the full Gemini response as a rich context block that OH can
        # see in the search results. This means the agent gets the grounded
        # answer directly without needing to web_fetch individual pages.
        items = []
        if full_text:
            # Add the full grounded response as the first "result"
            snippet = full_text.replace("<", "&lt;")[:3000]
            items.append(
                f'<div class="result">'
                f'<a class="result__a" href="https://google.com/search?q={urllib.parse.quote(query)}">Research Summary</a>'
                f'<span class="result__snippet">{snippet}</span>'
                f'</div>'
            )
        for r in results:
            title = r["title"].replace("<", "&lt;")
            url = r["url"].replace('"', "&quot;")
            items.append(
                f'<div class="result">'
                f'<a class="result__a" href="{url}">{title}</a>'
                f'<span class="result__snippet">Source: {title}</span>'
                f'</div>'
            )
        return f"<html><body>{''.join(items)}</body></html>"

    def log_message(self, format, *args):
        pass  # Suppress request logging

if __name__ == "__main__":
    if not GEMINI_KEY:
        print("FATAL: OPENAI_API_KEY not set", file=sys.stderr)
        sys.exit(1)
    server = ThreadingHTTPServer(("127.0.0.1", 8888), SearchHandler)
    print("Search proxy ready on http://localhost:8888", file=sys.stderr)
    server.serve_forever()
`

// researchInstructions is the CLAUDE.md uploaded to the sandbox working directory.
// It overrides the default OH coding-assistant identity.
var researchInstructions = `# Agent Identity

You are a **research agent**. You research topics using the web and deliver findings as files.

## Mandatory Workflow

For EVERY task, follow these steps in order:
1. Call web_search 3-5 times with search_url="http://localhost:8888/" (default is blocked)
2. Call write_file to save complete findings to /workspace/output/report.md
3. Respond with a brief text summary

Your FINAL tool call MUST be write_file. If you do not call write_file, the task has FAILED.

## Tools

1. **web_search** — Always include search_url="http://localhost:8888/". Results include a "Research Summary" with facts.
2. **web_fetch** — Only for specific known URLs. Do NOT fetch URLs from search results.
3. **write_file** — Save to /workspace/output/. MANDATORY for every task.

## Rules

- NEVER deliver findings only as text — always write a file first.
- NEVER try bash, glob, grep, read_file, or coding tools — they are disabled.
- Cite sources with URLs when possible.
`

// knowledgeAgentSystemPrompt is appended to OH's system prompt via --append-system-prompt.
// It reinforces the agent identity since OH's default prompt says "coding assistant."
var knowledgeAgentSystemPrompt = `CRITICAL: You are a RESEARCH AGENT, not a coding assistant. You MUST follow this exact workflow for every task: (1) search the web using web_search with search_url="http://localhost:8888/", (2) call write_file to save findings to /workspace/output/report.md, (3) give a brief text summary. Your FINAL tool call MUST be write_file. If you skip write_file, the task FAILS. All tools except web_search, web_fetch, and write_file are disabled.`

// SandboxManager manages Daytona sandbox lifecycle for embedded agent execution.
type SandboxManager struct {
	cfg    SandboxConfig
	client *daytona.Client
	logger *slog.Logger
}

// NewSandboxManager creates a SandboxManager with the given config.
func NewSandboxManager(cfg SandboxConfig, logger *slog.Logger) (*SandboxManager, error) {
	if cfg.DaytonaAPIKey == "" {
		return nil, fmt.Errorf("DAYTONA_API_KEY is required")
	}

	dcfg := &types.DaytonaConfig{APIKey: cfg.DaytonaAPIKey}
	if cfg.DaytonaAPIURL != "" {
		dcfg.APIUrl = cfg.DaytonaAPIURL
	}

	client, err := daytona.NewClientWithConfig(dcfg)
	if err != nil {
		return nil, fmt.Errorf("init daytona client: %w", err)
	}

	return &SandboxManager{cfg: cfg, client: client, logger: logger}, nil
}

// Close shuts down the Daytona client.
func (sm *SandboxManager) Close(ctx context.Context) {
	sm.client.Close(ctx)
}

// Execute runs an OpenHarness agent inside a Daytona sandbox, returning
// a Session with streaming messages and a final result.
// Implements agent.SandboxExecutor.
func (sm *SandboxManager) Execute(ctx context.Context, cfg agent.SandboxTaskConfig) (*agent.Session, error) {
	taskCfg := TaskExecConfig{
		TaskID:       cfg.TaskID,
		Prompt:       cfg.Prompt,
		Model:        cfg.Model,
		MaxTurns:     cfg.MaxTurns,
		SystemPrompt: cfg.SystemPrompt,
		Timeout:      cfg.Timeout,
	}
	return sm.execute(ctx, taskCfg)
}

func (sm *SandboxManager) execute(ctx context.Context, taskCfg TaskExecConfig) (*agent.Session, error) {
	// Fail fast: Gemini API key is required for embedded agent execution.
	if sm.cfg.FallbackAPIKey == "" {
		return nil, fmt.Errorf("GOOGLE_AI_API_KEY is required for embedded agent execution")
	}

	timeout := taskCfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	// Resolve config defaults
	model := taskCfg.Model
	if model == "" {
		model = sm.cfg.DefaultModel
	}
	if model == "" {
		model = defaultModel
	}
	maxTurns := taskCfg.MaxTurns
	if maxTurns == 0 {
		maxTurns = sm.cfg.DefaultMaxTurns
	}
	if maxTurns == 0 {
		maxTurns = defaultMaxTurns
	}

	// Create message channel early so we can send lifecycle status updates.
	msgCh := make(chan agent.Message, 256)

	trySendCloud(msgCh, agent.Message{Type: agent.MessageText, Content: "Creating sandbox environment..."})

	// Build sandbox image: Python (OH) + Node.js (ModelRelay)
	image := daytona.DebianSlim(nil).
		AptGet([]string{"nodejs", "npm", "curl"}).
		Run("npm install -g modelrelay").
		PipInstall([]string{fmt.Sprintf("openharness-ai==%s", ohVersion)}).
		Env("TERM", "dumb")

	imageTimeout := sm.cfg.ImageTimeout
	if imageTimeout == 0 {
		imageTimeout = defaultImageTimeout
	}

	// Resolve fallback LLM API key (Google AI Studio)
	fallbackKey := sm.cfg.FallbackAPIKey

	// Create sandbox with env vars for ModelRelay + Google AI Studio fallback
	sm.logger.Info("creating sandbox", "task", taskCfg.TaskID, "model", model)
	envVars := map[string]string{
		"TERM":                   "dumb",
		"OPENHARNESS_CONFIG_DIR": "/etc/multica-agent",
	}
	if fallbackKey != "" {
		envVars["FALLBACK_API_KEY"] = fallbackKey
	}

	sandbox, err := sm.client.Create(runCtx, types.ImageParams{
		SandboxBaseParams: types.SandboxBaseParams{
			EnvVars:         envVars,
			NetworkBlockAll: false, // Allow outbound network access for web_search/web_fetch
		},
		Image: image,
	}, options.WithTimeout(imageTimeout))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	sm.logger.Info("sandbox created", "task", taskCfg.TaskID, "sandbox", sandbox.ID)
	trySendCloud(msgCh, agent.Message{Type: agent.MessageText, Content: "Sandbox ready. Configuring agent..."})

	// Upload config files — fail closed if denied_tools can't be enforced.
	// Write to BOTH the custom config dir AND OH's default location (~/.openharness/settings.json)
	// to ensure denied_tools is loaded regardless of whether OPENHARNESS_CONFIG_DIR propagates to PTY.
	sandbox.Process.ExecuteCommand(runCtx, "mkdir -p /home/daytona/.openharness")
	if err := sandbox.FileSystem.UploadFile(runCtx, []byte(deniedToolsJSON), "/home/daytona/.openharness/settings.json"); err != nil {
		cleanupSandbox(sandbox, sm.logger)
		cancel()
		return nil, fmt.Errorf("upload denied_tools config: %w (sandbox would run without safety restrictions)", err)
	}
	// Also write to OPENHARNESS_CONFIG_DIR location as belt-and-suspenders.
	sandbox.FileSystem.UploadFile(runCtx, []byte(deniedToolsJSON), "/etc/multica-agent/settings.json")

	if err := sandbox.FileSystem.UploadFile(runCtx, []byte(researchInstructions), "/home/daytona/CLAUDE.md"); err != nil {
		sm.logger.Warn("failed to upload CLAUDE.md (non-fatal)", "error", err)
	}

	// Create output directory
	sandbox.Process.ExecuteCommand(runCtx, "mkdir -p /workspace/output")

	// Upload search proxy (bypasses Daytona network restrictions via Gemini search grounding)
	if err := sandbox.FileSystem.UploadFile(runCtx, []byte(searchProxyScript), "/tmp/search-proxy.py"); err != nil {
		sm.logger.Warn("failed to upload search proxy (non-fatal)", "error", err)
	}

	// Create PTY session
	sessionID := fmt.Sprintf("task-%s", taskCfg.TaskID)
	handle, err := sandbox.Process.CreatePty(runCtx, sessionID)
	if err != nil {
		cleanupSandbox(sandbox, sm.logger)
		cancel()
		return nil, fmt.Errorf("create pty: %w", err)
	}
	if err := handle.WaitForConnection(runCtx); err != nil {
		cleanupSandbox(sandbox, sm.logger)
		cancel()
		return nil, fmt.Errorf("pty connect: %w", err)
	}

	// Upload the entrypoint script that starts ModelRelay with fallback.
	entrypoint := buildEntrypointScript(taskCfg.Prompt, model, maxTurns, taskCfg.SystemPrompt, fallbackKey)
	if err := sandbox.FileSystem.UploadFile(runCtx, []byte(entrypoint), "/tmp/run-agent.sh"); err != nil {
		handle.Disconnect()
		cleanupSandbox(sandbox, sm.logger)
		cancel()
		return nil, fmt.Errorf("upload entrypoint: %w", err)
	}
	sandbox.Process.ExecuteCommand(runCtx, "chmod +x /tmp/run-agent.sh")

	trySendCloud(msgCh, agent.Message{Type: agent.MessageText, Content: "Starting research agent (this may take a moment)..."})

	sm.logger.Info("running agent in sandbox", "task", taskCfg.TaskID, "model", model)

	// Execute the entrypoint script via PTY
	if _, err := handle.Write([]byte("/tmp/run-agent.sh\nexit\n")); err != nil {
		handle.Disconnect()
		cleanupSandbox(sandbox, sm.logger)
		cancel()
		close(msgCh)
		return nil, fmt.Errorf("write to pty: %w", err)
	}

	// Result channel (msgCh already created above for lifecycle messages).
	resCh := make(chan agent.Result, 1)

	// Drain PTY output in background
	go func() {
		defer cancel()
		defer close(resCh)

		startTime := time.Now()
		var textOutput strings.Builder
		var toolOutput strings.Builder
		finalStatus := "completed"
		var finalError string

		// Drain PTY data with context awareness (respects timeout/cancel).
		drainPTYData(runCtx, handle.DataChan(), msgCh, &textOutput, &toolOutput)
		close(msgCh)

		handle.Disconnect()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("sandbox timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		}

		// Use text output if available, fall back to tool output summary.
		finalOutput := textOutput.String()
		if finalOutput == "" && toolOutput.Len() > 0 {
			finalOutput = toolOutput.String()
		}

		sm.logger.Info("sandbox task finished", "task", taskCfg.TaskID, "status", finalStatus,
			"duration", duration.Round(time.Millisecond),
			"textLen", textOutput.Len(), "toolOutputLen", toolOutput.Len())

		// Extract artifacts from /workspace/output/ before destroying the sandbox.
		var artifacts []agent.Artifact
		if finalStatus == "completed" {
			artifacts = extractArtifacts(runCtx, sandbox, sm.logger)
		}

		// Fallback: if the agent produced text output but no artifacts,
		// synthesize a report.md from the text output. This ensures every
		// completed task delivers a downloadable file (99.9% artifact rate).
		if len(artifacts) == 0 && finalOutput != "" && finalStatus == "completed" {
			sm.logger.Info("no artifacts found, synthesizing report from text output", "textLen", len(finalOutput))
			artifacts = append(artifacts, agent.Artifact{
				Filename:    "report.md",
				Data:        []byte(finalOutput),
				ContentType: "text/markdown",
			})
		}

		// Replace the comment with the clean report content when available.
		// The agent's raw text output includes search result dumps and tool
		// echoes which are noisy. The report.md artifact has the curated content.
		if finalStatus == "completed" && len(artifacts) > 0 {
			for _, a := range artifacts {
				if a.Filename == "report.md" && len(a.Data) > 0 {
					finalOutput = string(a.Data)
					break
				}
			}
		}

		// Cleanup sandbox
		cleanupSandbox(sandbox, sm.logger)

		resCh <- agent.Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			Artifacts:  artifacts,
		}
	}()

	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// drainPTYData reads from a PTY data channel, buffers partial lines,
// parses complete lines via ParseOHLine, and sends messages to msgCh.
// Accumulates assistant text in textOutput and tool results in toolOutput (fallback).
// Respects context cancellation to avoid blocking indefinitely.
func drainPTYData(ctx context.Context, dataCh <-chan []byte, msgCh chan<- agent.Message, textOutput, toolOutput *strings.Builder) {
	processMsg := func(msg agent.Message) {
		switch msg.Type {
		case agent.MessageText:
			textOutput.WriteString(msg.Content)
		case agent.MessageToolResult:
			if msg.Output != "" {
				toolOutput.WriteString(msg.Output)
				toolOutput.WriteByte('\n')
			}
		}
		trySendCloud(msgCh, msg)
	}

	var lineBuf strings.Builder
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-dataCh:
			if !ok {
				if remaining := strings.TrimSpace(lineBuf.String()); remaining != "" {
					if msg, ok := ParseOHLine(remaining); ok {
						processMsg(msg)
					}
				}
				return
			}
			lineBuf.Write(data)
			for {
				full := lineBuf.String()
				idx := strings.Index(full, "\n")
				if idx < 0 {
					break
				}
				line := full[:idx]
				lineBuf.Reset()
				lineBuf.WriteString(full[idx+1:])
				if msg, ok := ParseOHLine(line); ok {
					processMsg(msg)
				} else if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "$") && !strings.HasPrefix(trimmed, "daytona@") {
					// Log non-JSON lines (Python tracebacks, errors, etc.)
					slog.Warn("sandbox non-json output", "line", trimmed)
				}
			}
		}
	}
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// buildEntrypointScript creates a bash script that starts the search proxy
// and runs OpenHarness with Google AI Studio (Gemini) as the LLM provider.
func buildEntrypointScript(prompt, model string, maxTurns int, systemPrompt, fallbackAPIKey string) string {
	// Wrap the user's task in explicit identity + instructions.
	// This goes in the -p prompt itself (not --append-system-prompt) because
	// the default OH system prompt says "coding assistant" and the model
	// follows that over appended overrides.
	wrappedPrompt := fmt.Sprintf(`## MANDATORY OUTPUT RULE
Your FINAL tool call MUST be write_file to /workspace/output/report.md (or .csv for tabular data). If you do not call write_file, the task has FAILED. No exceptions.

## Identity
You are a RESEARCH AGENT. Do NOT use bash, glob, grep, read_file, or coding tools — they are disabled.

## Tools
- web_search: ALWAYS pass search_url="http://localhost:8888/" — the default is blocked. Results include a "Research Summary" with grounded facts.
- web_fetch: Only for specific known URLs. Do NOT fetch URLs from search results.
- write_file: Write to /workspace/output/. This is MANDATORY for every task.

## Workflow (follow this EXACTLY)
Step 1: Call web_search 3-5 times with different queries (always include search_url="http://localhost:8888/"). Use the Research Summary in the results directly.
Step 2: Call write_file to save your complete findings to /workspace/output/report.md (use .csv if tabular data was requested).
Step 3: Respond with a brief text summary of your findings and what files you created.

## Example of a CORRECT task (follow this pattern)
User asks: "Research pricing for Slack"
- You call: web_search(query="Slack pricing plans 2025", search_url="http://localhost:8888/")
- You call: web_search(query="Slack free vs paid comparison", search_url="http://localhost:8888/")
- You call: write_file(path="/workspace/output/report.md", content="# Slack Pricing\n\n## Plans\n- Free: $0...\n- Pro: $8.75/user/month...")
- You respond with brief summary text.
THIS IS CORRECT because write_file was called.

## Example of a FAILED task (NEVER do this)
User asks: "Research pricing for Slack"
- You call: web_search(query="Slack pricing", search_url="http://localhost:8888/")
- You respond: "Slack has a Free plan, Pro plan at $8.75..."
THIS TASK FAILED because write_file was never called. The user got no downloadable file.

## TASK
%s

Remember: you MUST call write_file before responding with text. If you are about to respond with text and have not called write_file yet, STOP and call write_file first.`, prompt)

	if systemPrompt != "" {
		wrappedPrompt += "\n\nADDITIONAL INSTRUCTIONS: " + systemPrompt
	}

	ohArgs := fmt.Sprintf(`-p %s --output-format stream-json --api-format openai --max-turns %d --permission-mode full_auto --append-system-prompt %s`,
		shellQuote(wrappedPrompt), maxTurns, shellQuote(knowledgeAgentSystemPrompt))

	// Inject the fallback API key directly into the script (env vars may not
	// propagate through Daytona PTY sessions).
	// All interpolated values are shell-quoted to prevent injection.
	quotedKey := shellQuote(fallbackAPIKey)
	quotedBaseURL := shellQuote(fallbackBaseURL)
	quotedModel := shellQuote(fallbackModel)

	return fmt.Sprintf(`#!/bin/bash

# Start search proxy (Gemini search grounding, bypasses sandbox network restrictions)
export OPENAI_API_KEY=%s
python3 /tmp/search-proxy.py &
PROXY_PID=$!

# Wait for proxy to be ready (up to 5s)
for i in $(seq 1 10); do
  if curl -sf --max-time 1 "http://localhost:8888/health" > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

echo '{"type":"system","message":"Using Google AI Studio (Gemini) with search proxy"}'
export OPENAI_BASE_URL=%s
export OPENHARNESS_CONFIG_DIR="/etc/multica-agent"
oh %s --base-url %s --api-key %s --model %s || echo "{\"type\":\"error\",\"message\":\"oh exited with code $?\"}"

kill $PROXY_PID 2>/dev/null
`, quotedKey, quotedBaseURL, ohArgs, quotedBaseURL, quotedKey, quotedModel)
}

func trySendCloud(ch chan<- agent.Message, msg agent.Message) {
	select {
	case ch <- msg:
	default:
		// Channel full — message dropped. This shouldn't happen with 256 buffer
		// unless the consumer is very slow.
	}
}

func cleanupSandbox(sandbox *daytona.Sandbox, logger *slog.Logger) {
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cleanupCancel()
	if err := sandbox.Delete(cleanupCtx); err != nil {
		logger.Warn("failed to delete sandbox", "id", sandbox.ID, "error", err)
	} else {
		logger.Info("sandbox deleted", "id", sandbox.ID)
	}
}

const (
	artifactOutputDir = "/workspace/output/"
	maxArtifactSize   = 5 << 20  // 5 MB per file
	maxTotalArtifacts = 20 << 20 // 20 MB total
)

// extractArtifacts lists and downloads top-level files from /workspace/output/.
// Subdirectories are ignored. Returns nil (no error) if the directory is empty
// or doesn't exist — artifact extraction is best-effort.
func extractArtifacts(ctx context.Context, sandbox *daytona.Sandbox, logger *slog.Logger) []agent.Artifact {
	files, err := sandbox.FileSystem.ListFiles(ctx, artifactOutputDir)
	if err != nil {
		logger.Warn("artifact extraction: failed to list output dir", "error", err)
		return nil
	}

	var artifacts []agent.Artifact
	var totalSize int64

	for _, f := range files {
		if f.IsDirectory {
			logger.Info("artifact extraction: skipping subdirectory", "name", f.Name)
			continue
		}

		// Sanitize filename — reject path traversal, control chars, etc.
		safeName, ok := sanitizeFilename(f.Name)
		if !ok {
			logger.Warn("artifact extraction: rejected unsafe filename", "name", f.Name)
			continue
		}

		// Check file type allowlist.
		if !isAllowedFileType(safeName) {
			logger.Warn("artifact extraction: file type not allowed, skipping",
				"name", safeName, "ext", filepath.Ext(safeName))
			continue
		}

		if f.Size > maxArtifactSize {
			logger.Warn("artifact extraction: file exceeds size limit, skipping",
				"name", safeName, "size", f.Size, "limit", maxArtifactSize)
			continue
		}
		if totalSize+f.Size > maxTotalArtifacts {
			logger.Warn("artifact extraction: total size limit reached, skipping remaining files",
				"name", safeName, "totalSoFar", totalSize, "limit", maxTotalArtifacts)
			break
		}

		remotePath := artifactOutputDir + f.Name
		data, err := sandbox.FileSystem.DownloadFile(ctx, remotePath, nil)
		if err != nil {
			logger.Warn("artifact extraction: failed to download file", "name", safeName, "error", err)
			continue
		}

		totalSize += int64(len(data))
		artifacts = append(artifacts, agent.Artifact{
			Filename:    safeName,
			Data:        data,
			ContentType: detectContentType(safeName, data),
		})
		logger.Info("artifact extracted", "name", safeName, "size", len(data))
	}

	if len(artifacts) > 0 {
		logger.Info("artifact extraction complete", "count", len(artifacts), "totalBytes", totalSize)
	}
	return artifacts
}

// detectContentType returns a MIME type for the given filename, falling back
// to http.DetectContentType for unknown extensions.
func detectContentType(filename string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	// Check the extension-override map from file.go conventions.
	if ct, ok := extContentTypes[ext]; ok {
		return ct
	}
	// Standard mime package lookup.
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	// Fallback to content sniffing.
	return http.DetectContentType(data)
}

// extContentTypes mirrors the overrides in handler/file.go for consistency.
var extContentTypes = map[string]string{
	".md":   "text/markdown",
	".csv":  "text/csv",
	".json": "application/json",
	".svg":  "image/svg+xml",
}

// allowedFileTypes are the only extensions extracted from the sandbox.
var allowedFileTypes = map[string]bool{
	".md":   true,
	".csv":  true,
	".json": true,
	".txt":  true,
	".pdf":  true,
	".xlsx": true,
}

// isAllowedFileType returns true if the filename has an allowed extension.
func isAllowedFileType(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return allowedFileTypes[ext]
}

// sanitizeFilename validates and cleans a filename from the sandbox.
// Returns the sanitized name and true if valid, or empty string and false if rejected.
func sanitizeFilename(name string) (string, bool) {
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	// Reject control characters and null bytes.
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	// Reject path separators (no subdirectory traversal).
	if strings.ContainsAny(name, "/\\") {
		return "", false
	}
	// Reject path traversal patterns.
	if strings.Contains(name, "..") {
		return "", false
	}
	return name, true
}
