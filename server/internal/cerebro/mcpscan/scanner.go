// Package mcpscan spawns a Model Context Protocol stdio server and reads its
// tools/list response (JEH-1710).
//
// The daemon uses this to discover what tools each MCP server in a runtime's
// tools_config exposes, so the workspace-admin UI can render and govern them
// without admins having to type tool names by hand.
//
// Wire protocol: JSON-RPC 2.0 over the child process's stdin/stdout, framed
// one message per line (LF-terminated). The scanner sends:
//
//  1. initialize       — handshake, advertises an empty client capability set
//  2. notifications/initialized — required by the spec before tool calls
//  3. tools/list       — requests the inventory
//
// Then closes stdin, waits up to the deadline for the child to exit, kills
// it on overrun. Stderr from the child is captured and surfaced on error so
// "command not found" / "permission denied" are reported back to the admin
// instead of timing out silently.
//
// This is intentionally a minimal client: it does NOT support
// streaming/notification handlers, resources, prompts, or sampling. tools/list
// is the only RPC we issue. Servers that need a richer handshake will simply
// return whatever tools they expose; servers that crash on an unknown
// notification are non-compliant and out of scope.
package mcpscan

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout caps a single MCP scan. A server that does not respond
// within this window is killed and treated as failed; the scanner moves on
// to the next server in the runtime's config.
const DefaultTimeout = 30 * time.Second

// ProtocolVersion is the value advertised in the initialize request. Bumping
// this only matters if a server gates behaviour on it; we pick the dated
// snapshot used by Claude Code's reference client.
const ProtocolVersion = "2024-11-05"

// ServerSpec describes one MCP server entry from the runtime's tools_config,
// in the Claude Code mcpServers format: {command, args, env}.
type ServerSpec struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

// Tool is one entry from the MCP server's tools/list response.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// Result is what Scan returns to the caller — either a populated Tools list
// or an Err describing why the scan failed for this server.
type Result struct {
	ServerName string
	Tools      []Tool
	Err        error
}

// Scan spawns the MCP server described by spec, performs the handshake,
// asks for the tool inventory and returns it. The child process is always
// terminated before Scan returns, regardless of outcome.
//
// timeout is the upper bound for the whole exchange (handshake + tools/list +
// graceful exit). Pass 0 to use DefaultTimeout.
func Scan(ctx context.Context, spec ServerSpec, timeout time.Duration) ([]Tool, error) {
	if spec.Command == "" {
		return nil, errors.New("mcpscan: server command is empty")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(scanCtx, spec.Command, spec.Args...)
	cmd.Env = buildEnv(spec.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpscan: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpscan: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcpscan: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcpscan: start %q: %w", spec.Command, err)
	}

	// Drain stderr into a bounded buffer so an error message survives the
	// process exiting before we read everything.
	var stderrBuf strings.Builder
	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		drainStderr(stderr, &stderrBuf)
	}()

	tools, scanErr := exchange(stdin, stdout)

	// Always close stdin to signal the server to exit, then wait briefly.
	// CommandContext will SIGKILL on timeout so we don't hang here.
	_ = stdin.Close()
	waitErr := cmd.Wait()
	stderrWG.Wait()

	if scanErr != nil {
		return nil, joinExitErr(scanErr, waitErr, stderrBuf.String())
	}
	return tools, nil
}

// exchange runs the initialize → initialized → tools/list dance over stdin/stdout.
func exchange(stdin io.WriteCloser, stdout io.Reader) ([]Tool, error) {
	reader := bufio.NewReader(stdout)

	if err := writeMessage(stdin, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "multica-daemon-mcpscan",
				"version": "1",
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("send initialize: %w", err)
	}
	if _, err := readResponse(reader, 1); err != nil {
		return nil, fmt.Errorf("read initialize: %w", err)
	}

	// Notifications carry no id and expect no response per JSON-RPC 2.0.
	if err := writeMessage(stdin, jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}); err != nil {
		return nil, fmt.Errorf("send initialized: %w", err)
	}

	if err := writeMessage(stdin, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
		Params:  map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}

	resp, err := readResponse(reader, 2)
	if err != nil {
		return nil, fmt.Errorf("read tools/list: %w", err)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode tools/list result: %w", err)
	}
	return result.Tools, nil
}

type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeMessage(w io.Writer, msg any) error {
	encoded, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// MCP stdio framing is one JSON message per line.
	if _, err := w.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

// readResponse pulls lines from r until one matches the given request id.
// Notifications and unrelated responses are tolerated and skipped so a
// server that emits log lines or progress notifications between handshake
// and result does not deadlock the scanner.
func readResponse(r *bufio.Reader, wantID int) (*jsonRPCResponse, error) {
	for {
		line, err := r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			if err != nil {
				return nil, err
			}
			continue
		}
		var resp jsonRPCResponse
		if jerr := json.Unmarshal([]byte(trimmed), &resp); jerr != nil {
			// Not a JSON-RPC frame — likely a stdout log from the server.
			// Skip; surface on timeout if no real response shows up.
			if err != nil {
				return nil, fmt.Errorf("no response for id %d: last line %q", wantID, trimmed)
			}
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("server error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		// Notifications have no id; skip them.
		if len(resp.ID) == 0 || string(resp.ID) == "null" {
			if err != nil {
				return nil, err
			}
			continue
		}
		var got int
		if jerr := json.Unmarshal(resp.ID, &got); jerr != nil || got != wantID {
			if err != nil {
				return nil, err
			}
			continue
		}
		return &resp, nil
	}
}

// drainStderr copies stderr into buf with a 4 KiB cap so a chatty server
// can't blow the heap. Any overflow is dropped silently — the first 4 KiB
// is plenty for the most useful error tail.
func drainStderr(r io.Reader, buf *strings.Builder) {
	const cap = 4 * 1024
	scratch := make([]byte, 1024)
	for {
		n, err := r.Read(scratch)
		if n > 0 {
			remaining := cap - buf.Len()
			if remaining > 0 {
				if n > remaining {
					n = remaining
				}
				buf.Write(scratch[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

// buildEnv assembles the environment slice for exec.Cmd. We deliberately do
// not inherit os.Environ — MCP servers should declare every variable they
// need so a daemon-side credential leak is unlikely.
func buildEnv(env map[string]string) []string {
	if len(env) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// joinExitErr produces a single error that carries the protocol failure,
// the wait-status (if useful), and any stderr tail.
func joinExitErr(scanErr, waitErr error, stderrTail string) error {
	parts := []string{scanErr.Error()}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		parts = append(parts, fmt.Sprintf("exit code %d", exitErr.ExitCode()))
	} else if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		parts = append(parts, waitErr.Error())
	}
	if tail := strings.TrimSpace(stderrTail); tail != "" {
		// Cap the surfaced tail so admin UI doesn't choke on a multi-page
		// stack trace.
		if len(tail) > 512 {
			tail = tail[:512] + "…"
		}
		parts = append(parts, "stderr: "+tail)
	}
	return errors.New(strings.Join(parts, ": "))
}
