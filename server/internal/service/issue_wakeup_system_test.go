package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func sub(stage int32, closed, cancelled bool) subIssue {
	s := subIssue{Closed: closed, Cancelled: cancelled}
	if stage > 0 {
		s.Stage = pgtype.Int4{Int32: stage, Valid: true}
	}
	return s
}

// The system rule's reading: a stage closing while a later one waits, then
// every sub-issue closing. People's children_done conditions keep theirs.
func TestStageProgress(t *testing.T) {
	cases := []struct {
		name        string
		children    []subIssue
		met         bool
		fingerprint string
		next        int32
	}{
		{"no sub-issues", nil, false, "", 0},
		{"unstaged, one open", []subIssue{sub(0, true, false), sub(0, false, false)}, false, "", 0},
		{"unstaged, all closed", []subIssue{sub(0, true, false), sub(0, true, true)}, true, "all", 0},
		{"stage 1 closed, stage 2 waits", []subIssue{sub(1, true, false), sub(1, true, true), sub(2, false, false)}, true, "stage:1", 2},
		{"two stages closed, stage 3 waits", []subIssue{sub(1, true, false), sub(2, true, false), sub(3, false, false)}, true, "stage:2", 3},
		{"a later stage closed first", []subIssue{sub(1, false, false), sub(2, true, false)}, false, "", 0},
		{"last stage closed, unstaged open", []subIssue{sub(1, true, false), sub(2, true, false), sub(0, false, false)}, false, "", 0},
		{"everything closed", []subIssue{sub(1, true, false), sub(2, true, true), sub(0, true, false)}, true, "all", 0},
		{"stage waits on unstaged-free set", []subIssue{sub(1, false, false), sub(0, true, false)}, false, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			met, fingerprint, observed := stageProgress(tc.children)
			if met != tc.met || fingerprint != tc.fingerprint {
				t.Fatalf("stageProgress = %v %q, want %v %q (%v)", met, fingerprint, tc.met, tc.fingerprint, observed)
			}
			if tc.next != 0 && observed["next_stage"] != tc.next {
				t.Fatalf("next_stage = %v, want %d", observed["next_stage"], tc.next)
			}
		})
	}
}

func TestStageProgressCountsCancellations(t *testing.T) {
	_, _, observed := stageProgress([]subIssue{sub(1, true, true), sub(1, true, false), sub(2, false, false)})
	stages := observed["stages"].([]stageCount)
	if observed["cancelled"] != 1 || len(stages) != 2 || stages[0] != (stageCount{Stage: 1, Total: 2, Closed: 2, Cancelled: 1}) {
		t.Fatalf("observed = %+v", observed)
	}
}

func TestChildrenDoneStageCondition(t *testing.T) {
	stage := int32(1)
	children := []subIssue{sub(1, true, false), sub(2, false, false), sub(0, false, false)}
	if met, _, _ := childrenDone(WakeupCondition{Type: "children_done", Stage: &stage}, children); !met {
		t.Fatal("stage 1 closed but the stage condition did not hold")
	}
	if met, _, _ := childrenDone(WakeupCondition{Type: "children_done"}, children); met {
		t.Fatal("the all-children condition held while sub-issues were open")
	}
	missing := int32(3)
	if met, _, _ := childrenDone(WakeupCondition{Type: "children_done", Stage: &missing}, children); met {
		t.Fatal("a stage without sub-issues held")
	}
}

func TestChildDoneInstructionPrecedence(t *testing.T) {
	settings := []byte(`{"system_wakeup_child_done":false,"system_wakeup_child_done_instruction":"  Workspace way  "}`)
	if enabled, instruction := SystemWakeupDefault(settings); enabled || instruction != "Workspace way" {
		t.Fatalf("default = %v %q", enabled, instruction)
	}
	if got := ChildDoneInstruction(" Issue way ", settings); got != "Issue way" {
		t.Fatalf("issue instruction = %q", got)
	}
	if got := ChildDoneInstruction("", settings); got != "Workspace way" {
		t.Fatalf("workspace instruction = %q", got)
	}
	if got := ChildDoneInstruction("", []byte(`{}`)); got != ChildDoneDefaultInstruction {
		t.Fatalf("built-in instruction = %q", got)
	}
	if enabled, _ := SystemWakeupDefault([]byte(`not json`)); !enabled {
		t.Fatal("unreadable settings must keep the rule on")
	}
}
