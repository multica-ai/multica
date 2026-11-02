package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// complianceRoutine creates a daily UTC routine on a fresh squad through the
// API, so the autopilot, trigger and link are the ones a user would get.
func complianceRoutine(t *testing.T, expected string, over ...map[string]any) (squadID, autopilotID string) {
	t.Helper()
	squadID = newRoutineSquad(t, "Compliance "+uuid.NewString()[:8])
	body := map[string]any{
		"cadence":         "daily",
		"timezone":        "UTC",
		"purpose":         "Daily check",
		"expected_output": expected,
	}
	for _, o := range over {
		for k, v := range o {
			body[k] = v
		}
	}
	var got SquadRoutineResponse
	createRoutineAs(t, "", squadID, body).Want(http.StatusCreated).JSON(&got)
	cleanupRoutineAutopilot(t, got.AutopilotID)
	return squadID, got.AutopilotID
}

// routineStateOf reads the squad's routines and returns the state of one.
func routineStateOf(t *testing.T, squadID, autopilotID string) string {
	t.Helper()
	var routines []SquadRoutineResponse
	listRoutinesAs(t, "", squadID).Want(http.StatusOK).JSON(&routines)
	for _, r := range routines {
		if r.AutopilotID == autopilotID {
			return r.State
		}
	}
	t.Fatalf("routine %s not listed for squad %s", autopilotID, squadID)
	return ""
}

func squadLeaderID(t *testing.T, squadID string) string {
	t.Helper()
	var id string
	dbfx.QueryRow(t, `SELECT leader_id::text FROM squad WHERE id = $1`, squadID).Scan(&id)
	return id
}

// insertRoutineRun records one run of the autopilot. The defaults describe a run
// triggered an hour ago that completed half an hour ago.
func insertRoutineRun(t *testing.T, squadID, autopilotID string, over ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{
		"autopilot_id": autopilotID,
		"squad_id":     squadID,
		"source":       "schedule",
		"status":       "completed",
		"triggered_at": testutil.Raw("now() - interval '1 hour'"),
		"completed_at": testutil.Raw("now() - interval '30 minutes'"),
		"created_at":   testutil.Raw("now() - interval '1 hour'"),
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return dbfx.Insert(t, "autopilot_run", cols)
}

// TestRoutineStateNeverRanAndPaused: a fresh routine has not run, and a paused
// autopilot is paused whatever its history.
func TestRoutineStateNeverRanAndPaused(t *testing.T) {
	squadID, autopilotID := complianceRoutine(t, "none")
	if got := routineStateOf(t, squadID, autopilotID); got != "never_ran" {
		t.Fatalf("fresh routine: state = %s, want never_ran", got)
	}

	insertRoutineRun(t, squadID, autopilotID, testutil.Cols{"status": "failed"})
	dbfx.Exec(t, `UPDATE autopilot SET status = 'paused' WHERE id = $1`, autopilotID)
	if got := routineStateOf(t, squadID, autopilotID); got != "paused" {
		t.Fatalf("paused routine with a failed run: state = %s, want paused", got)
	}
}

// TestRoutineStateOnTimeForNoneRun: a completed run decides a "none" routine.
func TestRoutineStateOnTimeForNoneRun(t *testing.T) {
	squadID, autopilotID := complianceRoutine(t, "none")
	insertRoutineRun(t, squadID, autopilotID)
	if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
		t.Fatalf("completed run, expected none: state = %s, want on_time", got)
	}
}

// TestRoutineStateFailedRunAndPolicySkips: a failed run, or a skip that is not a
// policy hold, is failed. A policy skip or a run still in flight does not decide
// the state, so the latest finished run before it still does.
func TestRoutineStateFailedRunAndPolicySkips(t *testing.T) {
	t.Run("failed", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "none")
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{"status": "failed"})
		if got := routineStateOf(t, squadID, autopilotID); got != "failed" {
			t.Fatalf("failed run: state = %s, want failed", got)
		}
	})

	t.Run("skipped for an operational reason", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "none")
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{
			"status": "skipped", "reason_code": "runtime_offline",
		})
		if got := routineStateOf(t, squadID, autopilotID); got != "failed" {
			t.Fatalf("skipped for runtime_offline: state = %s, want failed", got)
		}
	})

	t.Run("policy skip and in-flight run do not decide", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "none")
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{
			"triggered_at": testutil.Raw("now() - interval '3 hours'"),
			"created_at":   testutil.Raw("now() - interval '3 hours'"),
		})
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{
			"status": "skipped", "reason_code": "quota_exceeded",
			"created_at": testutil.Raw("now() - interval '2 hours'"),
		})
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{
			"status": "running", "completed_at": nil,
			"created_at": testutil.Raw("now() - interval '10 minutes'"),
		})
		if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
			t.Fatalf("earlier completed run with later policy skip and in-flight run: state = %s, want on_time", got)
		}
	})
}

