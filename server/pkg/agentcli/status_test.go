package agentcli

import (
	"encoding/json"
	"testing"
)

func TestAggregateStatus(t *testing.T) {
	cases := []struct {
		name       string
		current    string
		latest     string
		status     string
		upgradable bool
	}{
		{name: "no current is missing even when latest exists", current: "", latest: "2.0.0", status: StatusMissing, upgradable: false},
		{name: "whitespace current is missing", current: "  ", latest: "2.0.0", status: StatusMissing, upgradable: false},
		{name: "current without latest is unknown", current: "1.2.3", latest: "", status: StatusUnknown, upgradable: false},
		{name: "current older than latest is outdated and upgradable", current: "1.0.0", latest: "1.1.0", status: StatusOutdated, upgradable: true},
		{name: "equal versions are up to date", current: "1.2.3", latest: "1.2.3", status: StatusUpToDate, upgradable: false},
		{name: "v prefix does not make equal versions outdated", current: "v1.2.3", latest: "1.2.3", status: StatusUpToDate, upgradable: false},
		{name: "current newer than latest is up to date and not upgradable", current: "1.3.0", latest: "1.2.9", status: StatusUpToDate, upgradable: false},
		{name: "unparsable current with a latest is unknown", current: "not-a-version", latest: "1.0.0", status: StatusUnknown, upgradable: false},
		{name: "unparsable latest is unknown", current: "1.0.0", latest: "latest", status: StatusUnknown, upgradable: false},
		{name: "numeric compare is not lexical", current: "1.9.0", latest: "1.10.0", status: StatusOutdated, upgradable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, upgradable := Aggregate(tc.current, tc.latest)
			if status != tc.status || upgradable != tc.upgradable {
				t.Fatalf("Aggregate(%q, %q) = %s upgradable=%v, want %s upgradable=%v", tc.current, tc.latest, status, upgradable, tc.status, tc.upgradable)
			}
		})
	}
}

func TestBuildAlwaysReportsTheThreeCLIs(t *testing.T) {
	probes := []Probe{
		{CLI: "codex", Current: "1.0.0", Path: "/usr/local/bin/codex", Channel: ChannelNPM, ProbedAt: "2026-11-02T00:00:00Z"},
		{CLI: "grok", Current: "1.0.14", Path: "/home/user/.grok/bin/grok", Channel: ChannelBinary, ProbedAt: "2026-11-02T00:00:00Z"},
		{CLI: "ignored", Current: "9.9.9"},
	}
	latest := func(cli string) (string, bool) {
		switch cli {
		case CLICodex:
			return "1.2.0", true
		case CLIClaude:
			return "2.0.0", true
		default:
			// No npm package: the lookup reports no published version.
			return "", false
		}
	}
	got := Build(probes, latest)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	if got[0].CLI != CLICodex || got[0].Status != StatusOutdated || !got[0].Upgradable || got[0].Latest != "1.2.0" || got[0].Path != "/usr/local/bin/codex" {
		t.Fatalf("codex = %+v", got[0])
	}
	if got[1].CLI != CLIClaude || got[1].Current != "" || got[1].Status != StatusMissing || got[1].Upgradable || got[1].Latest != "2.0.0" {
		t.Fatalf("claude = %+v", got[1])
	}
	if got[2].CLI != CLIGrok || got[2].Status != StatusUnknown || got[2].Upgradable || got[2].Latest != "" || got[2].Channel != ChannelBinary {
		t.Fatalf("grok = %+v", got[2])
	}
}

func TestBuildWithoutPackageIsNotUpgradable(t *testing.T) {
	got := Build([]Probe{{CLI: CLIGrok, Current: "1.0.0"}}, func(cli string) (string, bool) {
		if cli == CLIGrok {
			return "2.0.0", true
		}
		return "", false
	})
	for _, row := range got {
		if row.CLI != CLIGrok {
			continue
		}
		if row.Status != StatusOutdated || row.Upgradable || row.Latest != "2.0.0" {
			t.Fatalf("row = %+v, want outdated and not upgradable", row)
		}
		return
	}
	t.Fatal("missing grok row")
}

func TestProbesFromMetadataLeavesCLIVersionAlone(t *testing.T) {
	raw := []byte(`{
		"version": "1.2.3",
		"cli_version": "v0.7.16",
		"launched_by": "desktop",
		"agent_cli_probes": [
			{"cli":"claude","current":"2.1.0","path":"/opt/homebrew/bin/claude","channel":"brew","probed_at":"2026-11-02T00:00:00Z"}
		]
	}`)
	probes := ProbesFromMetadata(raw)
	if len(probes) != 1 || probes[0].CLI != CLIClaude || probes[0].Current != "2.1.0" || probes[0].Channel != ChannelBrew {
		t.Fatalf("probes = %+v", probes)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["cli_version"] != "v0.7.16" {
		t.Fatalf("cli_version = %#v, want the daemon version left untouched", meta["cli_version"])
	}
	if ProbesFromMetadata([]byte(`{"cli_version":"v0.7.16"}`)) != nil {
		t.Fatal("metadata without probes should not invent a current version")
	}
}

func TestNormalizeProbesDropsUnknownAndCanonicalizes(t *testing.T) {
	got := NormalizeProbes([]Probe{
		{CLI: " GROK ", Current: " 1.0.0 ", Channel: "BINARY"},
		{CLI: "copilot", Current: "1.0.0"},
		{CLI: "codex", Current: "0.1.0", Channel: "nope"},
	})
	if len(got) != 2 || got[0].CLI != CLICodex || got[0].Channel != ChannelUnknown || got[1].CLI != CLIGrok || got[1].Channel != ChannelBinary || got[1].Current != "1.0.0" {
		t.Fatalf("normalized = %+v", got)
	}
}

func TestMetadataWithProbesPreservesCLIVersion(t *testing.T) {
	existing := []byte(`{"cli_version":"v0.7.16","version":"1.0.0"}`)
	out := MetadataWithProbes(existing, []Probe{
		{CLI: "codex", Current: "0.1.0", Channel: ChannelNPM, ProbedAt: "2026-11-02T00:00:00Z"},
	})
	var meta map[string]any
	if err := json.Unmarshal(out, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["cli_version"] != "v0.7.16" {
		t.Fatalf("cli_version lost: %#v", meta)
	}
	probes := ProbesFromMetadata(out)
	if len(probes) != 1 || probes[0].CLI != CLICodex || probes[0].Current != "0.1.0" {
		t.Fatalf("probes = %+v", probes)
	}
}
