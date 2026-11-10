package wecom

import (
	"regexp"
	"strings"
	"time"
)

// progress_export.go — the step vocabulary for adapters that show a run's
// steps somewhere other than a WeCom bubble.
//
// What a tool call is, which of its arguments names the work, and what has to
// be cut or masked before a chat may show it are all decided in
// progress_render.go and progress_secrets.go. Another adapter that worded steps
// itself would have to keep its own copy of the tool table and the redactor in
// step with these, so it asks here instead.

// StepKind is what sort of work a step is, for an adapter that marks each
// sort its own way.
type StepKind string

const (
	StepStarting StepKind = "starting"
	StepRead     StepKind = "read"
	StepEdit     StepKind = "edit"
	StepCommand  StepKind = "command"
	StepSearch   StepKind = "search"
	StepWeb      StepKind = "web"
	StepSubtask  StepKind = "subtask"
	StepPlan     StepKind = "plan"
	StepService  StepKind = "service"
	StepSkill    StepKind = "skill"
	StepTool     StepKind = "tool"
	StepError    StepKind = "error"
)

// Step is one step of a run, worded and safe to show.
type Step struct {
	Kind StepKind
	Text string
}

// DescribeStep words one task:message as a step of the run, in the given
// locale. It reports false for a message that is not a step: prose, a tool's
// output, reasoning.
//
// withArgument false leaves out everything the agent composed — the path, the
// command line, the search term, an error's text — and keeps the kind of work
// and the identifiers the workspace configured, for a reader who is not the
// one the run belongs to.
func DescribeStep(payload any, l Locale, withArgument bool) (Step, bool) {
	msg := taskMessageFields(payload)
	if msg.msgType != "tool_use" && msg.msgType != "error" {
		return Step{}, false
	}
	step, ok := stepFromMessage(msg)
	if !ok {
		return Step{}, false
	}
	if !withArgument {
		step.args = ""
		named := step.kind == progressService ||
			(step.kind == progressTool && toolIdentifier.MatchString(step.arg))
		if !named {
			step.arg = ""
		}
	}
	text := strings.TrimSpace(step.line(copyFor(l), progressLevelDetail))
	if text == "" {
		return Step{}, false
	}
	return Step{Kind: exportedKind(step.kind), Text: text}, true
}

// toolIdentifier is a tool's own name: `frobnicate`, `web.run`. Some agent
// backends pass a call's title as its name instead, and the agent composed the
// title: "Delete /srv/acme/customers.csv".
var toolIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,48}$`)

// StartingStep is the run getting under way, before its first tool call.
func StartingStep(l Locale) Step {
	return Step{Kind: StepStarting, Text: copyFor(l).Progress.Starting}
}

// FormatElapsed renders how long a run has taken, the way the bubble and the
// web chat's status pill do.
func FormatElapsed(d time.Duration) string { return formatElapsed(d) }

func exportedKind(k progressKind) StepKind {
	switch k {
	case progressStarting:
		return StepStarting
	case progressRead:
		return StepRead
	case progressEdit:
		return StepEdit
	case progressCommand:
		return StepCommand
	case progressSearch:
		return StepSearch
	case progressWeb:
		return StepWeb
	case progressSubtask:
		return StepSubtask
	case progressPlan:
		return StepPlan
	case progressService:
		return StepService
	case progressSkill:
		return StepSkill
	case progressError:
		return StepError
	}
	return StepTool
}
