// Package cloudtoolscan implements the server-side "Scan now" path for cloud
// (firtal-gateway) runtimes (FIR-2284).
//
// Local daemon runtimes (the normal case — agents run on a local daemon) are
// scanned by pushing a tools/list request over the daemon websocket; the daemon
// spawns each MCP server and reports back, and the server bridges that result
// into the capability register (see persistScannedToolsToCapabilityRegister).
// The firtal gateway is the one cloud runtime: it runs inside the Multica server
// and never connects to the daemon websocket, so the daemon-push path returns
// "daemon offline" for it. This scanner is that runtime's "Scan now" path.
//
// This scanner runs entirely server-side. It enumerates the runtime's live
// built-in tool surface (the callable gateway tools, supplied by the caller)
// and records it into both inventories the admin UI reads from:
//
//   - cerebro_capability — the workspace capability register that the unified
//     FIR-2230 tool-policy table renders. This is the table that was empty for
//     cloud runtimes, because only daemons report capabilities.
//   - cerebro_runtime_tool — the legacy per-runtime tool list, refreshed with a
//     fresh last_scanned_at so the "Last scanned" label updates too.
//
// Capability keys are the bare tool name (e.g. "add_comment"), matching the key
// the gateway's policy enforcement resolves on (toolpolicy.Query.ToolKey =
// toolName), so an Allow/Ask/Deny an admin sets on a scanned row actually binds
// to the real call.
package cloudtoolscan

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	"github.com/multica-ai/multica/server/internal/util"
)

// ToolMeta is the static metadata for one callable built-in tool the scanner
// records. The caller supplies the list (from the gateway tool registry) so
// this package stays free of the runtime executor's import surface.
type ToolMeta struct {
	Name        string
	Description string
}

// capabilityReporter is the slice of *capabilityregistry.Service the scanner
// needs. Kept as an interface so tests can substitute a fake without a DB.
type capabilityReporter interface {
	Report(ctx context.Context, workspaceID pgtype.UUID, reporter capabilityregistry.Subject, caps []capabilityregistry.ReportInput) ([]capabilityregistry.View, error)
}

// toolUpserter is the slice of *runtimetools.Service the scanner needs.
type toolUpserter interface {
	UpsertTool(ctx context.Context, in runtimetools.UpsertToolInput) (runtimetools.Tool, error)
}

// Scanner records a cloud runtime's built-in tool surface into the capability
// register and the legacy runtime-tool inventory.
type Scanner struct {
	caps     capabilityReporter
	tools    toolUpserter
	builtins []ToolMeta
}

// New builds a Scanner. builtins is the callable built-in tool set the gateway
// exposes (cloud + Multica MCP tools that are actually implemented).
func New(caps capabilityReporter, tools toolUpserter, builtins []ToolMeta) *Scanner {
	return &Scanner{caps: caps, tools: tools, builtins: builtins}
}

// Scan records every built-in tool for the given cloud runtime. It is
// idempotent: re-running upserts the same rows and only bumps last_scanned_at /
// last_reported_at. Enabled flags are preserved — UpsertTool with a nil Enabled
// keeps any admin override, so a re-scan never silently re-enables a tool an
// admin turned off.
func (s *Scanner) Scan(ctx context.Context, runtimeID, workspaceID pgtype.UUID) error {
	if s == nil {
		return fmt.Errorf("cloud tool scan: nil scanner")
	}
	if !runtimeID.Valid || !workspaceID.Valid {
		return fmt.Errorf("cloud tool scan: runtime and workspace ids are required")
	}
	reporter := capabilityregistry.Subject{Type: "runtime", ID: util.UUIDToString(runtimeID)}

	// 1. Capability register — the source the unified tool-policy table reads.
	reports := make([]capabilityregistry.ReportInput, 0, len(s.builtins))
	for _, t := range s.builtins {
		if t.Name == "" {
			continue
		}
		reports = append(reports, capabilityregistry.ReportInput{
			Key:         t.Name,
			Title:       t.Name,
			Category:    "tools",
			Description: t.Description,
			Source:      "scan",
			Owners:      []capabilityregistry.Subject{reporter},
			Users:       []capabilityregistry.Subject{reporter},
		})
	}
	if len(reports) > 0 {
		if _, err := s.caps.Report(ctx, workspaceID, reporter, reports); err != nil {
			return fmt.Errorf("cloud tool scan: report capabilities: %w", err)
		}
	}

	// 2. Legacy per-runtime inventory — keep it in step and stamp last_scanned_at.
	scannedAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	for _, t := range s.builtins {
		if t.Name == "" {
			continue
		}
		if _, err := s.tools.UpsertTool(ctx, runtimetools.UpsertToolInput{
			RuntimeID:     runtimeID,
			ToolName:      t.Name,
			Source:        runtimetools.SourceCloud,
			Description:   t.Description,
			LastScannedAt: scannedAt,
		}); err != nil {
			return fmt.Errorf("cloud tool scan: upsert runtime tool %q: %w", t.Name, err)
		}
	}
	return nil
}
