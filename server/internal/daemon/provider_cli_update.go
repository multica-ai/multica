package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultProviderCLIUpdateInterval       = 24 * time.Hour
	DefaultProviderCLIUpdateWindowStart    = 4 * time.Hour
	DefaultProviderCLIUpdateWindowDuration = 2 * time.Hour
)

type ProviderCLIUpdateMode string

const (
	ProviderCLIUpdateOff    ProviderCLIUpdateMode = "off"
	ProviderCLIUpdateDryRun ProviderCLIUpdateMode = "dry-run"
	ProviderCLIUpdateApply  ProviderCLIUpdateMode = "apply"
)

type providerCLIUpdateStatus string

const (
	providerCLIUpdateInstalling       providerCLIUpdateStatus = "installing"
	providerCLIUpdatePendingVerify    providerCLIUpdateStatus = "pending_verify"
	providerCLIUpdateVerified         providerCLIUpdateStatus = "verified"
	providerCLIUpdateRollbackRequired providerCLIUpdateStatus = "rollback_required"
	providerCLIUpdateManualRequired   providerCLIUpdateStatus = "manual_required"
)

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
	VersionCommandTemplate       []string `json:"version_command_template"`
	UpgradeCommandTemplate       []string `json:"upgrade_command_template"`
}

// ProviderCLIUpdateRequest describes a planned provider CLI update. A pinned
// version wins over TargetVersion so operators can hold a cloud runtime on a
// known-good provider release.
type ProviderCLIUpdateRequest struct {
	RuntimeID       string `json:"runtime_id,omitempty"`
	Provider        string `json:"provider"`
	CurrentVersion  string `json:"current_version,omitempty"`
	LatestVersion   string `json:"latest_version,omitempty"`
	TargetVersion   string `json:"target_version,omitempty"`
	PinnedVersion   string `json:"pinned_version,omitempty"`
	RollbackVersion string `json:"rollback_version,omitempty"`
	InstallPath     string `json:"install_path,omitempty"`
	InstallPrefix   string `json:"install_prefix,omitempty"`
	Mode            string `json:"mode,omitempty"`
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
	LatestVersion   string                   `json:"latest_version,omitempty"`
	TargetVersion   string                   `json:"target_version,omitempty"`
	PinnedVersion   string                   `json:"pinned_version,omitempty"`
	RollbackVersion string                   `json:"rollback_version,omitempty"`
	InstallPath     string                   `json:"install_path,omitempty"`
	InstallPrefix   string                   `json:"install_prefix,omitempty"`
	Mode            string                   `json:"mode,omitempty"`
	Source          ProviderCLISource        `json:"source"`
	DryRun          bool                     `json:"dry_run"`
	Valid           bool                     `json:"valid"`
	InvalidReason   string                   `json:"invalid_reason,omitempty"`
	ObservedIdle    bool                     `json:"observed_idle"`
	PlanWarning     string                   `json:"plan_warning,omitempty"`
	Phases          []ProviderCLIUpdatePhase `json:"phases"`
}

