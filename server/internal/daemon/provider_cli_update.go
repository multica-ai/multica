package daemon

import "fmt"

// ProviderCLISource is the daemon-owned allowlist for provider CLI update
// metadata. It is intentionally declarative: planning can cite the official
// source and package manager without executing installs from untrusted input.
type ProviderCLISource struct {
	Provider             string   `json:"provider"`
	BinaryName           string   `json:"binary_name"`
	OfficialSourceURL    string   `json:"official_source_url"`
	PackageManager       string   `json:"package_manager"`
	PackageName          string   `json:"package_name"`
	LatestVersionCommand []string `json:"latest_version_command"`
	VersionCommand        []string `json:"version_command"`
	UpgradeCommand        []string `json:"upgrade_command"`
}

// ProviderCLIUpdateRequest describes a planned provider CLI update. A pinned
// version wins over TargetVersion so operators can hold a cloud runtime on a
// known-good provider release.
type ProviderCLIUpdateRequest struct {
	RuntimeID       string `json:"runtime_id,omitempty"`
	Provider        string `json:"provider"`
	CurrentVersion  string `json:"current_version,omitempty"`
	TargetVersion   string `json:"target_version,omitempty"`
	PinnedVersion   string `json:"pinned_version,omitempty"`
	RollbackVersion string `json:"rollback_version,omitempty"`
}

// ProviderCLIUpdatePhase is a productized execution step. The daemon starts
// with dry-run planning only; Execute remains false until a separate worker is
// wired to run an approved command against an idle runtime.
type ProviderCLIUpdatePhase struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     []string `json:"command,omitempty"`
	Execute     bool     `json:"execute"`
}

// ProviderCLIUpdatePlan is the minimum closed-loop contract for provider CLI
// updates: trusted source, pinned target, rollback point, idle gate, and the
// restart/re-register path after upgrade.
type ProviderCLIUpdatePlan struct {
	RuntimeID       string                   `json:"runtime_id,omitempty"`
	Provider        string                   `json:"provider"`
	CurrentVersion  string                   `json:"current_version,omitempty"`
	TargetVersion   string                   `json:"target_version,omitempty"`
	PinnedVersion   string                   `json:"pinned_version,omitempty"`
	RollbackVersion string                   `json:"rollback_version,omitempty"`
	Source          ProviderCLISource        `json:"source"`
	DryRun          bool                     `json:"dry_run"`
	CanStart        bool                     `json:"can_start"`
	BlockedReason   string                   `json:"blocked_reason,omitempty"`
	Phases          []ProviderCLIUpdatePhase `json:"phases"`
}

var providerCLISources = map[string]ProviderCLISource{
	"claude": {
		Provider:             "claude",
		BinaryName:           "claude",
		OfficialSourceURL:    "https://docs.anthropic.com/en/docs/claude-code",
		PackageManager:       "npm",
		PackageName:          "@anthropic-ai/claude-code",
		LatestVersionCommand: []string{"npm", "view", "@anthropic-ai/claude-code", "version"},
		VersionCommand:        []string{"claude", "--version"},
		UpgradeCommand:        []string{"npm", "install", "-g", "@anthropic-ai/claude-code@<version>"},
	},
	"codex": {
		Provider:             "codex",
		BinaryName:           "codex",
		OfficialSourceURL:    "https://github.com/openai/codex",
		PackageManager:       "npm",
		PackageName:          "@openai/codex",
		LatestVersionCommand: []string{"npm", "view", "@openai/codex", "version"},
		VersionCommand:        []string{"codex", "--version"},
		UpgradeCommand:        []string{"npm", "install", "-g", "@openai/codex@<version>"},
	},
	"gemini": {
		Provider:             "gemini",
		BinaryName:           "gemini",
		OfficialSourceURL:    "https://github.com/google-gemini/gemini-cli",
		PackageManager:       "npm",
		PackageName:          "@google/gemini-cli",
		LatestVersionCommand: []string{"npm", "view", "@google/gemini-cli", "version"},
		VersionCommand:        []string{"gemini", "--version"},
		UpgradeCommand:        []string{"npm", "install", "-g", "@google/gemini-cli@<version>"},
	},
	"kimi": {
		Provider:             "kimi",
		BinaryName:           "kimi",
		OfficialSourceURL:    "https://github.com/MoonshotAI/kimi-code",
		PackageManager:       "npm",
		PackageName:          "@moonshot-ai/kimi-code",
		LatestVersionCommand: []string{"npm", "view", "@moonshot-ai/kimi-code", "version"},
		VersionCommand:        []string{"kimi", "--version"},
		UpgradeCommand:        []string{"npm", "install", "-g", "@moonshot-ai/kimi-code@<version>"},
	},
	"opencode": {
		Provider:             "opencode",
		BinaryName:           "opencode",
		OfficialSourceURL:    "https://github.com/sst/opencode",
		PackageManager:       "npm",
		PackageName:          "opencode-ai",
		LatestVersionCommand: []string{"npm", "view", "opencode-ai", "version"},
		VersionCommand:        []string{"opencode", "--version"},
		UpgradeCommand:        []string{"npm", "install", "-g", "opencode-ai@<version>"},
	},
}

