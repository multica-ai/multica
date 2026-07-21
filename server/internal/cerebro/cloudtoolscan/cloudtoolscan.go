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
// and records it into the canonical capability register:
//
//   - cerebro_capability — the workspace capability register that the unified
//     FIR-2230 tool-policy table renders. This is the table that was empty for
//     cloud runtimes, because only daemons report capabilities.
//
// Capability keys are the bare tool name (e.g. "add_comment"), matching the key
// the gateway's policy enforcement resolves on (toolpolicy.Query.ToolKey =
// toolName), so an Allow/Ask/Deny an admin sets on a scanned row actually binds
// to the real call.
package cloudtoolscan

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
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

// Scanner records a cloud runtime's built-in tool surface into the capability
// register.
type Scanner struct {
	caps     capabilityReporter
	builtins []ToolMeta
}

// New builds a Scanner. builtins is the callable built-in tool set the gateway
// exposes (cloud + Multica MCP tools that are actually implemented).
func New(caps capabilityReporter, builtins []ToolMeta) *Scanner {
	return &Scanner{caps: caps, builtins: builtins}
}

// Scan records every built-in tool for the given cloud runtime. It is
// idempotent: re-running upserts the same rows and only bumps last_reported_at.
func (s *Scanner) Scan(ctx context.Context, runtimeID, workspaceID pgtype.UUID) error {
	if s == nil {
		return fmt.Errorf("cloud tool scan: nil scanner")
	}
	if !runtimeID.Valid || !workspaceID.Valid {
		return fmt.Errorf("cloud tool scan: runtime and workspace ids are required")
	}
	reporter := capabilityregistry.Subject{Type: "runtime", ID: util.UUIDToString(runtimeID)}

	// Capability register — the canonical inventory read by the unified
	// tool-policy table. Runtime availability is evaluated separately from
	// availability evidence and never from a mutable inventory toggle.
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
	return nil
}
