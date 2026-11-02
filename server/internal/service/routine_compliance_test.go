package service

import (
	"testing"
	"time"
)

// complianceNow is a fixed clock: 2 November 2026, noon UTC.
var complianceNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// dailyRoutine is an active routine on "0 9 * * *" (UTC) whose last fire was 31
// October at 09:00, so the next fire is 1 November at 09:00. At noon on 2
// November that fire is 27 hours overdue.
func dailyRoutine(expected string) routineFacts {
	return routineFacts{
		autopilotStatus:  "active",
		expectedOutput:   expected,
		graceHours:       24,
		hasSchedule:      true,
		cronExpression:   "0 9 * * *",
		timezone:         "UTC",
		scheduleBaseline: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC),
	}
}

// onTimeRoutine is dailyRoutine with its fire inside the grace: the last fire
// was 2 November at 09:00, three hours before now.
func onTimeRoutine(expected string) routineFacts {
	f := dailyRoutine(expected)
	f.scheduleBaseline = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	return f
}

func completedDecider(f routineFacts) routineFacts {
	f.hasDecider = true
	f.deciderStatus = "completed"
	return f
}

func TestRoutineStateOnePerState(t *testing.T) {
	cases := []struct {
		name string
		f    routineFacts
		want string
	}{
		{
			name: "paused autopilot is paused even when overdue",
			f: func() routineFacts {
				f := dailyRoutine("none")
				f.autopilotStatus = "paused"
				return f
			}(),
			want: RoutineStatePaused,
		},
		{
			name: "completed run with nothing produced is no_output",
			f:    completedDecider(onTimeRoutine("proposal")),
			want: RoutineStateNoOutput,
		},
		{
			name: "fire overdue beyond grace is late",
			f:    completedDecider(dailyRoutine("none")),
			want: RoutineStateLate,
		},
		{
			name: "latest finished run failed",
			f: func() routineFacts {
				f := onTimeRoutine("none")
				f.hasDecider = true
				f.deciderStatus = "failed"
				return f
			}(),
			want: RoutineStateFailed,
		},
		{
			name: "no finished run and the first fire has not come",
			f: func() routineFacts {
				f := onTimeRoutine("none")
				f.scheduleBaseline = complianceNow.Add(-time.Hour)
				return f
			}(),
			want: RoutineStateNeverRan,
		},
		{
			name: "completed run for a no-output kind is on_time when the output exists",
			f: func() routineFacts {
				f := completedDecider(onTimeRoutine("issue"))
				f.hasIssue = true
				return f
			}(),
			want: RoutineStateOnTime,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveRoutineState(tc.f, complianceNow); got != tc.want {
				t.Fatalf("state = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestRoutineStateExpectedOutputPerKind: each expected kind is satisfied by its
// own evidence and by nothing else. A "none" routine is decided by its run alone.
func TestRoutineStateExpectedOutputPerKind(t *testing.T) {
	kinds := []struct {
		kind string
		set  func(*routineFacts, bool)
	}{
		{"issue", func(f *routineFacts, v bool) { f.hasIssue = v }},
		{"proposal", func(f *routineFacts, v bool) { f.hasProposal = v }},
		{"kb_document", func(f *routineFacts, v bool) { f.hasKBDocument = v }},
		{"report", func(f *routineFacts, v bool) { f.hasReport = v }},
	}
	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			base := completedDecider(onTimeRoutine(k.kind))
			if got := deriveRoutineState(base, complianceNow); got != RoutineStateNoOutput {
				t.Fatalf("%s without its output: state = %s, want no_output", k.kind, got)
			}

			own := base
			k.set(&own, true)
			if got := deriveRoutineState(own, complianceNow); got != RoutineStateOnTime {
				t.Fatalf("%s with its output: state = %s, want on_time", k.kind, got)
			}

			// Every other kind's evidence, with this kind's left out.
			others := completedDecider(onTimeRoutine(k.kind))
			for _, o := range kinds {
				if o.kind != k.kind {
					o.set(&others, true)
				}
			}
			if got := deriveRoutineState(others, complianceNow); got != RoutineStateNoOutput {
				t.Fatalf("%s must not be satisfied by another kind's output: state = %s", k.kind, got)
			}
		})
	}

	t.Run("none is decided by the run alone", func(t *testing.T) {
		if got := deriveRoutineState(completedDecider(onTimeRoutine("none")), complianceNow); got != RoutineStateOnTime {
			t.Fatalf("state = %s, want on_time", got)
		}
	})
}

// TestRoutineStateNoActionWithReasonIsOnTime: an honest no action, recorded with a
// reason, is a delivered routine for any kind.
func TestRoutineStateNoActionWithReasonIsOnTime(t *testing.T) {
	for _, kind := range []string{"issue", "proposal", "kb_document", "report"} {
		f := completedDecider(onTimeRoutine(kind))
		f.hasNoAction = true
		if got := deriveRoutineState(f, complianceNow); got != RoutineStateOnTime {
			t.Fatalf("%s with no action recorded: state = %s, want on_time", kind, got)
		}
	}
}

// TestRoutineStateLateHonoursGrace: the same overdue fire is late with a 24 hour
// grace and not late with 48, which the 27 hour lateness fits inside.
func TestRoutineStateLateHonoursGrace(t *testing.T) {
	f := completedDecider(dailyRoutine("none"))
	if got := deriveRoutineState(f, complianceNow); got != RoutineStateLate {
		t.Fatalf("grace 24h, fire 27h overdue: state = %s, want late", got)
	}
	f.graceHours = 48
	if got := deriveRoutineState(f, complianceNow); got != RoutineStateOnTime {
		t.Fatalf("grace 48h, fire 27h overdue: state = %s, want on_time", got)
	}
}

// TestRoutineStateLateBeforeAnyRunAndNeverRanBeforeFirstFire: a schedule that has
// never fired counts from its creation. Created a week ago it is late; created an
// hour ago it has never run.
func TestRoutineStateLateBeforeAnyRunAndNeverRanBeforeFirstFire(t *testing.T) {
	created := dailyRoutine("none")
	created.scheduleBaseline = complianceNow.Add(-7 * 24 * time.Hour)
	if got := deriveRoutineState(created, complianceNow); got != RoutineStateLate {
		t.Fatalf("created a week ago with no run: state = %s, want late", got)
	}

	fresh := dailyRoutine("none")
	fresh.scheduleBaseline = complianceNow.Add(-time.Hour)
	if got := deriveRoutineState(fresh, complianceNow); got != RoutineStateNeverRan {
		t.Fatalf("created an hour ago: state = %s, want never_ran", got)
	}
}

// TestRoutineStatePrecedence: paused is not judged; late wins over a finished
// run; a run that finished decides the rest.
func TestRoutineStatePrecedence(t *testing.T) {
	failedOverdue := dailyRoutine("none")
	failedOverdue.hasDecider = true
	failedOverdue.deciderStatus = "failed"
	if got := deriveRoutineState(failedOverdue, complianceNow); got != RoutineStateLate {
		t.Fatalf("overdue after a failed run: state = %s, want late", got)
	}

	paused := failedOverdue
	paused.autopilotStatus = "paused"
	if got := deriveRoutineState(paused, complianceNow); got != RoutineStatePaused {
		t.Fatalf("paused overrides every other state: got %s", got)
	}
}

// TestRoutineStateWithoutSchedule: a routine whose autopilot has no enabled
// schedule is never late; it is judged by its runs only.
func TestRoutineStateWithoutSchedule(t *testing.T) {
	never := routineFacts{autopilotStatus: "active", expectedOutput: "none", graceHours: 24}
	if got := deriveRoutineState(never, complianceNow); got != RoutineStateNeverRan {
		t.Fatalf("no schedule, no run: state = %s, want never_ran", got)
	}
	ran := never
	ran.hasDecider, ran.deciderStatus = true, "completed"
	if got := deriveRoutineState(ran, complianceNow); got != RoutineStateOnTime {
		t.Fatalf("no schedule, completed run: state = %s, want on_time", got)
	}
}

// TestRoutineStateOperatingReviewOutput: the review's expected output is a report,
// but a proposal the squad filed during its run is the review's output too. A
// no action with a reason is on_time for any kind. Another routine with only a
// proposal still needs its own report.
func TestRoutineStateOperatingReviewOutput(t *testing.T) {
	review := func() routineFacts {
		f := completedDecider(onTimeRoutine("report"))
		f.operatingReview = true
		return f
	}

	if got := deriveRoutineState(review(), complianceNow); got != RoutineStateNoOutput {
		t.Fatalf("review that filed nothing: state = %s, want no_output", got)
	}

	withProposal := review()
	withProposal.hasProposal = true
	if got := deriveRoutineState(withProposal, complianceNow); got != RoutineStateOnTime {
		t.Fatalf("review that filed a proposal and no comment: state = %s, want on_time", got)
	}

	withNoAction := review()
	withNoAction.hasNoAction = true
	if got := deriveRoutineState(withNoAction, complianceNow); got != RoutineStateOnTime {
		t.Fatalf("review that recorded no action with a reason: state = %s, want on_time", got)
	}

	other := completedDecider(onTimeRoutine("report"))
	other.hasProposal = true
	if got := deriveRoutineState(other, complianceNow); got != RoutineStateNoOutput {
		t.Fatalf("non-review report routine with only a proposal: state = %s, want no_output", got)
	}
}
