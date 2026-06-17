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
	}

	tools, repos, connPerms := classifyCapabilityRows(rows)

	if len(tools) != 1 || tools[0].Key != "add_comment" || tools[0].Permission != "allow" {
		t.Fatalf("expected only the general tool, got %+v", tools)
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
	// connection-wide + endpoint rows must NOT leak into tools.
	for _, tl := range tools {
		if strings.HasPrefix(tl.Key, "connection:") {
			t.Fatalf("connection row leaked into tools: %+v", tl)
		}
	}
}

func TestAgentCapabilityConnections_AllConnsToolsStampedSecretsDropped(t *testing.T) {
	h := &Handler{CapabilityConnections: fakeConnLister{conns: []cerebroconnections.Connection{
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
	}}}

	connPerms := map[string]map[string]string{"bigquery": {"bigquery.insert": "deny"}}
	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.agentCapabilityConnections(req, pgtype.UUID{}, connPerms)

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

func TestAgentCapabilityConnections_NilSeamReturnsEmpty(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.agentCapabilityConnections(req, pgtype.UUID{}, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty (non-nil) slice when seam unset, got %v", got)
	}
}
