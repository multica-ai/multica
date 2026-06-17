package handler

// CEREBRO-PATCH(agent-capabilities-card-sources-test): TECH-3642 unit tests for
// the tool-policy and connections mapping the capabilities card performs,
// including the hard guarantee that connection auth secrets never leave the
// handler.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebroconnections "github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// CEREBRO-PATCH(agent-capabilities-card-sections-test): TECH-3642 cover the
// row-classification split (tools/repos/connection verdicts) and secret-drop.
type fakeConnLister struct {
	conns []cerebroconnections.Connection
	err   error
}

func (f fakeConnLister) List(context.Context, pgtype.UUID) ([]cerebroconnections.Connection, error) {
	return f.conns, f.err
}

func eff(s cerebrotoolpolicy.Setting, by cerebrotoolpolicy.Layer) cerebrotoolpolicy.Effective {
	return cerebrotoolpolicy.Effective{Setting: s, DecidedBy: by}
}

func TestClassifyCapabilityRows_SplitsToolsReposAndConnVerdicts(t *testing.T) {
	rows := []cerebrotoolpolicy.TableRow{
		// general tool
		{ToolKey: "add_comment", Title: "Add comment", Source: "report", Effective: eff(cerebrotoolpolicy.SettingAllow, cerebrotoolpolicy.LayerRuntime)},
		// repo: three permission rows for one repo URL
		{ToolKey: "repo.read", Title: "Read code", Source: "repo", ResourcePattern: "github.com/firtal-group/firtal-cerebro", Effective: eff(cerebrotoolpolicy.SettingAllow, cerebrotoolpolicy.LayerWorkspace)},
		{ToolKey: "repo.checkout", Title: "Check out", Source: "repo", ResourcePattern: "github.com/firtal-group/firtal-cerebro", Effective: eff(cerebrotoolpolicy.SettingAsk, cerebrotoolpolicy.LayerAgent)},
		{ToolKey: "repo.push", Title: "Push changes", Source: "repo", ResourcePattern: "github.com/firtal-group/firtal-cerebro", Effective: eff(cerebrotoolpolicy.SettingDeny, cerebrotoolpolicy.LayerAgent)},
		// connection-tool verdict
		{ToolKey: "connection:bigquery", Source: "connection-tool", ResourcePattern: "bigquery.insert", Effective: eff(cerebrotoolpolicy.SettingDeny, cerebrotoolpolicy.LayerAgent)},
		// connection-wide + endpoint rows are rendered structurally — must be skipped
		{ToolKey: "connection:bigquery", Source: "connection", ResourcePattern: "", Effective: eff(cerebrotoolpolicy.SettingAllow, cerebrotoolpolicy.LayerWorkspace)},
		{ToolKey: "connection:registry", Source: "connection-endpoint", ResourcePattern: "/datasets", Effective: eff(cerebrotoolpolicy.SettingAllow, cerebrotoolpolicy.LayerWorkspace)},
		// scanned MCP tool that belongs to the bigquery connection (Category =
		// connection name) — must be grouped under the connection, not flat.
		{ToolKey: "bigquery.query", Title: "query", Category: "bigquery", Source: "scan", Effective: eff(cerebrotoolpolicy.SettingAllow, cerebrotoolpolicy.LayerWorkspace)},
		// scanned MCP tool from a server that is NOT a workspace connection —
		// must stay in the flat list.
		{ToolKey: "local.echo", Title: "echo", Category: "local", Source: "scan", Effective: eff(cerebrotoolpolicy.SettingAllow, cerebrotoolpolicy.LayerWorkspace)},
	}

	tools, repos, connPerms, connTools := classifyCapabilityRows(rows, map[string]bool{"bigquery": true})

	// The general tool and the non-connection scanned tool stay flat; the
	// connection-owned scanned tool does not.
	if len(tools) != 2 {
		t.Fatalf("expected the general tool + non-connection scanned tool, got %+v", tools)
	}
	for _, tl := range tools {
		if tl.Key == "bigquery.query" {
			t.Fatalf("connection-owned scanned tool leaked into flat tools: %+v", tl)
		}
		if strings.HasPrefix(tl.Key, "connection:") {
			t.Fatalf("connection row leaked into tools: %+v", tl)
		}
	}
	if len(repos) != 1 || repos[0].URL != "github.com/firtal-group/firtal-cerebro" {
		t.Fatalf("expected one repo group, got %+v", repos)
	}
	if len(repos[0].Permissions) != 3 || repos[0].Permissions[2].Permission != "deny" {
		t.Fatalf("expected repo to carry read/checkout/push verdicts, got %+v", repos[0].Permissions)
	}
	if connPerms["bigquery"]["bigquery.insert"] != "deny" {
		t.Fatalf("expected connection-tool verdict mapped, got %v", connPerms)
	}
	// The scanned tool is grouped under its connection.
	if len(connTools["bigquery"]) != 1 || connTools["bigquery"][0].Title != "query" {
		t.Fatalf("expected the scanned tool grouped under bigquery, got %+v", connTools)
	}
}

