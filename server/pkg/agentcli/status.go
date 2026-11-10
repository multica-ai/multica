// Package agentcli reports whether the codex, claude, and grok CLIs on a
// daemon host are missing, current, or behind the published release.
//
// This is the agent CLI (codex / claude / grok), not the Multica daemon's own
// cli_version. The daemon version stays in runtime metadata under cli_version
// and is not an input to this package.
package agentcli

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// ManagedCLIs is the fixed set reported by cli-status, in response order.
var ManagedCLIs = []string{CLICodex, CLIClaude, CLIGrok}

// managedCLIPathEnv pins an already-installed binary. It does not decide
// whether the CLI can be installed; that is npmPackages.
var managedCLIPathEnv = map[string]string{
	CLICodex:  "MULTICA_CODEX_PATH",
	CLIClaude: "MULTICA_CLAUDE_PATH",
	CLIGrok:   "MULTICA_GROK_PATH",
}

// PathEnv is the operator pin for an already-installed binary. An empty
// result means the CLI has no pin.
func PathEnv(cli string) string {
	return managedCLIPathEnv[strings.ToLower(strings.TrimSpace(cli))]
}

const (
	CLICodex  = "codex"
	CLIClaude = "claude"
	CLIGrok   = "grok"

	ChannelNPM     = "npm"
	ChannelBrew    = "brew"
	ChannelBinary  = "binary"
	ChannelUnknown = "unknown"

	StatusUpToDate = "up_to_date"
	StatusOutdated = "outdated"
	StatusMissing  = "missing"
	StatusUnknown  = "unknown"

	// MetadataKey is the runtime metadata field that carries Probe values.
	// It sits beside cli_version and does not replace it.
	MetadataKey = "agent_cli_probes"
)

// Probe is one daemon observation of an agent CLI.
type Probe struct {
	CLI      string `json:"cli"`
	Current  string `json:"current,omitempty"`
	Path     string `json:"path,omitempty"`
	Channel  string `json:"channel,omitempty"`
	ProbedAt string `json:"probed_at,omitempty"`
}

// CLIStatus is one row of GET /api/runtimes/{id}/cli-status.
type CLIStatus struct {
	CLI        string `json:"cli"`
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Status     string `json:"status"`
	Upgradable bool   `json:"upgradable"`
	Path       string `json:"path,omitempty"`
	Channel    string `json:"channel,omitempty"`
	ProbedAt   string `json:"probed_at,omitempty"`
}

// IsManaged reports whether cli is one of codex, claude, or grok.
func IsManaged(cli string) bool {
	switch strings.ToLower(strings.TrimSpace(cli)) {
	case CLICodex, CLIClaude, CLIGrok:
		return true
	default:
		return false
	}
}

// NormalizeChannel maps a reported install channel onto the four allowed
// values. Anything else, including empty, is unknown.
func NormalizeChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case ChannelNPM:
		return ChannelNPM
	case ChannelBrew:
		return ChannelBrew
	case ChannelBinary:
		return ChannelBinary
	case ChannelUnknown:
		return ChannelUnknown
	default:
		return ChannelUnknown
	}
}

// NormalizeProbes keeps at most one probe per managed CLI, in ManagedCLIs
// order. Unknown CLIs are dropped. Empty current is preserved: that is how a
// probe says the CLI is not installed.
func NormalizeProbes(in []Probe) []Probe {
	if len(in) == 0 {
		return nil
	}
	by := make(map[string]Probe, len(ManagedCLIs))
	for _, p := range in {
		cli := strings.ToLower(strings.TrimSpace(p.CLI))
		if !IsManaged(cli) {
			continue
		}
		p.CLI = cli
		p.Current = trimCap(p.Current, 128)
		p.Path = trimCap(p.Path, 4096)
		p.Channel = NormalizeChannel(p.Channel)
		p.ProbedAt = trimCap(p.ProbedAt, 64)
		by[cli] = p
	}
	if len(by) == 0 {
		return nil
	}
	out := make([]Probe, 0, len(by))
	for _, cli := range ManagedCLIs {
		if p, ok := by[cli]; ok {
			out = append(out, p)
		}
	}
	return out
}

func trimCap(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return s[:max]
	}
	return s
}

// Aggregate decides status from the probed version and the published latest.
//
//   - no current → missing, not upgradable
//   - current set, latest missing or unparsable → unknown, not upgradable
//   - current < latest → outdated, upgradable
//   - current == latest, or current > latest → up_to_date, not upgradable
//
// Upgradable is true only for outdated. A newer-than-published build is not
// something this package offers to upgrade.
func Aggregate(current, latest string) (status string, upgradable bool) {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	if current == "" {
		return StatusMissing, false
	}
	if latest == "" {
		return StatusUnknown, false
	}
	cmp, err := agent.CompareVersions(current, latest)
	if err != nil {
		return StatusUnknown, false
	}
	if cmp < 0 {
		return StatusOutdated, true
	}
	return StatusUpToDate, false
}

// Build returns one CLIStatus per managed CLI. latest reports the published
// version; ok false (or a blank version) leaves latest empty. A CLI with no
// probe is treated as having no current version.
func Build(probes []Probe, latest func(cli string) (version string, ok bool)) []CLIStatus {
	by := map[string]Probe{}
	for _, p := range NormalizeProbes(probes) {
		by[p.CLI] = p
	}
	out := make([]CLIStatus, 0, len(ManagedCLIs))
	for _, cli := range ManagedCLIs {
		p := by[cli]
		var latestVer string
		if latest != nil {
			if v, ok := latest(cli); ok {
				latestVer = strings.TrimSpace(v)
			}
		}
		status, upgradable := Aggregate(p.Current, latestVer)
		if _, installable := NPMPackage(cli); !installable {
			upgradable = false
		}
		channel := p.Channel
		if channel == "" {
			channel = ChannelUnknown
		}
		out = append(out, CLIStatus{
			CLI:        cli,
			Current:    p.Current,
			Latest:     latestVer,
			Status:     status,
			Upgradable: upgradable,
			Path:       p.Path,
			Channel:    channel,
			ProbedAt:   p.ProbedAt,
		})
	}
	return out
}

// ProbesFromMetadata reads MetadataKey from an agent_runtime.metadata
// document. A missing key or malformed document yields nil, which Build
// treats as "no current version" for every CLI.
func ProbesFromMetadata(raw []byte) []Probe {
	if len(raw) == 0 {
		return nil
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	body, ok := meta[MetadataKey]
	if !ok || len(body) == 0 {
		return nil
	}
	var probes []Probe
	if err := json.Unmarshal(body, &probes); err != nil {
		return nil
	}
	return NormalizeProbes(probes)
}

// MetadataWithProbes returns a copy of existing runtime metadata JSON with
// MetadataKey set to the normalized probes. Other keys, including
// cli_version, are preserved. An empty probe list clears the key so a
// deliberate "all missing" snapshot can replace a stale one; callers that
// want to leave prior probes alone must not call this with an empty list.
func MetadataWithProbes(existing []byte, probes []Probe) []byte {
	meta := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &meta); err != nil {
			meta = map[string]json.RawMessage{}
		}
	}
	normalized := NormalizeProbes(probes)
	if len(normalized) == 0 {
		delete(meta, MetadataKey)
	} else {
		body, err := json.Marshal(normalized)
		if err != nil {
			return existing
		}
		meta[MetadataKey] = body
	}
	out, err := json.Marshal(meta)
	if err != nil {
		return existing
	}
	return out
}
