package issueworkflow

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// builtInMeanings say what each built-in status means for the issue. Custom
// statuses carry their catalog description instead.
var builtInMeanings = map[string]string{
	issuestatus.Backlog:    "Not planned yet.",
	issuestatus.Todo:       "Not started.",
	issuestatus.InProgress: "Work underway.",
	issuestatus.InReview:   "Delivered; awaiting review or acceptance.",
	issuestatus.Blocked:    "Can't proceed; something is missing.",
	issuestatus.Done:       "Finished and accepted.",
	issuestatus.Cancelled:  "Won't be done: dropped or a duplicate.",
}

// maxEntryHintRunes caps the line a step's instructions lend to its map
// entry; the full instructions are the handler's to read.
const maxEntryHintRunes = 160

// statusMapBrief renders the workflow as a status map: every status with its
// lifecycle category, what it means, who entering it hands off to and where
// its author routed the work on. The agent picks the status that matches
// where the issue stands, under the rules its runtime brief states, instead
// of following a move computed for each step (MUL-9913).
func statusMapBrief(in BriefInput) string {
	step, _ := in.Workflow.Step(in.StatusKey)
	stepMoved := in.BoundStep != "" && in.BoundStep != in.StatusKey
	terminal := in.atTerminal()
	terminalMoves := terminal && (step.NextStatusKey != "" || step.BackStatusKey != "")

	var b strings.Builder
	writeBriefTitle(&b, in)
	switch {
	case stepMoved:
		writeStepMovedIntro(&b, in)
	case in.Observer:
		writeObserverIntro(&b, in)
	case terminal && !terminalMoves:
		writeTerminalIntro(&b, in)
		writeStepInstructions(&b, step)
	case step.HandsOff():
		fmt.Fprintf(&b, "You are handling the %q step of %s.\n", in.statusName(in.StatusKey), in.IssueIdentifier)
		writeStepInstructions(&b, step)
	default:
		// A status without a handler is a progress marker, not a stage of
		// its own (MUL-8231).
		fmt.Fprintf(&b, "%s is at %s, which has no handler: the work the issue asks for is yours ",
			in.IssueIdentifier, statusLabel(in.statusName(in.StatusKey), in.StatusKey))
		if in.Workflow.HandsOffAnywhere() {
			b.WriteString("until it reaches a status that hands off.\n")
		} else {
			b.WriteString("to finish.\n")
		}
	}

	b.WriteString("\nStatus map:\n")
	for _, s := range in.Workflow.Steps {
		writeStatusMapEntry(&b, in, s)
	}
	if stepMoved {
		return b.String()
	}
	if in.Observer {
		b.WriteString("\nMoving the issue into a status that hands off reassigns it to that handler and starts their run.\n")
		return b.String()
	}
	if terminal && !terminalMoves {
		return b.String()
	}
	fmt.Fprintf(&b, "\nSet one with: multica issue status %s <status>\n", in.IssueIdentifier)
	return b.String()
}

// writeStatusMapEntry renders one status: its key, name and category and
// what it means on the first line, then who entering it hands off to and
// where the work goes from it.
func writeStatusMapEntry(b *strings.Builder, in BriefInput, s Step) {
	fmt.Fprintf(b, "- `%s`", s.StatusKey)
	if n := in.statusName(s.StatusKey); n != s.StatusKey {
		fmt.Fprintf(b, " %s", n)
	}
	if category := in.category(s.StatusKey); category != "" {
		fmt.Fprintf(b, " (%s)", category)
	}
	if meaning := in.meaning(s.StatusKey); meaning != "" {
		fmt.Fprintf(b, " — %s", meaning)
	}
	if s.StatusKey == in.StatusKey {
		b.WriteString("   ← current")
	}
	b.WriteString("\n")

	if who := in.handlerOf(s.StatusKey); who != "" {
		fmt.Fprintf(b, "    Hands off to %s.", who)
		// The current step's instructions are printed in full above; for any
		// other step, their first line says what its handler expects to
		// receive, which a workspace-wide description cannot.
		if s.StatusKey != in.StatusKey {
			if hint := firstLine(s.Instructions); hint != "" {
				fmt.Fprintf(b, " Their step: %s", hint)
			}
		}
		b.WriteString("\n")
	}
	var routes []string
	if next := in.routedNext(s); next != "" {
		routes = append(routes, fmt.Sprintf("next `%s`", next))
	}
	if s.BackStatusKey != "" {
		routes = append(routes, fmt.Sprintf("send back `%s`", s.BackStatusKey))
	}
	if len(routes) > 0 {
		fmt.Fprintf(b, "    Routes: %s.\n", strings.Join(routes, ", "))
	}
}

// routedNext is where finished work at s goes: its author's choice, or for a
// step that hands off, the next status on the board — the stage that follows
// it. A status without a handler names none; whoever owns the issue there
// works on through the map.
func (in BriefInput) routedNext(s Step) string {
	if s.NextStatusKey != "" {
		return s.NextStatusKey
	}
	if !s.HandsOff() {
		return ""
	}
	return in.Workflow.nextStatus(s.StatusKey, in.EffectiveStatus)
}

func (in BriefInput) category(key string) string {
	if in.StatusCategory != nil {
		if c := in.StatusCategory(key); c != "" {
			return c
		}
	}
	c, _ := issuestatus.CategoryForBehavior(key)
	return c
}

func (in BriefInput) meaning(key string) string {
	if m, ok := builtInMeanings[key]; ok {
		return m
	}
	if in.StatusDescription == nil {
		return ""
	}
	return briefLine(in.StatusDescription(key))
}

// firstLine is the first non-empty line of user-authored text, cut to
// maxEntryHintRunes.
func firstLine(s string) string {
	for _, line := range strings.Split(briefText(s), "\n") {
		line = briefLine(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > maxEntryHintRunes {
			line = string([]rune(line)[:maxEntryHintRunes-1]) + "…"
		}
		return line
	}
	return ""
}