// TestRoutineStateLateHonoursGrace: a schedule that has never fired and was created
// five days ago is late under a one day grace, and not late under a fourteen day one.
func TestRoutineStateLateHonoursGrace(t *testing.T) {
	t.Run("overdue beyond grace", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "none")
		dbfx.Exec(t, `UPDATE autopilot_trigger SET created_at = now() - interval '5 days' WHERE autopilot_id = $1`, autopilotID)
		if got := routineStateOf(t, squadID, autopilotID); got != "late" {
			t.Fatalf("five days overdue on a daily routine: state = %s, want late", got)
		}
	})

	t.Run("within a long grace", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "none", map[string]any{"grace_hours": 336})
		dbfx.Exec(t, `UPDATE autopilot_trigger SET created_at = now() - interval '5 days' WHERE autopilot_id = $1`, autopilotID)
		if got := routineStateOf(t, squadID, autopilotID); got != "never_ran" {
			t.Fatalf("five days overdue inside a 14 day grace: state = %s, want never_ran", got)
		}
	})

	t.Run("a completed run after the last fire is not late", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "none")
		dbfx.Exec(t, `UPDATE autopilot_trigger SET created_at = now() - interval '5 days' WHERE autopilot_id = $1`, autopilotID)
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{
			"planned_at":   testutil.Raw("now() - interval '5 days'"),
			"triggered_at": testutil.Raw("now() - interval '5 days'"),
			"created_at":   testutil.Raw("now() - interval '5 days'"),
			"completed_at": testutil.Raw("now() - interval '5 days' + interval '5 minutes'"),
		})
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{
			"planned_at": testutil.Raw("now() - interval '2 hours'"),
		})
		if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
			t.Fatalf("latest scheduled fire dispatched within the grace: state = %s, want on_time", got)
		}
	})
}

// TestRoutineStateProposalKindFollowsTheRunWindow: a proposal counts for a
// proposal routine only when the squad filed it after the run started.
func TestRoutineStateProposalKindFollowsTheRunWindow(t *testing.T) {
	squadID, autopilotID := complianceRoutine(t, "proposal")
	leader := squadLeaderID(t, squadID)
	insertRoutineRun(t, squadID, autopilotID)
	if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
		t.Fatalf("completed run, no proposal: state = %s, want no_output", got)
	}

	// Filed before the run started: not this run's output.
	earlier := dbfx.Issue(t, "earlier proposal", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
	dbfx.Insert(t, "proposal", testutil.Cols{
		"workspace_id": dbfx.WorkspaceID, "squad_id": squadID, "issue_id": earlier,
		"created_by_type": "agent", "created_by_id": leader,
		"created_at": testutil.Raw("now() - interval '2 hours'"),
	})
	if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
		t.Fatalf("proposal filed before the run: state = %s, want no_output", got)
	}

	issue := dbfx.Issue(t, "new proposal", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
	dbfx.Insert(t, "proposal", testutil.Cols{
		"workspace_id": dbfx.WorkspaceID, "squad_id": squadID, "issue_id": issue,
		"created_by_type": "agent", "created_by_id": leader,
	})
	if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
		t.Fatalf("proposal filed by the squad during the run: state = %s, want on_time", got)
	}
}

// TestRoutineStateKnowledgeKindCountsSquadAgentsOnly: a knowledge document counts
// when an agent of the squad wrote it, and not when an outsider did.
func TestRoutineStateKnowledgeKindCountsSquadAgentsOnly(t *testing.T) {
	squadID, autopilotID := complianceRoutine(t, "kb_document")
	insertRoutineRun(t, squadID, autopilotID)

	outsider := createHandlerTestAgent(t, "compliance outsider "+uuid.NewString()[:8], nil)
	addKnowledgeLink(t, "agent", outsider)
	if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
		t.Fatalf("document by an agent outside the squad: state = %s, want no_output", got)
	}

	member := createHandlerTestAgent(t, "compliance member "+uuid.NewString()[:8], nil)
	dbfx.SquadMember(t, squadID, "agent", member)
	addKnowledgeLink(t, "agent", member)
	if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
		t.Fatalf("document by a squad member agent: state = %s, want on_time", got)
	}
}

// addKnowledgeLink records a published knowledge document by the given creator.
func addKnowledgeLink(t *testing.T, createdByType, createdByID string) {
	t.Helper()
	dbfx.Insert(t, "knowledge_link", testutil.Cols{
		"workspace_id":         dbfx.WorkspaceID,
		"knowledge_base_id":    uuid.NewString(),
		"weknora_knowledge_id": uuid.NewString(),
		"source_type":          "issue",
		"idempotency_key":      uuid.NewString(),
		"state":                "published",
		"created_by_type":      createdByType,
		"created_by_id":        createdByID,
	})
}

