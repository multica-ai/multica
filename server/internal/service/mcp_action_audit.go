package service

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	mcpAuditRetentionDefault = 30
	mcpAuditRetentionMin     = 1
	mcpAuditRetentionMax     = 365
	mcpAuditSweepEvery       = time.Hour
)

// brokerMCPServer matches names minted by remoteMCPServerName: a plugin-
// prefix plus a 16-hex digest of the contribution id. Those calls are audited
// at the broker, so the transcript writer skips them.
var brokerMCPServer = regexp.MustCompile(`^plugin-.+-[0-9a-f]{16}$`)

var transcriptDuration = regexp.MustCompile(`(?m)^duration:\s+(\d+)\s+ms\s*$`)

// ClampMCPAuditRetentionDays bounds a configured window to 1–365 days.
func ClampMCPAuditRetentionDays(n int) int {
	if n < mcpAuditRetentionMin {
		return mcpAuditRetentionMin
	}
	if n > mcpAuditRetentionMax {
		return mcpAuditRetentionMax
	}
	return n
}

// DefaultMCPAuditRetentionDays is the sweep fallback: MULTICA_MCP_AUDIT_RETENTION_DAYS, else 30.
// A workspace settings value overrides this at delete time, so changing the
// workspace key applies on the next sweep without a process restart.
func DefaultMCPAuditRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("MULTICA_MCP_AUDIT_RETENTION_DAYS"))
	if raw == "" {
		return mcpAuditRetentionDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return mcpAuditRetentionDefault
	}
	return ClampMCPAuditRetentionDays(n)
}

// SkipProviderStreamMCPServer reports servers whose calls are recorded on
// another writer. multica-plugins is the local plugin-hook MCP server.
// Broker-minted names are audited in the proxy.
func SkipProviderStreamMCPServer(server string) bool {
	if server == "" || server == "multica-plugins" {
		return true
	}
	return brokerMCPServer.MatchString(server)
}

// TranscriptMCPOutcome reads the summary Codex stores for an MCP tool result:
// a status word, an optional duration line, and an optional sanitized error.
// It does not keep result content; that summary is the whole outcome.
func TranscriptMCPOutcome(output string) (outcome string, durationMs *int32, errText string) {
	outcome = "ok"
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "failed") || strings.Contains(output, "\nerror:") {
		outcome = "error"
	}
	if m := transcriptDuration.FindStringSubmatch(output); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 0 {
			v := int32(n)
			durationMs = &v
		}
	}
	if i := strings.Index(output, "\nerror:"); i >= 0 {
		errText = strings.TrimSpace(output[i+len("\nerror:"):])
		if nl := strings.IndexByte(errText, '\n'); nl >= 0 {
			errText = strings.TrimSpace(errText[:nl])
		}
	}
	return outcome, durationMs, errText
}

// StartMCPActionAuditSweep deletes expired audit rows once an hour.
// The first pass waits for the first tick, matching the plugin invocation sweep:
// a process that is still starting should not touch the database immediately.
func StartMCPActionAuditSweep(queries *db.Queries) {
	if queries == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(mcpAuditSweepEvery)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			removed, err := queries.DeleteExpiredMCPActionAudits(ctx, int32(DefaultMCPAuditRetentionDays()))
			cancel()
			if err != nil {
				slog.Warn("mcp action audit sweep failed", "error", err)
				continue
			}
			if removed > 0 {
				slog.Info("mcp action audit sweep removed rows", "removed", removed)
			}
		}
	}()
}
