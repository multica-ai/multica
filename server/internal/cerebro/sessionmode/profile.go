// Package sessionmode defines the fixed execution profiles shared by Issue and Chat sessions.
package sessionmode

import (
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
	Mode           Mode
	Version        string
	Model          string
	ThinkingLevel  string
	Timeout        time.Duration
	MaxTurns       int
	AllowsWrite    bool
	Instruction    string
	AllowedTools   []string
	DataSources    []string
	ApprovalPolicy string
	WorkflowID     string
	EvalSkillIDs   []string
}

// Config is the persisted, interface-editable shape of a Mode profile.
// Durations are stored as minutes so snapshots stay provider-neutral JSON.
type Config struct {
	Mode           Mode     `json:"mode"`
	Version        string   `json:"version,omitempty"`
	Instruction    string   `json:"instruction"`
	Model          string   `json:"model,omitempty"`
	ThinkingLevel  string   `json:"thinking_level"`
	TimeoutMinutes int      `json:"timeout_minutes"`
	MaxTurns       int      `json:"max_turns"`
	AllowsWrite    bool     `json:"allows_write"`
	AllowedTools   []string `json:"allowed_tools"`
	DataSources    []string `json:"data_sources"`
	ApprovalPolicy string   `json:"approval_policy"`
	WorkflowID     string   `json:"workflow_id,omitempty"`
	EvalSkillIDs   []string `json:"eval_skill_ids"`
}

var defaultConfigs = map[Mode]Config{
	Plan: {
		Mode: Plan, Version: "1", ThinkingLevel: "medium", TimeoutMinutes: 30, MaxTurns: 20, ApprovalPolicy: "inherit",
		Instruction: "This session is planning-only. You may investigate and save a concrete plan, but you must NOT write or edit code, run migrations, deploy, open a pull request, or make external mutations. Finish with a saved plan and its acceptance criteria.",
	},
	Build: {
		Mode: Build, Version: "1", ThinkingLevel: "high", TimeoutMinutes: 120, MaxTurns: 80, AllowsWrite: true, ApprovalPolicy: "inherit",
		Instruction: "Implement the requested outcome within the approved scope. Follow test-driven development, preserve existing permissions and approvals, and finish only after fresh verification evidence.",
	},
	Research: {
		Mode: Research, Version: "1", ThinkingLevel: "high", TimeoutMinutes: 60, MaxTurns: 40, ApprovalPolicy: "inherit",
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
		MaxTurns: c.MaxTurns, AllowsWrite: c.AllowsWrite, Instruction: c.Instruction,
		AllowedTools: append([]string(nil), c.AllowedTools...), DataSources: append([]string(nil), c.DataSources...),
		ApprovalPolicy: c.ApprovalPolicy, WorkflowID: c.WorkflowID, EvalSkillIDs: append([]string(nil), c.EvalSkillIDs...),
	}
}

func DefaultConfigs() map[Mode]Config {
	out := make(map[Mode]Config, len(defaultConfigs))
	for mode, config := range defaultConfigs {
		config.AllowedTools = append([]string(nil), config.AllowedTools...)
		config.DataSources = append([]string(nil), config.DataSources...)
		config.EvalSkillIDs = append([]string(nil), config.EvalSkillIDs...)
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
	if config.TimeoutMinutes < 1 || config.TimeoutMinutes > 1440 {
		return fmt.Errorf("timeout_minutes must be between 1 and 1440")
	}
	if config.MaxTurns < 1 || config.MaxTurns > 200 {
		return fmt.Errorf("max_turns must be between 1 and 200")
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
