// Package sessionmode defines the fixed execution profiles shared by Issue and Chat sessions.
package sessionmode

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Mode string

const (
	Plan     Mode = "plan"
	Build    Mode = "build"
	Research Mode = "research"
	Review   Mode = "review"
)

type Profile struct {
	Mode            Mode
	Version         string
	Model           string
	ThinkingLevel   string
	Timeout         time.Duration
	MaxTurns        int
	AllowsWrite     bool
	AllowsPlanWrite bool
	Instruction     string
	AllowedTools    []string
	DataSources     []string
	ApprovalPolicy  string
	ExtraSkillIDs   []string
}

// Config is the persisted, interface-editable shape of a Mode profile.
// Durations are stored as minutes so snapshots stay provider-neutral JSON.
//
// AllowsWrite covers code and connected data. AllowsPlanWrite covers only the
// session's own written output — a plan, a note, an artifact. They are separate
// because a single write switch made Plan Mode self-contradictory: its
// instruction told the agent to save a plan while the rendered policy told it
// writes were disabled, so the agent skipped the one deliverable Plan Mode
// exists to produce (FIR-4047).
type Config struct {
	Mode            Mode     `json:"mode"`
	Version         string   `json:"version,omitempty"`
	Instruction     string   `json:"instruction"`
	Model           string   `json:"model,omitempty"`
	ThinkingLevel   string   `json:"thinking_level"`
	TimeoutMinutes  int      `json:"timeout_minutes"`
	MaxTurns        int      `json:"max_turns"`
	AllowsWrite     bool     `json:"allows_write"`
	AllowsPlanWrite bool     `json:"allows_plan_write"`
	AllowedTools    []string `json:"allowed_tools"`
	DataSources     []string `json:"data_sources"`
	ApprovalPolicy  string   `json:"approval_policy"`
	ExtraSkillIDs   []string `json:"extra_skill_ids"`
}

// CanWritePlans reports whether the Mode may save its own written output.
// A Mode that may write code may always write a plan, so a published snapshot
// from before AllowsPlanWrite existed keeps behaving the way it did.
func (c Config) CanWritePlans() bool { return c.AllowsPlanWrite || c.AllowsWrite }

// UnmarshalJSON accepts the legacy eval_skill_ids key. Published Mode versions
// are immutable history, so snapshots written before the field was renamed to
// extra_skill_ids must stay readable. New writes only emit the new key.
func (c *Config) UnmarshalJSON(data []byte) error {
	type alias Config
	var decoded struct {
		alias
		LegacyEvalSkillIDs []string `json:"eval_skill_ids"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Config(decoded.alias)
	if len(c.ExtraSkillIDs) == 0 {
		c.ExtraSkillIDs = decoded.LegacyEvalSkillIDs
	}
	return nil
}

var defaultConfigs = map[Mode]Config{
	Plan: {
		Mode: Plan, Version: "1", ThinkingLevel: "medium", TimeoutMinutes: 30, MaxTurns: 20, AllowsPlanWrite: true, ApprovalPolicy: "inherit",
		Instruction: "This session is planning-only. You may investigate and save a concrete plan, but you must NOT write or edit code, run migrations, deploy, open a pull request, or make external mutations. Finish with a saved plan and its acceptance criteria.",
	},
	Build: {
		Mode: Build, Version: "1", ThinkingLevel: "high", TimeoutMinutes: 120, MaxTurns: 80, AllowsWrite: true, AllowsPlanWrite: true, ApprovalPolicy: "inherit",
		Instruction: "Implement the requested outcome within the approved scope. Follow test-driven development, preserve existing permissions and approvals, and finish only after fresh verification evidence.",
	},
	// Research saves a written report, which is neither code nor connected data,
	// so it gets AllowsPlanWrite for the same reason Plan does. Review does not:
	// its deliverable is inline findings on the work under review, not a document.
	Research: {
		Mode: Research, Version: "1", ThinkingLevel: "high", TimeoutMinutes: 60, MaxTurns: 40, AllowsPlanWrite: true, ApprovalPolicy: "inherit",
		Instruction: "This session is a read-only investigation. You must not edit code or data, deploy, open or merge a pull request, or make external mutations. Cite sources, distinguish facts from inference, and state material uncertainty.",
	},
	Review: {
		Mode: Review, Version: "1", ThinkingLevel: "high", TimeoutMinutes: 45, MaxTurns: 30, ApprovalPolicy: "inherit",
		Instruction: "Review only. You must NOT edit the reviewed code, merge, deploy, or make external mutations. Report findings by severity with concrete evidence and file/line references; say explicitly when no findings remain.",
	},
}

func (c Config) Profile() Profile {
	return Profile{
		Mode: c.Mode, Version: c.Version, Model: c.Model,
		ThinkingLevel: c.ThinkingLevel, Timeout: time.Duration(c.TimeoutMinutes) * time.Minute,
		MaxTurns: c.MaxTurns, AllowsWrite: c.AllowsWrite, AllowsPlanWrite: c.CanWritePlans(), Instruction: c.Instruction,
		AllowedTools: append([]string(nil), c.AllowedTools...), DataSources: append([]string(nil), c.DataSources...),
		ApprovalPolicy: c.ApprovalPolicy, ExtraSkillIDs: append([]string(nil), c.ExtraSkillIDs...),
	}
}

func DefaultConfigs() map[Mode]Config {
	out := make(map[Mode]Config, len(defaultConfigs))
	for mode, config := range defaultConfigs {
		config.AllowedTools = append([]string(nil), config.AllowedTools...)
		config.DataSources = append([]string(nil), config.DataSources...)
		config.ExtraSkillIDs = append([]string(nil), config.ExtraSkillIDs...)
		out[mode] = config
	}
	return out
}

func ValidateConfig(config Config) error {
	if normalized, ok := Normalize(string(config.Mode)); !ok || normalized != config.Mode {
		return fmt.Errorf("invalid mode %q", config.Mode)
	}
	if strings.TrimSpace(config.Instruction) == "" {
		return fmt.Errorf("instruction is required")
	}
	switch config.ThinkingLevel {
	case "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("invalid thinking level %q", config.ThinkingLevel)
	}
	if config.TimeoutMinutes < 0 {
		return fmt.Errorf("timeout_minutes must be zero or positive")
	}
	if config.MaxTurns < 0 {
		return fmt.Errorf("max_turns must be zero or positive")
	}
	switch config.ApprovalPolicy {
	case "inherit", "require", "deny_external":
	default:
		return fmt.Errorf("invalid approval policy %q", config.ApprovalPolicy)
	}
	return nil
}

func Normalize(raw string) (Mode, bool) {
	value := Mode(strings.ToLower(strings.TrimSpace(raw)))
	if value == "default" || value == "auto" {
		return Build, true
	}
	_, ok := defaultConfigs[value]
	return value, ok
}

func ProfileFor(mode Mode) Profile {
	if config, ok := defaultConfigs[mode]; ok {
		return config.Profile()
	}
	return defaultConfigs[Build].Profile()
}

// EffectiveTimeout preserves an operator's shorter runtime cap and otherwise
// applies the Mode cap. Zero means uncapped.
func EffectiveTimeout(runtimeLimit, modeLimit time.Duration) time.Duration {
	if runtimeLimit <= 0 {
		return modeLimit
	}
	if modeLimit <= 0 || runtimeLimit < modeLimit {
		return runtimeLimit
	}
	return modeLimit
}
