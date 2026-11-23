package issueworkflow

import (
	"strings"
	"testing"
)

// reviewedWithWontFix is the MUL-9913 board: a custom closed status sits
// after the review step, before blocked / done / cancelled.
func reviewedWithWontFix() BriefInput {
	def := Definition{Name: "Delivery", Steps: stepsFor(ArrangeKeys([]string{"in_progress", "in_review", "wont_fix", "blocked"}))}
	def = withHandoffs(def, []Step{{
		StatusKey: "in_review", Handler: Handler{Type: HandlerAgent, ID: testAgentID},
		Instructions: "\n Review the PR:\ttests and scope.\nThen approve.", BackStatusKey: "in_progress",
	}})
	return BriefInput{
		Workflow: def, ProjectName: "Payments", IssueIdentifier: "MUL-7",
		StatusName: func(key string) string {
			if key == "wont_fix" {
				return "Won't Fix"
			}
			return ""
		},
		StatusDescription: func(key string) string {
			if key == "wont_fix" {
				return "Decided not to do:\nduplicate or out of scope."
			}
			return "ignored for built-ins"
		},
		StatusCategory: func(key string) string {
			if key == "wont_fix" {
				return "closed"
			}
			return ""
		},
		EffectiveStatus: func(key string) string {
			if key == "wont_fix" {
				return "cancelled"
			}
			return key
		},
		HandlerName: func(step Step) string {
			if step.HandsOff() {
				return "Sentinel"
			}
			return ""
		},
	}
}

func TestStatusMapDescribesEveryStatus(t *testing.T) {
	in := reviewedWithWontFix()
	in.StatusKey = "in_progress"
	brief := Brief(in)
	for _, want := range []string{
		"This project's workflow — Delivery (Payments)\n",
		`MUL-7 is at "in_progress", which has no handler: the work the issue asks for is yours until it reaches a status that hands off.` + "\n",
		"\nStatus map:\n",
		"- `in_progress` (started) — Work underway.   ← current\n",
		"- `in_review` (started) — Delivered; awaiting review or acceptance.\n" +
			"    Hands off to Sentinel. Their step: Review the PR: tests and scope.\n" +
			"    Routes: next `done`, send back `in_progress`.\n",
		// The custom status carries its category and catalog description, so
		// the agent can tell when it fits.
		"- `wont_fix` Won't Fix (closed) — Decided not to do: duplicate or out of scope.\n",
		"- `done` (done) — Finished and accepted.\n",
		"- `cancelled` (closed) — Won't be done: dropped or a duplicate.\n",
		"\nSet one with: multica issue status MUL-7 <status>\n",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief is missing %q:\n%s", want, brief)
		}
	}
	// No move is computed for the agent: nothing routes work into wont_fix.
	for _, unwanted := range []string{"Your moves:", "Step done", "Starting work", "`wont_fix`.", "ignored for built-ins"} {
		if strings.Contains(brief, unwanted) {
			t.Errorf("brief carries %q:\n%s", unwanted, brief)
		}
	}
}

func TestStatusMapForTheStepYouHandle(t *testing.T) {
	in := reviewedWithWontFix()
	in.StatusKey = "in_review"
	brief := Brief(in)
	for _, want := range []string{
		`You are handling the "in_review" step of MUL-7.` + "\n",
		"\nStep instructions:\n> Review the PR:\ttests and scope.\n> Then approve.\n",
		"awaiting review or acceptance.   ← current\n    Hands off to Sentinel.\n",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief is missing %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "Their step:") {
		t.Errorf("the current step's instructions are already printed in full:\n%s", brief)
	}
}

func TestStatusMapRoutesOnlyWhatItsAuthorOrABoardStageDecides(t *testing.T) {
	in := reviewedWithWontFix()
	in.StatusKey = "in_progress"
	// A step that hands off falls back to the next stage on the board,
	// passing over the closed status; an author's own choice is kept.
	in.Workflow = withHandoffs(in.Workflow, []Step{{StatusKey: "in_review", Handler: Handler{Type: HandlerAgent, ID: testAgentID}}})
	if brief := Brief(in); !strings.Contains(brief, "    Routes: next `done`.\n") {
		t.Errorf("board fallback must skip the closed status:\n%s", brief)
	}
	in.Workflow = withHandoffs(in.Workflow, []Step{{StatusKey: "in_review", Handler: Handler{Type: HandlerAgent, ID: testAgentID}, NextStatusKey: "wont_fix"}})
	if brief := Brief(in); !strings.Contains(brief, "    Routes: next `wont_fix`.\n") {
		t.Errorf("a configured Next must be kept:\n%s", brief)
	}
	// Statuses without a handler route nothing: their owner works on.
	if brief := Brief(in); strings.Contains(brief, "underway.   ← current\n    Routes") {
		t.Errorf("a status without a handler must not route work:\n%s", brief)
	}
}

func TestStatusMapWithoutHandoffsLeavesTheWorkToFinish(t *testing.T) {
	def := Definition{Name: "Plain", Steps: stepsFor(ArrangeKeys([]string{"in_progress"}))}
	brief := Brief(BriefInput{Workflow: def, StatusKey: "todo", IssueIdentifier: "MUL-7"})
	if !strings.Contains(brief, `MUL-7 is at "todo", which has no handler: the work the issue asks for is yours to finish.`) {
		t.Fatalf("brief must leave the work to finish:\n%s", brief)
	}
	if strings.Contains(brief, "hands off") {
		t.Fatalf("nothing hands off in this workflow:\n%s", brief)
	}
}

func TestStatusMapForObserversAndTerminalStatuses(t *testing.T) {
	in := reviewedWithWontFix()
	in.StatusKey = "in_review"
	in.Observer = true
	in.AssigneeName = "Sentinel"
	observer := Brief(in)
	for _, want := range []string{
		`MUL-7 is at the "in_review" step and is assigned to Sentinel. You are not its assignee`,
		"\nStatus map:\n",
		"\nMoving the issue into a status that hands off reassigns it to that handler and starts their run.\n",
	} {
		if !strings.Contains(observer, want) {
			t.Errorf("observer brief is missing %q:\n%s", want, observer)
		}
	}
	if strings.Contains(observer, "Set one with") || strings.Contains(observer, "Step instructions") {
		t.Errorf("observer brief must leave the status to the assignee:\n%s", observer)
	}

	in = reviewedWithWontFix()
	in.StatusKey = "wont_fix"
	terminal := Brief(in)
	if !strings.Contains(terminal, `MUL-7 has reached the terminal status "Won't Fix" (wont_fix). Leave the issue at this status`) ||
		!strings.Contains(terminal, "(closed) — Decided not to do: duplicate or out of scope.   ← current\n") ||
		strings.Contains(terminal, "Set one with") {
		t.Errorf("terminal brief must keep the issue where it is:\n%s", terminal)
	}
}

func TestStatusMapCutsLongInstructionHints(t *testing.T) {
	in := reviewedWithWontFix()
	in.StatusKey = "in_progress"
	in.Workflow = withHandoffs(in.Workflow, []Step{{
		StatusKey: "in_review", Handler: Handler{Type: HandlerAgent, ID: testAgentID},
		Instructions: strings.Repeat("a", maxEntryHintRunes+20) + "\x01",
	}})
	brief := Brief(in)
	want := "Their step: " + strings.Repeat("a", maxEntryHintRunes-1) + "…\n"
	if !strings.Contains(brief, want) {
		t.Fatalf("hint must be cut to %d runes:\n%s", maxEntryHintRunes, brief)
	}
}