func ProviderCLISources() map[string]ProviderCLISource {
	out := make(map[string]ProviderCLISource, len(providerCLISources))
	for provider, source := range providerCLISources {
		out[provider] = source
	}
	return out
}

func (d *Daemon) PlanProviderCLIUpdate(req ProviderCLIUpdateRequest) ProviderCLIUpdatePlan {
	plan := ProviderCLIUpdatePlan{
		RuntimeID:       req.RuntimeID,
		Provider:        req.Provider,
		CurrentVersion:  req.CurrentVersion,
		TargetVersion:   req.TargetVersion,
		PinnedVersion:   req.PinnedVersion,
		RollbackVersion: req.RollbackVersion,
		DryRun:          true,
		CanStart:        true,
	}

	source, ok := providerCLISources[req.Provider]
	if !ok {
		plan.CanStart = false
		plan.BlockedReason = fmt.Sprintf("provider %q has no official update source configured", req.Provider)
		return plan
	}
	plan.Source = source

	if plan.PinnedVersion != "" {
		plan.TargetVersion = plan.PinnedVersion
	}
	if plan.RollbackVersion == "" {
		plan.RollbackVersion = req.CurrentVersion
	}
	if plan.TargetVersion == "" {
		plan.CanStart = false
		plan.BlockedReason = "target_version or pinned_version is required"
	}
	if idle, reason := d.providerCLIUpdateIdleGate(); !idle {
		plan.CanStart = false
		plan.BlockedReason = reason
	}

	plan.Phases = []ProviderCLIUpdatePhase{
		{
			Name:        "official_source_check",
			Description: "Resolve latest provider CLI version from the configured official source only.",
			Command:     source.LatestVersionCommand,
			Execute:     false,
		},
		{
			Name:        "idle_gate",
			Description: "Proceed only when the daemon has no active task, no claim in flight, and no update barrier already held.",
			Execute:     false,
		},
		{
			Name:        "pin_target_and_rollback",
			Description: fmt.Sprintf("Use target %q and keep rollback version %q for operator-driven revert.", plan.TargetVersion, plan.RollbackVersion),
			Execute:     false,
		},
		{
			Name:        "upgrade_provider_cli",
			Description: "Install the pinned provider CLI version. This dry-run planner never executes the command.",
			Command:     source.UpgradeCommand,
			Execute:     false,
		},
		{
			Name:        "restart_daemon",
			Description: "After a real provider CLI upgrade, restart the daemon so future agent subprocesses inherit the new executable.",
			Execute:     false,
		},
		{
			Name:        "reregister_runtime",
			Description: "Daemon startup re-runs provider version detection and registerRuntimesForWorkspace so the server records the new CLI version.",
			Command:     source.VersionCommand,
			Execute:     false,
		},
	}
	return plan
}

func (d *Daemon) providerCLIUpdateIdleGate() (bool, string) {
	if d == nil {
		return true, ""
	}
	if d.updating.Load() {
		return false, "daemon update already in progress"
	}
	if running := d.activeTasks.Load(); running > 0 {
		return false, fmt.Sprintf("runtime has %d active task(s)", running)
	}
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	if d.pauseClaims {
		return false, "claim barrier is already held"
	}
	if d.claimsInFlight > 0 {
		return false, fmt.Sprintf("runtime has %d claim(s) in flight", d.claimsInFlight)
	}
	return true, ""
}