func TestAgentCapabilityConnections_FallbackToPersistedToolsStampedSecretsDropped(t *testing.T) {
	// No scanned rows for these connections — fall back to the persisted
	// tools/list a "Test connection" populated, stamped from connPerms.
	conns := []cerebroconnections.Connection{
		{
			Name: "bigquery", DisplayName: "BigQuery", Type: "mcp_http",
			URL: "https://bq-mcp.firtal.internal", Internal: true, Enabled: true,
			AuthConfig: cerebroconnections.AuthConfig{BearerToken: "super-secret-token", CFAccessSecret: "cf-secret"},
			Tools:      []cerebroconnections.Tool{{Name: "bigquery.query"}, {Name: "bigquery.insert"}},
		},
		{
			Name: "registry", Type: "api", URL: "https://registry.firtal.com", Enabled: false,
			EndpointPermissions: []cerebroconnections.EndpointPermission{{Path: "/datasets", Methods: []string{"GET"}}},
		},
	}

	connPerms := map[string]map[string]string{"bigquery": {"bigquery.insert": "deny"}}
	got := buildAgentCapabilityConnections(conns, connPerms, nil)

	if len(got) != 2 {
		t.Fatalf("expected ALL connections (enabled + disabled), got %d", len(got))
	}
	if got[1].Enabled {
		t.Fatalf("expected the disabled connection to report enabled=false")
	}
	// Per-tool permission stamped from connPerms.
	var insert AgentCapabilityConnTool
	for _, tl := range got[0].Tools {
		if tl.Name == "bigquery.insert" {
			insert = tl
		}
	}
	if insert.Permission != "deny" {
		t.Fatalf("expected bigquery.insert stamped deny, got %q", insert.Permission)
	}

	// Hard guarantee: no auth secret may appear anywhere in the serialized card.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	blob := string(raw)
	for _, secret := range []string{"super-secret-token", "cf-secret"} {
		if strings.Contains(blob, secret) {
			t.Fatalf("connection auth secret leaked: found %q in %s", secret, blob)
		}
	}
}

// CEREBRO-PATCH(agent-capabilities-card-connection-nesting): TECH-3642 cover the
// scanned MCP tools being grouped under their connection (not the flat list).
func TestAgentCapabilityConnections_NestsScannedToolsUnderConnection(t *testing.T) {
	// The connection's persisted tools/list is empty (no manual test ever ran);
	// the live scanned inventory (connTools) supplies the tools instead.
	conns := []cerebroconnections.Connection{
		{Name: "customer-service", DisplayName: "Customer Service", Type: "mcp_http", Enabled: true},
	}
	connTools := map[string][]AgentCapabilityTool{
		"customer-service": {
			{Key: "customer-service.dixa_get_conversation", Title: "dixa_get_conversation", Permission: "allow"},
			{Key: "customer-service.draft_reply", Title: "draft_reply", Permission: "ask"},
		},
	}

	got := buildAgentCapabilityConnections(conns, nil, connTools)

	if len(got) != 1 || len(got[0].Tools) != 2 {
		t.Fatalf("expected the connection to nest its 2 scanned tools, got %+v", got)
	}
	if got[0].Tools[0].Name != "dixa_get_conversation" || got[0].Tools[0].Permission != "allow" {
		t.Fatalf("expected bare tool name + permission from the scan, got %+v", got[0].Tools[0])
	}
	if got[0].Tools[1].Permission != "ask" {
		t.Fatalf("expected draft_reply stamped ask, got %+v", got[0].Tools[1])
	}
}

func TestBuildAgentCapabilityConnections_NilConnsReturnsEmpty(t *testing.T) {
	got := buildAgentCapabilityConnections(nil, nil, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty (non-nil) slice for no connections, got %v", got)
	}
}

func TestListCapabilityConnections_NilSeamReturnsNil(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	if got := h.listCapabilityConnections(req, pgtype.UUID{}); got != nil {
		t.Fatalf("expected nil when seam unset, got %v", got)
	}
}

func TestListCapabilityConnections_ReturnsSeamConnections(t *testing.T) {
	h := &Handler{CapabilityConnections: fakeConnLister{conns: []cerebroconnections.Connection{
		{Name: "customer-service", Type: "mcp_http", Enabled: true},
	}}}
	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.listCapabilityConnections(req, pgtype.UUID{})
	if len(got) != 1 || got[0].Name != "customer-service" {
		t.Fatalf("expected the seam's connection, got %+v", got)
	}
}
