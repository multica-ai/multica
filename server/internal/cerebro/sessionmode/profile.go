// Package sessionmode defines the fixed execution profiles shared by Issue and Chat sessions.
package sessionmode

import (
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
	Mode          Mode
	ThinkingLevel string
	Timeout       time.Duration
	MaxTurns      int
	AllowsWrite   bool
	Instruction   string
}

var profiles = map[Mode]Profile{
	Plan: {
		Mode: Plan, ThinkingLevel: "medium", Timeout: 30 * time.Minute, MaxTurns: 20,
		Instruction: "This session is planning-only. You may investigate and save a concrete plan, but you must NOT write or edit code, run migrations, deploy, open a pull request, or make external mutations. Finish with a saved plan and its acceptance criteria.",
	},
	Build: {
		Mode: Build, ThinkingLevel: "high", Timeout: 120 * time.Minute, MaxTurns: 80, AllowsWrite: true,
		Instruction: "Implement the requested outcome within the approved scope. Follow test-driven development, preserve existing permissions and approvals, and finish only after fresh verification evidence.",
	},
	Research: {
		Mode: Research, ThinkingLevel: "high", Timeout: 60 * time.Minute, MaxTurns: 40,
		Instruction: "This session is a read-only investigation. You must not edit code or data, deploy, open or merge a pull request, or make external mutations. Cite sources, distinguish facts from inference, and state material uncertainty.",
	},
	Review: {
		Mode: Review, ThinkingLevel: "high", Timeout: 45 * time.Minute, MaxTurns: 30,
		Instruction: "Review only. You must NOT edit the reviewed code, merge, deploy, or make external mutations. Report findings by severity with concrete evidence and file/line references; say explicitly when no findings remain.",
	},
}

func Normalize(raw string) (Mode, bool) {
	value := Mode(strings.ToLower(strings.TrimSpace(raw)))
	if value == "default" || value == "auto" {
		return Build, true
	}
	_, ok := profiles[value]
	return value, ok
}

func ProfileFor(mode Mode) Profile {
	if profile, ok := profiles[mode]; ok {
		return profile
	}
	return profiles[Build]
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