type providerCLIUpdateRecord struct {
	UpdateID        string                  `json:"update_id"`
	Provider        string                  `json:"provider"`
	TargetVersion   string                  `json:"target_version"`
	RollbackVersion string                  `json:"rollback_version,omitempty"`
	InstallPath     string                  `json:"install_path"`
	InstallPrefix   string                  `json:"install_prefix"`
	Status          providerCLIUpdateStatus `json:"status"`
	Error           string                  `json:"error,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

var providerCLISources = map[string]ProviderCLISource{
	"claude": {
		Provider:                     "claude",
		BinaryName:                   "claude",
		OfficialSourceURL:            "https://docs.anthropic.com/en/docs/claude-code",
		PackageManager:               "npm",
		PackageName:                  "@anthropic-ai/claude-code",
		LatestVersionCommandTemplate: []string{"npm", "view", "@anthropic-ai/claude-code", "version"},
		VersionCommandTemplate:       []string{"claude", "--version"},
		UpgradeCommandTemplate:       []string{"npm", "install", "-g", "--prefix", "<install_prefix>", "@anthropic-ai/claude-code@<version>"},
	},
	"codex": {
		Provider:                     "codex",
		BinaryName:                   "codex",
		OfficialSourceURL:            "https://github.com/openai/codex",
		PackageManager:               "npm",
		PackageName:                  "@openai/codex",
		LatestVersionCommandTemplate: []string{"npm", "view", "@openai/codex", "version"},
		VersionCommandTemplate:       []string{"codex", "--version"},
		UpgradeCommandTemplate:       []string{"npm", "install", "-g", "--prefix", "<install_prefix>", "@openai/codex@<version>"},
	},
	"gemini": {
		Provider:                     "gemini",
		BinaryName:                   "gemini",
		OfficialSourceURL:            "https://github.com/google-gemini/gemini-cli",
		PackageManager:               "npm",
		PackageName:                  "@google/gemini-cli",
		LatestVersionCommandTemplate: []string{"npm", "view", "@google/gemini-cli", "version"},
		VersionCommandTemplate:       []string{"gemini", "--version"},
		UpgradeCommandTemplate:       []string{"npm", "install", "-g", "--prefix", "<install_prefix>", "@google/gemini-cli@<version>"},
	},
	"kimi": {
		Provider:                     "kimi",
		BinaryName:                   "kimi",
		OfficialSourceURL:            "https://github.com/MoonshotAI/kimi-code",
		PackageManager:               "npm",
		PackageName:                  "@moonshot-ai/kimi-code",
		LatestVersionCommandTemplate: []string{"npm", "view", "@moonshot-ai/kimi-code", "version"},
		VersionCommandTemplate:       []string{"kimi", "--version"},
		UpgradeCommandTemplate:       []string{"npm", "install", "-g", "--prefix", "<install_prefix>", "@moonshot-ai/kimi-code@<version>"},
	},
	"opencode": {
		Provider:                     "opencode",
		BinaryName:                   "opencode",
		OfficialSourceURL:            "https://github.com/sst/opencode",
		PackageManager:               "npm",
		PackageName:                  "opencode-ai",
		LatestVersionCommandTemplate: []string{"npm", "view", "opencode-ai", "version"},
		VersionCommandTemplate:       []string{"opencode", "--version"},
		UpgradeCommandTemplate:       []string{"npm", "install", "-g", "--prefix", "<install_prefix>", "opencode-ai@<version>"},
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
		LatestVersion:   req.LatestVersion,
		TargetVersion:   req.TargetVersion,
		PinnedVersion:   req.PinnedVersion,
		RollbackVersion: req.RollbackVersion,
		InstallPath:     req.InstallPath,
		InstallPrefix:   req.InstallPrefix,
		Mode:            req.Mode,
		DryRun:          req.Mode != string(ProviderCLIUpdateApply),
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
	} else if plan.TargetVersion == "" {
		plan.TargetVersion = plan.LatestVersion
	}
	if plan.RollbackVersion == "" {
		plan.RollbackVersion = req.CurrentVersion
	}
	if plan.TargetVersion == "" {
		plan.Valid = false
		plan.InvalidReason = "target_version, latest_version, or pinned_version is required"
	}
	if idle, reason := d.providerCLIUpdateIdleSnapshot(); !idle {
		plan.ObservedIdle = false
		plan.PlanWarning = reason
	} else {
		plan.ObservedIdle = true
	}

	plan.Phases = []ProviderCLIUpdatePhase{
		{Name: "official_source_check", Description: "Resolve latest provider CLI version from the configured official source only.", CommandTemplate: plan.Source.LatestVersionCommandTemplate},
		{Name: "idle_snapshot", Description: "Report whether the daemon looked idle at planning time. This is not authorization to execute an upgrade."},
		{Name: "pin_target_and_rollback", Description: fmt.Sprintf("Use target %q and keep rollback version %q for operator-driven revert.", plan.TargetVersion, plan.RollbackVersion)},
		{Name: "install_location", Description: fmt.Sprintf("Install into daemon PATH location %q using prefix %q.", plan.InstallPath, plan.InstallPrefix)},
		{Name: "upgrade_provider_cli_template", Description: "Review-only template for installing the pinned provider CLI version. A real executor must atomically hold updating and the claim barrier before running anything.", CommandTemplate: plan.Source.UpgradeCommandTemplate},
		{Name: "restart_daemon", Description: "After a real provider CLI upgrade, restart the daemon so future agent subprocesses inherit the new executable."},
		{Name: "reregister_runtime", Description: "Daemon startup re-runs provider version detection and registerRuntimesForWorkspace so the server records the new CLI version.", CommandTemplate: plan.Source.VersionCommandTemplate},
		{Name: "verify_runtime_list", Description: "After re-registration, verify the provider version via multica runtime list.", CommandTemplate: []string{"multica", "runtime", "list", "--output", "json"}},
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

// providerCLICommandRunner is overridden by tests so update planning and
// apply-mode safety can be exercised without touching npm or provider CLIs.
var providerCLICommandRunner = runProviderCLICommand

func runProviderCLICommand(ctx context.Context, command []string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("empty command")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

var providerCLIUpdateInitialDelay = 5 * time.Minute

func (d *Daemon) providerCLIAutoUpdateLoop(ctx context.Context) {
	mode := d.providerCLIUpdateMode()
	if mode == ProviderCLIUpdateOff {
		d.logger.Info("provider CLI auto-update: disabled")
		return
	}
	interval := d.cfg.ProviderCLIUpdateInterval
	if interval <= 0 {
		interval = DefaultProviderCLIUpdateInterval
	}
	d.logger.Info("provider CLI auto-update: started", "mode", mode, "interval", interval)

	if err := sleepWithContext(ctx, providerCLIUpdateInitialDelay); err != nil {
		return
	}
	d.tryProviderCLIAutoUpdate(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.tryProviderCLIAutoUpdate(ctx)
		}
	}
}

func (d *Daemon) tryProviderCLIAutoUpdate(ctx context.Context) {
	mode := d.providerCLIUpdateMode()
	if mode == ProviderCLIUpdateOff || ctx.Err() != nil {
		return
	}
	if mode == ProviderCLIUpdateApply && !d.inProviderCLIUpdateWindow(time.Now()) {
		d.logger.Debug("provider CLI auto-update: outside apply window")
		return
	}

	for provider, entry := range d.cfg.Agents {
		if ctx.Err() != nil {
			return
		}
		plan, err := d.buildProviderCLIAutoUpdatePlan(ctx, provider, entry, mode)
		if err != nil {
			d.logger.Warn("provider CLI auto-update: plan failed", "provider", provider, "error", err)
			continue
		}
		if !plan.Valid {
			d.logger.Warn("provider CLI auto-update: invalid plan", "provider", provider, "reason", plan.InvalidReason)
			continue
		}
		if plan.TargetVersion == "" || versionOutputMatches(plan.CurrentVersion, plan.TargetVersion) {
			d.logger.Debug("provider CLI auto-update: no update needed", "provider", provider, "current", plan.CurrentVersion, "target", plan.TargetVersion)
			continue
		}
		if mode != ProviderCLIUpdateApply {
			d.logger.Info("provider CLI auto-update: dry-run update available", "provider", provider, "current", plan.CurrentVersion, "target", plan.TargetVersion, "install_path", plan.InstallPath, "install_prefix", plan.InstallPrefix)
			continue
		}
		if err := d.applyProviderCLIUpdate(ctx, plan); err != nil {
			d.logger.Warn("provider CLI auto-update: apply failed", "provider", provider, "error", err)
		}
	}
}

func (d *Daemon) buildProviderCLIAutoUpdatePlan(ctx context.Context, provider string, entry AgentEntry, mode ProviderCLIUpdateMode) (ProviderCLIUpdatePlan, error) {
	source, ok := providerCLISources[provider]
	if !ok {
		return d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{Provider: provider, Mode: string(mode)}), nil
	}
	current := d.agentVersion(provider)
	if current == "" {
		detectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if version, err := detectAgentVersion(detectCtx, entry.Path); err == nil {
			current = version
		}
	}
	latest := ""
	pinned := d.cfg.ProviderCLIPinnedVersions[provider]
	if pinned == "" {
		latestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := providerCLICommandRunner(latestCtx, source.LatestVersionCommandTemplate)
		if err != nil {
			return ProviderCLIUpdatePlan{}, fmt.Errorf("fetch latest provider version: %w", err)
		}
		latest = strings.TrimSpace(out)
	}
	installPath, installPrefix, err := providerCLIInstallLocation(entry.Path, d.cfg.ProviderCLIInstallPrefixes[provider])
	if err != nil {
		return ProviderCLIUpdatePlan{}, err
	}
	return d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{Provider: provider, CurrentVersion: current, LatestVersion: latest, PinnedVersion: pinned, RollbackVersion: d.cfg.ProviderCLIRollbackVersions[provider], InstallPath: installPath, InstallPrefix: installPrefix, Mode: string(mode)}), nil
}

func (d *Daemon) applyProviderCLIUpdate(ctx context.Context, plan ProviderCLIUpdatePlan) error {
	return d.applyProviderCLIUpdateWithMode(ctx, plan, d.providerCLIUpdateMode(), true, true)
}

func (d *Daemon) applyProviderCLIUpdateWithMode(ctx context.Context, plan ProviderCLIUpdatePlan, mode ProviderCLIUpdateMode, enforceWindow bool, restartOnSuccess bool) error {
	if mode != ProviderCLIUpdateApply {
		return fmt.Errorf("provider CLI update mode is not apply")
	}
	if enforceWindow && !d.inProviderCLIUpdateWindow(time.Now()) {
		return fmt.Errorf("outside provider CLI update window")
	}
	if !plan.Valid {
		return fmt.Errorf("invalid provider CLI update plan: %s", plan.InvalidReason)
	}
	if plan.TargetVersion == "" {
		return fmt.Errorf("target version is required")
	}
	if plan.InstallPrefix == "" {
		return fmt.Errorf("install prefix is required")
	}
	if !d.updating.CompareAndSwap(false, true) {
		return fmt.Errorf("daemon update already in progress")
	}
	released := false
	defer func() {
		if !released {
			d.updating.Store(false)
		}
	}()
	if !d.trySetClaimBarrier() {
		return fmt.Errorf("task or claim in flight at barrier check")
	}
	barrierReleased := false
	defer func() {
		if !barrierReleased {
			d.releaseClaimBarrier()
		}
	}()

	record := providerCLIUpdateRecord{UpdateID: newProviderCLIUpdateID(), Provider: plan.Provider, TargetVersion: plan.TargetVersion, RollbackVersion: plan.RollbackVersion, InstallPath: plan.InstallPath, InstallPrefix: plan.InstallPrefix, Status: providerCLIUpdateInstalling, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := d.saveProviderCLIUpdateRecord(record); err != nil {
		return err
	}

	command := materializeProviderCLICommand(plan.Source.UpgradeCommandTemplate, plan.TargetVersion, plan.InstallPrefix)
	output, err := providerCLICommandRunner(ctx, command)
	if err != nil {
		if plan.RollbackVersion != "" {
			record.Status = providerCLIUpdateRollbackRequired
			record.Error = fmt.Sprintf("install failed: %v (%s); rollback version %q requires operator action", err, output, plan.RollbackVersion)
		} else {
			record.Status = providerCLIUpdateManualRequired
			record.Error = fmt.Sprintf("install failed: %v (%s)", err, output)
		}
		record.UpdatedAt = time.Now()
		_ = d.saveProviderCLIUpdateRecord(record)
		return fmt.Errorf("install failed: %w (%s)", err, output)
	}
	if err := d.verifyProviderCLIPathVersion(ctx, plan); err != nil {
		record.Error = err.Error()
		if plan.RollbackVersion != "" {
			rollback := materializeProviderCLICommand(plan.Source.UpgradeCommandTemplate, plan.RollbackVersion, plan.InstallPrefix)
			if rollbackOutput, rollbackErr := providerCLICommandRunner(ctx, rollback); rollbackErr != nil {
				record.Status = providerCLIUpdateRollbackRequired
				record.Error = fmt.Sprintf("verify failed: %v; rollback failed: %v (%s)", err, rollbackErr, rollbackOutput)
			} else {
				record.Status = providerCLIUpdateManualRequired
				record.Error = fmt.Sprintf("verify failed after install; rollback command ran, manual version check required: %v", err)
			}
		} else {
			record.Status = providerCLIUpdateManualRequired
		}
		record.UpdatedAt = time.Now()
		_ = d.saveProviderCLIUpdateRecord(record)
		return err
	}

	record.Status = providerCLIUpdatePendingVerify
	record.UpdatedAt = time.Now()
	if err := d.saveProviderCLIUpdateRecord(record); err != nil {
		return err
	}
	d.logger.Info("provider CLI auto-update: install completed, restart pending", "provider", plan.Provider, "target", plan.TargetVersion, "update_id", record.UpdateID, "output", output)
	released = true
	barrierReleased = true
	if restartOnSuccess {
		d.triggerRestart()
	}
	return nil
}

func (d *Daemon) verifyPendingProviderCLIUpdates() error {
	records, err := d.loadProviderCLIUpdateRecords()
	if err != nil {
		return err
	}
	changed := false
	now := time.Now()
	for id, record := range records {
		if record.Status != providerCLIUpdatePendingVerify {
			continue
		}
		current := d.agentVersion(record.Provider)
		if versionOutputMatches(current, record.TargetVersion) {
			record.Status = providerCLIUpdateVerified
			record.Error = ""
		} else if record.RollbackVersion != "" {
			record.Status = providerCLIUpdateRollbackRequired
			record.Error = fmt.Sprintf("registered version %q does not match target %q; rollback version %q requires operator action", current, record.TargetVersion, record.RollbackVersion)
		} else {
			record.Status = providerCLIUpdateManualRequired
			record.Error = fmt.Sprintf("registered version %q does not match target %q", current, record.TargetVersion)
		}
		record.UpdatedAt = now
		records[id] = record
		changed = true
	}
	if !changed {
		return nil
	}
	return d.saveProviderCLIUpdateRecords(records)
}

func (d *Daemon) verifyProviderCLIPathVersion(ctx context.Context, plan ProviderCLIUpdatePlan) error {
	if plan.InstallPath == "" {
		return fmt.Errorf("install path is required for version verification")
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := providerCLICommandRunner(verifyCtx, []string{plan.InstallPath, "--version"})
	if err != nil {
		return fmt.Errorf("verify provider CLI version: %w (%s)", err, out)
	}
	if !versionOutputMatches(out, plan.TargetVersion) {
		return fmt.Errorf("provider CLI at %s reports %q, want %q", plan.InstallPath, out, plan.TargetVersion)
	}
	return nil
}

func (d *Daemon) saveProviderCLIUpdateRecord(record providerCLIUpdateRecord) error {
	records, err := d.loadProviderCLIUpdateRecords()
	if err != nil {
		return err
	}
	records[record.UpdateID] = record
	return d.saveProviderCLIUpdateRecords(records)
}

func (d *Daemon) loadProviderCLIUpdateRecords() (map[string]providerCLIUpdateRecord, error) {
	path := d.providerCLIUpdateStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]providerCLIUpdateRecord{}, nil
		}
		return nil, err
	}
	var records map[string]providerCLIUpdateRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	if records == nil {
		records = map[string]providerCLIUpdateRecord{}
	}
	return records, nil
}

func (d *Daemon) saveProviderCLIUpdateRecords(records map[string]providerCLIUpdateRecord) error {
	path := d.providerCLIUpdateStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (d *Daemon) providerCLIUpdateStatePath() string {
	root := d.cfg.WorkspacesRoot
	if root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, ".provider-cli-updates.json")
}

func (d *Daemon) providerCLIUpdateMode() ProviderCLIUpdateMode {
	if d.cfg.ProviderCLIUpdateMode == "" {
		return ProviderCLIUpdateDryRun
	}
	return d.cfg.ProviderCLIUpdateMode
}

func (d *Daemon) inProviderCLIUpdateWindow(now time.Time) bool {
	start := d.cfg.ProviderCLIUpdateWindowStart
	if start == 0 && !d.cfg.ProviderCLIUpdateWindowStartConfigured {
		start = DefaultProviderCLIUpdateWindowStart
	}
	duration := d.cfg.ProviderCLIUpdateWindowDuration
	if duration <= 0 {
		duration = DefaultProviderCLIUpdateWindowDuration
	}
	return timeOfDayInWindow(now, start, duration)
}

func timeOfDayInWindow(now time.Time, start, duration time.Duration) bool {
	if duration <= 0 {
		return false
	}
	day := 24 * time.Hour
	if duration >= day {
		return true
	}
	offset := time.Duration(now.Hour())*time.Hour + time.Duration(now.Minute())*time.Minute + time.Duration(now.Second())*time.Second
	start = ((start % day) + day) % day
	end := (start + duration) % day
	if start+duration < day {
		return offset >= start && offset < start+duration
	}
	return offset >= start || offset < end
}

func materializeProviderCLICommand(template []string, version, installPrefix string) []string {
	out := make([]string, len(template))
	for i, part := range template {
		part = strings.ReplaceAll(part, "<version>", version)
		part = strings.ReplaceAll(part, "<install_prefix>", installPrefix)
		out[i] = part
	}
	return out
}

func providerCLIInstallLocation(configuredPath, explicitPrefix string) (string, string, error) {
	resolved := strings.TrimSpace(configuredPath)
	if resolved == "" {
		return "", "", fmt.Errorf("provider CLI path is empty")
	}
	if !strings.ContainsAny(resolved, `/\`) {
		path, err := exec.LookPath(resolved)
		if err != nil {
			return "", "", err
		}
		resolved = path
	}
	if eval, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = eval
	}
	if explicitPrefix != "" {
		return resolved, explicitPrefix, nil
	}
	if providerCLIPathLooksLikeShim(resolved) || providerCLIPathLooksLikeShim(configuredPath) {
		return "", "", fmt.Errorf("provider CLI path %q looks like a version-manager shim; set MULTICA_PROVIDER_CLI_INSTALL_PREFIXES explicitly", configuredPath)
	}
	dir := filepath.Dir(resolved)
	if filepath.Base(dir) == "bin" {
		return resolved, filepath.Dir(dir), nil
	}
	return resolved, dir, nil
}

func providerCLIPathLooksLikeShim(path string) bool {
	p := filepath.ToSlash(strings.ToLower(path))
	for _, marker := range []string{"/.volta/bin/", "/.asdf/shims/", "/.nvm/", "/.fnm/", "/fnm_multishells/"} {
		if strings.Contains(p, marker) {
			return true
		}
	}
	return false
}

func parseProviderCLIUpdateMode(raw string) (ProviderCLIUpdateMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "dry-run", "dry_run", "dryrun", "plan", "true", "1", "yes", "on":
		return ProviderCLIUpdateDryRun, nil
	case "off", "false", "0", "no":
		return ProviderCLIUpdateOff, nil
	case "apply":
		return ProviderCLIUpdateApply, nil
	default:
		return "", fmt.Errorf("invalid provider CLI update mode %q", raw)
	}
}

func parseProviderCLIVersionMap(raw string) map[string]string {
	out := map[string]string{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func versionOutputMatches(output, version string) bool {
	return version != "" && strings.Contains(strings.TrimSpace(output), strings.TrimSpace(version))
}

func newProviderCLIUpdateID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
