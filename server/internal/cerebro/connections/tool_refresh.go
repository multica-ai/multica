package connections

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

// Tool-list refresh (FIR-4187).
//
// The permissions table enumerates one row per tool from the `tools` column on
// workspace_connection (see toolpolicy.appendConnectionToolRows). A tool that is
// not in that cached list gets no row, and toolpolicy.ConnectionToolEffective
// resolves an unmatched tool to SettingAllow — so a tool the server exposes but
// the cache has not seen is silently ungated, no matter what rules an admin
// writes. Keeping the cache current is therefore a permission-correctness
// requirement, not a cosmetic refresh.
//
// Until now the cache was only ever written by the Test handler, i.e. when an
// admin pressed the button in the connection editor. A server that grew new
// tools after the last button press stayed gated on its old list indefinitely.

// refreshTimeout bounds a single probe. doTestConnection already applies
// testTimeout per request; this is the outer ceiling for the whole refresh.
const refreshTimeout = 30 * time.Second

// DefaultRefreshInterval runs the sweep nightly. Tool lists change at deploy
// cadence, so anything tighter is wasted probing.
const DefaultRefreshInterval = 24 * time.Hour

// RefreshTools re-probes a saved mcp_http connection and persists the tool list
// it reports. It is the shared implementation behind all three refresh
// triggers: the Test button, connection create/update, and the nightly sweeper.
//
// Best-effort by design, mirroring the Test handler: an unreachable server or
// an empty tool list leaves the previous list untouched rather than blanking
// the permissions table. Blanking would be the dangerous failure — every tool
// would lose its row and fall back to ungated.
func RefreshTools(ctx context.Context, store *Store, c Connection) (int, error) {
	if c.Type != TypeMCPHTTP {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	result := doTestConnection(ctx, testConnectionRequest{
		URL:        c.URL,
		Type:       c.Type,
		AuthConfig: c.AuthConfig,
	})
	if !result.Reachable {
		return 0, fmt.Errorf("connections: refresh %s: unreachable: %s", c.Name, result.Error)
	}
	if len(result.Tools) == 0 {
		return 0, fmt.Errorf("connections: refresh %s: server reported no tools", c.Name)
	}

	id, err := util.ParseUUID(c.ID)
	if err != nil {
		return 0, fmt.Errorf("connections: refresh %s: bad connection id: %w", c.Name, err)
	}
	wsID, err := util.ParseUUID(c.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("connections: refresh %s: bad workspace id: %w", c.Name, err)
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, t := range result.Tools {
		tools = append(tools, Tool{Name: t.Name, Description: t.Description})
	}
	if err := store.UpdateTools(ctx, id, wsID, tools); err != nil {
		return 0, err
	}
	return len(tools), nil
}

// refreshInBackground re-probes a connection off the request goroutine. Create
// and Update call this so saving a connection also refreshes its tool list;
// a probe costs up to refreshTimeout, which is far too slow to hold an HTTP
// response open for.
func refreshInBackground(store *Store, c Connection) {
	go func() {
		n, err := RefreshTools(context.Background(), store, c)
		if err != nil {
			slog.Warn("connection tool refresh failed", "connection", c.Name, "error", err)
			return
		}
		slog.Info("connection tool refresh", "connection", c.Name, "tools", n)
	}()
}

// ToolRefreshSweeper keeps every enabled mcp_http connection's cached tool list
// current, so a server that gains a tool between admin visits cannot expose it
// ungated for longer than one night.
type ToolRefreshSweeper struct {
	store *Store
}

func NewToolRefreshSweeper(store *Store) *ToolRefreshSweeper {
	return &ToolRefreshSweeper{store: store}
}

// Run sweeps once at startup and then on every tick. The startup sweep matters:
// it closes the gap for a server whose tools changed while cerebro was down.
func (s *ToolRefreshSweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	if err := s.SweepOnce(ctx); err != nil {
		slog.Warn("connection tool refresh sweep failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SweepOnce(ctx); err != nil {
				slog.Warn("connection tool refresh sweep failed", "error", err)
			}
		}
	}
}

// SweepOnce refreshes every enabled mcp_http connection across all workspaces.
// Probes run sequentially: this is a nightly job over a handful of connections,
// and serial probing keeps the load off the MCP servers it is polling.
func (s *ToolRefreshSweeper) SweepOnce(ctx context.Context) error {
	conns, err := s.store.ListAllEnabledMCP(ctx)
	if err != nil {
		return err
	}
	for _, c := range conns {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		before := len(c.Tools)
		n, err := RefreshTools(ctx, s.store, c)
		if err != nil {
			slog.Warn("connection tool refresh failed", "connection", c.Name, "error", err)
			continue
		}
		if n != before {
			slog.Info("connection tool list changed", "connection", c.Name, "before", before, "after", n)
		}
	}
	return nil
}
