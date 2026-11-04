package service

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// TestFloorChiefRoutineIsCreatedOncePerChief covers the floor brief routine (spec
// 4.8): naming a chief creates one daily routine for that chief on that floor,
// naming the same chief again creates nothing, a new chief pauses the previous
// routine and gets its own, and clearing a chief pauses its routine.
func TestFloorChiefRoutineIsCreatedOncePerChief(t *testing.T) {
	f, _ := officeWorkspace(t)
	ctx := context.Background()
	ws := util.MustParseUUID(f.WorkspaceID)
	actor := util.MustParseUUID(f.UserID)
	runtimeID, chiefA := ownerAndLeader(t, f, "Chief A")
	chiefB := f.Agent(t, "Chief B", runtimeID)
	chiefC := f.Agent(t, "Chief C", runtimeID)
	floorA := f.Insert(t, "office_floor", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "Research", "position": 0})
	floorB := f.Insert(t, "office_floor", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "Build", "position": 1})

	name := func(floor, agent string) FloorChiefInput {
		return FloorChiefInput{
			WorkspaceID: ws, FloorID: util.MustParseUUID(floor), AgentID: util.MustParseUUID(agent),
			ActorUserID: actor, RoutineStatus: "active",
		}
	}
	setChief := func(in FloorChiefInput) string {
		t.Helper()
		row, err := SetFloorChiefIn(ctx, f.q, in)
		if err != nil {
			t.Fatalf("set chief: %v", err)
		}
		if !row.BriefAutopilotID.Valid {
			t.Fatalf("a named chief has a floor brief routine")
		}
		return util.UUIDToString(row.BriefAutopilotID)
	}
	routines := func(status string) int {
		return f.Count(t, `SELECT COUNT(*) FROM autopilot
			WHERE workspace_id = $1 AND title LIKE 'Daily floor brief: %' AND status = $2`, f.WorkspaceID, status)
	}

	first := setChief(name(floorA, chiefA))
	if again := setChief(name(floorA, chiefA)); again != first {
		t.Fatalf("naming the same chief again must reuse its routine: first %s, again %s", first, again)
	}
	if n := f.Count(t, `SELECT COUNT(*) FROM autopilot WHERE workspace_id = $1 AND title LIKE 'Daily floor brief: %'`, f.WorkspaceID); n != 1 {
		t.Fatalf("one chief, one routine: got %d", n)
	}
	setChief(name(floorB, chiefB))
	if n := f.Count(t, `SELECT COUNT(*) FROM autopilot WHERE workspace_id = $1 AND title LIKE 'Daily floor brief: %'`, f.WorkspaceID); n != 2 {
		t.Fatalf("two floors, two chiefs, two routines: got %d", n)
	}

	var assignee, description, mode string
	var triggers int
	if err := f.Pool.QueryRow(ctx, `SELECT assignee_id::text, COALESCE(description, ''), execution_mode,
		(SELECT COUNT(*) FROM autopilot_trigger t WHERE t.autopilot_id = a.id AND t.kind = 'schedule' AND t.enabled)
		FROM autopilot a WHERE a.id = $1`, first).Scan(&assignee, &description, &mode, &triggers); err != nil {
		t.Fatalf("read routine: %v", err)
	}
	if assignee != chiefA || mode != "run_only" || triggers != 1 {
		t.Fatalf("the routine is the chief's, runs its task, and has one schedule: assignee %s mode %s triggers %d", assignee, mode, triggers)
	}
	if !strings.Contains(description, "--floor "+floorA) {
		t.Fatalf("the routine names its floor in the run prompt: %q", description)
	}

	// A new chief on floor A: the previous routine pauses and the new chief gets its own.
	second := setChief(name(floorA, chiefC))
	if second == first {
		t.Fatalf("a new chief gets a routine of its own")
	}
	if n := routines("paused"); n != 1 {
		t.Fatalf("the previous chief's routine is paused: %d paused", n)
	}
	if n := routines("active"); n != 2 {
		t.Fatalf("the new chief's and floor B's routines stay active: %d active", n)
	}

	// Clearing floor B's chief pauses its routine and keeps the row count.
	if err := ClearFloorChiefIn(ctx, f.q, ws, util.MustParseUUID(floorB)); err != nil {
		t.Fatalf("clear chief: %v", err)
	}
	if n := routines("paused"); n != 2 {
		t.Fatalf("clearing a chief pauses its routine: %d paused", n)
	}

	// Naming the cleared floor's chief again finds its routine, resumes it, and creates nothing.
	setChief(name(floorB, chiefB))
	if n := f.Count(t, `SELECT COUNT(*) FROM autopilot WHERE workspace_id = $1 AND title = 'Daily floor brief: Build'`, f.WorkspaceID); n != 1 {
		t.Fatalf("the floor's routine is found again, not duplicated: %d", n)
	}
	if n := routines("active"); n != 2 {
		t.Fatalf("naming a chief resumes its routine: %d active", n)
	}
}
