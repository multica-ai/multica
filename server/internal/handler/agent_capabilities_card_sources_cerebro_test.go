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

type fakeTabler struct {
	rows []cerebrotoolpolicy.TableRow
	err  error
}

func (f fakeTabler) Table(context.Context, cerebrotoolpolicy.TableQuery) ([]cerebrotoolpolicy.TableRow, error) {
	return f.rows, f.err
}

type fakeConnLister struct {
	conns []cerebroconnections.Connection
	err   error
}

func (f fakeConnLister) ListEnabled(context.Context, pgtype.UUID) ([]cerebroconnections.Connection, error) {
	return f.conns, f.err
}

func TestAgentCapabilityTools_MapsVerdictsAndFiltersResourceRows(t *testing.T) {
	h := &Handler{CapabilityToolPolicy: fakeTabler{rows: []cerebrotoolpolicy.TableRow{
		{
			ToolKey:   "add_comment",
			Title:     "Add comment",
			Source:    "report",
			Effective: cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAllow, DecidedBy: cerebrotoolpolicy.LayerRuntime, Reason: "Allowed by runtime default"},
		},
		{
			ToolKey:        "create_local_runtime",
			Effective:      cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingDeny, DecidedBy: cerebrotoolpolicy.LayerGroup},
			CappedByGroups: []cerebrotoolpolicy.GroupAttribution{{Name: "builders", Owner: "Jesper"}},
		},
		{
			// Per-resource row must be filtered out — it is admin detail.
			ToolKey:         "repo.checkout",
			ResourcePattern: "github.com/firtal-group/firtal-cerebro",
			Effective:       cerebrotoolpolicy.Effective{Setting: cerebrotoolpolicy.SettingAsk},
		},
	}}}

	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.agentCapabilityTools(req, pgtype.UUID{}, pgtype.UUID{})

	if len(got) != 2 {
		t.Fatalf("expected 2 capability-wide tools (resource row filtered), got %d: %+v", len(got), got)
	}
	if got[0].Key != "add_comment" || got[0].Permission != "allow" || got[0].DecidedBy != "runtime" {
		t.Fatalf("unexpected first tool mapping: %+v", got[0])
	}
	if got[0].Reason != "Allowed by runtime default" {
		t.Fatalf("expected reason carried through, got %q", got[0].Reason)
	}
	if got[1].Permission != "deny" {
		t.Fatalf("expected deny on second tool, got %q", got[1].Permission)
	}
	if len(got[1].CappedByGroups) != 1 || got[1].CappedByGroups[0] != "builders (Jesper)" {
		t.Fatalf("expected capped-by-group label 'builders (Jesper)', got %v", got[1].CappedByGroups)
	}
}

func TestAgentCapabilityTools_NilSeamReturnsEmpty(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.agentCapabilityTools(req, pgtype.UUID{}, pgtype.UUID{})
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty (non-nil) slice when seam unset, got %v", got)
	}
}

func TestAgentCapabilityConnections_MapsEndpointsAndToolsAndDropsSecrets(t *testing.T) {
	h := &Handler{CapabilityConnections: fakeConnLister{conns: []cerebroconnections.Connection{
		{
			Name:        "bigquery",
			DisplayName: "BigQuery",
			Type:        "mcp_http",
			URL:         "https://bq-mcp.firtal.internal",
			Internal:    true,
			// Secret material that MUST NOT appear in the card output.
			AuthConfig: cerebroconnections.AuthConfig{BearerToken: "super-secret-token", CFAccessSecret: "cf-secret"},
			Tools:      []cerebroconnections.Tool{{Name: "bigquery.query", Description: "run a read query"}},
		},
		{
			Name: "registry",
			Type: "api",
			URL:  "https://registry.firtal.com",
			EndpointPermissions: []cerebroconnections.EndpointPermission{
				{Path: "/datasets", Methods: []string{"GET"}},
				{Path: "/datasets/{id}/labels", Methods: []string{"POST", "PUT"}},
			},
		},
	}}}

	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.agentCapabilityConnections(req, pgtype.UUID{})

	if len(got) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(got))
	}
	if got[0].Name != "bigquery" || got[0].Type != "mcp_http" || !got[0].Internal {
		t.Fatalf("unexpected mcp connection mapping: %+v", got[0])
	}
	if len(got[0].Tools) != 1 || got[0].Tools[0].Name != "bigquery.query" {
		t.Fatalf("expected mcp tool mapped, got %+v", got[0].Tools)
	}
	if len(got[1].Endpoints) != 2 || got[1].Endpoints[1].Path != "/datasets/{id}/labels" ||
		len(got[1].Endpoints[1].Methods) != 2 {
		t.Fatalf("expected REST endpoints mapped, got %+v", got[1].Endpoints)
	}

	// Hard guarantee: no auth secret may appear anywhere in the serialized card.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal card connections: %v", err)
	}
	blob := string(raw)
	for _, secret := range []string{"super-secret-token", "cf-secret"} {
		if strings.Contains(blob, secret) {
			t.Fatalf("connection auth secret leaked into card output: found %q in %s", secret, blob)
		}
	}
}

func TestAgentCapabilityConnections_NilSeamReturnsEmpty(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "/api/agents/x/capabilities", nil)
	got := h.agentCapabilityConnections(req, pgtype.UUID{})
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty (non-nil) slice when seam unset, got %v", got)
	}
}