// TestRoutineStateIssueAndReportKinds: an issue kind is satisfied by the issue the
// run created; a report kind by a comment the squad's agent left on that issue.
func TestRoutineStateIssueAndReportKinds(t *testing.T) {
	t.Run("issue", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "issue")
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{"status": "completed"})
		if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
			t.Fatalf("run without an issue: state = %s, want no_output", got)
		}
		issue := dbfx.Issue(t, "run issue", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{"issue_id": issue})
		if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
			t.Fatalf("run that created an issue: state = %s, want on_time", got)
		}
	})

	t.Run("report", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "report")
		leader := squadLeaderID(t, squadID)
		issue := dbfx.Issue(t, "report issue", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
		insertRoutineRun(t, squadID, autopilotID, testutil.Cols{"issue_id": issue})

		// A member's comment is not the squad's report.
		dbfx.Comment(t, issue, "a member's note")
		if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
			t.Fatalf("member comment only: state = %s, want no_output", got)
		}
		dbfx.Comment(t, issue, "weekly report", testutil.Cols{"author_type": "agent", "author_id": leader})
		if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
			t.Fatalf("leader comment on the run's issue: state = %s, want on_time", got)
		}
	})
}

// TestRoutineStateNoActionNeedsAReason: a recorded no action with a reason is a
// delivered routine; without a reason it is not.
func TestRoutineStateNoActionNeedsAReason(t *testing.T) {
	squadID, autopilotID := complianceRoutine(t, "proposal")
	leader := squadLeaderID(t, squadID)
	issue := dbfx.Issue(t, "no action issue", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
	insertRoutineRun(t, squadID, autopilotID, testutil.Cols{"issue_id": issue})

	recordNoAction := func(details string) {
		dbfx.Insert(t, "activity_log", testutil.Cols{
			"workspace_id": dbfx.WorkspaceID,
			"issue_id":     issue,
			"actor_type":   "agent",
			"actor_id":     leader,
			"action":       "squad_leader_evaluated",
			"details":      details,
		})
	}
	recordNoAction(`{"outcome":"no_action","reason":"   "}`)
	if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
		t.Fatalf("no action without a reason: state = %s, want no_output", got)
	}
	recordNoAction(`{"outcome":"no_action","reason":"nothing new since last week"}`)
	if got := routineStateOf(t, squadID, autopilotID); got != "on_time" {
		t.Fatalf("no action with a reason: state = %s, want on_time", got)
	}
}

// TestOperatingReviewCountsAProposalOrANoAction: the weekly review is delivered by a
// proposal it files or a no action with a reason, with or without a comment. A
// routine with another purpose still needs its own report.
func TestOperatingReviewCountsAProposalOrANoAction(t *testing.T) {
	// reviewRun creates a squad with its operating review and one completed run of
	// the review, on an issue the run created.
	reviewRun := func(t *testing.T) (squadID, reviewID, issue string) {
		t.Helper()
		leaderID := createHandlerTestAgent(t, "review-output-leader-"+uniqueSuffix(), nil)
		squadID = newCharterSquad(t, "Review Output Squad", leaderID)
		saveCharterWithSwitch(t, squadID, 0, true)
		reviewID = operatingReviewOf(t, squadID)
		issue = dbfx.Issue(t, "review run", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
		insertRoutineRun(t, squadID, reviewID, testutil.Cols{"issue_id": issue})
		return squadID, reviewID, issue
	}

	fileProposal := func(t *testing.T, squadID string) {
		t.Helper()
		leader := squadLeaderID(t, squadID)
		proposalIssue := dbfx.Issue(t, "review proposal", testutil.Cols{"workspace_id": dbfx.WorkspaceID})
		dbfx.Insert(t, "proposal", testutil.Cols{
			"workspace_id": dbfx.WorkspaceID, "squad_id": squadID, "issue_id": proposalIssue,
			"created_by_type": "agent", "created_by_id": leader,
		})
	}

	t.Run("nothing filed is no_output", func(t *testing.T) {
		squadID, reviewID, _ := reviewRun(t)
		if got := routineStateOf(t, squadID, reviewID); got != "no_output" {
			t.Fatalf("review with nothing recorded: state = %s, want no_output", got)
		}
	})

	t.Run("proposal filed without a comment is on_time", func(t *testing.T) {
		squadID, reviewID, _ := reviewRun(t)
		fileProposal(t, squadID)
		if got := routineStateOf(t, squadID, reviewID); got != "on_time" {
			t.Fatalf("review that filed a proposal and posted no comment: state = %s, want on_time", got)
		}
	})

	t.Run("no action with a reason without a comment is on_time", func(t *testing.T) {
		squadID, reviewID, issue := reviewRun(t)
		dbfx.Insert(t, "activity_log", testutil.Cols{
			"workspace_id": dbfx.WorkspaceID,
			"issue_id":     issue,
			"actor_type":   "agent",
			"actor_id":     squadLeaderID(t, squadID),
			"action":       "squad_leader_evaluated",
			"details":      `{"outcome":"no_action","reason":"nothing new this week"}`,
		})
		if got := routineStateOf(t, squadID, reviewID); got != "on_time" {
			t.Fatalf("review that recorded no action with a reason: state = %s, want on_time", got)
		}
	})

	t.Run("another routine with only a proposal is no_output", func(t *testing.T) {
		squadID, autopilotID := complianceRoutine(t, "report")
		insertRoutineRun(t, squadID, autopilotID)
		fileProposal(t, squadID)
		if got := routineStateOf(t, squadID, autopilotID); got != "no_output" {
			t.Fatalf("report routine with only a proposal: state = %s, want no_output", got)
		}
	})
}
