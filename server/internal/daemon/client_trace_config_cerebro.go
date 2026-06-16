// CEREBRO-PATCH(daemon-client-trace-config): TECH-3621 fleet rollout — daemon
// pulls the agent-trace upload config (ingest endpoint + key) from the server
// instead of reading it from local env. This is the fork-wide rollout path:
// one server-side setting turns trace upload on for every daemon, and the
// ingest key never sits in plaintext on a machine. Net-new fork file in the
// upstream daemon package; the HTTP call is the only thing that lives here.
package daemon

import (
	"context"
	"fmt"
)

// TraceConfigResponse mirrors handler.traceConfigResponse on the server. The
// json tags MUST stay in sync with that struct.
type TraceConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	Endpoint    string `json:"endpoint"`
	APIKey      string `json:"api_key"`
	MaxAgeHours int    `json:"max_age_hours"`
}

// FetchTraceConfig asks the server for the fleet trace-upload configuration.
// The endpoint is runtime-scoped + daemon-auth-scoped (requireDaemonRuntimeAccess),
// so the server only answers a daemon that owns the runtime. A nil error with
// Enabled=false means the server has the feature off; callers do nothing then.
func (c *Client) FetchTraceConfig(ctx context.Context, runtimeID string) (*TraceConfigResponse, error) {
	var resp TraceConfigResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/trace-config", runtimeID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
