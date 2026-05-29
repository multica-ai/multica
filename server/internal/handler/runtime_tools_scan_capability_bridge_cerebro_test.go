package handler

// CEREBRO-PATCH(runtime-tools-scan-capability-bridge-test): FIR-2284 — the
// daemon scan → capability register mapping that surfaces scanned MCP tools in
// the unified tool-policy table.

import "testing"

func TestScannedMCPToolCapabilityReports(t *testing.T) {
	reporter := CapabilitySubject{Type: "runtime", ID: "11111111-1111-1111-1111-111111111111"}
	servers := []RuntimeToolScanServer{
		{
			Name: "bigquery",
			Tools: []RuntimeToolScanTool{
				{Name: "query", Description: "Run a query."},
				{Name: "", Description: "blank — skipped"},
			},
		},
		{Name: "github", Tools: []RuntimeToolScanTool{{Name: "create_issue"}}},
		{Name: "broken", Error: "server offline", Tools: []RuntimeToolScanTool{{Name: "never"}}},
		{Name: "", Tools: []RuntimeToolScanTool{{Name: "no_server_name"}}},
	}

	got := scannedMCPToolCapabilityReports(servers, reporter)

	if len(got) != 2 {
		t.Fatalf("got %d reports, want 2 (blank tool, errored server, and blank server skipped): %#v", len(got), got)
	}

	first := got[0]
	if first.Key != "bigquery.query" {
		t.Errorf("key = %q, want namespaced server.tool %q", first.Key, "bigquery.query")
	}
	if first.Title != "query" {
		t.Errorf("title = %q, want bare tool name %q", first.Title, "query")
	}
	if first.Category != "bigquery" {
		t.Errorf("category = %q, want server name %q", first.Category, "bigquery")
	}
	if first.Source != "scan" {
		t.Errorf("source = %q, want %q", first.Source, "scan")
	}
	if first.Description != "Run a query." {
		t.Errorf("description = %q, want passthrough", first.Description)
	}
	if len(first.Owners) != 1 || first.Owners[0].ID != reporter.ID {
		t.Errorf("owners = %#v, want the reporting runtime", first.Owners)
	}
	if len(first.Users) != 1 || first.Users[0].ID != reporter.ID {
		t.Errorf("users = %#v, want the reporting runtime", first.Users)
	}

	if got[1].Key != "github.create_issue" {
		t.Errorf("second key = %q, want %q", got[1].Key, "github.create_issue")
	}
}

func TestScannedMCPToolCapabilityReportsEmpty(t *testing.T) {
	reporter := CapabilitySubject{Type: "runtime", ID: "11111111-1111-1111-1111-111111111111"}
	if got := scannedMCPToolCapabilityReports(nil, reporter); len(got) != 0 {
		t.Fatalf("nil servers should yield no reports, got %#v", got)
	}
	onlyErrors := []RuntimeToolScanServer{{Name: "x", Error: "boom", Tools: []RuntimeToolScanTool{{Name: "t"}}}}
	if got := scannedMCPToolCapabilityReports(onlyErrors, reporter); len(got) != 0 {
		t.Fatalf("errored servers should yield no reports, got %#v", got)
	}
}
