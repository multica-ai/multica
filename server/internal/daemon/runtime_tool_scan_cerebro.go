// CEREBRO-PATCH(daemon-runtime-tool-scan): JEH-1710 — periodic MCP tools/list
// scan that keeps the per-runtime tool registry in sync with whatever the
// admin has wired up in runtime.tools_config. Net-new fork file.
//
// The daemon does the spawning so the server stays platform-agnostic — it
// never runs untrusted MCP binaries itself. Each scan tick:
//
//  1. GET /api/daemon/runtimes/{id}/mcp-config — pulls the current tools_config.
//  2. For each declared mcpServers entry, spawn it and call tools/list.
//  3. POST /api/daemon/runtimes/{id}/tool-scan — reports per-server result
//     (tools or error string).
//
// Server-side, the receiving handler upserts the (runtime, server, tool)
// rows so the workspace-admin UI can render fresh inventory without admins
// having to type tool names by hand.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/mcpscan"
)

// runtimeToolScanInterval bounds how often a single runtime is scanned. We
// keep it conservative on purpose: every scan spawns subprocesses, and most
// admins change tools_config seconds at a time, not per minute. The admin
// UI gets a manual "rescan" trigger in a later PR; this loop is the
// background safety net.
const runtimeToolScanInterval = 15 * time.Minute

// scanToolsTimeout is the per-MCP-server budget. Servers slower than this
// look unhealthy to a human too, so the timeout is the right signal.
const scanToolsTimeout = 30 * time.Second

// runtimeToolScanState tracks the last-scan timestamp per runtime so the
// daemon's heartbeat loop can skip runtimes that were scanned recently. It
// is intentionally process-local: the server's last_scanned_at column is
// the durable record; this map only debounces work within one daemon
// process lifetime.
type runtimeToolScanState struct {
	mu       sync.Mutex
	lastScan map[string]time.Time
}

func newRuntimeToolScanState() *runtimeToolScanState {
	return &runtimeToolScanState{lastScan: make(map[string]time.Time)}
}

func (s *runtimeToolScanState) due(runtimeID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastScan[runtimeID]
	if !ok {
		return true
	}
	return now.Sub(last) >= runtimeToolScanInterval
}

func (s *runtimeToolScanState) mark(runtimeID string, when time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastScan[runtimeID] = when
}

// mcpServerEntry is the decoded shape of one entry in
// tools_config.mcpServers. Mirrors Claude Code's --mcp-config schema; extra
// fields (e.g. cwd, type) are tolerated and ignored.
type mcpServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// parseMcpConfig pulls the {name → entry} map out of a tools_config blob.
// Returns an empty map when the config has no mcpServers — the daemon treats
// that as "no MCP servers to scan", not an error.
func parseMcpConfig(raw json.RawMessage) (map[string]mcpServerEntry, error) {
	if len(raw) == 0 {
		return map[string]mcpServerEntry{}, nil
	}
	var doc struct {
		McpServers map[string]mcpServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse mcp config: %w", err)
	}
	if doc.McpServers == nil {
		return map[string]mcpServerEntry{}, nil
	}
	return doc.McpServers, nil
}

// scanRuntimeTools runs one scan cycle against a single runtime, then
// reports the result to the server. Failures are logged and reported as a
// per-server error string so the admin UI can show what broke without the
// daemon retrying in a hot loop.
func (d *Daemon) scanRuntimeTools(ctx context.Context, runtimeID string) {
	cfg, err := d.client.GetRuntimeMcpConfig(ctx, runtimeID)
	if err != nil {
		d.logger.Debug("runtime tool scan: fetch mcp-config failed",
			"runtime_id", runtimeID, "error", err)
		return
	}
	servers, err := parseMcpConfig(cfg)
	if err != nil {
		d.logger.Warn("runtime tool scan: invalid mcp-config",
			"runtime_id", runtimeID, "error", err)
		return
	}
	if len(servers) == 0 {
		// Still mark scanned so we don't churn the empty case every tick.
		d.toolScanState.mark(runtimeID, time.Now())
		return
	}

	scannedAt := time.Now().UTC()
	results := make([]RuntimeToolScanServer, 0, len(servers))
	for name, entry := range servers {
		spec := mcpscan.ServerSpec{
			Name:    name,
			Command: entry.Command,
			Args:    entry.Args,
			Env:     entry.Env,
		}
		tools, err := mcpscan.Scan(ctx, spec, scanToolsTimeout)
		if err != nil {
			d.logger.Info("runtime tool scan: server failed",
				"runtime_id", runtimeID, "server", name, "error", err)
			results = append(results, RuntimeToolScanServer{
				Name:  name,
				Error: err.Error(),
			})
			continue
		}
		wire := make([]RuntimeToolScanTool, 0, len(tools))
		for _, t := range tools {
			wire = append(wire, RuntimeToolScanTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
		results = append(results, RuntimeToolScanServer{Name: name, Tools: wire})
	}

	if err := d.client.ReportRuntimeToolScan(ctx, runtimeID, scannedAt, results); err != nil {
		d.logger.Warn("runtime tool scan: report failed",
			"runtime_id", runtimeID, "error", err)
		return
	}
	d.toolScanState.mark(runtimeID, time.Now())
	d.logger.Debug("runtime tool scan: reported",
		"runtime_id", runtimeID, "servers", len(results))
}

// maybeScanRuntimeTools is the heartbeat-loop hook. Called per runtime per
// heartbeat tick; the state's debounce keeps the actual work rate down to
// runtimeToolScanInterval.
func (d *Daemon) maybeScanRuntimeTools(ctx context.Context, runtimeID string) {
	if d.toolScanState == nil {
		return
	}
	if !d.toolScanState.due(runtimeID, time.Now()) {
		return
	}
	// Run in its own goroutine so a slow MCP server can't block the
	// heartbeat loop. We pass a derived context that survives the
	// heartbeat-tick context cancellation; the daemon's root context is
	// what should bound this work.
	go d.scanRuntimeTools(d.rootCtx, runtimeID)
}
