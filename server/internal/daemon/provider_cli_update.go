package daemon

import "fmt"

// ProviderCLISource is the daemon-owned allowlist for provider CLI update
// metadata. It is intentionally declarative: planning can cite the official
// source and package manager without executing installs from untrusted input.
type ProviderCLISource struct {
	Provider                     string   `json:"provider"`
	BinaryName                   string   `json:"binary_name"`
	OfficialSourceURL            string   `json:"official_source_url"`
	PackageManager               string   `json:"package_manager"`
	PackageName                  string   `json:"package_name"`
	LatestVersionCommandTemplate []string `json:"latest_version_command_template"`
	VersionCommandTemplate        []string `json:"version_command_template"`
	UpgradeCommandTemplate        []string `json:"upgrade_command_template"`
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

// ProviderCLIUpdatePhase is a productized dry-run planning step. Command
// values are templates for review and UI copy, never execution authority.
type ProviderCLIUpdatePhase struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	CommandTemplate []string `json:"command_template,omitempty"`
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
	Valid           bool                     `json:"valid"`
	InvalidReason   string                   `json:"invalid_reason,omitempty"`
	ObservedIdle    bool                     `json:"observed_idle"`
	PlanWarning     string                   `json:"plan_warning,omitempty"`
	Phases          []ProviderCLIUpdatePhase `json:"phases"`
}

var providerCLISources = map[string]ProviderCLISource{
	"claude": {
		Provider:                     "claude",
		BinaryName:                   "claude",
		OfficialSourceURL:            "https://docs.anthropic.com/en/docs/claude-code",
		PackageManager:               "npm",
		PackageName:                  "@anthropic-ai/claude-code",
		LatestVersionCommandTemplate: []string{"npm", "view", "@anthropic-ai/claude-code", "version"},
		VersionCommandTemplate:        []string{"claude", "--version"},
		UpgradeCommandTemplate:        []string{"npm", "install", "-g", "@anthropic-ai/claude-code@<version>"},
	},
	"codex": {
		Provider:                     "codex",
		BinaryName:                   "codex",
		OfficialSourceURL:            "https://github.com/openai/codex",
		PackageManager:               "npm",
		PackageName:                  "@openai/codex",
		LatestVersionCommandTemplate: []string{"npm", "view", "@openai/codex", "version"},
		VersionCommandTemplate:        []string{"codex", "--version"},
		UpgradeCommandTemplate:        []string{"npm", "install", "-g", "@openai/codex@<version>"},
	},
	"gemini": {
		Provider:                     "gemini",
		BinaryName:                   "gemini",
		OfficialSourceURL:            "https://github.com/google-gemini/gemini-cli",
		PackageManager:               "npm",
		PackageName:                  "@google/gemini-cli",
		LatestVersionCommandTemplate: []string{"npm", "view", "@google/gemini-cli", "version"},
		VersionCommandTemplate:        []string{"gemini", "--version"},
		UpgradeCommandTemplate:        []string{"npm", "install", "-g", "@google/gemini-cli@<version>"},
	},
	"kimi": {
		Provider:                     "kimi",
		BinaryName:                   "kimi",
		OfficialSourceURL:            "https://github.com/MoonshotAI/kimi-code",
		PackageManager:               "npm",
		PackageName:                  "@moonshot-ai/kimi-code",
		LatestVersionCommandTemplate: []string{"npm", "view", "@moonshot-ai/kimi-code", "version"},
		VersionCommandTemplate:        []string{"kimi", "--version"},
		UpgradeCommandTemplate:        []string{"npm", "install", "-g", "@moonshot-ai/kimi-code@<version>"},
	},
	"opencode": {
		Provider:                     "opencode",
		BinaryName:                   "opencode",
		OfficialSourceURL:            "https://github.com/sst/opencode",
		PackageManager:               "npm",
		PackageName:                  "opencode-ai",
		LatestVersionCommandTemplate: []string{"npm", "view", "opencode-ai", "version"},
		VersionCommandTemplate:        []string{"opencode", "--version"},
		UpgradeCommandTemplate:        []string{"npm", "install", "-g", "opencode-ai@<version>"},
	},
}

func ProviderCLISources() map[string]ProviderCLISource {
	out := make(map[string]ProviderCLISource, len(providerCLISources))
	for provider, source := range providerCLISources {
		out[provider] = cloneProviderCLISource(source)
	}
	return out
}

func cloneProviderCLISource(source ProviderCLISource) ProviderCLISource {
	source.LatestVersionCommandTemplate = cloneStringSlice(source.LatestVersionCommandTemplate)
	source.VersionCommandTemplate = cloneStringSlice(source.VersionCommandTemplate)
	source.UpgradeCommandTemplate = cloneStringSlice(source.UpgradeCommandTemplate)
	return source
}

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
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
		Valid:           true,
	}

	source, ok := providerCLISources[req.Provider]
	if !ok {
		plan.Valid = false
		plan.InvalidReason = fmt.Sprintf("provider %q has no official update source configured", req.Provider)
		return plan
	}
	plan.Source = cloneProviderCLISource(source)

	if plan.PinnedVersion != "" {
		plan.TargetVersion = plan.PinnedVersion
	}
	if plan.RollbackVersion == "" {
		plan.RollbackVersion = req.CurrentVersion
	}
	if plan.TargetVersion == "" {
		plan.Valid = false
		plan.InvalidReason = "target_version or pinned_version is required"
	}
	if idle, reason := d.providerCLIUpdateIdleSnapshot(); !idle {
		plan.ObservedIdle = false
		plan.PlanWarning = reason
	} else {
		plan.ObservedIdle = true
	}

	plan.Phases = []ProviderCLIUpdatePhase{
		{
			Name:            "official_source_check",
			Description:     "Resolve latest provider CLI version from the configured official source only.",
			CommandTemplate: plan.Source.LatestVersionCommandTemplate,
		},
		{
			Name:        "idle_snapshot",
			Description: "Report whether the daemon looked idle at planning time. This is not authorization to execute an upgrade.",
		},
		{
			Name:        "pin_target_and_rollback",
			Description: fmt.Sprintf("Use target %q and keep rollback version %q for operator-driven revert.", plan.TargetVersion, plan.RollbackVersion),
		},
		{
			Name:            "upgrade_provider_cli_template",
			Description:     "Review-only template for installing the pinned provider CLI version. A real executor must atomically hold updating and the claim barrier before running anything.",
			CommandTemplate: plan.Source.UpgradeCommandTemplate,
		},
		{
			Name:        "restart_daemon",
			Description: "After a real provider CLI upgrade, restart the daemon so future agent subprocesses inherit the new executable.",
		},
		{
			Name:            "reregister_runtime",
			Description:     "Daemon startup re-runs provider version detection and registerRuntimesForWorkspace so the server records the new CLI version.",
			CommandTemplate: plan.Source.VersionCommandTemplate,
		},
	}
	return plan
}

func (d *Daemon) providerCLIUpdateIdleSnapshot() (bool, string) {
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
